package transport

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/gifmode"
)

func makeLongMARYGIFPackets(t *testing.T) [][]byte {
	t.Helper()
	frame := image.NewRGBA(image.Rect(0, 0, 640, 480))
	// Deterministic high-detail content approximates the difficult JPEG/SSDV
	// packet distribution seen with photographic GIF frames.
	var state uint32 = 0x6d2b79f5
	for y := 0; y < frame.Bounds().Dy(); y++ {
		for x := 0; x < frame.Bounds().Dx(); x++ {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			frame.SetRGBA(x, y, color.RGBA{R: uint8(state), G: uint8(state >> 8), B: uint8(state >> 16), A: 255})
		}
	}
	packets, _, err := gifmode.Encode([]gifmode.Frame{{Image: frame, DelayCS: 12}}, "N0CALL", 171, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) < 70 {
		t.Fatalf("fixture generated only %d GIF packets, want at least 70", len(packets))
	}
	// Match the 60–70 data-packet size exercised by the desktop screenshots.
	return packets[:70]
}

func resampleMARYClockOffset(input []float32, ppm float64) []float32 {
	step := 1 + ppm*1e-6
	out := make([]float32, 0, int(float64(len(input))/step))
	for position := 0.0; position < float64(len(input)-1); position += step {
		index := int(position)
		fraction := float32(position - float64(index))
		out = append(out, input[index]*(1-fraction)+input[index+1]*fraction)
	}
	return append(out, make([]float32, 64)...)
}

func addMARYLoopbackNoise(input []float32, snrDB float64, seed int64) []float32 {
	var signalPower float64
	for _, value := range input {
		signalPower += float64(value * value)
	}
	if len(input) == 0 {
		return input
	}
	signalRMS := math.Sqrt(signalPower / float64(len(input)))
	noiseRMS := signalRMS / math.Pow(10, snrDB/20)
	rng := rand.New(rand.NewSource(seed))
	out := make([]float32, len(input))
	for i, value := range input {
		out[i] = value + float32(rng.NormFloat64()*noiseRMS)
	}
	return out
}

func TestMARYLongGIFLoopbackAutoDetection(t *testing.T) {
	packets := makeLongMARYGIFPackets(t)
	t.Logf("GIF source packets=%d", len(packets))
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			encoder, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000, MARYBurstGroupSize: 1})
			if err != nil {
				t.Fatal(err)
			}
			var waveform []float32
			if _, err := encoder.StreamPackets(context.Background(), packets, func(block []float32) error {
				waveform = append(waveform, block...)
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			// One 70-packet automatic-detection run per constellation keeps CI
			// bounded while testing a substantial positive clock mismatch. The
			// shorter sound-card clock test covers both polarities for all modes.
			ppm := 500.0
			t.Run(fmt.Sprintf("clock_%+.0fppm", ppm), func(t *testing.T) {
				loopback := resampleMARYClockOffset(waveform, ppm)
				loopback = addMARYLoopbackNoise(loopback, 24, int64(0x4d415259+profileIndex(profile)*100+int(ppm)))
				auto, err := NewAutoStreamDecoder(48000)
				if err != nil {
					t.Fatal(err)
				}
				seen := make(map[uint16]bool)
				multi := 0
				for offset := 0; offset < len(loopback); offset += 997 {
					end := min(offset+997, len(loopback))
					decoded, err := auto.Feed(loopback[offset:end])
					if err != nil {
						t.Fatalf("Feed offset=%d: %v", offset, err)
					}
					if len(decoded) > 1 {
						multi++
					}
					for _, packet := range decoded {
						seen[packet.PacketID] = true
					}
				}
				expected := make(map[uint16]bool)
				for _, raw := range packets {
					expected[uint16(raw[7])<<8|uint16(raw[8])] = true
				}
				missing := make([]uint16, 0)
				for id := range expected {
					if !seen[id] {
						missing = append(missing, id)
					}
				}
				diagnostics := auto.Diagnostics()
				if len(missing) != 0 {
					t.Fatalf("auto loopback recovered %d/%d unique image packets; missing=%v multi-packet callbacks=%d diagnostics=%+v", len(seen), len(expected), missing[:min(12, len(missing))], multi, diagnostics)
				}
				if multi != 0 {
					t.Fatalf("one audio callback delivered multiple image packets %d times", multi)
				}
				locked := false
				fecOK, fecBad, selectedSamples := uint64(0), uint64(0), uint64(0)
				maxOtherSamples := uint64(0)
				for _, candidate := range diagnostics.MARYCandidates {
					if candidate.Profile == string(profile) {
						locked = candidate.DecodedPackets == uint64(len(seen))
						fecOK, fecBad = candidate.FECChunksOK, candidate.FECChunksBad
						selectedSamples = candidate.InputSamples
					} else if candidate.InputSamples > maxOtherSamples {
						maxOtherSamples = candidate.InputSamples
					}
				}
				if !locked || fecOK+fecBad < uint64(len(packets)) || fecBad > 3 {
					t.Fatalf("selected MARY branch did not preserve packet/FEC recovery: locked=%v FEC=%d/%d; diagnostics=%+v", locked, fecOK, fecBad, diagnostics)
				}
				if selectedSamples > 0 && maxOtherSamples > selectedSamples/8 {
					t.Fatalf("automatic detector continued feeding inactive MARY candidates: selected=%d samples, other=%d; diagnostics=%+v", selectedSamples, maxOtherSamples, diagnostics)
				}
				t.Logf("clock=%+.0f ppm SNR=24 dB recovered %d/%d image IDs; callbacks with >1 packet=%d; FEC=%d/%d; selected samples=%d other max=%d", ppm, len(seen), len(expected), multi, fecOK, fecBad, selectedSamples, maxOtherSamples)
			})
		})
	}
}

func profileIndex(profile domain.TransportMode) int {
	switch profile {
	case domain.SSDVQPSK1600:
		return 1
	case domain.SSDV8PSK1600:
		return 2
	case domain.SSDV16QAM1600:
		return 3
	default:
		return 0
	}
}
