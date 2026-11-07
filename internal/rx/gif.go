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

type gifFrameBuffer struct {
	imageID         uint8
	packets         map[uint16][256]byte
	keyframe        bool
	width           int
	height          int
	quality         uint8
	fec             bool
	delayCS         uint16
	frameIndex      uint16
	expectedPackets int
	controlExpected int
	startSeen       bool
	endSeen         bool
	receivedPackets int // unique frame-open, data and frame-close packets
	startedAt       time.Time
	baseCanvas      *image.RGBA // SGV2 motion-compensated base for this frame
	previewPNG      []byte      // latest independently decodable partial frame snapshot
	decodedMCUs     int
}

const (
	maxGIFStreams       = 4
	maxGIFExportFrames  = 120
	maxGIFExportPNGSize = 32 << 20
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
			stream.startControlID = parsed.ImageID
			stream.hasStartControl = true
		} else if stream.pending != nil && stream.pending.imageID == parsed.ImageID && stream.pending.startSeen {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false
		}
	}

	if parsed.GIFControl && parsed.GIFFrameStart {
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

	if !parsed.GIFControl {
		if stream.pending == nil && stream.hasFrame && parsed.ImageID == stream.lastFrameID {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false // late duplicate payload after a committed/abandoned frame
		}
		if stream.pending != nil && stream.pending.imageID != parsed.ImageID {
			if !isNewerGIFFrame(parsed.ImageID, stream.pending.imageID) {
				stream.lastUpdate = now
				m.mu.Unlock()
				return Update{}, false // late packet from an older frame must not replace the active one
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
		frame := stream.pending
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
		rawPackets := orderedGIFPackets(frame.packets)
		canvas := cloneRGBA(frame.baseCanvas)
		if canvas == nil {
			canvas = cloneRGBA(stream.canvas)
		}
		if canvas == nil && !frame.keyframe {
			canvas = newGIFPlaceholder(frame.width, frame.height)
		}
		frameCopy := *frame
		frameCopy.packets = nil
		update := gifProgressUpdate(stream, frame, packet, false, nil, 0)
		update.GIFAwaitingKeyframe = stream.awaitingKeyframe
		m.mu.Unlock()

		preview, metadata, decodedMCUs, previewErr := decodeGIFFramePreview(canvas, &frameCopy, rawPackets)
		if previewErr == nil && preview != nil {
			pngBytes, encodeErr := encodeGIFPreviewPNG(preview)
			if encodeErr == nil {
				m.mu.Lock()
				if current := m.gifStreams[key]; current == stream && current.pending == frame {
					frame.previewPNG = append(frame.previewPNG[:0], pngBytes...)
					frame.decodedMCUs = decodedMCUs
					current.canvas = cloneRGBA(preview)
					current.lastUpdate = time.Now()
					current.archiveDirty = true
					if frame.keyframe {
						keyframeComplete := frame.expectedPackets > 0 && len(frame.packets) >= frame.expectedPackets &&
							metadata.MCUCount > 0 && decodedMCUs >= int(metadata.MCUCount)
						current.awaitingKeyframe = !keyframeComplete
					}
				}
				update.PNGBase64 = base64.StdEncoding.EncodeToString(pngBytes)
				update.ImageWidth, update.ImageHeight = preview.Bounds().Dx(), preview.Bounds().Dy()
				if metadata.Width > 0 {
					update.ImageWidth, update.ImageHeight = metadata.Width, metadata.Height
				}
				update.Coverage = gifFrameCoverage(decodedMCUs, metadata.MCUCount)
				m.mu.Unlock()
			}
		}
		// A missing keyframe does not poison the stream. Delta frames are decoded
		// over a neutral placeholder until a later periodic keyframe can restore
		// the complete reference canvas.
		m.mu.Lock()
		update.GIFAwaitingKeyframe = stream.awaitingKeyframe
		update.GIFStreamIncomplete = stream.incomplete
		m.mu.Unlock()
		return update, true
	}

	// Frame-close marker. A close for an older frame is ignored; a newer close
	// without its open marker is still accepted as a legacy/partially received
	// frame so that later keyframes can re-synchronize the stream.
	if stream.pending != nil && stream.pending.imageID != parsed.ImageID {
		if !isNewerGIFFrame(parsed.ImageID, stream.pending.imageID) {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false
		}
		abandonGIFFrameLocked(stream, stream.pending)
	}
	if stream.pending == nil {
		if stream.hasLastEnd && stream.lastEndImageID == parsed.ImageID {
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
	rawPackets := orderedGIFPackets(frame.packets)
	canvas := cloneRGBA(frame.baseCanvas)
	if canvas == nil {
		canvas = cloneRGBA(stream.canvas)
	}
	if canvas == nil && !frame.keyframe {
		canvas = newGIFPlaceholder(frame.width, frame.height)
	}
	frameDataPacketCount := len(frame.packets)
	frameCopy := *frame
	frameCopy.packets = nil
	stream.pending = nil
	stream.lastUpdate = now
	m.mu.Unlock()

	var preview *image.RGBA
	var metadata ssdv.Packet
	decodedMCUs := 0
	if len(rawPackets) > 0 {
		preview, metadata, decodedMCUs, err = decodeGIFFramePreview(canvas, &frameCopy, rawPackets)
	} else if canvas != nil && !frameCopy.keyframe {
		preview = canvas
		metadata = ssdv.Packet{Width: canvas.Bounds().Dx(), Height: canvas.Bounds().Dy(), MCUCount: uint32((canvas.Bounds().Dx() / 16) * (canvas.Bounds().Dy() / 16))}
	} else {
		err = fmt.Errorf("GIF keyframe arrived without image packets")
	}
	if err != nil || preview == nil {
		if err == nil {
			err = fmt.Errorf("GIF frame has no decodable canvas")
		}
		m.mu.Lock()
		if current := m.gifStreams[key]; current == stream {
			abandonGIFFrameLocked(stream, &frameCopy)
			stream.lastFrameID, stream.hasFrame = parsed.ImageID, true
			stream.lastEndImageID, stream.hasLastEnd = parsed.ImageID, true
			// A failed keyframe invalidates the reference image even when an older
			// canvas exists. Keep accepting later delta frames over that best-known
			// canvas/placeholder until a complete periodic keyframe arrives.
			if frameCopy.keyframe {
				stream.awaitingKeyframe = true
			}
			stream.incomplete = true
			stream.complete = parsed.GIFEnd
			stream.lastUpdate = time.Now()
		}
		update := gifProgressUpdate(stream, &frameCopy, packet, false, nil, 0)
		update.Complete = false
		update.GIFFrameComplete = false
		update.GIFFrameAbandoned = true
		update.GIFEnd = parsed.GIFEnd
		update.GIFAwaitingKeyframe = stream.awaitingKeyframe
		update.GIFStreamIncomplete = true
		update.Error = err.Error()
		m.mu.Unlock()
		return update, true
	}
	pngBytes, err := encodeGIFPreviewPNG(preview)
	if err != nil {
		return Update{Profile: packet.Profile, Codec: "SSDV-GIF", Callsign: packet.Callsign, ImageID: packet.ImageID,
			Error: fmt.Errorf("encode GIF frame preview: %w", err).Error(), GIFMode: true}, true
	}

	m.mu.Lock()
	if current := m.gifStreams[key]; current == stream {
		stream.canvas = cloneRGBA(preview)
		stream.lastFrameID, stream.hasFrame, stream.lastUpdate = parsed.ImageID, true, time.Now()
		stream.lastEndImageID, stream.hasLastEnd = parsed.ImageID, true
		// A frame-close can arrive even when one or more data packets were lost.
		// Keep its partially decoded canvas and continue the stream, but remember
		// that the final album item cannot be reported as a complete animation.
		framePacketsComplete := frameCopy.expectedPackets == 0 || frameDataPacketCount >= frameCopy.expectedPackets
		keyframeCoverageComplete := !frameCopy.keyframe || metadata.MCUCount == 0 || decodedMCUs >= int(metadata.MCUCount)
		if !framePacketsComplete || !keyframeCoverageComplete {
			stream.incomplete = true
		}
		if !appendGIFFrameSnapshotLocked(stream, pngBytes, frameCopy.delayCS) {
			// exportErr is surfaced by ExportGIFSnapshot; keep decoding even when
			// the album export budget has been reached.
		}
		if frameCopy.keyframe {
			// Do not treat a decodable preview as a valid reference keyframe
			// unless all advertised packets and the expected MCU coverage arrived.
			stream.awaitingKeyframe = !(framePacketsComplete && keyframeCoverageComplete)
		}
		if parsed.GIFEnd {
			stream.complete = true
		}
	}
	update := gifProgressUpdate(stream, &frameCopy, packet, true, preview, decodedMCUs)
	update.PNGBase64 = base64.StdEncoding.EncodeToString(pngBytes)
	update.ImageWidth, update.ImageHeight = preview.Bounds().Dx(), preview.Bounds().Dy()
	if metadata.Width > 0 {
		update.ImageWidth, update.ImageHeight = metadata.Width, metadata.Height
	}
	update.Coverage = gifFrameCoverage(decodedMCUs, metadata.MCUCount)
	update.GIFFrameComplete = true
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
	stream.incomplete = true
	if len(frame.previewPNG) > 0 {
		appendGIFFrameSnapshotLocked(stream, frame.previewPNG, frame.delayCS)
	}
	stream.lastFrameID, stream.hasFrame = frame.imageID, true
	stream.lastEndImageID, stream.hasLastEnd = frame.imageID, true
	stream.pending = nil
	if frame.keyframe || stream.canvas == nil {
		stream.awaitingKeyframe = true
	}
	stream.lastUpdate = time.Now()
}

func newGIFFrameBuffer(packet ssdv.Packet, now time.Time) *gifFrameBuffer {
	return &gifFrameBuffer{
		imageID: packet.ImageID, packets: make(map[uint16][256]byte),
		keyframe: packet.GIFKeyframe, width: packet.Width, height: packet.Height,
		quality: packet.Quality, fec: packet.FEC, delayCS: packet.GIFDelayCS, startedAt: now,
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
