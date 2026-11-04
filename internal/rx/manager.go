// Package rx tracks received image packets and performs image reconstruction
// away from the real-time audio capture callback.
package rx

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"sort"
	"sync"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

const (
	queueCapacity         = 2048
	pcsiPreviewInterval   = 6
	pcsiPreviewIterations = 48
	pcsiFinalIterations   = 96
	ssdvArchiveInterval   = 8
	maxImageSessions      = 24
	maxSSDVPackets        = 8192
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
	ArchivePreview   bool    `json:"archivePreview,omitempty"`
	PNGBase64        string  `json:"pngBase64,omitempty"`
	PacketDataBase64 string  `json:"packetDataBase64,omitempty"`
	PacketPath       string  `json:"packetPath,omitempty"`
	ImageWidth       int     `json:"imageWidth,omitempty"`
	ImageHeight      int     `json:"imageHeight,omitempty"`
	Quality          uint8   `json:"quality,omitempty"`
	FEC              bool    `json:"fec"`
	Subsampling      uint8   `json:"subsampling,omitempty"`
	Error            string  `json:"error,omitempty"`
	Started          bool    `json:"started,omitempty"`
	SessionID        string  `json:"sessionId,omitempty"`
	HasAX25SSID      bool    `json:"hasAX25SSID,omitempty"`
	AX25SourceSSID   uint8   `json:"ax25SourceSSID,omitempty"`
}

type imageSession struct {
	mu          sync.Mutex
	decoder     *pcsi.DecoderState
	lastUpdate  time.Time
	queued      bool
	running     bool
	dirty       bool
	complete    bool
	profile     string
	callsign    string
	imageID     uint8
	ax25SSID    uint8
	hasAX25SSID bool
	packetZero  []byte
	key         string
	sessionID   string
}

type reconstructionResult struct {
	session  *imageSession
	snapshot *pcsi.DecoderState
	update   Update
}

type ssdvSession struct {
	packets            map[uint16][256]byte
	expected           int
	width              int
	height             int
	quality            uint8
	fec                bool
	subsampling        uint8
	sessionID          string
	lastUpdate         time.Time
	packetZero         []byte
	complete           bool
	lastPreviewPackets int
	lastArchivePackets int
	lastPreviewImage   *image.RGBA
	highestPacketID    uint16
	hasHighestPacketID bool
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

// Submit copies a decoded packet into a bounded worker queue. When the queue is
// full it applies backpressure instead of silently dropping a valid RF packet.
// It never runs image reconstruction on the caller's audio callback goroutine.
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
			ax25SSID, hasAX25SSID := session.ax25SSID, session.hasAX25SSID
			sessionID := session.sessionID
			complete := session.complete
			session.mu.Unlock()

			iterations := pcsiPreviewIterations
			if complete {
				// The reference PCSI OWL-QN solver runs until convergence. The Go
				// implementation uses a bounded budget, and 96 iterations provides
				// enough room for the corrected optimizer to converge on real images
				// while retaining a hard upper bound for CPU use.
				iterations = pcsiFinalIterations
			}
			img, stats, err := decoder.ReconstructContext(m.ctx, iterations)
			update := Update{
				Profile: profile, Codec: "PCSI-MMT", Callsign: callsign, ImageID: imageID,
				PacketsReceived: stats.PacketsReceived, PacketsExpected: stats.PacketsExpected,
				Coverage: stats.Coverage, Complete: complete, ArchivePreview: true, SessionID: sessionID,
				HasAX25SSID: hasAX25SSID, AX25SourceSSID: ax25SSID,
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
	m.mu.Lock()
	active := m.sessions[result.session.key] == result.session
	m.mu.Unlock()
	if !active {
		m.retryPendingReconstructions()
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
		started := false
		if ss != nil && packet.PacketID == 0 {
			hasPacketZero := len(ss.packetZero) > 0
			samePacketZero := hasPacketZero && bytes.Equal(ss.packetZero, packet.Raw)
			idleFor := time.Since(ss.lastUpdate)
			// Packet 0 can arrive last in an out-of-order burst. Do not reset on
			// packet number order alone; use its payload fingerprint, completion
			// state, or a long inter-image pause to distinguish a new image.
			newSession := (hasPacketZero && !samePacketZero && len(ss.packets) >= 1) ||
				(hasPacketZero && ss.complete) ||
				(idleFor > 5*time.Second && (hasPacketZero || len(ss.packets) >= 10))
			if newSession {
				ss = nil
				started = true
			}
		}
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
			ss = &ssdvSession{packets: make(map[uint16][256]byte), width: packet.ImageWidth, height: packet.ImageHeight, quality: packet.Quality, fec: packet.FEC, subsampling: packet.Subsampling, sessionID: fmt.Sprintf("%d", time.Now().UnixNano())}
			started = true
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
		if !ss.hasHighestPacketID || packet.PacketID > ss.highestPacketID {
			ss.highestPacketID, ss.hasHighestPacketID = packet.PacketID, true
		}
		if packet.PacketID == 0 {
			ss.packetZero = append(ss.packetZero[:0], packet.Raw...)
		}
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
		ss.complete = complete
		archivePreview := complete || count >= ss.lastArchivePackets+ssdvArchiveInterval
		update := Update{Profile: packet.Profile, Codec: packet.Codec, Callsign: packet.Callsign, ImageID: packet.ImageID,
			PacketsReceived: count, PacketsExpected: expected, Coverage: coverage, Complete: complete,
			ImageWidth: ss.width, ImageHeight: ss.height, Quality: ss.quality, FEC: ss.fec, Subsampling: ss.subsampling,
			Started: started, SessionID: ss.sessionID, ArchivePreview: archivePreview,
			HasAX25SSID: packet.HasAX25SSID, AX25SourceSSID: packet.AX25SourceSSID}
		var ordered, prefix [][]byte
		var packetArchive []byte
		for id := 0; id < maxSSDVPackets; id++ {
			part, ok := ss.packets[uint16(id)]
			if !ok {
				break
			}
			prefix = append(prefix, append([]byte(nil), part[:]...))
		}
		// Keep the whole accepted set in packet-ID order. SSDV MCU restart
		// metadata makes every received packet independently decodable, even
		// when earlier packet IDs are missing or arrive later.
		ids := make([]int, 0, len(ss.packets))
		for id := range ss.packets {
			ids = append(ids, int(id))
		}
		sort.Ints(ids)
		ordered = make([][]byte, 0, len(ids))
		for _, id := range ids {
			part := ss.packets[uint16(id)]
			ordered = append(ordered, append([]byte(nil), part[:]...))
		}
		shouldPreview := !complete
		if archivePreview {
			packetArchive = make([]byte, 0, len(ids)*256)
			for _, id := range ids {
				part := ss.packets[uint16(id)]
				packetArchive = append(packetArchive, part[:]...)
			}
			ss.lastArchivePackets = count
		}
		m.mu.Unlock()
		if len(packetArchive) > 0 {
			update.PacketDataBase64 = base64.StdEncoding.EncodeToString(packetArchive)
		}

		// Decode outside the manager lock so packet ingestion remains responsive.
		// New transmitters put a byte-aligned MCU restart point in every SSDV
		// packet, so each accepted packet can update its own 16x16 block region
		// immediately without waiting for preceding packet IDs.
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
						ss.lastPreviewImage = cloneRGBA(decodedToRGBA(decoded))
					}
				}
			}
		} else if shouldPreview {
			preview, metadata, _, decodeErr := ssdv.DecodePacketsToPreview(ordered)
			if decodeErr == nil {
				ss.lastPreviewImage = preview
				var pngBuffer bytes.Buffer
				if imageErr := png.Encode(&pngBuffer, preview); imageErr == nil {
					update.PNGBase64 = base64.StdEncoding.EncodeToString(pngBuffer.Bytes())
					update.ImageWidth, update.ImageHeight = metadata.Width, metadata.Height
				}
			} else if len(prefix) > 0 {
				// Backward-compatible row-prefix fallback for old SSDV transmitters
				// whose packets use the 0xff/0xffff MCU sentinel.
				if jpegData, _, _, legacyErr := ssdv.DecodePacketPrefixToJPEG(prefix); legacyErr == nil {
					if decoded, imageErr := jpeg.Decode(bytes.NewReader(jpegData)); imageErr == nil {
						canvas := image.NewRGBA(image.Rect(0, 0, ss.width, ss.height))
						draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{R: 10, G: 16, B: 24, A: 255}}, image.Point{}, draw.Src)
						draw.Draw(canvas, image.Rect(0, 0, decoded.Bounds().Dx(), decoded.Bounds().Dy()), decoded, decoded.Bounds().Min, draw.Src)
						ss.lastPreviewImage = canvas
					}
				}
				if ss.lastPreviewImage != nil {
					canvas := cloneRGBA(ss.lastPreviewImage)
					drawSSDVDamageRegions(canvas, ss.packets, ss.highestPacketID, ss.hasHighestPacketID, ss.expected)
					var pngBuffer bytes.Buffer
					if imageErr := png.Encode(&pngBuffer, canvas); imageErr == nil {
						update.PNGBase64 = base64.StdEncoding.EncodeToString(pngBuffer.Bytes())
						update.ImageWidth, update.ImageHeight = ss.width, ss.height
					}
				}
			} else if ss.lastPreviewImage != nil {
				canvas := cloneRGBA(ss.lastPreviewImage)
				var pngBuffer bytes.Buffer
				if imageErr := png.Encode(&pngBuffer, canvas); imageErr == nil {
					update.PNGBase64 = base64.StdEncoding.EncodeToString(pngBuffer.Bytes())
					update.ImageWidth, update.ImageHeight = ss.width, ss.height
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
	started := false
	if session != nil && info.PacketNumber == 0 {
		session.mu.Lock()
		seenCount := len(session.decoder.Seen)
		hasPacketZero := len(session.packetZero) > 0
		samePacketZero := hasPacketZero && bytes.Equal(session.packetZero, corrected)
		previousComplete := session.complete
		session.mu.Unlock()
		idleFor := time.Since(session.lastUpdate)
		// Image-ID reuse is ambiguous at the transport layer. A changed packet
		// 0 or completed prior image is strong boundary evidence; sequence
		// ordering alone is not, since packet 0 may be delayed.
		newSession := (hasPacketZero && !samePacketZero && seenCount >= 1) ||
			(hasPacketZero && previousComplete) ||
			(idleFor > 5*time.Second && (hasPacketZero || seenCount >= 10))
		if newSession {
			session = nil
			started = true
		}
	}
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
		session = &imageSession{decoder: pcsi.NewDecoderState(info), profile: packet.Profile, callsign: info.Callsign, imageID: info.ImageID,
			ax25SSID: packet.AX25SourceSSID, hasAX25SSID: packet.HasAX25SSID,
			key: key, sessionID: fmt.Sprintf("%d", time.Now().UnixNano())}
		started = true
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
	if info.PacketNumber == 0 {
		session.packetZero = append(session.packetZero[:0], corrected...)
	}
	stats := session.decoder.Stats()
	complete := stats.PacketsExpected > 0 && stats.PacketsReceived >= stats.PacketsExpected
	if complete {
		session.complete = true
	}
	update := Update{
		Profile: packet.Profile, Codec: packet.Codec, Callsign: info.Callsign, ImageID: info.ImageID,
		PacketsReceived: stats.PacketsReceived, PacketsExpected: stats.PacketsExpected, Coverage: stats.Coverage,
		Started: started, SessionID: session.sessionID, HasAX25SSID: packet.HasAX25SSID, AX25SourceSSID: packet.AX25SourceSSID,
		// The transport may be complete while the numerical image reconstruction
		// is still running. Only the reconstruction result sets Complete=true.
		Complete: false,
	}
	shouldReconstruct := stats.PacketsReceived%pcsiPreviewInterval == 0 || complete
	session.mu.Unlock()
	if !shouldReconstruct {
		if started {
			return update, true
		}
		return Update{}, false
	}
	m.scheduleReconstruction(session)
	return update, true
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	if src == nil {
		return nil
	}
	out := image.NewRGBA(src.Bounds())
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	return out
}

// drawSSDVDamageRegions marks known packet gaps over the last valid preview.
// SSDV streams without MCU-resynchronization metadata cannot place a damaged
// packet exactly; proportional bands make loss visible without fabricating
// recovered pixels. When a missing packet arrives the band disappears.
func drawSSDVDamageRegions(dst *image.RGBA, packets map[uint16][256]byte, highest uint16, hasHighest bool, expected int) {
	if dst == nil || len(packets) == 0 || !hasHighest {
		return
	}
	limit := expected
	if limit <= 0 || limit < int(highest)+1 {
		limit = int(highest) + 1
	}
	if limit <= 0 {
		return
	}
	width, height := dst.Bounds().Dx(), dst.Bounds().Dy()
	for id := 0; id <= int(highest); id++ {
		if _, ok := packets[uint16(id)]; ok {
			continue
		}
		y0 := id * height / limit
		y1 := (id + 1) * height / limit
		if y1 <= y0 {
			y1 = y0 + 1
		}
		if y0 >= height {
			continue
		}
		if y1 > height {
			y1 = height
		}
		for y := y0; y < y1; y++ {
			for x := 0; x < width; x++ {
				if ((x+y)/7)%2 == 0 {
					dst.SetRGBA(x, y, color.RGBA{R: 155, G: 35, B: 48, A: 255})
				} else {
					dst.SetRGBA(x, y, color.RGBA{R: 65, G: 23, B: 34, A: 255})
				}
			}
		}
	}
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

func decodedToRGBA(src image.Image) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	return out
}
