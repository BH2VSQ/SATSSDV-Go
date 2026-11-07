package rx

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/gifmode"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

func TestGIFFramesDecodeEndToEndThroughCCSDSAndRXManager(t *testing.T) {
	first := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(first, first.Bounds(), image.NewUniform(color.RGBA{R: 30, G: 70, B: 110, A: 255}), image.Point{}, draw.Src)
	second := image.NewRGBA(first.Bounds())
	draw.Draw(second, second.Bounds(), first, first.Bounds().Min, draw.Src)
	draw.Draw(second, image.Rect(96, 80, 112, 96), image.NewUniform(color.RGBA{R: 245, G: 25, B: 30, A: 255}), image.Point{}, draw.Src)
	packets, _, err := gifmode.Encode([]gifmode.Frame{{Image: first, DelayCS: 12}, {Image: second, DelayCS: 24}}, "N0CALL", 37, 4)
	if err != nil {
		t.Fatal(err)
	}
	profile := domain.SSDVCCSDS1200
	encoder, err := transport.NewEncoder(transport.Config{Profile: profile, AudioSampleRate: 19200})
	if err != nil {
		t.Fatal(err)
	}
	waveform, err := encoder.EncodePackets(packets)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := transport.NewStreamDecoder(profile, 19200)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	var updates []Update
	for offset := 0; offset < len(waveform); offset += 1920 {
		end := min(offset+1920, len(waveform))
		decoded, feedErr := decoder.Feed(waveform[offset:end])
		if feedErr != nil {
			t.Fatalf("decode CCSDS GIF waveform: %v", feedErr)
		}
		for _, packet := range decoded {
			update, ok := manager.process(packet)
			if !ok {
				continue
			}
			if update.Error != "" {
				t.Fatalf("RX rejected transmitted GIF: %s", update.Error)
			}
			if update.PNGBase64 != "" {
				updates = append(updates, update)
			}
		}
	}
	var completed []Update
	partialRefreshes := 0
	for _, update := range updates {
		if update.GIFFrameComplete {
			completed = append(completed, update)
		} else if update.PNGBase64 != "" {
			partialRefreshes++
		}
	}
	if len(completed) != 2 || completed[0].ImageID != 37 || completed[1].ImageID != 38 || !completed[1].GIFEnd {
		t.Fatalf("unexpected completed end-to-end GIF frames: %+v", completed)
	}
	if partialRefreshes < 2 {
		t.Fatalf("RX published only %d partial updates; want multiple progressive MCU refreshes", partialRefreshes)
	}
	if completed[1].PacketsReceived != completed[1].PacketsExpected {
		t.Fatalf("RX packet counters do not reconcile: %d/%d", completed[1].PacketsReceived, completed[1].PacketsExpected)
	}
	if _, callsign, count, exportErr := manager.ExportGIF(completed[1].SessionID); exportErr != nil {
		t.Fatalf("export end-to-end GIF: %v", exportErr)
	} else if callsign != "N0CALL" || count != 2 {
		t.Fatalf("unexpected exported GIF metadata: callsign=%s frames=%d", callsign, count)
	}
}

func TestGIFManagerAppliesSparseFrameUpdates(t *testing.T) {
	first := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(first, first.Bounds(), image.NewUniform(color.RGBA{R: 25, G: 55, B: 85, A: 255}), image.Point{}, draw.Src)
	second := image.NewRGBA(first.Bounds())
	draw.Draw(second, second.Bounds(), first, first.Bounds().Min, draw.Src)
	draw.Draw(second, image.Rect(64, 64, 80, 80), image.NewUniform(color.RGBA{R: 225, G: 20, B: 15, A: 255}), image.Point{}, draw.Src)
	packets, _, err := gifmode.Encode([]gifmode.Frame{{Image: first, DelayCS: 10}, {Image: second, DelayCS: 20}}, "N0CALL", 90, 4)
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	var firstFrame, secondFrame *image.RGBA
	var sessionID string
	for _, raw := range packets {
		parsed, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		update, ok := m.processGIFPacket(transport.ReceivedPacket{
			Profile: "ssdv-ccsds-1200", Codec: "SSDV-GIF", Callsign: "N0CALL",
			ImageID: parsed.ImageID, PacketID: parsed.PacketID, Raw: raw,
			GIFMode: true, GIFKeyframe: parsed.GIFKeyframe, GIFControl: parsed.GIFControl,
		})
		if !ok || update.PNGBase64 == "" {
			continue
		}
		if update.GIFStart || sessionID == "" {
			sessionID = update.SessionID
		}
		pngBytes, decodeErr := base64.StdEncoding.DecodeString(update.PNGBase64)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		img, decodeErr := png.Decode(bytes.NewReader(pngBytes))
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		rgba := image.NewRGBA(img.Bounds())
		draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
		if update.ImageID == 90 {
			firstFrame = rgba
		} else if update.ImageID == 91 {
			secondFrame = rgba
		}
	}
	if firstFrame == nil || secondFrame == nil {
		t.Fatalf("did not get both frames (first=%v second=%v)", firstFrame != nil, secondFrame != nil)
	}
	if exported, call, count, exportErr := m.ExportGIF(sessionID); exportErr != nil {
		t.Fatalf("ExportGIF: %v", exportErr)
	} else {
		decodedGIF, decodeErr := gif.DecodeAll(bytes.NewReader(exported))
		if decodeErr != nil {
			t.Fatalf("decode exported GIF: %v", decodeErr)
		}
		if call != "N0CALL" || count != 2 || len(decodedGIF.Image) != 2 {
			t.Fatalf("unexpected exported GIF metadata: callsign=%q count=%d frames=%d", call, count, len(decodedGIF.Image))
		}
		if len(decodedGIF.Delay) != 2 || decodedGIF.Delay[0] != 10 || decodedGIF.Delay[1] != 20 {
			t.Fatalf("exported GIF delays = %v, want [10 20]", decodedGIF.Delay)
		}
	}
	// A new transfer from the same station starts a fresh export sequence;
	// periodic keyframes within an existing stream must not reset it.
	secondTransfer, _, err := gifmode.Encode([]gifmode.Frame{{Image: second, DelayCS: 15}}, "N0CALL", 120, 4)
	if err != nil {
		t.Fatal(err)
	}
	var secondSessionID string
	for _, raw := range secondTransfer {
		parsed, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		update, ok := m.processGIFPacket(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Callsign: "N0CALL", Raw: raw})
		if ok && update.PNGBase64 != "" {
			secondSessionID = update.SessionID
		}
		_ = parsed
	}
	if secondSessionID == "" {
		t.Fatal("new GIF transfer did not produce a session ID")
	}
	if exported, _, count, exportErr := m.ExportGIF(secondSessionID); exportErr != nil {
		t.Fatalf("ExportGIF after a new transfer: %v", exportErr)
	} else if decodedGIF, decodeErr := gif.DecodeAll(bytes.NewReader(exported)); decodeErr != nil {
		t.Fatal(decodeErr)
	} else if count != 1 || len(decodedGIF.Image) != 1 || decodedGIF.Delay[0] != 15 {
		t.Fatalf("new transfer did not reset export frames: count=%d delays=%v", count, decodedGIF.Delay)
	}
	if _, _, _, exportErr := m.ExportGIF("not-a-real-session"); exportErr == nil {
		t.Fatal("ExportGIF should reject an unknown session ID")
	}
	oldR, oldG, oldB, _ := firstFrame.At(72, 72).RGBA()
	newR, newG, newB, _ := secondFrame.At(72, 72).RGBA()
	if oldR == newR && oldG == newG && oldB == newB {
		t.Fatalf("changed MCU was not applied: first=%04x/%04x/%04x second=%04x/%04x/%04x", oldR, oldG, oldB, newR, newG, newB)
	}
	outsideR1, outsideG1, outsideB1, _ := firstFrame.At(24, 24).RGBA()
	outsideR2, outsideG2, outsideB2, _ := secondFrame.At(24, 24).RGBA()
	if outsideR1 != outsideR2 || outsideG1 != outsideG2 || outsideB1 != outsideB2 {
		t.Fatal("unchanged MCU changed between keyframe and delta update")
	}
}

func TestGIFTemplateFrameSetRoundTripsThrough16QAMAndRXManager(t *testing.T) {
	paletteColors := color.Palette{color.RGBA{A: 0}, color.RGBA{R: 230, G: 30, B: 40, A: 255}}
	gifFrame1 := image.NewPaletted(image.Rect(0, 0, 32, 32), paletteColors)
	draw.Draw(gifFrame1, gifFrame1.Bounds(), image.NewUniform(paletteColors[1]), image.Point{}, draw.Src)
	gifFrame2 := image.NewPaletted(image.Rect(0, 0, 32, 32), paletteColors)
	draw.Draw(gifFrame2, image.Rect(0, 0, 16, 32), image.NewUniform(paletteColors[1]), image.Point{}, draw.Src)
	animation := &gif.GIF{
		Image: []*image.Paletted{gifFrame1, gifFrame2}, Delay: []int{10, 20},
		Disposal: []byte{gif.DisposalNone, gif.DisposalNone},
		Config:   image.Config{ColorModel: paletteColors, Width: 32, Height: 32}, LoopCount: 0,
	}
	var sourceGIF bytes.Buffer
	if err := gif.EncodeAll(&sourceGIF, animation); err != nil {
		t.Fatal(err)
	}
	base := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(base, base.Bounds(), image.NewUniform(color.RGBA{B: 90, A: 255}), image.Point{}, draw.Src)
	overlay := image.NewRGBA(base.Bounds())
	draw.Draw(overlay, image.Rect(4, 4, 20, 20), image.NewUniform(color.RGBA{G: 190, A: 255}), image.Point{}, draw.Src)
	frames, err := gifmode.DecodeTemplate(sourceGIF.Bytes(), "low", base, overlay, gifmode.Rect{X: 128, Y: 96, Width: 64, Height: 64})
	if err != nil {
		t.Fatal(err)
	}
	packets, _, err := gifmode.Encode(frames, "N0CALL", 51, 4)
	if err != nil {
		t.Fatal(err)
	}
	profile := domain.SSDV16QAM1600
	encoder, err := transport.NewEncoder(transport.Config{Profile: profile, AudioSampleRate: 48000})
	if err != nil {
		t.Fatal(err)
	}
	waveform, err := encoder.EncodePackets(packets)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.FilterTXWaveformInPlace(profile, waveform, 48000); err != nil {
		t.Fatal(err)
	}
	decoder, err := transport.NewStreamDecoder(profile, 48000)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	var updates []Update
	for offset := 0; offset < len(waveform); offset += 997 {
		end := min(offset+997, len(waveform))
		received, feedErr := decoder.Feed(waveform[offset:end])
		if feedErr != nil {
			t.Fatalf("decode 16QAM GIF waveform: %v", feedErr)
		}
		for _, packet := range received {
			update, ok := manager.process(packet)
			if !ok {
				continue
			}
			if update.Error != "" {
				t.Fatalf("RX rejected 16QAM GIF packet: %s", update.Error)
			}
			if update.PNGBase64 != "" {
				updates = append(updates, update)
			}
		}
	}
	var completed []Update
	partialRefreshes := 0
	for _, update := range updates {
		if update.GIFFrameComplete {
			completed = append(completed, update)
		} else if update.PNGBase64 != "" {
			partialRefreshes++
		}
	}
	if len(completed) != 2 || !completed[len(completed)-1].GIFEnd {
		t.Fatalf("16QAM GIF RX returned %d completed frames (%d partial refreshes)", len(completed), partialRefreshes)
	}
	if partialRefreshes < 2 {
		t.Fatalf("16QAM GIF RX published only %d partial updates; want multiple MCU refreshes", partialRefreshes)
	}
	output, callsign, frameCount, err := manager.ExportGIF(completed[len(completed)-1].SessionID)
	if err != nil {
		t.Fatalf("export 16QAM GIF: %v", err)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("decode exported 16QAM GIF: %v", err)
	}
	if callsign != "N0CALL" || frameCount != 2 || len(decoded.Image) != 2 {
		t.Fatalf("unexpected exported GIF: callsign=%q frames=%d decoded=%d", callsign, frameCount, len(decoded.Image))
	}
}

func TestRepeatedGIFBurstPassesStreamDecoderAndStartsNewRXSession(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(frame, frame.Bounds(), image.NewUniform(color.RGBA{R: 50, G: 90, B: 130, A: 255}), image.Point{}, draw.Src)
	packets, _, err := gifmode.Encode([]gifmode.Frame{{Image: frame, DelayCS: 10}}, "N0CALL", 73, 4)
	if err != nil {
		t.Fatal(err)
	}
	profile := domain.SSDVCCSDS1200
	encoder, err := transport.NewEncoder(transport.Config{Profile: profile, AudioSampleRate: 19200})
	if err != nil {
		t.Fatal(err)
	}
	waveform, err := encoder.EncodePackets(packets)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := transport.NewStreamDecoder(profile, 19200)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	decodeBurst := func() Update {
		var final Update
		for offset := 0; offset < len(waveform); offset += 1920 {
			end := min(offset+1920, len(waveform))
			received, feedErr := decoder.Feed(waveform[offset:end])
			if feedErr != nil {
				t.Fatalf("decode repeated GIF waveform: %v", feedErr)
			}
			for _, packet := range received {
				update, ok := manager.process(packet)
				if !ok {
					continue
				}
				if update.Error != "" {
					t.Fatalf("RX rejected repeated GIF packet: %s", update.Error)
				}
				if update.GIFEnd {
					final = update
				}
			}
		}
		if final.SessionID == "" {
			t.Fatal("GIF stream-end marker was not recovered")
		}
		return final
	}
	first := decodeBurst()
	second := decodeBurst() // identical RF payload and packet IDs, as in repeat TX
	if first.SessionID == second.SessionID {
		t.Fatalf("repeated GIF burst reused the first RX session ID %q", first.SessionID)
	}
	if _, _, count, exportErr := manager.ExportGIF(second.SessionID); exportErr != nil || count != 1 {
		t.Fatalf("repeated burst did not produce a fresh one-frame animation: count=%d err=%v", count, exportErr)
	}
}

func TestGIFManagerDeduplicatesRepeatedControlAndAcceptsRepeatedTX(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(frame, frame.Bounds(), image.NewUniform(color.RGBA{R: 40, G: 80, B: 120, A: 255}), image.Point{}, draw.Src)
	packets, _, err := gifmode.Encode([]gifmode.Frame{{Image: frame, DelayCS: 12}}, "N0CALL", 7, 4)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	var updates []Update
	feed := func() {
		for _, raw := range packets {
			parsed, parseErr := ssdv.ParsePacket(raw)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			update, ok := manager.processGIFPacket(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Callsign: "N0CALL", ImageID: parsed.ImageID, PacketID: parsed.PacketID, Raw: raw})
			if ok && update.PNGBase64 != "" {
				updates = append(updates, update)
			}
		}
	}
	feed()
	var completed []Update
	for _, update := range updates {
		if update.GIFFrameComplete {
			completed = append(completed, update)
		}
	}
	if len(completed) != 1 || !completed[0].GIFEnd {
		t.Fatalf("first burst produced %d completed frame updates", len(completed))
	}
	firstSession := completed[0].SessionID
	if _, _, count, exportErr := manager.ExportGIF(firstSession); exportErr != nil || count != 1 {
		t.Fatalf("first burst export failed: count=%d err=%v", count, exportErr)
	}
	updates = nil
	feed() // Same packet IDs and bytes, as produced by the existing repeat option.
	completed = completed[:0]
	for _, update := range updates {
		if update.GIFFrameComplete {
			completed = append(completed, update)
		}
	}
	if len(completed) != 1 || !completed[0].GIFEnd {
		t.Fatalf("repeated burst produced %d completed frame updates", len(completed))
	}
	if completed[0].SessionID == firstSession {
		t.Fatal("repeated burst reused the old GIF export session")
	}
	if _, _, count, exportErr := manager.ExportGIF(completed[0].SessionID); exportErr != nil || count != 1 {
		t.Fatalf("repeated burst export failed: count=%d err=%v", count, exportErr)
	}
}

func TestReleaseGIFExportDropsFrameSnapshotsButKeepsDuplicateSuppressionState(t *testing.T) {
	canvas := image.NewRGBA(image.Rect(0, 0, 320, 240))
	stream := &gifStreamSession{
		sessionID: "completed-session", complete: true,
		frames: [][]byte{{1, 2, 3}, {4, 5, 6}}, delays: []int{10, 20},
		frameBytes: 6, canvas: canvas,
		lastEndImageID: 42, hasLastEnd: true,
	}
	manager := &Manager{gifStreams: map[string]*gifStreamSession{"station": stream}}

	if manager.ReleaseGIFExport("still-active") {
		t.Fatal("ReleaseGIFExport released an unknown session")
	}
	if len(stream.frames) != 2 {
		t.Fatal("ReleaseGIFExport modified a different session")
	}
	if !manager.ReleaseGIFExport("completed-session") {
		t.Fatal("ReleaseGIFExport did not release the completed session")
	}
	if stream.frames != nil || stream.delays != nil || stream.frameBytes != 0 || stream.canvas != nil || stream.pending != nil {
		t.Fatalf("GIF frame buffers were retained: frames=%d delays=%d bytes=%d canvas=%v pending=%v", len(stream.frames), len(stream.delays), stream.frameBytes, stream.canvas != nil, stream.pending != nil)
	}
	if !stream.complete || !stream.hasLastEnd || stream.lastEndImageID != 42 || stream.sessionID != "completed-session" {
		t.Fatal("release removed metadata required for duplicate suppression")
	}
	if manager.ReleaseGIFExport("completed-session") {
		t.Fatal("second release unexpectedly reported retained frame data")
	}
}
