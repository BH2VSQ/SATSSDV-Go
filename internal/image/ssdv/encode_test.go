package ssdv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"testing"
)

func fixtureImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x*3 + y) & 0xff),
				G: uint8((x + y*5) & 0xff),
				B: uint8((x ^ (y * 7)) & 0xff), A: 0xff,
			})
		}
	}
	return img
}

func TestEncodeBase40MatchesSSDVByteOrder(t *testing.T) {
	value, err := EncodeBase40("N0CALL")
	if err != nil {
		t.Fatal(err)
	}
	if got := DecodeBase40(value); got != "N0CALL" {
		t.Fatalf("Base40 round trip = %q, want N0CALL", got)
	}
	if _, err := EncodeBase40("N0/CALL"); err == nil {
		t.Fatal("invalid callsign character was accepted")
	}
}

func TestEncodeImageProducesValidFECSSDVPackets(t *testing.T) {
	img := fixtureImage(320, 240)
	packets, err := EncodeImage(img, EncodeConfig{Callsign: "N0CALL", ImageID: 37, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) < 2 {
		t.Fatalf("expected multiple packets, got %d", len(packets))
	}
	for i, raw := range packets {
		p, err := ParsePacket(raw)
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		if p.Callsign != "N0CALL" || p.ImageID != 37 || p.PacketID != uint16(i) || !p.FEC {
			t.Fatalf("packet %d metadata mismatch: %+v", i, p)
		}
		if p.Width != 320 || p.Height != 240 || p.Quality != 4 || p.Subsampling != 0 {
			t.Fatalf("packet %d image metadata mismatch: %+v", i, p)
		}
		if p.EOI != (i == len(packets)-1) {
			t.Fatalf("packet %d EOI=%v", i, p.EOI)
		}
		if p.MCUOffset == 0xff || p.MCUIndex == 0xffff {
			t.Fatalf("packet %d is missing independent MCU restart metadata", i)
		}
		if int(p.MCUOffset) >= FECDataLength {
			t.Fatalf("packet %d MCU offset %d exceeds payload", i, p.MCUOffset)
		}
	}
}

func TestEncodeImageSupportsNoFECAndAllQuantizerLevels(t *testing.T) {
	img := fixtureImage(320, 240)
	packetCounts := make([]int, 8)
	for quality := uint8(0); quality <= 7; quality++ {
		packets, err := EncodeImage(img, EncodeConfig{Callsign: "JA1ABC", ImageID: quality, Quality: quality, FEC: false})
		if err != nil {
			t.Fatalf("quality %d: %v", quality, err)
		}
		packetCounts[quality] = len(packets)
		for i, raw := range packets {
			p, err := ParsePacket(raw)
			if err != nil {
				t.Fatalf("quality %d packet %d: %v", quality, i, err)
			}
			if p.FEC || p.Quality != quality || p.ImageID != quality {
				t.Fatalf("quality %d packet %d metadata mismatch: %+v", quality, i, p)
			}
		}
	}
	if packetCounts[0] >= packetCounts[7] {
		t.Fatalf("expected high-quality entropy to use more packets, counts=%v", packetCounts)
	}
}

func TestDecodePacketsToJPEGReassemblesOutOfOrderPackets(t *testing.T) {
	packets, err := EncodeImage(fixtureImage(320, 240), EncodeConfig{Callsign: "N0CALL", ImageID: 91, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	shuffled := make([][]byte, 0, len(packets)+1)
	for i := len(packets) - 1; i >= 0; i-- {
		shuffled = append(shuffled, packets[i])
	}
	shuffled = append(shuffled, packets[len(packets)/2]) // identical duplicate is ignored.
	jpegData, metadata, err := DecodePacketsToJPEG(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		t.Fatalf("reconstructed JPEG decode failed: %v", err)
	}
	if decoded.Bounds().Dx() != 320 || decoded.Bounds().Dy() != 240 {
		t.Fatalf("unexpected decoded dimensions %v", decoded.Bounds())
	}
	if metadata.Callsign != "N0CALL" || metadata.ImageID != 91 {
		t.Fatalf("unexpected metadata %+v", metadata)
	}
}

func TestDecodePacketsToJPEGRejectsMissingPacket(t *testing.T) {
	packets, err := EncodeImage(fixtureImage(320, 240), EncodeConfig{Callsign: "N0CALL", ImageID: 92, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) < 3 {
		t.Fatalf("expected at least three packets, got %d", len(packets))
	}
	missing := append([][]byte(nil), packets[:1]...)
	missing = append(missing, packets[2:]...)
	if _, _, err := DecodePacketsToJPEG(missing); err == nil {
		t.Fatal("missing packet was accepted as a complete image")
	}
}

func TestEncodeImageRejectsUnsupportedDimensionsAndQuality(t *testing.T) {
	if _, err := EncodeImage(fixtureImage(319, 240), EncodeConfig{Callsign: "N0CALL", FEC: true}); err == nil {
		t.Fatal("non-MCU-aligned image width was accepted")
	}
	if _, err := EncodeImage(fixtureImage(320, 240), EncodeConfig{Callsign: "N0CALL", Quality: 8, FEC: true}); err == nil {
		t.Fatal("out-of-range SSDV quality was accepted")
	}
}

func TestTranscodedScanIsValidBaselineJPEG(t *testing.T) {
	img := fixtureImage(320, 240)
	var source bytes.Buffer
	if err := jpeg.Encode(&source, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	frame, err := parseBaselineJPEG(source.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	const quality = uint8(4)
	entropy, err := transcodeJPEGScan(frame, quality)
	if err != nil {
		t.Fatal(err)
	}
	wireJPEG, err := testJPEGFromSSDVScan(source.Bytes(), entropy, quality)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(wireJPEG))
	if err != nil {
		t.Fatalf("transcoded SSDV scan is not a valid JPEG: %v", err)
	}
	if decoded.Bounds().Dx() != 320 || decoded.Bounds().Dy() != 240 {
		t.Fatalf("decoded size = %v", decoded.Bounds())
	}
}

func testJPEGFromSSDVScan(source, entropy []byte, quality uint8) ([]byte, error) {
	out := append([]byte(nil), source...)
	off := 2
	headerEnd := 0
	for off < len(out) {
		for off < len(out) && out[off] != 0xff {
			off++
		}
		for off < len(out) && out[off] == 0xff {
			off++
		}
		if off >= len(out) {
			break
		}
		marker := out[off]
		off++
		if marker == 0xd8 || marker == 0xd9 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if off+2 > len(out) {
			return nil, errors.New("truncated test JPEG marker")
		}
		length := int(binary.BigEndian.Uint16(out[off : off+2]))
		if length < 2 || off+length > len(out) {
			return nil, errors.New("invalid test JPEG marker length")
		}
		if marker == 0xdb {
			pos := off + 2
			end := off + length
			for pos < end {
				info := out[pos]
				pos++
				if info>>4 != 0 || end-pos < 64 {
					return nil, errors.New("unexpected test quantization table format")
				}
				table := ssdvLumaQuant[:]
				if info&0x0f == 1 {
					table = ssdvChromaQuant[:]
				}
				for i, base := range table {
					v := (int(base)*ssdvQualityScale[quality] + 50) / 100
					if v < 1 {
						v = 1
					}
					if v > 255 {
						v = 255
					}
					out[pos+i] = byte(v)
				}
				pos += 64
			}
		}
		if marker == 0xda {
			headerEnd = off + length
			break
		}
		off += length
	}
	if headerEnd == 0 {
		return nil, errors.New("test JPEG SOS not found")
	}
	result := append([]byte(nil), out[:headerEnd]...)
	for _, b := range entropy {
		result = append(result, b)
		if b == 0xff {
			result = append(result, 0)
		}
	}
	result = append(result, 0xff, 0xd9)
	return result, nil
}

func TestEncodeDecodePreservesImageContent(t *testing.T) {
	const width, height = 320, 240
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Smooth gradients and broad color regions expose coefficient,
			// component-order and quantization mistakes without requiring a
			// lossless comparison from a lossy radio image codec.
			source.SetRGBA(x, y, color.RGBA{
				R: uint8((x*3/4 + y/5) & 0xff),
				G: uint8((y*4/5 + x/7) & 0xff),
				B: uint8(((x+y)*2/3 + 24) & 0xff), A: 0xff,
			})
		}
	}
	packets, err := EncodeImage(source, EncodeConfig{Callsign: "N0CALL", ImageID: 101, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	jpegData, _, err := DecodePacketsToJPEG(packets)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		t.Fatal(err)
	}
	var absoluteError uint64
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			a := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			b := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			for _, delta := range []int{int(a.R) - int(b.R), int(a.G) - int(b.G), int(a.B) - int(b.B)} {
				if delta < 0 {
					delta = -delta
				}
				absoluteError += uint64(delta)
			}
		}
	}
	mae := float64(absoluteError) / float64(width*height*3)
	if mae > 16 {
		t.Fatalf("mean absolute RGB error %.2f exceeds quality-4 limit 16", mae)
	}
}

func TestDecodePacketPrefixToJPEGProducesBlockPreview(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	packets, err := EncodeImage(source, EncodeConfig{Callsign: "N0CALL", ImageID: 8, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	var jpegData []byte
	var height int
	for count := 1; count < len(packets); count++ {
		jpegData, _, height, err = DecodePacketPrefixToJPEG(packets[:count])
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("could not create an incremental block preview: %v", err)
	}
	if height != 240 {
		t.Fatalf("block preview height = %d, want fixed canvas height 240", height)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		t.Fatalf("decode block preview JPEG: %v", err)
	}
	if decoded.Bounds().Dx() != 320 || decoded.Bounds().Dy() != height {
		t.Fatalf("unexpected preview bounds: %v (reported height %d)", decoded.Bounds(), height)
	}
}

func TestDecodePacketsToPreviewFillsIndependentMCUBlocksAndMarksGaps(t *testing.T) {
	source := fixtureImage(320, 240)
	packets, err := EncodeImage(source, EncodeConfig{Callsign: "N0CALL", ImageID: 44, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) < 4 {
		t.Fatalf("expected several SSDV packets, got %d", len(packets))
	}
	// Decode packet 0 and a later packet while omitting packet 1. Independent
	// MCU markers must place the later packet at its own block index.
	selected := [][]byte{packets[0], packets[2]}
	preview, metadata, blocks, err := DecodePacketsToPreview(selected)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Bounds().Dx() != 320 || preview.Bounds().Dy() != 240 {
		t.Fatalf("preview bounds = %v", preview.Bounds())
	}
	if blocks == 0 || blocks >= int(metadata.MCUCount) {
		t.Fatalf("unexpected covered MCU count %d of %d", blocks, metadata.MCUCount)
	}
	first, err := ParsePacket(packets[0])
	if err != nil {
		t.Fatal(err)
	}
	later, err := ParsePacket(packets[2])
	if err != nil {
		t.Fatal(err)
	}
	if first.MCUIndex != 0 || later.MCUIndex <= first.MCUIndex {
		t.Fatalf("packet MCU indices do not advance: first=%d later=%d", first.MCUIndex, later.MCUIndex)
	}
	missing, err := ParsePacket(packets[1])
	if err != nil { t.Fatal(err) }
	missingIndex := int(missing.MCUIndex)
	missingX, missingY := (missingIndex%(320/16))*16+8, (missingIndex/(320/16))*16+8
	r, g, b, _ := preview.At(missingX, missingY).RGBA()
	if uint8(r>>8) != 218 || uint8(g>>8) != 220 || uint8(b>>8) != 222 {
		t.Fatalf("missing MCU pixel = #%02x%02x%02x, want uniform light grey #dadcde", uint8(r>>8), uint8(g>>8), uint8(b>>8))
	}
	fullPreview, fullMetadata, fullBlocks, err := DecodePacketsToPreview(packets)
	if err != nil {
		t.Fatalf("decode complete block preview: %v", err)
	}
	wantBlocks := int(fullMetadata.MCUCount)
	if fullPreview.Bounds().Dx() != 320 || fullPreview.Bounds().Dy() != 240 || fullBlocks != wantBlocks {
		t.Fatalf("complete block preview = %v, %d MCUs; want 320x240, %d MCUs", fullPreview.Bounds(), fullBlocks, wantBlocks)
	}
}


func TestIncrementalPreviewMatchesCompletePreview(t *testing.T) {
	packets, err := EncodeImage(fixtureImage(320, 240), EncodeConfig{Callsign: "N0CALL", ImageID: 67, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	var incremental *image.RGBA
	for i := range packets {
		neighbors := make([][]byte, 0, 2)
		if i > 0 {
			neighbors = append(neighbors, packets[i-1])
		}
		neighbors = append(neighbors, packets[i])
		if incremental == nil {
			incremental, _, _, err = DecodePacketsToPreview(neighbors)
		} else {
			incremental, _, _, err = UpdatePacketsPreview(incremental, neighbors)
		}
		if err != nil {
			t.Fatalf("incremental packet %d: %v", i, err)
		}
	}
	full, _, _, err := DecodePacketsToPreview(packets)
	if err != nil {
		t.Fatal(err)
	}
	if incremental.Bounds() != full.Bounds() || !bytes.Equal(incremental.Pix, full.Pix) {
		t.Fatal("incremental SSDV preview differs from full packet-set preview")
	}
}

func TestDefaultSSDVCompressionPrefilterReducesAirtime(t *testing.T) {
	source := fixtureImage(320, 240)
	var baselineJPEG bytes.Buffer
	if err := jpeg.Encode(&baselineJPEG, source, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	baselineFrame, err := parseBaselineJPEG(baselineJPEG.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	baselineEntropy, err := transcodeJPEGScan(baselineFrame, DefaultJPEGQuality)
	if err != nil {
		t.Fatal(err)
	}
	baselinePackets := (len(baselineEntropy) + FECDataLength - 1) / FECDataLength

	packets, err := EncodeImage(source, EncodeConfig{Callsign: "N0CALL", ImageID: 100, Quality: DefaultJPEGQuality, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(packets)*100 > baselinePackets*95 {
		t.Fatalf("adaptive SSDV source prefilter did not reduce packet count by at least 5%%: got %d, baseline %d", len(packets), baselinePackets)
	}
	jpegData, _, err := DecodePacketsToJPEG(packets)
	if err != nil {
		t.Fatalf("decode compressed SSDV image: %v", err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		t.Fatal(err)
	}
	var squaredError float64
	var samples float64
	for y := 0; y < source.Bounds().Dy(); y += 2 {
		for x := 0; x < source.Bounds().Dx(); x += 2 {
			r1, g1, b1, _ := source.At(x, y).RGBA()
			r2, g2, b2, _ := decoded.At(x, y).RGBA()
			for _, delta := range []float64{float64(r1>>8) - float64(r2>>8), float64(g1>>8) - float64(g2>>8), float64(b1>>8) - float64(b2>>8)} {
				squaredError += delta * delta
				samples++
			}
		}
	}
	mse := squaredError / samples
	psnr := 10 * math.Log10(255*255/mse)
	if psnr < 21.6 {
		t.Fatalf("adaptive SSDV compression PSNR %.2f dB fell below the 21.6 dB fixture guardrail", psnr)
	}
	t.Logf("SSDV source prefilter: %d -> %d packets (%.1f%% fewer), sampled PSNR %.2f dB", baselinePackets, len(packets), 100*(1-float64(len(packets))/float64(baselinePackets)), psnr)
}

func TestPacketizedSSDVEncodingEfficiency(t *testing.T) {
	source := fixtureImage(320, 240)
	var baseline bytes.Buffer
	if err := jpeg.Encode(&baseline, source, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	frame, err := parseBaselineJPEG(baseline.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	continuous, err := transcodeJPEGScan(frame, 4)
	if err != nil {
		t.Fatal(err)
	}
	restartStream, _, err := transcodeJPEGScanPacketized(frame, 4, FECDataLength)
	if err != nil {
		t.Fatal(err)
	}
	legacyCount := (len(continuous) + FECDataLength - 1) / FECDataLength
	restartCount := (len(restartStream) + FECDataLength - 1) / FECDataLength
	overhead := float64(restartCount-legacyCount) / float64(legacyCount)
	t.Logf("continuous stream: %d bytes / %d packets; MCU restart stream: %d bytes / %d packets; packet overhead %.1f%%", len(continuous), legacyCount, len(restartStream), restartCount, overhead*100)
	if overhead > 0.02 {
		t.Fatalf("MCU restart packetization overhead %.1f%% exceeds the 2%% guardrail", overhead*100)
	}
}

func BenchmarkEncodeImage320x240(b *testing.B) {
	source := fixtureImage(320, 240)
	cfg := EncodeConfig{Callsign: "N0CALL", ImageID: 1, Quality: 4, FEC: true}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := EncodeImage(source, cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodePacketsToPreview320x240(b *testing.B) {
	packets, err := EncodeImage(fixtureImage(320, 240), EncodeConfig{Callsign: "N0CALL", ImageID: 1, Quality: DefaultJPEGQuality, FEC: true})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := DecodePacketsToPreview(packets); err != nil {
			b.Fatal(err)
		}
	}
}
