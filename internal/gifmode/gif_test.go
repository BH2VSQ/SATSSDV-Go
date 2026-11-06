package gifmode

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"math/rand"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/fec"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
)

func TestEncodeSparseDeltaAcceptsEntropyTailWithoutMCURestart(t *testing.T) {
	// High-entropy frames routinely end with a final SSDV packet that contains
	// only the tail of the preceding MCU and therefore has no new restart point.
	// This used to abort GIF transmission with “encoder did not provide MCU
	// restart points” even though earlier packets contained valid boundaries.
	current := image.NewRGBA(image.Rect(0, 0, 320, 240))
	random := rand.New(rand.NewSource(1))
	for y := 0; y < current.Bounds().Dy(); y++ {
		for x := 0; x < current.Bounds().Dx(); x++ {
			current.SetRGBA(x, y, color.RGBA{R: uint8(random.Intn(256)), G: uint8(random.Intn(256)), B: uint8(random.Intn(256)), A: 255})
		}
	}
	next := cloneRGBA(current)
	draw.Draw(next, image.Rect(304, 224, 320, 240), image.NewUniform(color.RGBA{R: 250, G: 12, B: 90, A: 255}), image.Point{}, draw.Src)

	_, stats, err := Encode([]Frame{{Image: current, DelayCS: 10}, {Image: next, DelayCS: 10}}, "N0CALL", 18, 4)
	if err != nil {
		t.Fatalf("Encode rejected an encoder entropy tail: %v", err)
	}
	if stats.Frames != 2 || stats.Keyframes != 1 || stats.DataPackets == 0 {
		t.Fatalf("unexpected stats after sparse tail handling: %+v", stats)
	}
	fullDelta, err := ssdv.EncodeImage(current, ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 19, Quality: 4, FEC: true, GIFMode: true})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ssdv.ParsePacket(fullDelta[0])
	if err != nil {
		t.Fatal(err)
	}
	totalMCUs := int(parsed.MCUCount)
	changed := make([]bool, totalMCUs)
	for i := range changed {
		changed[i] = true
	}
	selected, sparseAvailable, err := selectChangedSegments(fullDelta, changed, totalMCUs)
	if err != nil {
		t.Fatalf("selectChangedSegments rejected a valid entropy tail: %v", err)
	}
	hasEOITail := false
	for _, raw := range selected {
		packet, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if packet.EOI && (packet.MCUOffset == 0xff || packet.MCUIndex == 0xffff) {
			hasEOITail = true
			break
		}
	}
	if !sparseAvailable {
		t.Fatal("valid MCU restart points were not recognized as sparse-capable")
	}
	if !hasEOITail {
		t.Fatalf("selector did not retain the final EOI entropy-tail packet; selected=%d full=%+v", len(selected), lastParsedPacket(t, fullDelta))
	}
}

func TestSelectChangedSegmentsSignalsFullKeyframeFallbackWhenMCUPointsAreMissing(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(frame, frame.Bounds(), image.NewUniform(color.RGBA{R: 80, G: 110, B: 140, A: 255}), image.Point{}, draw.Src)
	packets, err := ssdv.EncodeImage(frame, ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 26, Quality: 4, FEC: true, GIFMode: true})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a legacy encoder that has no per-packet MCU restart metadata.
	for _, raw := range packets {
		raw[12] = 0xff
		binary.BigEndian.PutUint16(raw[13:15], 0xffff)
		binary.BigEndian.PutUint32(raw[220:224], crc32.ChecksumIEEE(raw[1:220]))
		parity, parityErr := fec.RS8Parity(raw[1:224])
		if parityErr != nil {
			t.Fatal(parityErr)
		}
		copy(raw[224:], parity)
	}
	changed := make([]bool, 320/16*240/16)
	for i := range changed {
		changed[i] = true
	}
	selected, sparseAvailable, err := selectChangedSegments(packets, changed, len(changed))
	if err != nil {
		t.Fatal(err)
	}
	if sparseAvailable {
		t.Fatal("legacy stream without MCU restart points was incorrectly marked sparse-capable")
	}
	if len(selected) != len(packets) {
		t.Fatalf("fallback selected %d/%d packets; expected the complete frame", len(selected), len(packets))
	}
}

func lastParsedPacket(t *testing.T, packets [][]byte) ssdv.Packet {
	t.Helper()
	if len(packets) == 0 {
		t.Fatal("no packets")
	}
	packet, err := ssdv.ParsePacket(packets[len(packets)-1])
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestDecodeTemplateCompositesGIFBetweenStaticLayers(t *testing.T) {
	colors := color.Palette{color.RGBA{A: 0}, color.RGBA{R: 220, G: 20, B: 30, A: 255}}
	frame := image.NewPaletted(image.Rect(0, 0, 16, 16), colors)
	draw.Draw(frame, frame.Bounds(), &image.Uniform{C: colors[0]}, image.Point{}, draw.Src)
	draw.Draw(frame, image.Rect(0, 0, 8, 16), &image.Uniform{C: colors[1]}, image.Point{}, draw.Src)
	animation := &gif.GIF{Image: []*image.Paletted{frame}, Delay: []int{8}, Config: image.Config{ColorModel: colors, Width: 16, Height: 16}}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animation); err != nil {
		t.Fatal(err)
	}

	base := image.NewRGBA(image.Rect(0, 0, 64, 48))
	draw.Draw(base, base.Bounds(), image.NewUniform(color.RGBA{B: 180, A: 255}), image.Point{}, draw.Src)
	overlay := image.NewRGBA(base.Bounds())
	draw.Draw(overlay, image.Rect(0, 0, 8, 8), image.NewUniform(color.RGBA{G: 210, A: 255}), image.Point{}, draw.Src)

	frames, err := DecodeTemplate(encoded.Bytes(), "low", base, overlay, Rect{X: 16, Y: 8, Width: 16, Height: 16})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || frames[0].DelayCS != 8 {
		t.Fatalf("unexpected template GIF frame result: %+v", frames)
	}
	bg := frames[0].Image.RGBAAt(50, 30)
	gifPixel := frames[0].Image.RGBAAt(20, 12)
	transparentPixel := frames[0].Image.RGBAAt(30, 12)
	overlayPixel := frames[0].Image.RGBAAt(2, 2)
	if bg.B != 180 || gifPixel.R < 200 || transparentPixel.B != 180 || transparentPixel.R > 8 || overlayPixel.G != 210 {
		t.Fatalf("template layers or GIF transparency were not composited correctly: bg=%+v gif=%+v transparent=%+v overlay=%+v", bg, gifPixel, transparentPixel, overlayPixel)
	}
}

func TestEncodeUsesSparseMCUUpdatesAndFrameControls(t *testing.T) {
	first := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(first, first.Bounds(), image.NewUniform(color.RGBA{R: 30, G: 60, B: 90, A: 255}), image.Point{}, draw.Src)
	second := cloneRGBA(first)
	draw.Draw(second, image.Rect(304, 224, 320, 240), image.NewUniform(color.RGBA{R: 220, G: 30, B: 20, A: 255}), image.Point{}, draw.Src)

	packets, stats, err := Encode([]Frame{{Image: first, DelayCS: 10}, {Image: second, DelayCS: 25}}, "N0CALL", 40, 4)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Frames != 2 || stats.Keyframes != 1 || stats.ControlPackets != 4*ControlPacketCopies || stats.TotalMCUs != 600 {
		t.Fatalf("unexpected GIF stats: %+v", stats)
	}
	frame1Data, frame1Control, frame2Data, frame2Control := 0, 0, 0, 0
	var frame1Open, frame1Close, frame2Open, frame2Close ssdv.Packet
	for _, raw := range packets {
		parsed, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatalf("ParsePacket: %v", parseErr)
		}
		if !parsed.GIFMode {
			t.Fatal("GIF flag not set")
		}
		switch parsed.ImageID {
		case 40:
			if parsed.GIFControl {
				frame1Control++
				if parsed.GIFFrameStart {
					frame1Open = parsed
				}
				if parsed.GIFFrameEnd {
					frame1Close = parsed
				}
			} else {
				frame1Data++
				if !parsed.GIFKeyframe {
					t.Fatal("first frame packet is not marked keyframe")
				}
			}
		case 41:
			if parsed.GIFControl {
				frame2Control++
				if parsed.GIFFrameStart {
					frame2Open = parsed
				}
				if parsed.GIFFrameEnd {
					frame2Close = parsed
				}
				if parsed.GIFDelayCS != 25 {
					t.Fatalf("frame delay = %d cs, want 25", parsed.GIFDelayCS)
				}
			} else {
				frame2Data++
				if parsed.GIFKeyframe {
					t.Fatal("delta frame unexpectedly marked keyframe")
				}
			}
		}
	}
	fullSecond, err := ssdv.EncodeImage(second, ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 41, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	if !frame1Open.GIFStart || frame1Open.GIFEnd || !frame1Open.GIFFrameStart || frame1Close.GIFStart || frame1Close.GIFEnd || !frame1Close.GIFFrameEnd || frame2Open.GIFStart || frame2Open.GIFEnd || !frame2Open.GIFFrameStart || frame2Close.GIFStart || !frame2Close.GIFEnd || !frame2Close.GIFFrameEnd {
		t.Fatalf("GIF stream/frame boundary flags are wrong: first-open=%+v first-close=%+v last-open=%+v last-close=%+v", frame1Open, frame1Close, frame2Open, frame2Close)
	}
	if frame1Data == 0 || frame1Control != 2*ControlPacketCopies || frame2Control != 2*ControlPacketCopies {
		t.Fatalf("unexpected frame packet counts: first=%d/%d second=%d/%d", frame1Data, frame1Control, frame2Data, frame2Control)
	}
	if frame2Data >= len(fullSecond) {
		t.Fatalf("delta sent %d packets; full frame needs %d; expected a sparse update", frame2Data, len(fullSecond))
	}
}

func TestDecodeCompositesGIFFrames(t *testing.T) {
	palette := color.Palette{color.RGBA{A: 0}, color.RGBA{R: 255, A: 255}, color.RGBA{G: 255, A: 255}}
	f0 := image.NewPaletted(image.Rect(0, 0, 32, 32), palette)
	draw.Draw(f0, f0.Bounds(), &image.Uniform{C: palette[1]}, image.Point{}, draw.Src)
	f1 := image.NewPaletted(image.Rect(16, 0, 32, 16), palette)
	draw.Draw(f1, f1.Bounds(), &image.Uniform{C: palette[2]}, image.Point{}, draw.Src)
	g := &gif.GIF{
		Image: []*image.Paletted{f0, f1}, Delay: []int{5, 20}, Disposal: []byte{gif.DisposalNone, gif.DisposalNone},
		Config: image.Config{ColorModel: palette, Width: 32, Height: 32}, BackgroundIndex: 0,
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	frames, err := Decode(buf.Bytes(), "low")
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || frames[0].DelayCS != 5 || frames[1].DelayCS != 20 {
		t.Fatalf("unexpected decoded GIF frames: %+v", frames)
	}
	if frames[0].Image.Bounds() != image.Rect(0, 0, 320, 240) {
		t.Fatalf("resized bounds = %v", frames[0].Image.Bounds())
	}
}

func TestEncodeUnchangedFrameSendsOnlyFrameControl(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(frame, frame.Bounds(), image.NewUniform(color.RGBA{R: 90, G: 110, B: 130, A: 255}), image.Point{}, draw.Src)
	packets, stats, err := Encode([]Frame{{Image: frame, DelayCS: 10}, {Image: cloneRGBA(frame), DelayCS: 30}}, "N0CALL", 70, 4)
	if err != nil {
		t.Fatal(err)
	}
	if stats.DataPackets == 0 || stats.ControlPackets != 4*ControlPacketCopies {
		t.Fatalf("unexpected packet stats: %+v", stats)
	}
	secondFramePackets, secondFrameControls := 0, 0
	for _, raw := range packets {
		packet, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if packet.ImageID == 71 {
			if packet.GIFControl {
				secondFrameControls++
			} else {
				secondFramePackets++
			}
		}
	}
	if secondFramePackets != 0 || secondFrameControls != 2*ControlPacketCopies {
		t.Fatalf("unchanged frame sent %d data and %d control packets", secondFramePackets, secondFrameControls)
	}
}

func TestDecodeRejectsOversizedLogicalScreenBeforeFrameAllocation(t *testing.T) {
	data := make([]byte, 13)
	copy(data[:6], "GIF89a")
	data[6], data[7] = 0x00, 0x10 // 4096 px wide
	data[8], data[9] = 0x00, 0x10 // 4096 px high
	if _, err := Decode(data, "low"); err == nil {
		t.Fatal("expected oversized GIF logical screen to be rejected")
	}
}

func TestEncodeContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	frame := image.NewRGBA(image.Rect(0, 0, 320, 240))
	if _, _, err := EncodeContext(ctx, []Frame{{Image: frame, DelayCS: 10}}, "N0CALL", 1, 4); err != context.Canceled {
		t.Fatalf("EncodeContext error = %v, want context.Canceled", err)
	}
}
