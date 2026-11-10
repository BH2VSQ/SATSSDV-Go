package mary

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

func resampleForClockOffset(input []float32, ppm float64) []float32 {
	step := 1 + ppm*1e-6
	out := make([]float32, 0, int(float64(len(input))/step))
	for position := 0.0; position < float64(len(input)-1); position += step {
		index := int(position)
		fraction := float32(position - float64(index))
		out = append(out, input[index]*(1-fraction)+input[index+1]*fraction)
	}
	return out
}

func TestMAModulationSampleClockOffsetRecovery(t *testing.T) {
	payload := make([]byte, 768)
	rng := rand.New(rand.NewSource(0x53434c4b))
	if _, err := rng.Read(payload); err != nil {
		t.Fatal(err)
	}
	for _, bps := range []int{2, 3, 4} {
		for _, carrierOffset := range []int{-200, 0, 200} {
			for _, ppm := range []float64{-750, -500, -100, 100, 500, 750} {
				t.Run(fmt.Sprintf("bps%d_cfo%+d_ppm%+.0f", bps, carrierOffset, ppm), func(t *testing.T) {
					txConfig := Config{SampleRate: 48000, SymbolRate: 1600, BitsPerSymbol: bps, CarrierHz: 1600 + carrierOffset}
					rxConfig := txConfig
					rxConfig.CarrierHz = 1600
					wave, err := ModulateBytes(payload, txConfig)
					if err != nil {
						t.Fatal(err)
					}
					wave = resampleForClockOffset(wave, ppm)
					wave = append(wave, make([]float32, 64)...)
					input := make([]float32, 13+12000, 13+12000+len(wave))
					input = append(input, wave...)
					demod, err := NewStreamDemodulator(rxConfig)
					if err != nil {
						t.Fatal(err)
					}
					var bits []bool
					for offset := 0; offset < len(input); offset += 997 {
						end := min(len(input), offset+997)
						bits = append(bits, demod.Feed(input[offset:end])...)
					}
					got := bitsToBytes(bits)
					if len(got) < len(payload) {
						t.Fatalf("clock-offset demod recovered %d/%d bytes (ppm=%+.0f CFO=%+d); diagnostics period=%.8f locked=%v", len(got), len(payload), ppm, carrierOffset, demod.samplesPerSymbol, demod.locked)
					}
					for i := range payload {
						if got[i] != payload[i] {
							t.Fatalf("byte %d mismatch (ppm=%+.0f CFO=%+d): got %02x want %02x; timing period=%.8f", i, ppm, carrierOffset, got[i], payload[i], demod.samplesPerSymbol)
						}
					}
					expectedPPM := (1/(1+ppm*1e-6) - 1) * 1e6
					measuredPPM := (demod.samplesPerSymbol/30 - 1) * 1e6
					if math.Abs(measuredPPM-expectedPPM) > 125 {
						t.Fatalf("estimated clock offset %.1f ppm, expected %.1f ppm", measuredPPM, expectedPPM)
					}
				})
			}
		}
	}
}
