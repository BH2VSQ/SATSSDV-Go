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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

const (
	queueCapacity         = 8192
	pcsiPreviewInterval   = 16
	pcsiPreviewIterations = 16
	pcsiFinalIterations   = 96
	ssdvArchiveInterval   = 8
	maxImageSessions      = 6
	maxSSDVPackets        = 8192
	sessionRetention      = 3 * time.Minute
	sessionSweepInterval  = 30 * time.Second
	gifPartialArchiveIdle = 2 * time.Second
)

type Update struct {
	Profile                 string  `json:"profile"`
	Codec                   string  `json:"codec"`
	Callsign                string  `json:"callsign"`
	ImageID                 uint8   `json:"imageId"`
	PacketsReceived         int     `json:"packetsReceived"`
	PacketsExpected         int     `json:"packetsExpected"`
	Coverage                float64 `json:"coverage"`
	Complete                bool    `json:"complete"`
	ArchivePreview          bool    `json:"archivePreview,omitempty"`
	PNGBase64               string  `json:"pngBase64,omitempty"`
	HasPreview              bool    `json:"hasPreview,omitempty"`
	PreviewDataURL          string  `json:"previewDataUrl,omitempty"`
	PacketDataBase64        string  `json:"packetDataBase64,omitempty"`
	PacketPath              string  `json:"packetPath,omitempty"`
	ImageWidth              int     `json:"imageWidth,omitempty"`
	ImageHeight             int     `json:"imageHeight,omitempty"`
	Quality                 uint8   `json:"quality,omitempty"`
	FEC                     bool    `json:"fec"`
	Subsampling             uint8   `json:"subsampling,omitempty"`
	GIFMode                 bool    `json:"gifMode,omitempty"`
	GIFFrame                uint16  `json:"gifFrame,omitempty"`
	GIFFrameComplete        bool    `json:"gifFrameComplete,omitempty"`
	GIFFramePacketsReceived int     `json:"gifFramePacketsReceived,omitempty"`
	GIFFramePacketsExpected int     `json:"gifFramePacketsExpected,omitempty"`
	GIFStart                bool    `json:"gifStart,omitempty"`
	GIFEnd                  bool    `json:"gifEnd,omitempty"`
	GIFSaved                bool    `json:"gifSaved,omitempty"`
	GIFPartialArchive       bool    `json:"gifPartialArchive,omitempty"`
	GIFFrameAbandoned       bool    `json:"gifFrameAbandoned,omitempty"`

	GIFAwaitingKeyframe bool   `json:"gifAwaitingKeyframe,omitempty"`
	GIFStreamIncomplete bool   `json:"gifStreamIncomplete,omitempty"`
	GIFSaveError        string `json:"gifSaveError,omitempty"`
	FrameDurationMS     int    `json:"frameDurationMs,omitempty"`
	Error               string `json:"error,omitempty"`
	Started             bool   `json:"started,omitempty"`
	SessionID           string `json:"sessionId,omitempty"`
	HasAX25SSID         bool   `json:"hasAX25SSID,omitempty"`
	AX25SourceSSID      uint8  `json:"ax25SourceSSID,omitempty"`
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

	mu               sync.Mutex
	closed           atomic.Bool
	droppedPackets   atomic.Uint64
	submittedPackets atomic.Uint64
	processedPackets atomic.Uint64
	lastSweep        time.Time
	sessions         map[string]*imageSession
	ssdv             map[string]*ssdvSession
	gifStreams       map[string]*gifStreamSession
}

func NewManager(onUpdate func(Update)) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		ctx: ctx, cancel: cancel, queue: make(chan transport.ReceivedPacket, queueCapacity),
		reconQueue:  make(chan *imageSession, maxImageSessions),
		reconResult: make(chan reconstructionResult, 1),
		done:        make(chan struct{}), reconDone: make(chan struct{}), on: onUpdate,
		sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession),
		gifStreams: make(map[string]*gifStreamSession),
	}
	go m.runReconstruction()
	go m.run()
	return m
}

// Submit copies a decoded packet into a bounded worker queue. It never blocks
// on the manager mutex or queue: when saturated, the new arrival is rejected
// and counted while already accepted FIFO packets are preserved. Independently
// decodable block codecs can localize resulting packet loss; legacy SSDV and
// PCSI keep their existing packet-level recovery behavior.
func (m *Manager) Submit(packet transport.ReceivedPacket) bool {
	if m == nil || m.closed.Load() {
		return false
	}
	if len(m.queue) >= cap(m.queue) {
		m.droppedPackets.Add(1)
		return false
	}
	packet.Raw = append([]byte(nil), packet.Raw...)
	select {
	case m.queue <- packet:
		m.submittedPackets.Add(1)
		// Avoid reporting success if Close won the race after the enqueue.
		return !m.closed.Load()
	case <-m.ctx.Done():
		return false
	default:
		// A concurrent sender may have filled the last slot after the len check.
		m.droppedPackets.Add(1)
		return false
	}
}

// DroppedPackets reports the cumulative number of packets shed under RX queue
// saturation. It is monotonic for the lifetime of the manager.
func (m *Manager) DroppedPackets() uint64 {
	if m == nil {
		return 0
	}
	return m.droppedPackets.Load()
}

type GIFStreamDiagnostics struct {
	Callsign             string `json:"callsign"`
	SessionID            string `json:"sessionId"`
	FrameID              uint8  `json:"frameId"`
	FrameIndex           uint16 `json:"frameIndex"`
	FramePacketsReceived int    `json:"framePacketsReceived"`
	FramePacketsExpected int    `json:"framePacketsExpected"`
	DecodedMCUs          int    `json:"decodedMcus"`
	ExpectedMCUs         int    `json:"expectedMcus"`
	AwaitingKeyframe     bool   `json:"awaitingKeyframe"`
	Incomplete           bool   `json:"incomplete"`
	Complete             bool   `json:"complete"`
	LastPacketAgeMS      int64  `json:"lastPacketAgeMs"`
	LastDecodeAgeMS      int64  `json:"lastDecodeAgeMs"`
	DecodeAttempts       uint64 `json:"decodeAttempts"`
	LastDecodeError      string `json:"lastDecodeError,omitempty"`
}

type ManagerDiagnostics struct {
	QueueDepth       int                    `json:"queueDepth"`
	QueueCapacity    int                    `json:"queueCapacity"`
	QueueDropped     uint64                 `json:"queueDropped"`
	SubmittedPackets uint64                 `json:"submittedPackets"`
	ProcessedPackets uint64                 `json:"processedPackets"`
	GIFStreams       []GIFStreamDiagnostics `json:"gifStreams,omitempty"`
}

// Diagnostics returns a bounded snapshot of RX queue and GIF reconstruction
// progress. It never exposes image buffers or packet payloads.
func (m *Manager) Diagnostics() ManagerDiagnostics {
	if m == nil {
		return ManagerDiagnostics{}
	}
	status := ManagerDiagnostics{
		QueueDepth: len(m.queue), QueueCapacity: cap(m.queue),
		QueueDropped: m.droppedPackets.Load(), SubmittedPackets: m.submittedPackets.Load(),
		ProcessedPackets: m.processedPackets.Load(),
	}
	now := time.Now()
	m.mu.Lock()
	for _, stream := range m.gifStreams {
		if stream == nil {
			continue
		}
		item := GIFStreamDiagnostics{
			Callsign: stream.callsign, SessionID: stream.sessionID,
			FrameID: stream.lastFrameID, AwaitingKeyframe: stream.awaitingKeyframe,
			Incomplete: stream.incomplete, Complete: stream.complete,
		}
		if !stream.lastUpdate.IsZero() {
			item.LastPacketAgeMS = now.Sub(stream.lastUpdate).Milliseconds()
		}
		frame := stream.pending
		if frame == nil && stream.hasLatestFrame {
			frame = stream.recentFrames[stream.latestFrameID]
		}
		if frame != nil {
			item.FrameID = frame.imageID
			item.FrameIndex = frame.frameIndex
			item.FramePacketsReceived = frame.receivedPackets
			item.FramePacketsExpected = frame.expectedPackets + frame.controlExpected
			item.DecodedMCUs = frame.decodedMCUs
			item.ExpectedMCUs = (frame.width / 16) * (frame.height / 16)
			item.DecodeAttempts = frame.decodeAttempts
			item.LastDecodeError = frame.lastDecodeError
			if !frame.lastDecodeAt.IsZero() {
				item.LastDecodeAgeMS = now.Sub(frame.lastDecodeAt).Milliseconds()
			}
		}
		status.GIFStreams = append(status.GIFStreams, item)
	}
	m.mu.Unlock()
	return status
}

func (m *Manager) run() {
	defer close(m.done)
	gifArchiveTicker := time.NewTicker(500 * time.Millisecond)
	defer gifArchiveTicker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case packet := <-m.queue:
			if update, ok := m.process(packet); ok && m.on != nil {
				m.on(update)
			}
			m.processedPackets.Add(1)
		case result := <-m.reconResult:
			m.finishReconstruction(result)
		case now := <-gifArchiveTicker.C:
			if m.on == nil {
				continue
			}
			for _, update := range m.collectIdleGIFArchiveUpdates(now) {
				m.on(update)
			}
		}
	}
}

// collectIdleGIFArchiveUpdates emits a checkpoint after a GIF stream has been
// quiet long enough. This makes a partially received animation durable even
// when RF reception stops before a frame-close or stream-end marker arrives.
// The caller archives the snapshot and acknowledges it with
// MarkGIFSnapshotArchived; failed saves remain dirty and are retried later.
func (m *Manager) collectIdleGIFArchiveUpdates(now time.Time) []Update {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	updates := make([]Update, 0)
	for _, stream := range m.gifStreams {
		if stream == nil || stream.complete || !stream.archiveDirty || now.Sub(stream.lastUpdate) < gifPartialArchiveIdle {
			continue
		}
		if !hasGIFSnapshotLocked(stream) {
			continue
		}
		if !stream.lastPartialArchive.IsZero() && now.Sub(stream.lastPartialArchive) < gifPartialArchiveIdle {
			continue
		}
		imageID := stream.lastFrameID
		if stream.pending != nil {
			imageID = stream.pending.imageID
		}
		width, height := 0, 0
		if stream.pending != nil {
			width, height = stream.pending.width, stream.pending.height
		}
		if stream.canvas != nil {
			width, height = stream.canvas.Bounds().Dx(), stream.canvas.Bounds().Dy()
		}
		updates = append(updates, Update{
			Profile: stream.profile, Codec: "SSDV-GIF", Callsign: stream.callsign,
			ImageID: imageID, PacketsReceived: stream.packetsReceived,
			PacketsExpected: stream.packetsExpected, ImageWidth: width, ImageHeight: height,
			GIFMode: true, GIFPartialArchive: true, GIFStreamIncomplete: true,
			GIFAwaitingKeyframe: stream.awaitingKeyframe, SessionID: stream.sessionID,
			HasAX25SSID: stream.hasSSID, AX25SourceSSID: stream.ssid,
		})
		stream.lastPartialArchive = now
	}
	return updates
}

// MarkGIFSnapshotArchived clears the dirty bit after the application has
// atomically persisted a partial or complete GIF snapshot to the album.
func (m *Manager) MarkGIFSnapshotArchived(sessionID string) bool {
	if m == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, stream := range m.gifStreams {
		if stream.sessionID == sessionID {
			stream.archiveDirty = false
			stream.lastPartialArchive = time.Now()
			return true
		}
	}
	return false
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

// pruneSessionsLocked bounds the retained sparse-sampling and packet history.
// Completed/abandoned sessions are useful for duplicate suppression only for
// a short period; keeping dozens of full-resolution PCSI coefficient vectors
// would retain hundreds of MiB after the image has already been archived.
func (m *Manager) pruneSessionsLocked(now time.Time) {
	if !m.lastSweep.IsZero() && now.Sub(m.lastSweep) < sessionSweepInterval {
		return
	}
	m.lastSweep = now
	for key, ss := range m.ssdv {
		if now.Sub(ss.lastUpdate) > sessionRetention {
			delete(m.ssdv, key)
		}
	}
	for key, stream := range m.gifStreams {
		if now.Sub(stream.lastUpdate) > sessionRetention {
			delete(m.gifStreams, key)
		}
	}
	for key, session := range m.sessions {
		if now.Sub(session.lastUpdate) <= sessionRetention {
			continue
		}
		session.mu.Lock()
		busy := session.running || session.queued
		session.mu.Unlock()
		if !busy {
			delete(m.sessions, key)
		}
	}
}

func (m *Manager) process(packet transport.ReceivedPacket) (Update, bool) {
	// Keep the private GIF extension out of the static SSDV assembler. GIF
	// packets use their own frame/session reconstruction state and sparse MCU
	// decoder; control markers must never be interpreted as JPEG payloads.
	if packet.GIFMode || packet.Codec == "SSDV-GIF" {
		return m.processGIFPacket(packet)
	}
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
		m.pruneSessionsLocked(time.Now())
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
			if len(m.ssdv) >= maxImageSessions*2 {
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
		var ordered, prefix, previewPackets [][]byte
		var packetArchive []byte
		legacyStream := parsed.MCUOffset == 0xff || parsed.MCUIndex == 0xffff
		needOrdered := complete
		needIDs := complete || archivePreview
		if legacyStream && !complete {
			// Legacy streams have no MCU restart points and must retain the
			// contiguous packet prefix for their row-based fallback decoder.
			for id := 0; id < maxSSDVPackets; id++ {
				part, ok := ss.packets[uint16(id)]
				if !ok {
					break
				}
				prefix = append(prefix, append([]byte(nil), part[:]...))
			}
			needOrdered = true
		}
		if !legacyStream && !complete {
			// Each new packet can make at most two MCU segments newly decodable:
			// the segment from the previous packet's restart marker to this one,
			// and this packet's segment to the next restart marker (or packet end).
			// Decoding only these neighbours avoids repeatedly parsing the entire
			// image packet set on every incoming packet.
			firstID := packet.PacketID
			if firstID > 0 {
				firstID--
			}
			lastID := packet.PacketID + 1
			for id := firstID; id <= lastID; id++ {
				if part, ok := ss.packets[id]; ok {
					previewPackets = append(previewPackets, append([]byte(nil), part[:]...))
				}
			}
		}
		var ids []int
		if needIDs {
			ids = make([]int, 0, len(ss.packets))
			for id := range ss.packets {
				ids = append(ids, int(id))
			}
			sort.Ints(ids)
		}
		if needOrdered {
			ordered = make([][]byte, 0, len(ids))
			for _, id := range ids {
				part := ss.packets[uint16(id)]
				ordered = append(ordered, append([]byte(nil), part[:]...))
			}
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
		} else if shouldPreview && !legacyStream {
			var preview *image.RGBA
			var metadata ssdv.Packet
			var decodeErr error
			if ss.lastPreviewImage == nil {
				preview, metadata, _, decodeErr = ssdv.DecodePacketsToPreview(previewPackets)
			} else {
				preview, metadata, _, decodeErr = ssdv.UpdatePacketsPreview(ss.lastPreviewImage, previewPackets)
			}
			if decodeErr == nil {
				ss.lastPreviewImage = preview
				var pngBuffer bytes.Buffer
				if imageErr := png.Encode(&pngBuffer, preview); imageErr == nil {
					update.PNGBase64 = base64.StdEncoding.EncodeToString(pngBuffer.Bytes())
					update.ImageWidth, update.ImageHeight = metadata.Width, metadata.Height
				}
			}
		} else if shouldPreview && legacyStream && len(prefix) > 0 {
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
	m.pruneSessionsLocked(time.Now())
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
	if !m.closed.CompareAndSwap(false, true) {
		return
	}
	m.cancel()
	<-m.done
	<-m.reconDone
}

func decodedToRGBA(src image.Image) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	return out
}
