package tximage

import (
	"image"
	"image/color"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
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

func TestEncodeAX25SupportsDestinationAndLocalSSID(t *testing.T) {
	result, err := EncodeImageWithAX25(txFixture(), string(domain.SSDVAX25), "low", "N0CALL", 9, 19200, 7, "JA1ABC")
	if err != nil {
		t.Fatalf("AX.25 encode with SSID and destination callsign failed: %v", err)
	}
	if result.PacketCount == 0 || len(result.Waveform) == 0 {
		t.Fatal("AX.25 encoder returned an empty transmission")
	}
	if _, err := EncodeImageWithAX25(txFixture(), string(domain.SSDVAX25), "low", "N0CALL", 9, 19200, 16, "JA1ABC"); err == nil {
		t.Fatal("AX.25 source SSID above 15 should be rejected")
	}
	if _, err := EncodeImageWithAX25(txFixture(), string(domain.SSDVAX25), "low", "N0CALL", 9, 19200, 0, "BAD CALL"); err == nil {
		t.Fatal("invalid AX.25 destination callsign should be rejected")
	}
}

func TestPCSITransmitPresetsReducePacketCount(t *testing.T) {
	source := txFixture()
	result, err := EncodeImage(source, string(domain.PCSICCSDS1200), "low", "N0CALL", 9, 19200)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := pcsi.NewEncoder(result.Image, pcsi.Config{Callsign: "N0CALL", ImageID: 9, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	if result.PacketCount >= legacy.PacketCount() {
		t.Fatalf("TX preset did not lower packet count: new=%d old=%d", result.PacketCount, legacy.PacketCount())
	}
}
