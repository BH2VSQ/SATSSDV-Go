// Package rx tracks received image packets and performs image reconstruction
// away from the real-time audio capture callback.
package rx

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/jpeg"
	"image/png"
	"sync"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

const (
	queueCapacity       = 128
	pcsiPreviewInterval = 8
	maxImageSessions    = 24
	maxSSDVPackets      = 8192
)

type Update struct {
	Profile          string  `json:"profile"`
	Codec            string  `json:"codec"`
	Callsign         string  `json:"callsign"`
	ImageID          uint8   `json:"imageId"`
	PacketsReceived  int     `json:"packetsReceived"`
	PacketsExpected  int     `json:"packetsExpected"`
	Coverage         float64 `json:"coverage"`
	Complete         bool    `json:"complete"`
	PNGBase64        string  `json:"pngBase64,omitempty"`
	PacketDataBase64 string  `json:"packetDataBase64,omitempty"`
	PacketPath       string  `json:"packetPath,omitempty"`
	ImageWidth       int     `json:"imageWidth,omitempty"`
	ImageHeight      int     `json:"imageHeight,omitempty"`
	Quality          uint8   `json:"quality,omitempty"`
	FEC              bool    `json:"fec"`
	Subsampling      uint8   `json:"subsampling,omitempty"`
	Error            string  `json:"error,omitempty"`
}

type imageSession struct {
	mu         sync.Mutex
	decoder    *pcsi.DecoderState
	lastUpdate time.Time
	queued     bool
	running    bool
	dirty      bool
	complete   bool
	profile    string
	callsign   string
	imageID    uint8
}

type reconstructionResult struct {
	session  *imageSession
	snapshot *pcsi.DecoderState
	update   Update
}

type ssdvSession struct {
	packets     map[uint16][256]byte
	expected    int
	width       int
	height      int
	quality     uint8
	fec         bool
	subsampling uint8
	lastUpdate  time.Time
}

type Manager struct {
	ctx         context.Context
	cancel      context.CancelFunc
	queue       chan transport.ReceivedPacket
	reconQueue  chan *imageSession
	reconResult chan reconstructionResult
	done        chan struct{}
	reconDone   chan struct{}
	on          func(Update)

	mu       sync.Mutex
	closed   bool
	sessions map[string]*imageSession
	ssdv     map[string]*ssdvSession
}

func NewManager(onUpdate func(Update)) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		ctx: ctx, cancel: cancel, queue: make(chan transport.ReceivedPacket, queueCapacity),
		reconQueue:  make(chan *imageSession, maxImageSessions),
		reconResult: make(chan reconstructionResult, 1),
		done:        make(chan struct{}), reconDone: make(chan struct{}), on: onUpdate,
		sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession),
	}
	go m.runReconstruction()
	go m.run()
	return m
}

// Submit copies a decoded packet into a bounded worker queue. It never runs
// image reconstruction on the caller's audio or demodulation goroutine.
func (m *Manager) Submit(packet transport.ReceivedPacket) bool {
	if m == nil {
		return false
	}
	packet.Raw = append([]byte(nil), packet.Raw...)
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return false
	}
	select {
	case m.queue <- packet:
		return true
	case <-m.ctx.Done():
		return false
	default:
		return false
	}
}

func (m *Manager) run() {
	defer close(m.done)
	for {
		select {
		case <-m.ctx.Done():
			return
		case packet := <-m.queue:
			if update, ok := m.process(packet); ok && m.on != nil {
				m.on(update)
			}
		case result := <-m.reconResult:
			m.finishReconstruction(result)
		}
	}
}

func (m *Manager) runReconstruction() {
	defer close(m.reconDone)
	for {
		select {
		case <-m.ctx.Done():
			return
		case session := <-m.reconQueue:
			if session == nil {
				continue
			}
			session.mu.Lock()
			session.queued = false
			session.running = true
			session.dirty = false
			decoder := session.decoder.Clone()
			profile, callsign, imageID := session.profile, session.callsign, session.imageID
			complete := session.complete
			session.mu.Unlock()

			iterations := 2
			if complete {
				iterations = 24
			}
			img, stats, err := decoder.ReconstructContext(m.ctx, iterations)
			update := Update{
				Profile: profile, Codec: "PCSI-MMT", Callsign: callsign, ImageID: imageID,
				PacketsReceived: stats.PacketsReceived, PacketsExpected: stats.PacketsExpected,
				Coverage: stats.Coverage, Complete: complete,
			}
			if err != nil {
				update.Error = err.Error()
			} else {
				var encoded bytes.Buffer
				if encodeErr := png.Encode(&encoded, img); encodeErr != nil {
					update.Error = encodeErr.Error()
				} else {
					update.PNGBase64 = base64.StdEncoding.EncodeToString(encoded.Bytes())
					update.ImageWidth, update.ImageHeight = img.Bounds().Dx(), img.Bounds().Dy()
				}
			}
			select {
			case m.reconResult <- reconstructionResult{session: session, snapshot: decoder, update: update}:
			case <-m.ctx.Done():
				return
			}
		}
	}
}

func (m *Manager) finishReconstruction(result reconstructionResult) {
	if result.session == nil {
		return
	}
	result.session.mu.Lock()
	result.session.decoder.CopyWarmStartFrom(result.snapshot)
	result.session.running = false
	needsAnotherPass := result.session.dirty
	result.session.mu.Unlock()
	if m.on != nil && (result.update.Error != "" || result.update.PNGBase64 != "") {
		m.on(result.update)
	}
	if needsAnotherPass {
		m.enqueuePendingReconstruction(result.session)
	}
	m.retryPendingReconstructions()
}

// retryPendingReconstructions recovers jobs that could not enter the bounded
// queue while old, evicted sessions were still waiting in it.
func (m *Manager) retryPendingReconstructions() {
	m.mu.Lock()
	sessions := make([]*imageSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.Unlock()
	for _, session := range sessions {
		m.enqueuePendingReconstruction(session)
	}
}

func (m *Manager) scheduleReconstruction(session *imageSession) {
	session.mu.Lock()
	if session.queued || session.running {
		session.dirty = true
		session.mu.Unlock()
		return
	}
	session.dirty = true
	session.mu.Unlock()
	m.enqueuePendingReconstruction(session)
}

func (m *Manager) enqueuePendingReconstruction(session *imageSession) {
	session.mu.Lock()
	if !session.dirty || session.queued || session.running {
		session.mu.Unlock()
		return
	}
	session.queued = true
	session.dirty = false
	session.mu.Unlock()
	select {
	case m.reconQueue <- session:
	case <-m.ctx.Done():
		session.mu.Lock()
		session.queued = false
		session.mu.Unlock()
	default:
		session.mu.Lock()
		session.queued = false
		session.dirty = true
		session.mu.Unlock()
	}
}

func (m *Manager) process(packet transport.ReceivedPacket) (Update, bool) {
	if packet.Codec == "SSDV" {
		parsed, err := ssdv.ParsePacket(packet.Raw)
		if err != nil {
			return Update{Profile: packet.Profile, Codec: packet.Codec, Callsign: packet.Callsign, ImageID: packet.ImageID, Error: err.Error()}, true
		}
		// The packet header is authoritative. Reuse its RS-corrected bytes so
		// the archived .ssdv stream is also clean, not the damaged RF payload.
		packet.Callsign, packet.ImageID, packet.PacketID = parsed.Callsign, parsed.ImageID, parsed.PacketID
		packet.ImageWidth, packet.ImageHeight, packet.Quality = parsed.Width, parsed.Height, parsed.Quality
		packet.EOI, packet.Subsampling, packet.MCUOffset, packet.MCUIndex, packet.MCUCount = parsed.EOI, parsed.Subsampling, parsed.MCUOffset, parsed.MCUIndex, parsed.MCUCount
		packet.FEC = parsed.FEC
		packet.Raw = append(packet.Raw[:0], parsed.Raw[:]...)
		key := imageKey(packet.Profile, packet.Callsign, packet.ImageID)
		m.mu.Lock()
		ss := m.ssdv[key]
		if ss == nil {
			if len(m.ssdv) >= maxImageSessions*4 {
				var oldestKey string
				var oldest time.Time
				for candidateKey, candidate := range m.ssdv {
					if oldestKey == "" || candidate.lastUpdate.Before(oldest) {
						oldestKey, oldest = candidateKey, candidate.lastUpdate
					}
				}
				delete(m.ssdv, oldestKey)
			}
			ss = &ssdvSession{packets: make(map[uint16][256]byte), width: packet.ImageWidth, height: packet.ImageHeight, quality: packet.Quality, fec: packet.FEC, subsampling: packet.Subsampling}
			m.ssdv[key] = ss
		}
		ss.lastUpdate = time.Now()
		if packet.PacketID >= maxSSDVPackets {
			m.mu.Unlock()
			return Update{Profile: packet.Profile, Codec: packet.Codec, Callsign: packet.Callsign, ImageID: packet.ImageID, Error: "SSDV packet index exceeds the configured session limit"}, true
		}
		if packet.ImageWidth > 0 {
			ss.width = packet.ImageWidth
		}
		if packet.ImageHeight > 0 {
			ss.height = packet.ImageHeight
		}
		ss.quality, ss.fec, ss.subsampling = packet.Quality, packet.FEC, packet.Subsampling
		if _, exists := ss.packets[packet.PacketID]; exists {
			m.mu.Unlock()
			return Update{}, false
		}
		var raw [256]byte
		copy(raw[:], packet.Raw)
		ss.packets[packet.PacketID] = raw
		if packet.EOI && int(packet.PacketID)+1 > ss.expected {
			ss.expected = int(packet.PacketID) + 1
		}
		count, expected := len(ss.packets), ss.expected
		complete, covered := expected > 0, 0
		for id := 0; id < expected; id++ {
			if _, ok := ss.packets[uint16(id)]; ok {
				covered++
			} else {
				complete = false
			}
		}
		coverage := 0.0
		if expected > 0 {
			coverage = float64(covered) / float64(expected)
		}
		update := Update{Profile: packet.Profile, Codec: packet.Codec, Callsign: packet.Callsign, ImageID: packet.ImageID,
			PacketsReceived: count, PacketsExpected: expected, Coverage: coverage, Complete: complete,
			ImageWidth: ss.width, ImageHeight: ss.height, Quality: ss.quality, FEC: ss.fec, Subsampling: ss.subsampling}
		var ordered [][]byte
		if complete {
			stream := make([]byte, 0, expected*256)
			ordered = make([][]byte, 0, expected)
			for id := 0; id < expected; id++ {
				part := ss.packets[uint16(id)]
				stream = append(stream, part[:]...)
				ordered = append(ordered, append([]byte(nil), part[:]...))
			}
			update.PacketDataBase64 = base64.StdEncoding.EncodeToString(stream)
		}
		m.mu.Unlock()

		// Do the final JPEG/PNG work after releasing the manager lock. Submit()
		// and image-session maintenance can continue while a large image is
		// reconstructed and archived.
		if complete {
			jpegData, metadata, decodeErr := ssdv.DecodePacketsToJPEG(ordered)
			if decodeErr != nil {
				update.Error = decodeErr.Error()
			} else {
				decoded, imageErr := jpeg.Decode(bytes.NewReader(jpegData))
				if imageErr != nil {
					update.Error = fmt.Errorf("decode reconstructed SSDV JPEG: %w", imageErr).Error()
				} else {
					var pngBuffer bytes.Buffer
					if imageErr = png.Encode(&pngBuffer, decoded); imageErr != nil {
						update.Error = fmt.Errorf("encode reconstructed SSDV PNG: %w", imageErr).Error()
					} else {
						update.PNGBase64 = base64.StdEncoding.EncodeToString(pngBuffer.Bytes())
						update.ImageWidth, update.ImageHeight = metadata.Width, metadata.Height
					}
				}
			}
		}
		return update, true
	}
	key := imageKey(packet.Profile, packet.Callsign, packet.ImageID)
	if packet.Codec != "PCSI-MMT" {
		return Update{}, false
	}
	corrected, _, err := pcsi.CorrectPacket(packet.Raw)
	if err != nil {
		return Update{Profile: packet.Profile, Codec: packet.Codec, Callsign: packet.Callsign, ImageID: packet.ImageID, Error: err.Error()}, true
	}
	info, err := pcsi.ParsePacket(corrected)
	if err != nil {
		return Update{Profile: packet.Profile, Codec: packet.Codec, Error: err.Error()}, true
	}
	m.mu.Lock()
	session := m.sessions[key]
	if session == nil {
		if len(m.sessions) >= maxImageSessions {
			var oldestKey string
			var oldest time.Time
			for candidateKey, candidate := range m.sessions {
				if oldestKey == "" || candidate.lastUpdate.Before(oldest) {
					oldestKey, oldest = candidateKey, candidate.lastUpdate
				}
			}
			delete(m.sessions, oldestKey)
		}
		session = &imageSession{decoder: pcsi.NewDecoderState(info), profile: packet.Profile, callsign: info.Callsign, imageID: info.ImageID}
		m.sessions[key] = session
	}
	session.lastUpdate = time.Now()
	m.mu.Unlock()
	session.mu.Lock()
	if session.decoder.Seen[info.PacketNumber] {
		stats := session.decoder.Stats()
		session.mu.Unlock()
		return Update{Profile: packet.Profile, Codec: packet.Codec, Callsign: info.Callsign, ImageID: info.ImageID,
			PacketsReceived: stats.PacketsReceived, PacketsExpected: stats.PacketsExpected, Coverage: stats.Coverage}, true
	}
	if err := session.decoder.Add(corrected); err != nil {
		session.mu.Unlock()
		return Update{Profile: packet.Profile, Codec: packet.Codec, Callsign: info.Callsign, ImageID: info.ImageID, Error: err.Error()}, true
	}
	stats := session.decoder.Stats()
	complete := stats.PacketsExpected > 0 && stats.PacketsReceived >= stats.PacketsExpected
	if complete {
		session.complete = true
	}
	update := Update{
		Profile: packet.Profile, Codec: packet.Codec, Callsign: info.Callsign, ImageID: info.ImageID,
		PacketsReceived: stats.PacketsReceived, PacketsExpected: stats.PacketsExpected, Coverage: stats.Coverage,
		// The transport may be complete while the numerical image reconstruction
		// is still running. Only the reconstruction result sets Complete=true.
		Complete: false,
	}
	shouldReconstruct := stats.PacketsReceived%pcsiPreviewInterval == 0 || complete
	session.mu.Unlock()
	if !shouldReconstruct {
		return Update{}, false
	}
	m.scheduleReconstruction(session)
	return update, true
}

func imageKey(profile, callsign string, imageID uint8) string {
	return fmt.Sprintf("%s|%s|%03d", profile, callsign, imageID)
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.mu.Unlock()
	m.cancel()
	<-m.done
	<-m.reconDone
}
