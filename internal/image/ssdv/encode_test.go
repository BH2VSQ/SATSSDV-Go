package ssdv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
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
		if p.MCUOffset != 0xff || p.MCUIndex != 0xffff {
			t.Fatalf("packet %d did not use contiguous-stream sentinel", i)
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
