package transport

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
)

func TestFSKStreamingAcquiresAfterIdleAudioNoise(t *testing.T) {
	packet := makeTestSSDVPacket(t)
	for _, profile := range []domain.TransportMode{domain.SSDVAX25, domain.SSDVCCSDS1200} {
		t.Run(string(profile), func(t *testing.T) {
			encoder, err := NewEncoder(Config{Profile: profile, Source: ax25.Address{Callsign: "N0CALL"}, Destination: ax25.Address{Callsign: "CQ"}})
			if err != nil {
				t.Fatal(err)
			}
			wave, err := encoder.EncodePacket(packet)
			if err != nil {
				t.Fatal(err)
			}

			for _, inputRate := range []int{19200, 48000} {
				var signal []float32
				if inputRate == 19200 {
					signal = append([]float32(nil), wave...)
				} else {
					signal, err = dsp.ResampleLinear(wave, 19200, inputRate)
					if err != nil {
						t.Fatal(err)
					}
				}
				// The RX worker may already be active on radio/audio idle noise
				// before the first image burst arrives. This used to trigger false
				// FSK lock and permanently poison both 1200-baud receive paths.
				rng := rand.New(rand.NewSource(1))
				noise := make([]float32, inputRate/2) // 500 ms
				for i := range noise {
					noise[i] = float32((rng.Float64()*2 - 1) * 0.01)
				}
				input := append(noise, signal...)
				for _, chunkSize := range []int{480, 997, 1920, len(input)} {
					t.Run(fmt.Sprintf("rate-%d-chunk-%d", inputRate, chunkSize), func(t *testing.T) {
						decoder, err := NewStreamDecoder(profile, inputRate)
						if err != nil {
							t.Fatal(err)
						}
						var received []ReceivedPacket
						for offset := 0; offset < len(input); {
							end := offset + chunkSize
							if end > len(input) {
								end = len(input)
							}
							batch, feedErr := decoder.Feed(input[offset:end])
							if feedErr != nil {
								t.Fatal(feedErr)
							}
							received = append(received, batch...)
							offset = end
						}
						if len(received) != 1 || received[0].PacketID != 12 {
							t.Fatalf("recovered %d packets after idle noise; want exactly one packet 12", len(received))
						}
					})
				}
			}
		})
	}
}
