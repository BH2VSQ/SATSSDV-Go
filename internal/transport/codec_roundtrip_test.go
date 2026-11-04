package transport

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
)

func roundTripFixtureImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x*3 + y) & 0xff),
				G: uint8((x + y*5) & 0xff),
				B: uint8((x ^ (y * 7)) & 0xff), A: 0xff,
			})
		}
	}
	return img
}

// This is an end-to-end regression for the production image -> link encoder ->
// waveform -> streaming demodulator -> frame parser -> image-packet decoder.
// The whole-waveform case is important: RX backends may deliver a large first
// callback. Demodulator acquisition must use the last 64 symbols to estimate
// timing, but must not throw away the preamble/frames before that window.
func TestSSDVAX25AndCCSDS1200ProductionCodecRoundTripAcrossAudioChunkSizes(t *testing.T) {
	imagePackets, err := ssdv.EncodeImage(roundTripFixtureImage(), ssdv.EncodeConfig{
		Callsign: "N0CALL", ImageID: 0x39, Quality: 4, FEC: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(imagePackets) < 3 {
		t.Fatalf("fixture must span multiple SSDV packets, got %d", len(imagePackets))
	}

	profiles := []domain.TransportMode{domain.SSDVAX25, domain.SSDVCCSDS1200}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			encoder, err := NewEncoder(Config{
				Profile:     profile,
				Source:      ax25.Address{Callsign: "N0CALL", SSID: 1},
				Destination: ax25.Address{Callsign: "CQ"},
			})
			if err != nil {
				t.Fatal(err)
			}
			wave, err := encoder.EncodePackets(imagePackets)
			if err != nil {
				t.Fatal(err)
			}
			// Exercise the standalone capture decoder too. The desktop RX path
			// uses the streaming decoder below, while file/capture diagnostics
			// use DecodeAudioAtRate.
			standalone, standaloneErr := DecodeAudioAtRate(profile, wave, 19200)
			if standaloneErr != nil {
				t.Fatalf("standalone waveform decode failed: %v", standaloneErr)
			}
			if len(standalone) != len(imagePackets) {
				t.Fatalf("standalone decoder recovered %d/%d image packets", len(standalone), len(imagePackets))
			}
			chunkSizes := []int{len(wave), 480, 997, 1920}
			for _, chunkSize := range chunkSizes {
				t.Run(fmtChunkSize(chunkSize), func(t *testing.T) {
					decoder, err := NewStreamDecoder(profile, 19200)
					if err != nil {
						t.Fatal(err)
					}
					var decoded []ReceivedPacket
					for offset := 0; offset < len(wave); offset += chunkSize {
						end := offset + chunkSize
						if end > len(wave) {
							end = len(wave)
						}
						packets, feedErr := decoder.Feed(wave[offset:end])
						if feedErr != nil {
							t.Fatalf("feed samples [%d:%d]: %v", offset, end, feedErr)
						}
						decoded = append(decoded, packets...)
					}
					if len(decoded) != len(imagePackets) {
						t.Fatalf("decoded %d/%d SSDV packets (waveform samples=%d, callback chunk=%d)", len(decoded), len(imagePackets), len(wave), chunkSize)
					}
					correctedPackets := make([][]byte, len(decoded))
					for i, packet := range decoded {
						if packet.PacketID != uint16(i) || packet.Callsign != "N0CALL" || packet.ImageID != 0x39 {
							t.Fatalf("packet %d metadata mismatch: %+v", i, packet)
						}
						correctedPackets[i] = packet.Raw
					}
					jpegBytes, metadata, decodeErr := ssdv.DecodePacketsToJPEG(correctedPackets)
					if decodeErr != nil {
						t.Fatalf("reassemble SSDV JPEG from received packets: %v", decodeErr)
					}
					decodedImage, jpegErr := jpeg.Decode(bytes.NewReader(jpegBytes))
					if jpegErr != nil {
						t.Fatalf("decode reassembled SSDV JPEG: %v", jpegErr)
					}
					if decodedImage.Bounds().Dx() != 320 || decodedImage.Bounds().Dy() != 240 || metadata.ImageID != 0x39 {
						t.Fatalf("reassembled image/metadata mismatch: bounds=%v metadata=%+v", decodedImage.Bounds(), metadata)
					}
				})
			}
		})
	}
}

func fmtChunkSize(n int) string {
	if n > 100000 {
		return "whole-waveform"
	}
	return fmt.Sprintf("chunk-%d", n)
}
