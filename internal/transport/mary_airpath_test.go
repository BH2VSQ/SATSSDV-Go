package transport

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
)

func TestMAModulationTXFilteredRXAirpath(t *testing.T) {
	packets, err := ssdv.EncodeImage(roundTripFixtureImage(), ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 0x65, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	chunks := []int{480, 960, 1920, 4096}
	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			for _, sampleRate := range []int{44100, 48000} {
				t.Run(fmt.Sprintf("audio-%d", sampleRate), func(t *testing.T) {
					enc, err := NewEncoder(Config{Profile: profile, AudioSampleRate: sampleRate})
					if err != nil {
						t.Fatal(err)
					}
					wave, err := enc.EncodePackets(packets)
					if err != nil {
						t.Fatal(err)
					}
					if err = dsp.FilterModulatedAudioInPlace(wave, sampleRate); err != nil {
						t.Fatal(err)
					}
					for _, gain := range []float32{0.20, 0.45, 1.0} {
						transmitted := make([]float32, len(wave))
						for i, sample := range wave {
							transmitted[i] = sample * gain
						}
						input := append(make([]float32, 2413), transmitted...)
						for _, chunk := range chunks {
							dec, err := NewStreamDecoder(profile, sampleRate)
							if err != nil {
								t.Fatal(err)
							}
							got := make([]ReceivedPacket, 0, len(packets))
							for i := 0; i < len(input); i += chunk {
								end := min(len(input), i+chunk)
								part, e := dec.Feed(input[i:end])
								if e != nil {
									t.Fatal(e)
								}
								got = append(got, part...)
							}
							if len(got) != len(packets) {
								t.Fatalf("gain=%.2f chunk=%d decoded %d/%d packets", gain, chunk, len(got), len(packets))
							}
							for i := range got {
								if got[i].PacketID != uint16(i) {
									t.Fatalf("gain=%.2f chunk=%d packet %d out of order: %d", gain, chunk, i, got[i].PacketID)
								}
							}
						}
					}
				})
			}
		})
	}
}

func TestMAModulationTXFilteredRXWithAudioNoise(t *testing.T) {
	packets, err := ssdv.EncodeImage(roundTripFixtureImage(), ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 0x66, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600} {
		t.Run(string(profile), func(t *testing.T) {
			enc, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 44100})
			if err != nil {
				t.Fatal(err)
			}
			wave, err := enc.EncodePackets(packets)
			if err != nil {
				t.Fatal(err)
			}
			if err := dsp.FilterModulatedAudioInPlace(wave, 44100); err != nil {
				t.Fatal(err)
			}
			const targetSNRdB = 34.0
			var sum float64
			for _, sample := range wave {
				sum += float64(sample) * float64(sample)
			}
			rms := math.Sqrt(sum / float64(len(wave)))
			noiseRMS := rms / math.Pow(10, targetSNRdB/20)
			rng := rand.New(rand.NewSource(20261104))
			input := make([]float32, 2413, 2413+len(wave))
			for _, sample := range wave {
				noise := rng.NormFloat64() * noiseRMS
				input = append(input, sample+float32(noise))
			}
			dec, err := NewStreamDecoder(profile, 44100)
			if err != nil {
				t.Fatal(err)
			}
			var got []ReceivedPacket
			for i := 0; i < len(input); i += 960 {
				part, e := dec.Feed(input[i:min(len(input), i+960)])
				if e != nil {
					t.Fatal(e)
				}
				got = append(got, part...)
			}
			if len(got) != len(packets) {
				t.Fatalf("at %.0f dB SNR decoded %d/%d packets", targetSNRdB, len(got), len(packets))
			}
		})
	}
}
