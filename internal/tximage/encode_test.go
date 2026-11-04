package tximage

import (
	"image"
	"image/color"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/imagequality"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
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

func BenchmarkEncodeImageQualityPresets(b *testing.B) {
	for _, quality := range []string{"low", "medium", "high"} {
		b.Run(quality, func(b *testing.B) {
			img := txFixture()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := EncodeImage(img, string(domain.SSDVCCSDS1200), quality, "N0CALL", uint8(i), 48000); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDecodeAudioAndSSDVQualityPresets measures the production audio
// demodulator plus SSDV packet-to-preview reconstruction for each image size.
func BenchmarkDecodeAudioAndSSDVQualityPresets(b *testing.B) {
	for _, quality := range []string{"low", "medium", "high"} {
		b.Run(quality, func(b *testing.B) {
			encoded, err := EncodeImage(txFixture(), string(domain.SSDVCCSDS1200), quality, "N0CALL", 41, 48000)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				received, decodeErr := transport.DecodeAudioAtRate(domain.SSDVCCSDS1200, encoded.Waveform, encoded.SampleRate)
				if decodeErr != nil {
					b.Fatal(decodeErr)
				}
				packets := make([][]byte, 0, len(received))
				for _, packet := range received {
					packets = append(packets, packet.Raw)
				}
				if len(packets) != encoded.PacketCount {
					b.Fatalf("decoded %d/%d packets", len(packets), encoded.PacketCount)
				}
				if _, _, _, decodeErr = ssdv.DecodePacketsToPreview(packets); decodeErr != nil {
					b.Fatal(decodeErr)
				}
			}
		})
	}
}

func BenchmarkEncodePCSIQualityPresets(b *testing.B) {
	for _, quality := range []string{"low", "medium", "high"} {
		b.Run(quality, func(b *testing.B) {
			img := txFixture()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := EncodeImage(img, string(domain.PCSICCSDS1200), quality, "N0CALL", uint8(i), 19200); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDecodePCSIQualityPresets measures PCSI packet RS correction and
// sparse-measurement ingestion for all three dimensions. It deliberately
// avoids audio waveform generation here: the SSDV benchmarks above measure
// audio demodulation, while this isolates the PCSI-specific decoder workload.
func BenchmarkDecodePCSIQualityPresets(b *testing.B) {
	for _, quality := range []string{"low", "medium", "high"} {
		b.Run(quality, func(b *testing.B) {
			resized, err := imagequality.ResizeCover(txFixture(), imagequality.Preset(quality))
			if err != nil {
				b.Fatal(err)
			}
			compression := 60
			if quality == "low" {
				compression = 80
			}
			if quality == "high" {
				compression = 40
			}
			encoder, err := pcsi.NewEncoder(resized, pcsi.Config{Callsign: "N0CALL", ImageID: 42, BitDepth: 12, ChromaCompression: compression})
			if err != nil {
				b.Fatal(err)
			}
			packets := make([][]byte, encoder.PacketCount())
			for i := range packets {
				packets[i], err = encoder.EncodePacket(i)
				if err != nil {
					b.Fatal(err)
				}
			}
			info, err := pcsi.ParsePacket(packets[0])
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := pcsi.NewDecoderState(info)
				for _, packet := range packets {
					if err := state.Add(packet); err != nil {
						b.Fatal(err)
					}
				}
				stats := state.Stats()
				if stats.PacketsReceived != encoder.PacketCount() {
					b.Fatalf("ingested %d/%d packets", stats.PacketsReceived, encoder.PacketCount())
				}
			}
		})
	}
}

// BenchmarkReconstructPCSIQualityPresetsOneIteration isolates one full-image
// OWL-QN iteration after packet ingestion at each production image size. It is
// intentionally one iteration: normal preview/final decoding uses a larger
// bounded budget, so callers should not treat these numbers as full-image
// completion latency.
func BenchmarkReconstructPCSIQualityPresetsOneIteration(b *testing.B) {
	for _, quality := range []string{"low", "medium", "high"} {
		b.Run(quality, func(b *testing.B) {
			resized, err := imagequality.ResizeCover(txFixture(), imagequality.Preset(quality))
			if err != nil {
				b.Fatal(err)
			}
			compression := 60
			if quality == "low" {
				compression = 80
			}
			if quality == "high" {
				compression = 40
			}
			encoder, err := pcsi.NewEncoder(resized, pcsi.Config{Callsign: "N0CALL", ImageID: 43, BitDepth: 12, ChromaCompression: compression})
			if err != nil {
				b.Fatal(err)
			}
			packets := make([][]byte, encoder.PacketCount())
			for i := range packets {
				packets[i], err = encoder.EncodePacket(i)
				if err != nil {
					b.Fatal(err)
				}
			}
			info, err := pcsi.ParsePacket(packets[0])
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := pcsi.NewDecoderState(info)
				for _, packet := range packets {
					if err := state.Add(packet); err != nil {
						b.Fatal(err)
					}
				}
				if _, _, err := state.Reconstruct(1); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPCSIReconstruction64x64 measures one OWL-QN solver iteration on a
// small common geometry. Production 320x240..640x480 reconstruction has a
// cubic-like memory/compute growth in the separable DCT workspaces and is
// intentionally not repeated here at 48-96 iterations.
func BenchmarkPCSIReconstruction64x64(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: uint8((x ^ y) * 4), A: 255})
		}
	}
	encoder, err := pcsi.NewEncoder(img, pcsi.Config{Callsign: "N0CALL", ImageID: 42, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		b.Fatal(err)
	}
	packets := make([][]byte, encoder.PacketCount())
	for i := range packets {
		packets[i], err = encoder.EncodePacket(i)
		if err != nil {
			b.Fatal(err)
		}
	}
	info, err := pcsi.ParsePacket(packets[0])
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state := pcsi.NewDecoderState(info)
		for _, packet := range packets {
			if err := state.Add(packet); err != nil {
				b.Fatal(err)
			}
		}
		if _, _, err := state.Reconstruct(1); err != nil {
			b.Fatal(err)
		}
	}
}
