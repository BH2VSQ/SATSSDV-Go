package tximage

import (
	"image"
	"image/color"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
)

func txFixture() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	return img
}

func TestEncodePCSIImageAtBothCCSDSRates(t *testing.T) {
	source := txFixture()
	for _, profile := range []string{string(domain.PCSICCSDS1200), string(domain.PCSICCSDS2400)} {
		result, err := EncodeImage(source, profile, "low", "N0CALL", 42, 19200)
		if err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
		if result.Image.Bounds().Dx() != 320 || result.Image.Bounds().Dy() != 240 {
			t.Fatalf("%s: output image size %v", profile, result.Image.Bounds())
		}
		if result.PacketCount <= 0 || len(result.Waveform) == 0 || result.SampleRate != 19200 {
			t.Fatalf("%s: invalid encoding result: packets=%d waveform=%d rate=%d", profile, result.PacketCount, len(result.Waveform), result.SampleRate)
		}
	}
}

func TestEncodeSSDVImageAtAllLinkProfiles(t *testing.T) {
	source := txFixture()
	profiles := []string{
		string(domain.SSDVAX25),
		string(domain.SSDVCCSDS1200),
		string(domain.SSDVCCSDS2400),
	}
	for _, profile := range profiles {
		result, err := EncodeImage(source, profile, "low", "N0CALL", 7, 48000)
		if err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
		if result.PacketCount <= 0 || len(result.Waveform) == 0 || result.SampleRate != 48000 {
			t.Fatalf("%s: invalid encoding result: packets=%d waveform=%d rate=%d", profile, result.PacketCount, len(result.Waveform), result.SampleRate)
		}
	}
}
