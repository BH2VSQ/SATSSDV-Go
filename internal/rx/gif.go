package rx

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"sort"
	"strings"
	"time"

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
}

const (
	maxGIFStreams       = 4
	maxGIFExportFrames  = 120
	maxGIFExportPNGSize = 32 << 20
)

type gifStreamSession struct {
	profile         string
	callsign        string
	ssid            uint8
	hasSSID         bool
	sessionID       string
	canvas          *image.RGBA
	pending         *gifFrameBuffer
	lastUpdate      time.Time
	lastFrameID     uint8
	hasFrame        bool
	lastEndImageID  uint8
	hasLastEnd      bool
	startControlID  uint8
	hasStartControl bool
	packetsReceived int      // unique decoded data and frame-control packets
	packetsExpected int      // expected unique data and frame-control packets
	frames          [][]byte // composed PNG frames; bounded for safe GIF export
	delays          []int
	frameBytes      int
	exportErr       string
	complete        bool
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

	// GIFStart is a stream-level marker, separate from the per-frame open
	// marker. It resets the RX animation session exactly once per new burst.
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
			stream.startControlID = parsed.ImageID
			stream.hasStartControl = true
		} else if stream.pending != nil && stream.pending.imageID == parsed.ImageID && stream.pending.startSeen {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false // repeated physical copy of the stream-start marker
		}
	}

	if parsed.GIFControl && parsed.GIFFrameStart {
		if stream.pending == nil || stream.pending.imageID != parsed.ImageID {
			stream.pending = newGIFFrameBuffer(parsed, now)
			stream.pending.frameIndex = uint16(len(stream.frames) + 1)
		}
		frame := stream.pending
		if frame.startSeen {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false // duplicate frame-open marker
		}
		frame.startSeen = true
		frame.receivedPackets++ // one de-duplicated frame-open control packet
		frame.controlExpected = 2
		frame.expectedPackets = int(parsed.GIFDataPackets)
		frame.keyframe = parsed.GIFKeyframe
		frame.width, frame.height = parsed.Width, parsed.Height
		frame.quality, frame.fec = parsed.Quality, parsed.FEC
		frame.delayCS = parsed.GIFDelayCS
		// Count one decoded frame-open and one expected frame-close control
		// packet in addition to the advertised image-data packet count. The
		// stream decoder removes the three redundant physical copies.
		stream.packetsExpected += frame.expectedPackets + 2
		stream.packetsReceived++
		stream.lastUpdate = now
		update := gifProgressUpdate(stream, frame, packet, false, nil, 0)
		m.mu.Unlock()
		return update, true
	}

	if !parsed.GIFControl {
		if stream.hasFrame && stream.pending == nil && parsed.ImageID == stream.lastFrameID {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false // late duplicate payload after the frame was committed
		}
		if stream.pending == nil || stream.pending.imageID != parsed.ImageID {
			stream.pending = newGIFFrameBuffer(parsed, now) // supports older streams without frame-open markers
			stream.pending.frameIndex = uint16(len(stream.frames) + 1)
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
		canvas := cloneRGBA(stream.canvas)
		frameCopy := *frame
		frameCopy.packets = nil
		update := gifProgressUpdate(stream, frame, packet, false, nil, 0)
		m.mu.Unlock()

		preview, metadata, decodedMCUs, previewErr := decodeGIFFramePreview(canvas, &frameCopy, rawPackets)
		if previewErr == nil && preview != nil {
			pngBytes, encodeErr := encodeGIFPreviewPNG(preview)
			if encodeErr == nil {
				m.mu.Lock()
				if current := m.gifStreams[key]; current == stream && current.pending != nil && current.pending.imageID == parsed.ImageID {
					current.canvas = cloneRGBA(preview)
					current.lastUpdate = time.Now()
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
		// A partial packet set often cannot yet form a complete MCU restart
		// interval. That is normal; still publish its exact packet counters.
		return update, true
	}

	// Frame-close marker: apply any newly complete MCU range (including the
	// final EOI tail), then commit one composed snapshot to the animation.
	if stream.pending == nil || stream.pending.imageID != parsed.ImageID {
		// Repeated END markers can arrive after the frame was committed. They
		// must not append a second identical animation frame. A new stream-start
		// control resets hasLastEnd before the next transfer is processed.
		if stream.hasLastEnd && stream.lastEndImageID == parsed.ImageID {
			stream.lastUpdate = now
			m.mu.Unlock()
			return Update{}, false
		}
		stream.pending = newGIFFrameBuffer(parsed, now) // legacy zero-data frame
		stream.pending.frameIndex = uint16(len(stream.frames) + 1)
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
		// Legacy frames without an open marker still have a close marker.
		frame.controlExpected = 1
		stream.packetsExpected += frame.expectedPackets + frame.controlExpected
	}
	frame.receivedPackets++ // one de-duplicated frame-close control packet
	stream.packetsReceived++
	rawPackets := orderedGIFPackets(frame.packets)
	canvas := cloneRGBA(stream.canvas)
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
		update := gifProgressUpdate(stream, frame, packet, true, nil, 0)
		update.Complete = true
		update.GIFFrameComplete = true
		update.GIFEnd = parsed.GIFEnd
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
		if stream.exportErr == "" {
			if len(stream.frames) >= maxGIFExportFrames {
				stream.exportErr = fmt.Sprintf("received GIF exceeds the %d-frame export limit", maxGIFExportFrames)
			} else if stream.frameBytes+len(pngBytes) > maxGIFExportPNGSize {
				stream.exportErr = fmt.Sprintf("received GIF exceeds the %d MiB export cache limit", maxGIFExportPNGSize/(1<<20))
			} else {
				stream.frames = append(stream.frames, append([]byte(nil), pngBytes...))
				delay := int(frameCopy.delayCS)
				if delay <= 0 {
					delay = 10
				}
				stream.delays = append(stream.delays, delay)
				stream.frameBytes += len(pngBytes)
			}
		}
		if parsed.GIFEnd {
			stream.complete = true
		}
	}
	update := gifProgressUpdate(stream, frame, packet, true, preview, decodedMCUs)
	update.PNGBase64 = base64.StdEncoding.EncodeToString(pngBytes)
	update.ImageWidth, update.ImageHeight = preview.Bounds().Dx(), preview.Bounds().Dy()
	if metadata.Width > 0 {
		update.ImageWidth, update.ImageHeight = metadata.Width, metadata.Height
	}
	update.Coverage = gifFrameCoverage(decodedMCUs, metadata.MCUCount)
	update.GIFFrameComplete = true
	update.Complete = true
	update.GIFEnd = parsed.GIFEnd
	m.mu.Unlock()
	return update, true
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
		return nil, ssdv.Packet{}, 0, fmt.Errorf("GIF delta frame arrived before a keyframe")
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

// ExportLatestGIF returns the most recently completed received animation.
func (m *Manager) ExportLatestGIF() ([]byte, string, int, error) {
	return m.exportGIF("")
}

// ExportGIF exports one exact RX GIF session. This avoids saving a different
// station's most recently completed animation when multiple streams are heard.
func (m *Manager) ExportGIF(sessionID string) ([]byte, string, int, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, "", 0, fmt.Errorf("GIF session ID is empty")
	}
	return m.exportGIF(sessionID)
}

// ReleaseGIFExport discards the composed PNG snapshots after the application
// has successfully written the completed animation to its album. The compact
// stream record remains so late duplicate packets can still be suppressed and
// the next GIFStart can establish a fresh session.
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
		stream.lastUpdate = time.Now()
		return true
	}
	return false
}

// RX stores composed PNG snapshots rather than raw SSDV packets, so the
// exported file reflects exactly the frames reconstructed by this receiver.
func (m *Manager) exportGIF(sessionID string) ([]byte, string, int, error) {
	if m == nil {
		return nil, "", 0, fmt.Errorf("RX manager is unavailable")
	}
	m.mu.Lock()
	var selected *gifStreamSession
	for _, candidate := range m.gifStreams {
		if !candidate.complete || len(candidate.frames) == 0 {
			continue
		}
		if sessionID != "" && candidate.sessionID != sessionID {
			continue
		}
		if selected == nil || candidate.lastUpdate.After(selected.lastUpdate) {
			selected = candidate
		}
	}
	if selected == nil {
		m.mu.Unlock()
		if sessionID != "" {
			return nil, "", 0, fmt.Errorf("completed received GIF session %q is unavailable", sessionID)
		}
		return nil, "", 0, fmt.Errorf("no completed received GIF is available")
	}
	if selected.exportErr != "" {
		err := selected.exportErr
		m.mu.Unlock()
		return nil, "", 0, fmt.Errorf("cannot export received GIF: %s", err)
	}
	frames := make([][]byte, len(selected.frames))
	for i := range selected.frames {
		frames[i] = append([]byte(nil), selected.frames[i]...)
	}
	delays := append([]int(nil), selected.delays...)
	callsign := selected.callsign
	m.mu.Unlock()

	animation := &gif.GIF{
		Image:     make([]*image.Paletted, len(frames)),
		Delay:     delays,
		Disposal:  make([]byte, len(frames)),
		LoopCount: 0,
	}
	for i, encoded := range frames {
		decoded, err := png.Decode(bytes.NewReader(encoded))
		if err != nil {
			return nil, "", 0, fmt.Errorf("decode received GIF frame %d: %w", i+1, err)
		}
		bounds := decoded.Bounds()
		paletted := image.NewPaletted(image.Rect(0, 0, bounds.Dx(), bounds.Dy()), palette.Plan9)
		draw.FloydSteinberg.Draw(paletted, paletted.Bounds(), decoded, bounds.Min)
		animation.Image[i] = paletted
		animation.Disposal[i] = gif.DisposalNone
	}
	var output bytes.Buffer
	if err := gif.EncodeAll(&output, animation); err != nil {
		return nil, "", 0, fmt.Errorf("encode received GIF: %w", err)
	}
	return output.Bytes(), callsign, len(frames), nil
}
