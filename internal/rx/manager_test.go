package rx

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

func TestManagerBuildsPCSIImageAndDeduplicatesPackets(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 7), B: uint8((x + y) * 3), A: 255})
		}
	}
	enc, err := pcsi.NewEncoder(img, pcsi.Config{Callsign: "N0CALL", ImageID: 23, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan Update, 8)
	manager := NewManager(func(update Update) { updates <- update })
	defer manager.Close()
	// Deliver packets in reverse order and repeat one packet. PCSI sampling
	// positions are packet-indexed, so reconstruction must not depend on RX
	// arrival order and duplicates must not inflate the packet count.
	for i := enc.PacketCount() - 1; i >= 0; i-- {
		packet, err := enc.EncodePacket(i)
		if err != nil {
			t.Fatal(err)
		}
		item := transport.ReceivedPacket{Profile: "pcsi-ccsds-1200", Codec: "PCSI-MMT", Callsign: "N0CALL", ImageID: 23, PacketID: uint16(i), Raw: packet}
		if !manager.Submit(item) {
			t.Fatal("manager rejected packet")
		}
		if i == enc.PacketCount()-1 && !manager.Submit(item) {
			t.Fatal("manager rejected duplicate packet submit")
		}
	}
	deadline := time.After(4 * time.Second)
	for {
		select {
		case update := <-updates:
			if update.Error != "" {
				t.Fatal(update.Error)
			}
			if update.Complete {
				if update.PNGBase64 == "" || update.PacketsReceived != enc.PacketCount() {
					t.Fatalf("incomplete final update: %+v", update)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for reconstructed PCSI image")
		}
	}
}

func TestManagerSubmitRejectsAfterClose(t *testing.T) {
	m := NewManager(nil)
	m.Close()
	if m.Submit(transport.ReceivedPacket{}) {
		t.Fatal("submit succeeded after manager close")
	}
}

func TestSSDVManagerReconstructsJPEGFromOutOfOrderPackets(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	rawPackets, err := ssdv.EncodeImage(source, ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 7, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan Update, 128)
	manager := NewManager(func(update Update) { updates <- update })
	defer manager.Close()
	submit := func(index int) {
		t.Helper()
		parsed, parseErr := ssdv.ParsePacket(rawPackets[index])
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		wirePacket := append([]byte(nil), rawPackets[index]...)
		if index == 1 {
			// Exercise the RX manager's own validation boundary. The transport
			// normally corrects FEC first, but the manager must never archive a
			// corrupted codeword if another caller submits it directly.
			wirePacket[37] ^= 0x5a
		}
		item := transport.ReceivedPacket{
			Profile: "ssdv-ccsds-1200", Codec: "SSDV", Callsign: parsed.Callsign, ImageID: parsed.ImageID,
			PacketID: parsed.PacketID, ImageWidth: parsed.Width, ImageHeight: parsed.Height,
			Quality: parsed.Quality, EOI: parsed.EOI, Subsampling: parsed.Subsampling,
			MCUOffset: parsed.MCUOffset, MCUIndex: parsed.MCUIndex, MCUCount: parsed.MCUCount,
			FEC: parsed.FEC, Raw: wirePacket,
		}
		if !manager.Submit(item) {
			t.Fatalf("submit packet %d failed", index)
		}
	}
	// EOI arrives first, then packets arrive out of order with one deliberate
	// gap. Duplicate packet 0 must not inflate the received count.
	submit(len(rawPackets) - 1)
	for i := len(rawPackets) - 2; i >= 1; i-- {
		if i == 2 {
			continue
		}
		submit(i)
	}
	submit(0)
	submit(0)
	deadline := time.After(3 * time.Second)
	seenGap := false
	for !seenGap {
		select {
		case update := <-updates:
			if update.Error != "" {
				t.Fatal(update.Error)
			}
			if update.PacketsExpected == len(rawPackets) && update.PacketsReceived == len(rawPackets)-1 && !update.Complete {
				seenGap = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for SSDV missing-packet progress")
		}
	}
	submit(2)
	for {
		select {
		case update := <-updates:
			if update.Error != "" {
				t.Fatal(update.Error)
			}
			if update.Complete {
				if update.PNGBase64 == "" || update.PacketsReceived != len(rawPackets) || update.PacketsExpected != len(rawPackets) {
					t.Fatalf("SSDV image was not reconstructed: %+v", update)
				}
				pngData, err := base64.StdEncoding.DecodeString(update.PNGBase64)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := png.Decode(bytes.NewReader(pngData))
				if err != nil {
					t.Fatalf("decode received SSDV PNG: %v", err)
				}
				if decoded.Bounds().Dx() != 320 || decoded.Bounds().Dy() != 240 {
					t.Fatalf("unexpected image dimensions: %v", decoded.Bounds())
				}
				if update.PacketDataBase64 == "" {
					t.Fatal("expected corrected packet stream for archival")
				}
				archived, err := base64.StdEncoding.DecodeString(update.PacketDataBase64)
				if err != nil {
					t.Fatal(err)
				}
				want := bytes.Join(rawPackets, nil)
				if !bytes.Equal(archived, want) {
					t.Fatal("SSDV packet archive did not contain RS-corrected packet bytes in packet order")
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for reconstructed SSDV image")
		}
	}
}

func TestManagerKeepsIngestingPCSIWhileReconstructionRuns(t *testing.T) {
	const width, height = 160, 160
	src := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 3), B: uint8(x + y), A: 255})
		}
	}
	enc, err := pcsi.NewEncoder(src, pcsi.Config{Callsign: "N0CALL", ImageID: 31, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan Update, 128)
	manager := NewManager(func(update Update) { updates <- update })
	defer manager.Close()
	for i := 0; i < enc.PacketCount(); i++ {
		packet, err := enc.EncodePacket(i)
		if err != nil {
			t.Fatal(err)
		}
		item := transport.ReceivedPacket{Profile: "pcsi-ccsds-2400", Codec: "PCSI-MMT", Callsign: "N0CALL", ImageID: 31, PacketID: uint16(i), Raw: packet}
		queueDeadline := time.Now().Add(5 * time.Second)
		for !manager.Submit(item) {
			if time.Now().After(queueDeadline) {
				t.Fatal("RX packet queue remained full while reconstruction was running")
			}
			time.Sleep(time.Millisecond)
		}
	}
	// This is an eventual-completion assertion, not a wall-clock latency SLA.
	// PCSI reconstruction is intentionally CPU intensive, and the Go race
	// detector can multiply its runtime when other package tests execute in
	// parallel. Keep the independent 5-second queue-ingest assertion above;
	// allow enough time here to avoid a false timeout under -race/CI load.
	deadline := time.After(45 * time.Second)
	for {
		select {
		case update := <-updates:
			if update.Error != "" {
				t.Fatal(update.Error)
			}
			if update.Complete {
				if update.PNGBase64 == "" || update.PacketsReceived != enc.PacketCount() || update.PacketsExpected != enc.PacketCount() {
					t.Fatalf("unexpected completed image update: %+v", update)
				}
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for final PCSI image, packets=%d", enc.PacketCount())
		}
	}
}

func TestManagerSubmitDoesNotBlockWhenQueueSaturates(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var callbacks atomic.Int64
	manager := NewManager(func(Update) {
		if callbacks.Add(1) == 1 {
			close(entered)
			<-release
		}
	})
	defer manager.Close()
	invalid := transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV", Raw: []byte{0}}
	if !manager.Submit(invalid) {
		t.Fatal("initial packet was rejected")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not enter callback")
	}
	const submitted = queueCapacity + 32
	start := time.Now()
	accepted := 0
	for i := 0; i < submitted; i++ {
		if manager.Submit(invalid) {
			accepted++
		}
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("Submit blocked for %s while RX worker was saturated", elapsed)
	}
	if accepted != queueCapacity {
		t.Fatalf("accepted queued packets=%d want=%d", accepted, queueCapacity)
	}
	if got := manager.DroppedPackets(); got != 32 {
		t.Fatalf("dropped packet count=%d want=32", got)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for callbacks.Load() < queueCapacity+1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := callbacks.Load(); got != queueCapacity+1 {
		t.Fatalf("worker processed %d packets; want %d accepted packets", got, queueCapacity+1)
	}
}

func TestSSDVIncompletePacketSnapshotIsAvailableBeforeEOI(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x*3 + y), G: uint8(x ^ (y * 7)), B: uint8(x*y + 17), A: 255})
		}
	}
	rawPackets, err := ssdv.EncodeImage(source, ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 41, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	const submitted = 48
	if len(rawPackets) <= submitted {
		t.Fatalf("fixture needs more than %d packets, got %d", submitted, len(rawPackets))
	}
	updates := make(chan Update, 64)
	manager := NewManager(func(update Update) { updates <- update })
	defer manager.Close()
	for i := 0; i < submitted; i++ {
		if !manager.Submit(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV", Raw: rawPackets[i]}) {
			t.Fatalf("submit packet %d failed", i)
		}
	}
	deadline := time.After(8 * time.Second)
	sawPacketArchive, sawFixedCanvasPreview := false, false
	for !sawPacketArchive || !sawFixedCanvasPreview {
		select {
		case update := <-updates:
			if update.Error != "" {
				t.Fatal(update.Error)
			}
			if update.PacketDataBase64 != "" {
				if update.Complete {
					t.Fatal("partial packet snapshot was unexpectedly marked complete")
				}
				archived, err := base64.StdEncoding.DecodeString(update.PacketDataBase64)
				if err != nil {
					t.Fatal(err)
				}
				if len(archived)%ssdv.PacketSize != 0 || len(archived) < ssdvArchiveInterval*ssdv.PacketSize {
					t.Fatalf("partial packet snapshot has invalid length: %d", len(archived))
				}
				packetCount := len(archived) / ssdv.PacketSize
				for i := 0; i < packetCount; i++ {
					if !bytes.Equal(archived[i*ssdv.PacketSize:(i+1)*ssdv.PacketSize], rawPackets[i]) {
						t.Fatalf("partial packet snapshot packet %d is not in sequence order", i)
					}
				}
				sawPacketArchive = true
			}
			if update.PNGBase64 != "" && !update.Complete {
				previewData, err := base64.StdEncoding.DecodeString(update.PNGBase64)
				if err != nil {
					t.Fatal(err)
				}
				preview, err := png.Decode(bytes.NewReader(previewData))
				if err != nil {
					t.Fatal(err)
				}
				if preview.Bounds().Dx() != 640 || preview.Bounds().Dy() != 480 {
					t.Fatalf("SSDV preview canvas must stay at full image dimensions, got %v", preview.Bounds())
				}
				sawFixedCanvasPreview = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for incomplete SSDV archive and fixed-size preview (archive=%v preview=%v)", sawPacketArchive, sawFixedCanvasPreview)
		}
	}
}

func TestSSDVImageIDReuseStartsFreshRXSession(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 30, A: 255})
		}
	}
	packets, err := ssdv.EncodeImage(source, ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 55, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{ctx: context.Background(), reconQueue: make(chan *imageSession, 1), ssdv: make(map[string]*ssdvSession), sessions: make(map[string]*imageSession)}
	var firstSession string
	for i := 0; i < 12; i++ {
		u, ok := m.process(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV", Raw: packets[i]})
		if !ok {
			t.Fatalf("packet %d produced no RX update", i)
		}
		if i == 0 {
			if !u.Started || u.SessionID == "" {
				t.Fatalf("first packet did not start a session: %+v", u)
			}
			firstSession = u.SessionID
		}
	}
	secondSource := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			secondSource.SetRGBA(x, y, color.RGBA{R: 20, G: uint8(x), B: uint8(y), A: 255})
		}
	}
	secondPackets, err := ssdv.EncodeImage(secondSource, ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 55, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	newStart, ok := m.process(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV", Raw: secondPackets[0]})
	if !ok || !newStart.Started || newStart.SessionID == "" || newStart.SessionID == firstSession || newStart.PacketsReceived != 1 {
		t.Fatalf("reused SSDV image ID with new packet-0 content did not create a clean session: %+v", newStart)
	}
}

func TestPCSIImageIDReuseStartsFreshRXSession(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 160, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 160; x++ {
			source.SetRGBA(x, y, color.RGBA{R: 220, G: 20, B: 10, A: 255})
		}
	}
	enc, err := pcsi.NewEncoder(source, pcsi.Config{Callsign: "N0CALL", ImageID: 56, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	if enc.PacketCount() < 12 {
		t.Fatalf("test image needs at least 12 packets, got %d", enc.PacketCount())
	}
	m := &Manager{ctx: context.Background(), reconQueue: make(chan *imageSession, 1), ssdv: make(map[string]*ssdvSession), sessions: make(map[string]*imageSession)}
	var firstSession string
	for i := 0; i < 12; i++ {
		packet, encodeErr := enc.EncodePacket(i)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		u, ok := m.process(transport.ReceivedPacket{Profile: "pcsi-ccsds-1200", Codec: "PCSI-MMT", Raw: packet})
		if i == 0 {
			if !ok || !u.Started || u.SessionID == "" {
				t.Fatalf("first PCSI packet did not start a session: %+v", u)
			}
			firstSession = u.SessionID
		}
	}
	secondSource := image.NewRGBA(image.Rect(0, 0, 160, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 160; x++ {
			secondSource.SetRGBA(x, y, color.RGBA{R: 10, G: 30, B: 220, A: 255})
		}
	}
	secondEnc, err := pcsi.NewEncoder(secondSource, pcsi.Config{Callsign: "N0CALL", ImageID: 56, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	packet0, err := secondEnc.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}
	newStart, ok := m.process(transport.ReceivedPacket{Profile: "pcsi-ccsds-1200", Codec: "PCSI-MMT", Raw: packet0})
	if !ok || !newStart.Started || newStart.SessionID == "" || newStart.SessionID == firstSession || newStart.PacketsReceived != 1 {
		t.Fatalf("reused PCSI image ID with new packet-0 content did not create a clean session: %+v", newStart)
	}
}

func TestPCSIProgressivePreviewDoesNotTurnRedImageGreen(t *testing.T) {
	const width, height = 320, 240
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			source.SetRGBA(x, y, color.RGBA{R: 220, G: 30, B: 10, A: 255})
		}
	}
	enc, err := pcsi.NewEncoder(source, pcsi.Config{Callsign: "N0CALL", ImageID: 57, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan Update, 16)
	m := NewManager(func(u Update) { updates <- u })
	defer m.Close()
	for i := 0; i < pcsiPreviewInterval; i++ {
		packet, encodeErr := enc.EncodePacket(i)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if !m.Submit(transport.ReceivedPacket{Profile: "pcsi-ccsds-2400", Codec: "PCSI-MMT", Raw: packet}) {
			t.Fatalf("submit PCSI packet %d failed", i)
		}
	}
	deadline := time.After(15 * time.Second)
	for {
		select {
		case u := <-updates:
			if u.Error != "" {
				t.Fatal(u.Error)
			}
			if u.PNGBase64 == "" {
				continue
			}
			data, decodeErr := base64.StdEncoding.DecodeString(u.PNGBase64)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			preview, decodeErr := png.Decode(bytes.NewReader(data))
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			r, g, b, _ := preview.At(width/2, height/2).RGBA()
			if r <= g*2 || r <= b*2 {
				t.Fatalf("early PCSI preview is still strongly green/blue: rgb=(%d,%d,%d), coverage %.3f", r>>8, g>>8, b>>8, u.Coverage)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for early PCSI preview")
		}
	}
}

func TestSSDVOutOfOrderPacketZeroKeepsExistingSession(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 70, A: 255})
		}
	}
	packets, err := ssdv.EncodeImage(source, ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 58, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{ctx: context.Background(), reconQueue: make(chan *imageSession, 1), ssdv: make(map[string]*ssdvSession), sessions: make(map[string]*imageSession)}
	for i := 12; i >= 1; i-- {
		if _, ok := m.process(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV", Raw: packets[i]}); !ok {
			t.Fatalf("out-of-order packet %d was not ingested", i)
		}
	}
	lateZero, ok := m.process(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV", Raw: packets[0]})
	if !ok || lateZero.Started || lateZero.PacketsReceived != 13 {
		t.Fatalf("late packet 0 incorrectly started a new image session: %+v", lateZero)
	}
}

func TestSSDVDamageRegionsMarkMissingPacketAreas(t *testing.T) {
	canvas := image.NewRGBA(image.Rect(0, 0, 48, 48))
	drawSSDVDamageRegions(canvas, map[uint16][256]byte{0: {}, 2: {}}, 2, true, 3)
	if got := canvas.RGBAAt(5, 20); got.R == 0 && got.G == 0 && got.B == 0 {
		t.Fatal("missing SSDV packet did not create a visible damaged region")
	}
	if got := canvas.RGBAAt(5, 5); got.R != 0 || got.G != 0 || got.B != 0 {
		t.Fatalf("valid packet region was incorrectly marked damaged: %+v", got)
	}
}

func TestSSDVPreviewAttemptIsMadeForEveryValidPacket(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	packets, err := ssdv.EncodeImage(source, ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 71, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(nil)
	defer manager.Close()
	firstPreviewAt := 0
	for i, raw := range packets {
		parsed, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		update, ok := manager.process(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV", Callsign: parsed.Callsign, ImageID: parsed.ImageID, PacketID: parsed.PacketID, Raw: raw})
		if !ok {
			t.Fatalf("valid packet %d did not produce an RX update", i)
		}
		if update.PacketsReceived != i+1 {
			t.Fatalf("packet %d update reported count %d", i, update.PacketsReceived)
		}
		if update.PNGBase64 != "" && firstPreviewAt == 0 {
			firstPreviewAt = i + 1
		}
		if firstPreviewAt > 0 && update.PNGBase64 == "" {
			t.Fatalf("packet %d arrived after the live preview was available but did not refresh it", i+1)
		}
		if i >= 7 && firstPreviewAt == 0 {
			t.Fatal("SSDV preview was still absent after eight contiguous packets")
		}
		if update.Complete {
			break
		}
	}
	if firstPreviewAt == 0 {
		t.Fatal("no SSDV preview was produced for the synthetic image")
	}
	if firstPreviewAt >= 8 {
		t.Fatalf("first SSDV preview was delayed until packet %d", firstPreviewAt)
	}
}

func TestGIFPacketsAreRoutedIntoDedicatedGIFDecoder(t *testing.T) {
	raw, err := ssdv.NewGIFFrameControlPacket("N0CALL", 2, 320, 240, 4, 10, 0, true, false, true, false, true)
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	update, handled := m.process(transport.ReceivedPacket{
		Profile: "ssdv-ccsds-1200", Codec: "SSDV-GIF", GIFMode: true, Raw: raw,
	})
	if !handled || update.Codec != "SSDV-GIF" || !update.GIFMode || update.Error != "" {
		t.Fatalf("GIF control packet was not accepted by the dedicated decoder: handled=%v update=%+v", handled, update)
	}
	if len(m.gifStreams) != 1 || m.gifStreams["ssdv-ccsds-1200|N0CALL|false|0"] == nil {
		t.Fatalf("GIF control packet did not create an isolated animation session: %+v", m.gifStreams)
	}
}

func TestManagerDiagnosticsReportsQueueAndProcessingProgress(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()
	const count = 32
	for i := 0; i < count; i++ {
		if !manager.Submit(transport.ReceivedPacket{Codec: "UNKNOWN", PacketID: uint16(i)}) {
			t.Fatalf("manager rejected diagnostic test packet %d", i)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for manager.Diagnostics().ProcessedPackets < count && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	status := manager.Diagnostics()
	if status.QueueCapacity == 0 || status.QueueCapacity < queueCapacity {
		t.Fatalf("invalid queue capacity in diagnostics: %+v", status)
	}
	if status.SubmittedPackets != count || status.ProcessedPackets != count {
		t.Fatalf("manager counters did not advance: %+v", status)
	}
	if status.QueueDropped != 0 {
		t.Fatalf("unexpected queue drops: %+v", status)
	}
}
