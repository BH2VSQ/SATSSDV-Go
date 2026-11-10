package rx

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"sort"
	"strings"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/gifmode"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

type gifSegmentKey struct {
	startPacket uint16
	endPacket   uint16
}

type gifFrameBuffer struct {
	imageID           uint8
	packets           map[uint16][256]byte
	keyframe          bool
	width             int
	height            int
	quality           uint8
	fec               bool
	delayCS           uint16
	frameIndex        uint16
	expectedPackets   int
	controlExpected   int
	startSeen         bool
	endSeen           bool
	receivedPackets   int // unique frame-open, data and frame-close packets
	startedAt         time.Time
	completedAt       time.Time
	baseCanvas        *image.RGBA // SGV2 motion-compensated base for this frame
	previewCanvas     *image.RGBA // mutated only by the RX manager worker
	previewPNG        []byte      // latest independently decodable partial frame snapshot
	decodedMCUs       int         // cumulative unique MCU coverage, not per-update coverage
	decodedSegments   map[gifSegmentKey]struct{}
	boundaryPacketIDs []uint16
	eoiPacketID       uint16
	hasEOI            bool
	coveredMCUs       []bool
	snapshotIndex     int // index in stream.frames; -1 until first archive snapshot
	committed         bool
	decodeAttempts    uint64
	lastDecodeAt      time.Time
	lastDecodeError   string
}

const (
	maxGIFStreams       = 4
	maxGIFExportFrames  = 120
	maxGIFExportPNGSize = 32 << 20
	maxGIFRecentFrames  = 3
	gifLatePacketWindow = 12 * time.Second
)

type gifStreamSession struct {
	profile            string
	callsign           string
	ssid               uint8
	hasSSID            bool
	sessionID          string
	canvas             *image.RGBA
	pending            *gifFrameBuffer
	lastUpdate         time.Time
	lastFrameID        uint8
	hasFrame           bool
	lastEndImageID     uint8
	hasLastEnd         bool
	startControlID     uint8
	hasStartControl    bool
	packetsReceived    int      // unique decoded data and frame-control packets
	packetsExpected    int      // expected unique data and frame-control packets
	frames             [][]byte // composed PNG frames; bounded for safe GIF export
	delays             []int
	frameBytes         int
	exportErr          string
	complete           bool // a stream-end marker was received
	incomplete         bool // at least one frame was abandoned or could not be decoded
	awaitingKeyframe   bool
	archiveDirty       bool
	lastPartialArchive time.Time
	framesSeen         uint16
	latestFrameID      uint8
	hasLatestFrame     bool
	recentFrames       map[uint8]*gifFrameBuffer // bounded late-FEC repair window
	incompleteFrames   map[uint8]bool
}

func gifStreamKey(packet transport.ReceivedPacket) string {
	return fmt.Sprintf("%s|%s|%t|%d", packet.Profile, packet.Callsign, packet.HasAX25SSID, packet.AX25SourceSSID)
}

// processGIFPacket handles the independent incremental GIF extension. Data
// packets are collected by frame ID; a reserved SSDV packet ID 0xffff closes
// the frame and causes its sparse MCU updates to be applied to the stream
// canvas. A lost data packet leaves the affected MCU at its previous value;
// periodic keyframes provide recovery.
func (m *Manager) processGIFPacket(packet transport.ReceivedPacket) (Update, bool) {
	parsed, err := ssdv.ParsePacket(packet.Raw)
	if err != nil {
		return Update{Profile: packet.Profile, Codec: "SSDV-GIF", Callsign: packet.Callsign, ImageID: packet.ImageID, Error: err.Error(), GIFMode: true}, true
	}
	if !parsed.GIFMode {
		return Update{}, false
	}
	if int64(parsed.Width)*int64(parsed.Height) > 8*1024*1024 {
		return Update{Profile: packet.Profile, Codec: "SSDV-GIF", Callsign: parsed.Callsign, ImageID: parsed.ImageID,
			Error: "GIF frame exceeds the 8 megapixel RX safety limit", GIFMode: true}, true
	}
	packet.Callsign, packet.ImageID, packet.PacketID = parsed.Callsign, parsed.ImageID, parsed.PacketID
	packet.ImageWidth, packet.ImageHeight, packet.Quality = parsed.Width, parsed.Height, parsed.Quality
	packet.GIFMode, packet.GIFKeyframe, packet.GIFControl = parsed.GIFMode, parsed.GIFKeyframe, parsed.GIFControl
	packet.GIFFrameStart, packet.GIFFrameEnd, packet.GIFDataPackets = parsed.GIFFrameStart, parsed.GIFFrameEnd, parsed.GIFDataPackets
	packet.GIFDelayCS, packet.GIFStart, packet.GIFEnd = parsed.GIFDelayCS, parsed.GIFStart, parsed.GIFEnd
	packet.Raw = append(packet.Raw[:0], parsed.Raw[:]...)

	key := gifStreamKey(packet)
	now := time.Now()
	m.mu.Lock()
	m.pruneSessionsLocked(now)
	stream := m.gifStreams[key]
	if stream == nil {
		stream = &gifStreamSession{
			profile: packet.Profile, callsign: packet.Callsign,
			ssid: packet.AX25SourceSSID, hasSSID: packet.HasAX25SSID,
			sessionID: fmt.Sprintf("%d", now.UnixNano()), lastUpdate: now,
			recentFrames: make(map[uint8]*gifFrameBuffer), incompleteFrames: make(map[uint8]bool),
		}
		if len(m.gifStreams) >= maxGIFStreams {
			oldestKey := ""
			var oldest time.Time
			for candidateKey, candidate := range m.gifStreams {
				if oldestKey == "" || candidate.lastUpdate.Before(oldest) {
					oldestKey, oldest = candidateKey, candidate.lastUpdate
				}
			}
			if oldestKey != "" {
				delete(m.gifStreams, oldestKey)
			}
		}
		m.gifStreams[key] = stream
	}
	if stream.recentFrames == nil {
		stream.recentFrames = make(map[uint8]*gifFrameBuffer)
	}
	if stream.incompleteFrames == nil {
		stream.incompleteFrames = make(map[uint8]bool)
	}
	pruneRecentGIFFramesLocked(stream, now)

	// GIFStart is repeated on the first frame-open marker. A completed or
	// expired stream starts a new album session; duplicate physical copies of
	// the same marker are ignored without resetting collected frames.
	if parsed.GIFStart {
		newStream := !stream.hasStartControl || stream.startControlID != parsed.ImageID || stream.complete || now.Sub(stream.lastUpdate) > 3*time.Second
		if newStream {
			stream.sessionID = fmt.Sprintf("%d", now.UnixNano())
			stream.canvas = nil
			stream.pending = nil
			stream.frames = nil
			stream.delays = nil
			stream.frameBytes = 0
			stream.exportErr = ""
			stream.packetsReceived = 0
			stream.packetsExpected = 0
			stream.hasFrame = false
			stream.hasLastEnd = false
			stream.complete = false
			stream.incomplete = false
			stream.awaitingKeyframe = false
			stream.archiveDirty = false
			stream.lastPartialArchive = time.Time{}
			stream.framesSeen = 0
			stream.hasLatestFrame = false
			stream.recentFrames = make(map[uint8]*gifFrameBuffer)
			stream.incompleteFrames = make(map[uint8]bool)
			stream.startControlID = parsed.ImageID
			stream.hasStartControl = true
		} else if stream.pending != nil && stream.pending.imageID == parsed.ImageID && stream.pending.startSeen {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false
		}
	}

	if parsed.GIFControl && parsed.GIFFrameStart {
		return m.processGIFFrameStartLocked(stream, packet, parsed, now)
	}

	if !parsed.GIFControl {
		return m.processGIFDataPacketLocked(key, stream, packet, parsed, now)
	}

	return m.processGIFFrameCloseLocked(key, stream, packet, parsed, now)
}

func (m *Manager) processGIFFrameStartLocked(stream *gifStreamSession, packet transport.ReceivedPacket, parsed ssdv.Packet, now time.Time) (Update, bool) {
	if stream.pending != nil && stream.pending.imageID != parsed.ImageID {
		if stream.hasLatestFrame && !isNewerGIFFrame(parsed.ImageID, stream.latestFrameID) {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false // late open marker from a frame already superseded
		}
		abandonGIFFrameLocked(stream, stream.pending)
	}
	if stream.pending == nil {
		if stream.hasFrame && parsed.ImageID == stream.lastFrameID {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false
		}
		if stream.hasLatestFrame && parsed.ImageID != stream.latestFrameID && !isNewerGIFFrame(parsed.ImageID, stream.latestFrameID) {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false
		}
		stream.pending = beginGIFFrameLocked(stream, parsed, now)
	}
	frame := stream.pending
	if frame.startSeen {
		stream.lastUpdate = now
		m.mu.Unlock()
		return Update{}, false
	}
	frame.startSeen = true
	frame.receivedPackets++
	frame.controlExpected = 2
	frame.expectedPackets = int(parsed.GIFDataPackets)
	frame.keyframe = parsed.GIFKeyframe
	frame.width, frame.height = parsed.Width, parsed.Height
	frame.quality, frame.fec = parsed.Quality, parsed.FEC
	frame.delayCS = parsed.GIFDelayCS
	stream.packetsExpected += frame.expectedPackets + 2
	stream.packetsReceived++
	stream.lastUpdate = now
	update := gifProgressUpdate(stream, frame, packet, false, nil, 0)
	update.GIFAwaitingKeyframe = stream.awaitingKeyframe
	m.mu.Unlock()
	return update, true

}

func (m *Manager) processGIFDataPacketLocked(key string, stream *gifStreamSession, packet transport.ReceivedPacket, parsed ssdv.Packet, now time.Time) (Update, bool) {
	var frame *gifFrameBuffer
	lateFrame := false
	if recent := stream.recentFrames[parsed.ImageID]; recent != nil && recent.committed && now.Sub(recent.completedAt) <= gifLatePacketWindow {
		frame = recent
		lateFrame = true
	} else {
		if stream.pending != nil && stream.pending.imageID != parsed.ImageID {
			if !isNewerGIFFrame(parsed.ImageID, stream.pending.imageID) {
				stream.lastUpdate = now
				m.mu.Unlock()
				return Update{}, false // late packet from an older frame outside the repair window
			}
			abandonGIFFrameLocked(stream, stream.pending)
		}
		if stream.pending == nil {
			if stream.hasLatestFrame && parsed.ImageID != stream.latestFrameID && !isNewerGIFFrame(parsed.ImageID, stream.latestFrameID) {
				stream.lastUpdate = now
				m.mu.Unlock()
				return Update{}, false
			}
			stream.pending = beginGIFFrameLocked(stream, parsed, now)
		}
		frame = stream.pending
	}

	var raw [256]byte
	copy(raw[:], parsed.Raw[:])
	if previous, exists := frame.packets[parsed.PacketID]; exists {
		stream.lastUpdate = now
		m.mu.Unlock()
		if previous != raw {
			return Update{Profile: packet.Profile, Codec: "SSDV-GIF", Callsign: packet.Callsign, ImageID: packet.ImageID,
				Error: fmt.Sprintf("conflicting duplicate GIF packet %d", parsed.PacketID), GIFMode: true}, true
		}
		return Update{}, false
	}
	frame.packets[parsed.PacketID] = raw
	frame.receivedPackets++
	frame.keyframe = frame.keyframe || parsed.GIFKeyframe
	frame.width, frame.height = parsed.Width, parsed.Height
	frame.quality, frame.fec = parsed.Quality, parsed.FEC
	stream.packetsReceived++
	stream.lastUpdate = now

	decodedNow, decodeErr := decodeGIFSegmentsNearPacket(frame, parsed)
	if decodeErr != nil {
		stream.incompleteFrames[frame.imageID] = true
		stream.incomplete = true
	}
	if decodedNow > 0 && !lateFrame && stream.pending == frame && frame.previewCanvas != nil {
		stream.canvas = cloneRGBA(frame.previewCanvas)
	}

	if frame.committed {
		frameDataComplete := frame.expectedPackets == 0 || len(frame.packets) >= frame.expectedPackets
		framePixelsComplete := !frame.keyframe || frame.decodedMCUs >= (frame.width/16)*(frame.height/16)
		if frameDataComplete && framePixelsComplete {
			delete(stream.incompleteFrames, frame.imageID)
		} else {
			stream.incompleteFrames[frame.imageID] = true
		}
		if frame.keyframe {
			stream.awaitingKeyframe = !(frameDataComplete && framePixelsComplete)
		}
		stream.incomplete = len(stream.incompleteFrames) > 0
	}

	shouldPublish := decodedNow > 0 && frame.previewCanvas != nil
	update := gifProgressUpdate(stream, frame, packet, frame.committed, frame.previewCanvas, frame.decodedMCUs)
	update.GIFAwaitingKeyframe = stream.awaitingKeyframe
	update.GIFStreamIncomplete = stream.incomplete
	update.GIFFrameComplete = frame.committed
	update.GIFPartialArchive = lateFrame && frame.committed
	update.GIFEnd = stream.complete
	update.Complete = stream.complete && !stream.incomplete
	if !shouldPublish {
		// Emit one lightweight progress update for every unique image data packet.
		// Attach a new PNG only when the packet completes at least one MCU; if it
		// does not, advance the UI packet counter without encoding an identical image.
		m.mu.Unlock()
		return update, true
	}
	preview := cloneRGBA(frame.previewCanvas)
	m.mu.Unlock()

	pngBytes, encodeErr := encodeGIFPreviewPNG(preview)
	if encodeErr != nil {
		update.Error = fmt.Errorf("encode GIF frame preview: %w", encodeErr).Error()
		return update, true
	}
	m.mu.Lock()
	if current := m.gifStreams[key]; current == stream {
		frame.previewPNG = append(frame.previewPNG[:0], pngBytes...)
		if frame.committed {
			storeGIFFrameSnapshotLocked(stream, frame)
		} else {
			stream.archiveDirty = true
		}
		if !lateFrame && stream.pending == frame {
			stream.canvas = cloneRGBA(preview)
		} else if lateFrame && stream.latestFrameID == frame.imageID && stream.pending == nil {
			stream.canvas = cloneRGBA(preview)
		}
		stream.lastUpdate = time.Now()
	}
	update.PNGBase64 = base64.StdEncoding.EncodeToString(pngBytes)
	update.ImageWidth, update.ImageHeight = preview.Bounds().Dx(), preview.Bounds().Dy()
	update.Coverage = gifFrameCoverage(frame.decodedMCUs, parsed.MCUCount)
	update.GIFFrameComplete = frame.committed
	update.GIFAwaitingKeyframe = stream.awaitingKeyframe
	update.GIFStreamIncomplete = stream.incomplete
	update.GIFPartialArchive = lateFrame && frame.committed
	update.GIFEnd = stream.complete
	update.Complete = stream.complete && !stream.incomplete
	m.mu.Unlock()
	return update, true

}

func (m *Manager) processGIFFrameCloseLocked(key string, stream *gifStreamSession, packet transport.ReceivedPacket, parsed ssdv.Packet, now time.Time) (Update, bool) {
	// Frame-close marker. The close packet finalizes only the current frame;
	// a missing data packet is recorded as an incomplete frame and must never
	// stop later frame IDs from entering the RX pipeline.
	if stream.pending != nil && stream.pending.imageID != parsed.ImageID {
		if !isNewerGIFFrame(parsed.ImageID, stream.pending.imageID) {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false
		}
		abandonGIFFrameLocked(stream, stream.pending)
	}
	if stream.pending == nil {
		if recent := stream.recentFrames[parsed.ImageID]; recent != nil && recent.committed {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false // duplicate close marker for a frame kept only for late FEC repair
		}
		if stream.hasLatestFrame && parsed.ImageID != stream.latestFrameID && !isNewerGIFFrame(parsed.ImageID, stream.latestFrameID) {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false
		}
		stream.pending = beginGIFFrameLocked(stream, parsed, now)
	}
	frame := stream.pending
	if frame.endSeen {
		stream.lastUpdate = now
		m.mu.Unlock()
		return Update{}, false
	}
	frame.endSeen = true
	if parsed.GIFDelayCS > 0 {
		frame.delayCS = parsed.GIFDelayCS
	}
	if !frame.startSeen {
		if parsed.GIFDataPackets > 0 {
			frame.expectedPackets = int(parsed.GIFDataPackets)
		}
		frame.controlExpected = 1
		stream.packetsExpected += frame.expectedPackets + frame.controlExpected
	}
	frame.receivedPackets++
	stream.packetsReceived++

	if err := ensureGIFFramePreviewCanvas(frame); err != nil {
		stream.pending = nil
		stream.incompleteFrames[frame.imageID] = true
		stream.incomplete = true
		stream.lastFrameID, stream.hasFrame = frame.imageID, true
		stream.lastEndImageID, stream.hasLastEnd = frame.imageID, true
		rememberRecentGIFFrameLocked(stream, frame, now)
		if frame.keyframe {
			stream.awaitingKeyframe = true
		}
		stream.complete = parsed.GIFEnd
		stream.lastUpdate = now
		update := gifProgressUpdate(stream, frame, packet, false, nil, 0)
		update.GIFFrameAbandoned = true
		update.GIFEnd = parsed.GIFEnd
		update.GIFAwaitingKeyframe = stream.awaitingKeyframe
		update.GIFStreamIncomplete = true
		update.Error = err.Error()
		m.mu.Unlock()
		return update, true
	}
	if _, err := decodeAllReadyGIFSegments(frame); err != nil {
		stream.incompleteFrames[frame.imageID] = true
		stream.incomplete = true
	}

	frameDataPacketCount := len(frame.packets)
	packetsComplete := frame.expectedPackets == 0 || frameDataPacketCount >= frame.expectedPackets
	keyframeCoverageComplete := !frame.keyframe || frame.decodedMCUs >= int(parsed.MCUCount)
	frameComplete := packetsComplete && keyframeCoverageComplete
	if frameComplete {
		delete(stream.incompleteFrames, frame.imageID)
	} else {
		stream.incompleteFrames[frame.imageID] = true
	}
	stream.incomplete = len(stream.incompleteFrames) > 0
	if frame.keyframe {
		stream.awaitingKeyframe = !frameComplete
	}

	hasDecodableCanvas := frame.decodedMCUs > 0 || (!frame.keyframe && frame.baseCanvas != nil)
	if !hasDecodableCanvas {
		stream.pending = nil
		stream.lastFrameID, stream.hasFrame = frame.imageID, true
		stream.lastEndImageID, stream.hasLastEnd = frame.imageID, true
		stream.complete = parsed.GIFEnd
		stream.lastUpdate = now
		rememberRecentGIFFrameLocked(stream, frame, now)
		if frame.keyframe {
			stream.awaitingKeyframe = true
		}
		update := gifProgressUpdate(stream, frame, packet, false, nil, frame.decodedMCUs)
		update.GIFFrameAbandoned = true
		update.GIFEnd = parsed.GIFEnd
		update.GIFAwaitingKeyframe = stream.awaitingKeyframe
		update.GIFStreamIncomplete = true
		update.Complete = false
		m.mu.Unlock()
		return update, true
	}

	preview := cloneRGBA(frame.previewCanvas)
	stream.pending = nil
	stream.canvas = cloneRGBA(preview)
	stream.lastFrameID, stream.hasFrame = frame.imageID, true
	stream.lastEndImageID, stream.hasLastEnd = frame.imageID, true
	stream.complete = parsed.GIFEnd
	stream.lastUpdate = now
	rememberRecentGIFFrameLocked(stream, frame, now)
	m.mu.Unlock()
	return m.publishGIFClosedFrame(key, stream, packet, parsed, frame, preview)
}

func (m *Manager) publishGIFClosedFrame(key string, stream *gifStreamSession, packet transport.ReceivedPacket, parsed ssdv.Packet, frame *gifFrameBuffer, preview *image.RGBA) (Update, bool) {

	pngBytes, encodeErr := encodeGIFPreviewPNG(preview)
	if encodeErr != nil {
		return Update{Profile: packet.Profile, Codec: "SSDV-GIF", Callsign: packet.Callsign, ImageID: packet.ImageID,
			Error: fmt.Errorf("encode GIF frame preview: %w", encodeErr).Error(), GIFMode: true}, true
	}

	m.mu.Lock()
	if current := m.gifStreams[key]; current == stream {
		frame.previewPNG = append(frame.previewPNG[:0], pngBytes...)
		storeGIFFrameSnapshotLocked(stream, frame)
		stream.archiveDirty = true
		stream.lastUpdate = time.Now()
	}
	update := gifProgressUpdate(stream, frame, packet, true, preview, frame.decodedMCUs)
	update.PNGBase64 = base64.StdEncoding.EncodeToString(pngBytes)
	update.ImageWidth, update.ImageHeight = preview.Bounds().Dx(), preview.Bounds().Dy()
	update.Coverage = gifFrameCoverage(frame.decodedMCUs, parsed.MCUCount)
	update.GIFFrameComplete = true // frame boundary arrived; completeness is reported separately
	update.Complete = parsed.GIFEnd && !stream.incomplete
	update.GIFEnd = parsed.GIFEnd
	update.GIFAwaitingKeyframe = stream.awaitingKeyframe
	update.GIFStreamIncomplete = stream.incomplete
	m.mu.Unlock()
	return update, true
}

func isNewerGIFFrame(candidate, current uint8) bool {
	delta := uint8(candidate - current)
	return delta != 0 && delta < 128
}

func newGIFPlaceholder(width, height int) *image.RGBA {
	if width <= 0 || height <= 0 || width > 4080 || height > 4080 {
		return nil
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{R: 218, G: 220, B: 222, A: 255}}, image.Point{}, draw.Src)
	return canvas
}

func beginGIFFrameLocked(stream *gifStreamSession, packet ssdv.Packet, now time.Time) *gifFrameBuffer {
	stream.framesSeen++
	frame := newGIFFrameBuffer(packet, now)
	frame.frameIndex = stream.framesSeen
	if !frame.keyframe {
		switch {
		case packet.GIFVersion >= 2 && len(packet.GIFMotion) > 0 && stream.canvas != nil:
			frame.baseCanvas = gifmode.ApplyMotionVectors(stream.canvas, packet.GIFMotion)
		case stream.canvas != nil:
			frame.baseCanvas = cloneRGBA(stream.canvas)
		default:
			// A missing first/periodic keyframe must not permanently stall RX.
			// Decode future sparse MCU segments over a neutral canvas until a
			// complete keyframe re-establishes the reference image.
			frame.baseCanvas = newGIFPlaceholder(packet.Width, packet.Height)
			stream.awaitingKeyframe = true
		}
	}
	if frame.baseCanvas != nil {
		frame.previewCanvas = cloneRGBA(frame.baseCanvas)
	} else {
		frame.previewCanvas = newGIFPlaceholder(frame.width, frame.height)
	}
	stream.latestFrameID, stream.hasLatestFrame = packet.ImageID, true
	return frame
}

func appendGIFFrameSnapshotLocked(stream *gifStreamSession, pngBytes []byte, delayCS uint16) bool {
	if stream == nil || len(pngBytes) == 0 {
		return false
	}
	if len(stream.frames) >= maxGIFExportFrames {
		stream.exportErr = fmt.Sprintf("received GIF exceeds the %d-frame export limit", maxGIFExportFrames)
		return false
	}
	if stream.frameBytes+len(pngBytes) > maxGIFExportPNGSize {
		stream.exportErr = fmt.Sprintf("received GIF exceeds the %d MiB export cache limit", maxGIFExportPNGSize/(1<<20))
		return false
	}
	stream.frames = append(stream.frames, append([]byte(nil), pngBytes...))
	delay := int(delayCS)
	if delay <= 0 {
		delay = 10
	}
	stream.delays = append(stream.delays, delay)
	stream.frameBytes += len(pngBytes)
	stream.archiveDirty = true
	return true
}

func abandonGIFFrameLocked(stream *gifStreamSession, frame *gifFrameBuffer) {
	if stream == nil || frame == nil {
		return
	}
	if stream.incompleteFrames == nil {
		stream.incompleteFrames = make(map[uint8]bool)
	}
	stream.incompleteFrames[frame.imageID] = true
	stream.incomplete = true
	if len(frame.previewPNG) > 0 {
		storeGIFFrameSnapshotLocked(stream, frame)
	}
	if frame.previewCanvas != nil {
		stream.canvas = cloneRGBA(frame.previewCanvas)
	}
	stream.lastFrameID, stream.hasFrame = frame.imageID, true
	stream.lastEndImageID, stream.hasLastEnd = frame.imageID, true
	stream.pending = nil
	if frame.keyframe || stream.canvas == nil {
		stream.awaitingKeyframe = true
	}
	rememberRecentGIFFrameLocked(stream, frame, time.Now())
	stream.lastUpdate = time.Now()
}

func newGIFFrameBuffer(packet ssdv.Packet, now time.Time) *gifFrameBuffer {
	return &gifFrameBuffer{
		imageID: packet.ImageID, packets: make(map[uint16][256]byte),
		keyframe: packet.GIFKeyframe, width: packet.Width, height: packet.Height,
		quality: packet.Quality, fec: packet.FEC, delayCS: packet.GIFDelayCS, startedAt: now,
		decodedSegments: make(map[gifSegmentKey]struct{}), snapshotIndex: -1,
	}
}

func orderedGIFPackets(packets map[uint16][256]byte) [][]byte {
	ids := make([]int, 0, len(packets))
	for id := range packets {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	out := make([][]byte, 0, len(ids))
	for _, id := range ids {
		raw := packets[uint16(id)]
		out = append(out, append([]byte(nil), raw[:]...))
	}
	return out
}

func pruneRecentGIFFramesLocked(stream *gifStreamSession, now time.Time) {
	if stream == nil || stream.recentFrames == nil {
		return
	}
	for imageID, frame := range stream.recentFrames {
		if frame == nil || frame.completedAt.IsZero() || now.Sub(frame.completedAt) > gifLatePacketWindow {
			delete(stream.recentFrames, imageID)
		}
	}
	for len(stream.recentFrames) > maxGIFRecentFrames {
		var oldestID uint8
		var oldest time.Time
		first := true
		for imageID, frame := range stream.recentFrames {
			if frame == nil {
				oldestID, first = imageID, false
				break
			}
			if first || frame.completedAt.Before(oldest) {
				oldestID, oldest, first = imageID, frame.completedAt, false
			}
		}
		delete(stream.recentFrames, oldestID)
	}
}

func rememberRecentGIFFrameLocked(stream *gifStreamSession, frame *gifFrameBuffer, now time.Time) {
	if stream == nil || frame == nil {
		return
	}
	if stream.recentFrames == nil {
		stream.recentFrames = make(map[uint8]*gifFrameBuffer)
	}
	frame.committed = true
	frame.completedAt = now
	stream.recentFrames[frame.imageID] = frame
	pruneRecentGIFFramesLocked(stream, now)
}

func ensureGIFFramePreviewCanvas(frame *gifFrameBuffer) error {
	if frame == nil {
		return fmt.Errorf("GIF frame is nil")
	}
	if frame.previewCanvas != nil {
		return nil
	}
	if frame.baseCanvas != nil {
		frame.previewCanvas = cloneRGBA(frame.baseCanvas)
	} else {
		frame.previewCanvas = newGIFPlaceholder(frame.width, frame.height)
	}
	if frame.previewCanvas == nil {
		return fmt.Errorf("GIF frame has invalid preview dimensions")
	}
	if frame.decodedSegments == nil {
		frame.decodedSegments = make(map[gifSegmentKey]struct{})
	}
	return nil
}

func insertGIFBoundary(frame *gifFrameBuffer, packet ssdv.Packet) {
	if frame == nil || packet.MCUOffset == 0xff || packet.MCUIndex == 0xffff {
		return
	}
	index := sort.Search(len(frame.boundaryPacketIDs), func(i int) bool {
		return frame.boundaryPacketIDs[i] >= packet.PacketID
	})
	if index < len(frame.boundaryPacketIDs) && frame.boundaryPacketIDs[index] == packet.PacketID {
		return
	}
	frame.boundaryPacketIDs = append(frame.boundaryPacketIDs, 0)
	copy(frame.boundaryPacketIDs[index+1:], frame.boundaryPacketIDs[index:])
	frame.boundaryPacketIDs[index] = packet.PacketID
}

func tryDecodeGIFSegment(frame *gifFrameBuffer, startID, endID uint16) (int, error) {
	if frame == nil || endID < startID {
		return 0, nil
	}
	key := gifSegmentKey{startPacket: startID, endPacket: endID}
	if _, done := frame.decodedSegments[key]; done {
		return 0, nil
	}
	if err := ensureGIFFramePreviewCanvas(frame); err != nil {
		return 0, err
	}
	count := int(endID) - int(startID) + 1
	if count <= 0 || count > len(frame.packets) {
		return 0, nil
	}
	rawPackets := make([][]byte, 0, count)
	for id := int(startID); id <= int(endID); id++ {
		raw, ok := frame.packets[uint16(id)]
		if !ok {
			return 0, nil // a packet gap blocks only this MCU segment
		}
		rawPackets = append(rawPackets, append([]byte(nil), raw[:]...))
	}
	_, _, decoded, err := ssdv.UpdatePacketsPreviewSparse(frame.previewCanvas, rawPackets)
	if err != nil {
		// Incomplete entropy segments are expected while packets are arriving.
		// Do not mark them decoded: an FEC-recovered packet can make this exact
		// segment decodable later.
		return 0, nil
	}
	if decoded <= 0 {
		return 0, nil
	}
	first, firstErr := ssdv.ParsePacket(rawPackets[0])
	last, lastErr := ssdv.ParsePacket(rawPackets[len(rawPackets)-1])
	if firstErr != nil || lastErr != nil || first.MCUIndex == 0xffff {
		return 0, nil
	}
	totalMCUs := int(first.MCUCount)
	segmentEndMCU := totalMCUs
	if last.MCUIndex != 0xffff && last.MCUIndex > first.MCUIndex {
		segmentEndMCU = int(last.MCUIndex)
	}
	if segmentEndMCU > totalMCUs {
		segmentEndMCU = totalMCUs
	}
	if len(frame.coveredMCUs) != totalMCUs {
		frame.coveredMCUs = make([]bool, totalMCUs)
	}
	// The sparse decoder reports how many MCU blocks it actually reconstructed.
	// Do not mark the whole [first, end) range merely because the EOI tail is
	// present: a damaged/truncated tail can decode only a prefix, and falsely
	// counting the remaining MCUs makes an incomplete keyframe look complete.
	decodedEndMCU := int(first.MCUIndex) + decoded
	if decodedEndMCU > segmentEndMCU {
		decodedEndMCU = segmentEndMCU
	}
	newlyCovered := 0
	for mcu := int(first.MCUIndex); mcu < decodedEndMCU; mcu++ {
		if !frame.coveredMCUs[mcu] {
			frame.coveredMCUs[mcu] = true
			newlyCovered++
		}
	}
	// Cache only a fully decoded bounded segment. A short final EOI tail remains
	// retryable so an 8+2/FEC-recovered packet arriving after the close marker
	// can complete it later.
	if decoded >= segmentEndMCU-int(first.MCUIndex) {
		frame.decodedSegments[key] = struct{}{}
	}
	frame.decodedMCUs += newlyCovered
	return newlyCovered, nil
}

// decodeGIFSegmentsNearPacket only revisits the one or two MCU intervals that
// can become complete when this packet arrives. It avoids sorting and
// re-decoding every packet accumulated in a long animation frame (the previous
// implementation performed O(n²) work and repeatedly encoded full PNG/Base64
// previews, which could saturate the RX worker queue).
func decodeGIFSegmentsNearPacket(frame *gifFrameBuffer, packet ssdv.Packet) (int, error) {
	if frame == nil {
		return 0, fmt.Errorf("GIF frame is nil")
	}
	if err := ensureGIFFramePreviewCanvas(frame); err != nil {
		return 0, err
	}
	insertGIFBoundary(frame, packet)
	if packet.EOI {
		frame.eoiPacketID = packet.PacketID
		frame.hasEOI = true
	}
	ids := frame.boundaryPacketIDs
	index := sort.Search(len(ids), func(i int) bool { return ids[i] >= packet.PacketID })
	newlyDecoded := 0
	try := func(start, end uint16) {
		frame.decodeAttempts++
		decoded, err := tryDecodeGIFSegment(frame, start, end)
		if err != nil {
			frame.lastDecodeError = err.Error()
			return
		}
		if decoded > 0 {
			frame.lastDecodeAt = time.Now()
			frame.lastDecodeError = ""
		}
		newlyDecoded += decoded
	}
	if packet.MCUOffset != 0xff && packet.MCUIndex != 0xffff {
		if index > 0 {
			try(ids[index-1], packet.PacketID)
		}
		if index+1 < len(ids) {
			try(packet.PacketID, ids[index+1])
		}
	} else if index > 0 && index < len(ids) {
		// This packet may be the missing middle payload between two restart
		// boundaries (for example, a late Raptor/8+2 recovery packet).
		try(ids[index-1], ids[index])
	}
	if frame.hasEOI && len(ids) > 0 {
		lastBoundary := ids[len(ids)-1]
		if lastBoundary <= frame.eoiPacketID && packet.PacketID <= frame.eoiPacketID {
			// This also handles a late non-boundary packet in the final entropy
			// tail. Such a packet has no following MCU restart marker, so the
			// ordinary predecessor/successor lookup cannot find its segment.
			try(lastBoundary, frame.eoiPacketID)
		}
	}
	return newlyDecoded, nil
}

func decodeAllReadyGIFSegments(frame *gifFrameBuffer) (int, error) {
	if frame == nil || len(frame.packets) == 0 {
		return 0, nil
	}
	if len(frame.boundaryPacketIDs) == 0 {
		// Compatibility with legacy continuous-JPEG GIF senders that do not
		// carry independent MCU restart metadata. Decode their complete stream
		// once, at close, rather than retrying it after every packet.
		raw := orderedGIFPackets(frame.packets)
		canvas := cloneRGBA(frame.baseCanvas)
		preview, _, decoded, err := decodeGIFFramePreview(canvas, frame, raw)
		if err != nil {
			return 0, err
		}
		frame.previewCanvas = preview
		frame.decodedMCUs += decoded
		return decoded, nil
	}
	newlyDecoded := 0
	ids := frame.boundaryPacketIDs
	for i := 0; i+1 < len(ids); i++ {
		frame.decodeAttempts++
		decoded, err := tryDecodeGIFSegment(frame, ids[i], ids[i+1])
		if err != nil {
			return newlyDecoded, err
		}
		newlyDecoded += decoded
		if decoded > 0 {
			frame.lastDecodeAt = time.Now()
			frame.lastDecodeError = ""
		}
	}
	// The final entropy tail ends at the SSDV EOI packet. It may have no MCU
	// restart metadata of its own, so it is handled separately.
	eoiID, hasEOI := frame.eoiPacketID, frame.hasEOI
	if !hasEOI {
		for id, raw := range frame.packets {
			parsed, err := ssdv.ParsePacket(raw[:])
			if err == nil && parsed.EOI && (!hasEOI || id > eoiID) {
				eoiID, hasEOI = id, true
			}
		}
	}
	if hasEOI && ids[len(ids)-1] <= eoiID {
		decoded, err := tryDecodeGIFSegment(frame, ids[len(ids)-1], eoiID)
		if err != nil {
			return newlyDecoded, err
		}
		newlyDecoded += decoded
	}
	return newlyDecoded, nil
}

func storeGIFFrameSnapshotLocked(stream *gifStreamSession, frame *gifFrameBuffer) bool {
	if stream == nil || frame == nil || len(frame.previewPNG) == 0 {
		return false
	}
	if frame.snapshotIndex >= 0 && frame.snapshotIndex < len(stream.frames) {
		stream.frameBytes -= len(stream.frames[frame.snapshotIndex])
		stream.frames[frame.snapshotIndex] = append(stream.frames[frame.snapshotIndex][:0], frame.previewPNG...)
		stream.frameBytes += len(frame.previewPNG)
		stream.archiveDirty = true
		return true
	}
	if !appendGIFFrameSnapshotLocked(stream, frame.previewPNG, frame.delayCS) {
		return false
	}
	frame.snapshotIndex = len(stream.frames) - 1
	return true
}

func decodeGIFFramePreview(canvas *image.RGBA, frame *gifFrameBuffer, rawPackets [][]byte) (*image.RGBA, ssdv.Packet, int, error) {
	if len(rawPackets) == 0 {
		return canvas, ssdv.Packet{}, 0, nil
	}
	if frame.keyframe {
		return ssdv.DecodePacketsToPreview(rawPackets)
	}
	if canvas == nil {
		canvas = newGIFPlaceholder(frame.width, frame.height)
		if canvas == nil {
			return nil, ssdv.Packet{}, 0, fmt.Errorf("GIF delta frame has invalid dimensions while awaiting a keyframe")
		}
	}
	return ssdv.UpdatePacketsPreviewSparse(canvas, rawPackets)
}

func encodeGIFPreviewPNG(canvas *image.RGBA) ([]byte, error) {
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func gifFrameCoverage(decodedMCUs int, total uint32) float64 {
	if total == 0 {
		return 0
	}
	coverage := float64(decodedMCUs) / float64(total)
	if coverage > 1 {
		coverage = 1
	}
	return coverage
}

func gifProgressUpdate(stream *gifStreamSession, frame *gifFrameBuffer, packet transport.ReceivedPacket, complete bool, canvas *image.RGBA, decodedMCUs int) Update {
	update := Update{
		Profile: packet.Profile, Codec: "SSDV-GIF", Callsign: packet.Callsign, ImageID: packet.ImageID,
		PacketsReceived: stream.packetsReceived, PacketsExpected: stream.packetsExpected,
		Complete: complete, GIFMode: true, GIFFrame: frame.frameIndex,
		GIFFramePacketsReceived: frame.receivedPackets, GIFFramePacketsExpected: frame.expectedPackets + frame.controlExpected,
		GIFStart: packet.GIFStart, GIFEnd: packet.GIFEnd,
		FrameDurationMS: int(frame.delayCS) * 10, SessionID: stream.sessionID,
		Quality: frame.quality, FEC: frame.fec,
		HasAX25SSID: packet.HasAX25SSID, AX25SourceSSID: packet.AX25SourceSSID,
	}
	if canvas != nil {
		update.ImageWidth, update.ImageHeight = canvas.Bounds().Dx(), canvas.Bounds().Dy()
		update.Coverage = gifFrameCoverage(decodedMCUs, uint32((update.ImageWidth/16)*(update.ImageHeight/16)))
	}
	return update
}

// ExportLatestGIF returns the most recently completed, fully reconstructed animation.
func (m *Manager) ExportLatestGIF() ([]byte, string, int, error) {
	data, callsign, count, complete, err := m.exportGIFSnapshot("", false)
	if err == nil && !complete {
		err = fmt.Errorf("received GIF is incomplete")
	}
	return data, callsign, count, err
}

// ExportGIF exports one exact, fully completed RX GIF session.
func (m *Manager) ExportGIF(sessionID string) ([]byte, string, int, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, "", 0, fmt.Errorf("GIF session ID is empty")
	}
	data, callsign, count, complete, err := m.exportGIFSnapshot(sessionID, false)
	if err == nil && !complete {
		err = fmt.Errorf("received GIF session %q is incomplete", sessionID)
	}
	return data, callsign, count, err
}

// ExportGIFSnapshot exports every decodable frame in a session, including the
// latest progressive preview for a frame whose close marker has not arrived.
// complete is true only when a stream-end marker was received and no frame in
// the stream was abandoned or failed to decode.
func (m *Manager) ExportGIFSnapshot(sessionID string) ([]byte, string, int, bool, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, "", 0, false, fmt.Errorf("GIF session ID is empty")
	}
	return m.exportGIFSnapshot(sessionID, true)
}

// ReleaseGIFExport discards composed PNG snapshots after the application has
// successfully persisted a terminal GIF snapshot. The compact stream record
// remains so late duplicate packets can still be suppressed.
func (m *Manager) ReleaseGIFExport(sessionID string) bool {
	if m == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, stream := range m.gifStreams {
		if stream.sessionID != sessionID || !stream.complete {
			continue
		}
		if stream.frames == nil && stream.delays == nil && stream.canvas == nil && stream.pending == nil {
			return false
		}
		stream.frames = nil
		stream.delays = nil
		stream.frameBytes = 0
		stream.canvas = nil
		stream.pending = nil
		stream.recentFrames = nil
		stream.archiveDirty = false
		stream.lastUpdate = time.Now()
		return true
	}
	return false
}

func hasGIFSnapshotLocked(stream *gifStreamSession) bool {
	if stream == nil {
		return false
	}
	if len(stream.frames) > 0 {
		return true
	}
	return stream.pending != nil && len(stream.pending.previewPNG) > 0
}

// RX stores composed PNG snapshots rather than raw SSDV packets, so exported
// files show exactly the reconstructed frames. A pending frame contributes at
// most one latest progressive snapshot; it is never duplicated per packet.
func (m *Manager) exportGIFSnapshot(sessionID string, allowPartial bool) ([]byte, string, int, bool, error) {
	if m == nil {
		return nil, "", 0, false, fmt.Errorf("RX manager is unavailable")
	}
	m.mu.Lock()
	var selected *gifStreamSession
	for _, candidate := range m.gifStreams {
		if sessionID != "" && candidate.sessionID != sessionID {
			continue
		}
		if allowPartial {
			if !hasGIFSnapshotLocked(candidate) {
				continue
			}
		} else if !candidate.complete || candidate.incomplete || len(candidate.frames) == 0 || candidate.pending != nil {
			continue
		}
		if selected == nil || candidate.lastUpdate.After(selected.lastUpdate) {
			selected = candidate
		}
	}
	if selected == nil {
		m.mu.Unlock()
		if sessionID != "" {
			return nil, "", 0, false, fmt.Errorf("received GIF session %q has no decodable frames", sessionID)
		}
		return nil, "", 0, false, fmt.Errorf("no received GIF with decodable frames is available")
	}
	if selected.exportErr != "" {
		err := selected.exportErr
		m.mu.Unlock()
		return nil, "", 0, false, fmt.Errorf("cannot export received GIF: %s", err)
	}
	frames := make([][]byte, 0, len(selected.frames)+1)
	for i := range selected.frames {
		frames = append(frames, append([]byte(nil), selected.frames[i]...))
	}
	delays := append([]int(nil), selected.delays...)
	if selected.pending != nil && len(selected.pending.previewPNG) > 0 {
		frames = append(frames, append([]byte(nil), selected.pending.previewPNG...))
		delay := int(selected.pending.delayCS)
		if delay <= 0 {
			delay = 10
		}
		delays = append(delays, delay)
	}
	callsign := selected.callsign
	complete := selected.complete && !selected.incomplete && selected.pending == nil
	m.mu.Unlock()

	if len(frames) == 0 {
		return nil, "", 0, false, fmt.Errorf("received GIF contains no decodable frame snapshots")
	}
	animation := &gif.GIF{
		Image:     make([]*image.Paletted, len(frames)),
		Delay:     delays,
		Disposal:  make([]byte, len(frames)),
		LoopCount: 0,
	}
	for i, encoded := range frames {
		decoded, err := png.Decode(bytes.NewReader(encoded))
		if err != nil {
			return nil, "", 0, false, fmt.Errorf("decode received GIF frame %d: %w", i+1, err)
		}
		bounds := decoded.Bounds()
		paletted := image.NewPaletted(image.Rect(0, 0, bounds.Dx(), bounds.Dy()), palette.Plan9)
		draw.FloydSteinberg.Draw(paletted, paletted.Bounds(), decoded, bounds.Min)
		animation.Image[i] = paletted
		animation.Disposal[i] = gif.DisposalNone
	}
	var output bytes.Buffer
	if err := gif.EncodeAll(&output, animation); err != nil {
		return nil, "", 0, false, fmt.Errorf("encode received GIF: %w", err)
	}
	return output.Bytes(), callsign, len(frames), complete, nil
}
