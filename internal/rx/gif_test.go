package rx

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"math/rand"
	"testing"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/gifmode"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

func TestSGV2MotionPredictedFramesReconstructThroughRXManager(t *testing.T) {
	const width, height = 320, 240
	first := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			first.SetRGBA(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 239), B: uint8((x*3 + y*7) % 241), A: 255})
		}
	}
	second := image.NewRGBA(first.Bounds())
	draw.Draw(second, second.Bounds(), first, first.Bounds().Min, draw.Src)
	const sx, sy, objectW, objectH, dx, dy = 4, 4, 4, 3, 2, 1
	sprite := image.NewRGBA(image.Rect(0, 0, objectW*16, objectH*16))
	for y := 0; y < objectH*16; y++ {
		for x := 0; x < objectW*16; x++ {
			sprite.SetRGBA(x, y, color.RGBA{R: uint8((x*11 + y*3) % 255), G: uint8((x*5 + y*17) % 255), B: uint8((x*7 + y*13) % 255), A: 255})
		}
	}
	draw.Draw(first, image.Rect(sx*16, sy*16, (sx+objectW)*16, (sy+objectH)*16), sprite, sprite.Bounds().Min, draw.Src)
	draw.Draw(second, image.Rect((sx+dx)*16, (sy+dy)*16, (sx+dx+objectW)*16, (sy+dy+objectH)*16), sprite, sprite.Bounds().Min, draw.Src)

	packets, stats, err := gifmode.EncodeContextV2(context.Background(), []gifmode.Frame{{Image: first, DelayCS: 10}, {Image: second, DelayCS: 20}}, "N0CALL", 201, 4)
	if err != nil {
		t.Fatal(err)
	}
	if stats.MotionVectors == 0 || stats.PredictedMCUs == 0 {
		t.Fatalf("SGV2 encoder did not use motion prediction: %+v", stats)
	}
	profile := domain.SSDVCCSDS1200
	encoder, err := transport.NewEncoder(transport.Config{Profile: profile, AudioSampleRate: 19200})
	if err != nil {
		t.Fatal(err)
	}
	var waveform []float32
	streamedSamples, err := encoder.StreamPackets(context.Background(), packets, func(block []float32) error {
		waveform = append(waveform, block...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(waveform)) != streamedSamples {
		t.Fatalf("streamed waveform count=%d, reported=%d", len(waveform), streamedSamples)
	}
	decoder, err := transport.NewStreamDecoder(profile, 19200)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	var final Update
	for offset := 0; offset < len(waveform); offset += 1920 {
		end := min(offset+1920, len(waveform))
		received, feedErr := decoder.Feed(waveform[offset:end])
		if feedErr != nil {
			t.Fatalf("decode SGV2 waveform: %v", feedErr)
		}
		for _, packet := range received {
			update, ok := manager.process(packet)
			if !ok {
				continue
			}
			if update.Error != "" {
				t.Fatalf("RX rejected SGV2 packet: %s", update.Error)
			}
			if update.GIFEnd {
				final = update
			}
		}
	}
	if final.SessionID == "" || !final.GIFFrameComplete {
		t.Fatal("SGV2 stream did not complete")
	}
	exported, _, frameCount, err := manager.ExportGIF(final.SessionID)
	if err != nil {
		t.Fatalf("export SGV2 GIF: %v", err)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(exported))
	if err != nil {
		t.Fatal(err)
	}
	if frameCount != 2 || len(decoded.Image) != 2 {
		t.Fatalf("SGV2 export frame count=%d decoded=%d", frameCount, len(decoded.Image))
	}
	// The second frame must show the sprite at the translated destination.
	actualR, actualG, actualB, _ := decoded.Image[1].At((sx+dx)*16+8, (sy+dy)*16+8).RGBA()
	want := sprite.RGBAAt(8, 8)
	if absIntRX(int(actualR>>8)-int(want.R)) > 60 || absIntRX(int(actualG>>8)-int(want.G)) > 60 || absIntRX(int(actualB>>8)-int(want.B)) > 60 {
		t.Fatalf("motion-predicted target pixel mismatch: got %d/%d/%d want approximately %d/%d/%d", actualR>>8, actualG>>8, actualB>>8, want.R, want.G, want.B)
	}
}

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
	if partialRefreshes < 1 {
		t.Fatalf("16QAM GIF RX did not publish a progressive MCU preview")
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

func absIntRX(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestGIFRXContinuesAfterDroppedPacketAndRepairsWhenLateFECPacketArrives(t *testing.T) {
	const width, height = 640, 480
	const imageID = uint8(201)
	rng := rand.New(rand.NewSource(20261107))
	frameImage := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			frameImage.SetRGBA(x, y, color.RGBA{R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: uint8(rng.Intn(256)), A: 255})
		}
	}
	packets, _, err := gifmode.EncodeContextV2(context.Background(), []gifmode.Frame{{Image: frameImage, DelayCS: 10}}, "N0CALL", imageID, 4)
	if err != nil {
		t.Fatalf("encode high-entropy GIF keyframe: %v", err)
	}
	var dataPackets []int
	for index, raw := range packets {
		parsed, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if !parsed.GIFControl {
			dataPackets = append(dataPackets, index)
		}
	}
	if len(dataPackets) < 8 {
		t.Fatalf("fixture has only %d image data packets; want at least 8", len(dataPackets))
	}
	// Prefer dropping a packet inside a multi-packet MCU entropy segment. This
	// reproduces the real failure mode more accurately than losing a whole
	// packet that also happens to be a restart boundary.
	var lostPacketIndex int = -1
	var lostMCUIndex int
	var previousBoundary *ssdv.Packet
	for _, index := range dataPackets {
		parsed, parseErr := ssdv.ParsePacket(packets[index])
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if parsed.MCUOffset == 0xff || parsed.MCUIndex == 0xffff {
			continue
		}
		if previousBoundary != nil && int(parsed.PacketID)-int(previousBoundary.PacketID) >= 3 {
			wantID := previousBoundary.PacketID + 1
			for _, candidateIndex := range dataPackets {
				candidate, candidateErr := ssdv.ParsePacket(packets[candidateIndex])
				if candidateErr == nil && candidate.PacketID == wantID {
					lostPacketIndex = candidateIndex
					lostMCUIndex = int(previousBoundary.MCUIndex)
					break
				}
			}
			if lostPacketIndex >= 0 {
				break
			}
		}
		copyOfBoundary := parsed
		previousBoundary = &copyOfBoundary
	}
	if lostPacketIndex < 0 {
		lostPacketIndex = dataPackets[len(dataPackets)/2]
		parsed, parseErr := ssdv.ParsePacket(packets[lostPacketIndex])
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		lostMCUIndex = int(parsed.MCUIndex)
	}
	manager := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	var closeUpdate Update
	for index, raw := range packets {
		if index == lostPacketIndex {
			continue
		}
		parsed, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		update, ok := manager.processGIFPacket(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV-GIF", Callsign: "N0CALL", Raw: raw})
		if ok && update.GIFEnd {
			closeUpdate = update
		}
		_ = parsed
	}
	manager.mu.Lock()
	var stream *gifStreamSession
	for _, candidate := range manager.gifStreams {
		stream = candidate
	}
	if stream == nil {
		manager.mu.Unlock()
		t.Fatal("GIF RX stream was not created")
	}
	partial := stream.recentFrames[imageID]
	if partial == nil || !partial.committed {
		manager.mu.Unlock()
		t.Fatal("closed keyframe was not retained for late packet recovery")
	}
	partialMCUs := partial.decodedMCUs
	partialPreview := len(partial.previewPNG) > 0
	partialIncomplete := stream.incomplete && stream.awaitingKeyframe
	laterMCUsDecoded := false
	for mcu := lostMCUIndex + 1; mcu < len(partial.coveredMCUs); mcu++ {
		if partial.coveredMCUs[mcu] {
			laterMCUsDecoded = true
			break
		}
	}
	manager.mu.Unlock()
	if partialMCUs == 0 || partialMCUs >= width/16*(height/16) {
		t.Fatalf("expected partial MCU decode after one lost packet, got %d", partialMCUs)
	}
	if !partialPreview || !partialIncomplete {
		t.Fatalf("partial keyframe state invalid: preview=%v incomplete=%v close=%+v", partialPreview, partialIncomplete, closeUpdate)
	}
	if !laterMCUsDecoded {
		t.Fatalf("decoder stopped after lost packet near MCU %d; no later MCU region was decoded", lostMCUIndex)
	}

	// Deliver the previously missing data packet after the frame-close marker,
	// as can happen when the packet-level erasure decoder repairs it late.
	lateRaw := packets[lostPacketIndex]
	lateUpdate, ok := manager.processGIFPacket(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV-GIF", Callsign: "N0CALL", Raw: lateRaw})
	if !ok {
		t.Fatal("late recovered packet was discarded instead of repairing the closed frame")
	}
	if !lateUpdate.GIFPartialArchive || lateUpdate.PNGBase64 == "" {
		t.Fatalf("late repair did not refresh preview/archive: %+v", lateUpdate)
	}
	manager.mu.Lock()
	full := stream.recentFrames[imageID]
	fullMCUs := 0
	if full != nil {
		fullMCUs = full.decodedMCUs
	}
	incomplete := stream.incomplete
	awaiting := stream.awaitingKeyframe
	manager.mu.Unlock()
	if fullMCUs != width/16*(height/16) || incomplete || awaiting {
		t.Fatalf("late packet did not restore complete keyframe: MCUs=%d incomplete=%v awaiting=%v", fullMCUs, incomplete, awaiting)
	}
	data, _, count, complete, exportErr := manager.ExportGIFSnapshot(lateUpdate.SessionID)
	if exportErr != nil {
		t.Fatalf("export repaired GIF: %v", exportErr)
	}
	decoded, decodeErr := gif.DecodeAll(bytes.NewReader(data))
	if decodeErr != nil || count != 1 || len(decoded.Image) != 1 || !complete {
		t.Fatalf("repaired GIF archive invalid: count=%d decoded=%d complete=%v err=%v", count, len(decoded.Image), complete, decodeErr)
	}
}

func TestGIFManagerDrainsHighEntropyBurstWithoutDroppingPackets(t *testing.T) {
	const width, height = 640, 480
	rng := rand.New(rand.NewSource(20261108))
	frames := make([]gifmode.Frame, 2)
	for frameIndex := range frames {
		frame := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				frame.SetRGBA(x, y, color.RGBA{R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: uint8(rng.Intn(256)), A: 255})
			}
		}
		frames[frameIndex] = gifmode.Frame{Image: frame, DelayCS: 10}
	}
	packets, _, err := gifmode.EncodeContextV2(context.Background(), frames, "N0CALL", 211, 4)
	if err != nil {
		t.Fatalf("encode high-entropy queue stress GIF: %v", err)
	}
	if len(packets) <= 512 {
		t.Fatalf("stress fixture has only %d packets; want to exceed the former 512-packet RX queue", len(packets))
	}
	updates := make(chan Update, len(packets)+32)
	manager := NewManager(func(update Update) { updates <- update })
	defer manager.Close()
	for index, raw := range packets {
		if !manager.Submit(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV-GIF", Callsign: "N0CALL", Raw: raw}) {
			t.Fatalf("RX manager rejected packet %d/%d; dropped=%d", index+1, len(packets), manager.DroppedPackets())
		}
	}
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case update := <-updates:
			if update.Error != "" {
				t.Fatalf("RX manager reported GIF decode error: %s", update.Error)
			}
			if update.GIFEnd {
				if got := manager.DroppedPackets(); got != 0 {
					t.Fatalf("RX manager dropped %d packets during high-entropy GIF burst", got)
				}
				data, _, count, complete, exportErr := manager.ExportGIFSnapshot(update.SessionID)
				if exportErr != nil {
					t.Fatalf("export high-entropy RX GIF: %v", exportErr)
				}
				decoded, decodeErr := gif.DecodeAll(bytes.NewReader(data))
				if decodeErr != nil || count != 2 || len(decoded.Image) != 2 || !complete {
					t.Fatalf("high-entropy GIF archive incomplete: count=%d decoded=%d complete=%v err=%v", count, len(decoded.Image), complete, decodeErr)
				}
				return
			}
		case <-deadline.C:
			t.Fatalf("timed out draining %d-packet GIF burst; dropped=%d", len(packets), manager.DroppedPackets())
		}
	}
}

func TestGIFRXResynchronizesAfterIncompleteKeyframeAndContinuesDeltaFrames(t *testing.T) {
	const width, height = 320, 240
	const baseImageID = uint8(70)
	frames := make([]gifmode.Frame, 13)
	for index := range frames {
		frame := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				frame.SetRGBA(x, y, color.RGBA{R: 24, G: 48, B: 72, A: 255})
			}
		}
		// Move a textured 4x4-MCU object by one MCU per frame. This keeps the
		// frames sparse while providing independently decodable delta regions.
		object := image.NewRGBA(image.Rect(0, 0, 64, 64))
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				object.SetRGBA(x, y, color.RGBA{
					R: uint8((x*7 + y*3 + 11) % 255),
					G: uint8((x*5 + y*13 + 29) % 255),
					B: uint8((x*17 + y*2 + 47) % 255), A: 255,
				})
			}
		}
		x := 32 + (index%8)*16
		draw.Draw(frame, image.Rect(x, 64, x+64, 128), object, object.Bounds().Min, draw.Src)
		frames[index] = gifmode.Frame{Image: frame, DelayCS: uint16(8 + index)}
	}
	packets, stats, err := gifmode.EncodeContextV2(context.Background(), frames, "N0CALL", baseImageID, 4)
	if err != nil {
		t.Fatalf("encode recovery GIF: %v", err)
	}
	if stats.Keyframes < 2 {
		t.Fatalf("test stream needs a periodic recovery keyframe, stats=%+v", stats)
	}

	manager := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	var firstFrameAbandoned, awaitingKeyframe bool
	deltaPreviews := 0
	var final Update
	for _, raw := range packets {
		parsed, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		// Simulate a completely missed initial keyframe payload while preserving
		// its open/close controls. Subsequent delta frames and the periodic
		// recovery keyframe remain on air.
		if parsed.ImageID == baseImageID && !parsed.GIFControl {
			continue
		}
		update, ok := manager.processGIFPacket(transport.ReceivedPacket{
			Profile: "ssdv-ccsds-1200", Codec: "SSDV-GIF", Callsign: "N0CALL", Raw: raw,
		})
		if !ok {
			continue
		}
		if parsed.ImageID == baseImageID && update.GIFFrameAbandoned {
			firstFrameAbandoned = true
			awaitingKeyframe = update.GIFAwaitingKeyframe
		}
		if parsed.ImageID != baseImageID && parsed.ImageID != baseImageID+12 && !parsed.GIFControl && update.PNGBase64 != "" {
			deltaPreviews++
		}
		if update.GIFEnd {
			final = update
		}
	}
	if !firstFrameAbandoned || !awaitingKeyframe {
		t.Fatalf("missing first keyframe did not enter recoverable state: abandoned=%v awaiting=%v", firstFrameAbandoned, awaitingKeyframe)
	}
	if deltaPreviews == 0 {
		t.Fatal("subsequent delta frames were not decoded over the recovery placeholder")
	}
	if final.SessionID == "" || !final.GIFFrameComplete || final.ImageID != baseImageID+12 {
		t.Fatalf("periodic keyframe did not restore decoding: %+v", final)
	}
	if !final.GIFStreamIncomplete {
		t.Fatal("stream should be marked partial because its initial keyframe was lost")
	}
	data, _, frameCount, complete, exportErr := manager.ExportGIFSnapshot(final.SessionID)
	if exportErr != nil {
		t.Fatalf("export recovered partial GIF: %v", exportErr)
	}
	if complete || frameCount == 0 {
		t.Fatalf("recovered stream should export a non-empty partial animation: complete=%v frames=%d", complete, frameCount)
	}
	decoded, decodeErr := gif.DecodeAll(bytes.NewReader(data))
	if decodeErr != nil || len(decoded.Image) != frameCount {
		t.Fatalf("recovered partial GIF is invalid: frames=%d decoded=%d err=%v", frameCount, len(decoded.Image), decodeErr)
	}
}

func TestGIFRXKeepsDecodingWhenOneKeyframeDataPacketIsLost(t *testing.T) {
	first := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < first.Bounds().Dy(); y++ {
		for x := 0; x < first.Bounds().Dx(); x++ {
			first.SetRGBA(x, y, color.RGBA{R: uint8((x * 3) % 255), G: uint8((y * 5) % 255), B: uint8((x + y) % 255), A: 255})
		}
	}
	second := cloneRGBA(first)
	draw.Draw(second, image.Rect(112, 96, 176, 160), image.NewUniform(color.RGBA{R: 250, G: 35, B: 20, A: 255}), image.Point{}, draw.Src)
	packets, _, err := gifmode.EncodeContextV2(context.Background(), []gifmode.Frame{{Image: first, DelayCS: 10}, {Image: second, DelayCS: 18}}, "N0CALL", 88, 4)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	lostOne := false
	deltaPreview := false
	var final Update
	for _, raw := range packets {
		parsed, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if parsed.ImageID == 88 && !parsed.GIFControl && !lostOne {
			lostOne = true
			continue
		}
		update, ok := manager.processGIFPacket(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV-GIF", Raw: raw})
		if !ok {
			continue
		}
		if update.PNGBase64 != "" && parsed.ImageID == 89 && !parsed.GIFControl {
			deltaPreview = true
		}
		if update.GIFEnd {
			final = update
		}
	}
	if !lostOne {
		t.Fatal("test fixture did not contain keyframe data packets")
	}
	if !deltaPreview || final.SessionID == "" || !final.GIFFrameComplete || !final.GIFStreamIncomplete || !final.GIFAwaitingKeyframe {
		t.Fatalf("RX did not continue while preserving keyframe resync state after packet loss: deltaPreview=%v final=%+v", deltaPreview, final)
	}
}

func TestGIFRXKeepsDecodingDeltaFramesAfterMidStreamKeyframeLoss(t *testing.T) {
	const width, height = 320, 240
	const baseImageID = uint8(32)
	frames := make([]gifmode.Frame, 25)
	sprite := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			sprite.SetRGBA(x, y, color.RGBA{R: uint8((x*7 + y*3 + 11) % 255), G: uint8((x*5 + y*13 + 29) % 255), B: uint8((x*17 + y*2 + 47) % 255), A: 255})
		}
	}
	for index := range frames {
		frame := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				frame.SetRGBA(x, y, color.RGBA{R: 18, G: 32, B: 48, A: 255})
			}
		}
		x := 16 + (index%12)*16
		draw.Draw(frame, image.Rect(x, 64, x+64, 128), sprite, sprite.Bounds().Min, draw.Src)
		// A small changing tile creates actual residual data even when the main
		// sprite motion is predicted perfectly by SGV2.
		tile := image.NewRGBA(image.Rect(0, 0, 32, 32))
		for ty := 0; ty < 32; ty++ {
			for tx := 0; tx < 32; tx++ {
				tile.SetRGBA(tx, ty, color.RGBA{R: uint8(index*23 + tx*5), G: uint8(index*11 + ty*7), B: uint8(index*17 + tx*ty), A: 255})
			}
		}
		draw.Draw(frame, image.Rect(256, 176, 288, 208), tile, tile.Bounds().Min, draw.Src)
		frames[index] = gifmode.Frame{Image: frame, DelayCS: 8}
	}
	packets, stats, err := gifmode.EncodeContextV2(context.Background(), frames, "N0CALL", baseImageID, 4)
	if err != nil {
		t.Fatalf("encode 25-frame recovery GIF: %v", err)
	}
	if stats.Keyframes < 3 {
		t.Fatalf("fixture needs keyframes at frames 0, 12 and 24; stats=%+v", stats)
	}
	// Lose one ordinary delta packet and one packet from the following
	// periodic keyframe. RX must keep advancing through both damaged regions.
	lostPacketIDs := make(map[uint8]uint16)
	for _, lostFrame := range []uint8{baseImageID + 5, baseImageID + 12} {
		var candidates []uint16
		for _, raw := range packets {
			parsed, parseErr := ssdv.ParsePacket(raw)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if parsed.ImageID == lostFrame && !parsed.GIFControl {
				candidates = append(candidates, parsed.PacketID)
			}
		}
		if len(candidates) == 0 {
			t.Fatalf("frame %d has no data packets; cannot create a realistic loss", lostFrame)
		}
		lostPacketIDs[lostFrame] = candidates[len(candidates)/2]
	}
	manager := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	deltaAfterDeltaLoss := 0
	deltaAfterKeyframeLoss := 0
	var final Update
	for _, raw := range packets {
		parsed, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if lostPacketID, losingFrame := lostPacketIDs[parsed.ImageID]; losingFrame && parsed.PacketID == lostPacketID && !parsed.GIFControl {
			continue
		}
		update, ok := manager.processGIFPacket(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV-GIF", Callsign: "N0CALL", Raw: raw})
		if !ok {
			continue
		}
		if parsed.GIFFrameEnd && update.GIFFrameComplete {
			if parsed.ImageID > baseImageID+5 && parsed.ImageID < baseImageID+12 {
				if update.GIFAwaitingKeyframe {
					t.Fatalf("delta frame %d incorrectly entered keyframe wait after only a delta packet loss", parsed.ImageID)
				}
				deltaAfterDeltaLoss++
			}
			if parsed.ImageID > baseImageID+12 && parsed.ImageID < baseImageID+24 {
				if !update.GIFAwaitingKeyframe {
					t.Fatalf("delta frame %d cleared keyframe wait before a complete recovery keyframe", parsed.ImageID)
				}
				deltaAfterKeyframeLoss++
			}
		}
		if update.GIFEnd {
			final = update
		}
	}
	if deltaAfterDeltaLoss < 6 {
		t.Fatalf("RX stopped after a lost delta packet: completed %d later delta frames, want at least 6", deltaAfterDeltaLoss)
	}
	if deltaAfterKeyframeLoss < 11 {
		t.Fatalf("RX stopped after a lost periodic keyframe packet: completed %d later delta frames, want at least 11", deltaAfterKeyframeLoss)
	}
	if final.ImageID != baseImageID+24 || !final.GIFFrameComplete || final.GIFAwaitingKeyframe {
		t.Fatalf("next periodic keyframe did not restore synchronization: %+v", final)
	}
	if !final.GIFStreamIncomplete {
		t.Fatal("album must remain partial because frames 5 and 12 lost packets")
	}
}

func TestGIFRXResynchronizesAfterIncompletePeriodicKeyframeWithExistingCanvas(t *testing.T) {
	const width, height = 320, 240
	const firstImageID = uint8(120)
	frames := make([]gifmode.Frame, 25)
	object := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			object.SetRGBA(x, y, color.RGBA{
				R: uint8((x*7 + y*3 + 11) % 255),
				G: uint8((x*5 + y*13 + 29) % 255),
				B: uint8((x*17 + y*2 + 47) % 255), A: 255,
			})
		}
	}
	for index := range frames {
		frame := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.Draw(frame, frame.Bounds(), image.NewUniform(color.RGBA{R: 24, G: 48, B: 72, A: 255}), image.Point{}, draw.Src)
		x := 16 + (index%10)*16
		draw.Draw(frame, image.Rect(x, 64, x+64, 128), object, object.Bounds().Min, draw.Src)
		frames[index] = gifmode.Frame{Image: frame, DelayCS: 8}
	}
	packets, stats, err := gifmode.EncodeContextV2(context.Background(), frames, "N0CALL", firstImageID, 4)
	if err != nil {
		t.Fatalf("encode periodic-keyframe recovery fixture: %v", err)
	}
	if stats.Keyframes < 3 {
		t.Fatalf("fixture needs initial, failed periodic and recovery keyframes; stats=%+v", stats)
	}

	manager := &Manager{gifStreams: make(map[string]*gifStreamSession), sessions: make(map[string]*imageSession), ssdv: make(map[string]*ssdvSession)}
	var hadReferenceCanvas, failedPeriodicKeyframe, remainedAwaiting bool
	deltaAfterFailure := 0
	var final Update
	failedKeyframeID := uint8(firstImageID + 12)
	recoveryKeyframeID := uint8(firstImageID + 24)
	for _, raw := range packets {
		parsed, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if parsed.ImageID == failedKeyframeID && !parsed.GIFControl {
			continue // Lose the complete payload of a periodic keyframe, not its controls.
		}
		update, ok := manager.processGIFPacket(transport.ReceivedPacket{
			Profile: "ssdv-ccsds-1200", Codec: "SSDV-GIF", Callsign: "N0CALL", Raw: raw,
		})
		if !ok {
			continue
		}
		if parsed.ImageID == firstImageID && update.GIFFrameComplete {
			hadReferenceCanvas = true
		}
		if parsed.ImageID == failedKeyframeID && update.GIFFrameAbandoned {
			failedPeriodicKeyframe = true
			remainedAwaiting = update.GIFAwaitingKeyframe
		}
		if parsed.ImageID > failedKeyframeID && parsed.ImageID < recoveryKeyframeID && !parsed.GIFControl && update.PNGBase64 != "" {
			deltaAfterFailure++
		}
		if parsed.ImageID == recoveryKeyframeID && update.GIFFrameComplete {
			final = update
		}
	}
	if !hadReferenceCanvas || !failedPeriodicKeyframe || !remainedAwaiting {
		t.Fatalf("periodic keyframe loss did not preserve resync state: initial=%v failed=%v awaiting=%v", hadReferenceCanvas, failedPeriodicKeyframe, remainedAwaiting)
	}
	if deltaAfterFailure == 0 {
		t.Fatal("delta frames after the missing periodic keyframe were not decoded")
	}
	if final.SessionID == "" || final.GIFAwaitingKeyframe || !final.GIFFrameComplete || final.ImageID != recoveryKeyframeID {
		t.Fatalf("later complete keyframe did not restore the reference canvas: %+v", final)
	}
}
