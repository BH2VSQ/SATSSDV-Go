package dsp

import (
	"math"
	"testing"
)

func TestDemodulateFSKFindsSamplePhase(t *testing.T) {
	const sampleRate, bitRate, mark, space = 19200, 1200, 1200, 2200
	bits := make([]bool, 96)
	for i := range bits {
		bits[i] = (i*13+i/3)%7 < 3
	}
	sps := sampleRate / bitRate
	samples := make([]float32, 7+len(bits)*sps+5)
	phase := 0.0
	for i, bit := range bits {
		freq := space
		if bit {
			freq = mark
		}
		inc := 2 * math.Pi * float64(freq) / sampleRate
		for j := 0; j < sps; j++ {
			samples[7+i*sps+j] = float32(.7 * math.Sin(phase))
			phase += inc
			if phase >= 2*math.Pi {
				phase -= 2 * math.Pi
			}
		}
	}
	got, offset, err := DemodulateFSK(samples, sampleRate, bitRate, mark, space)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("detected phase offset %d", offset)
	if len(got) != len(bits) {
		t.Fatalf("bits %d, want %d", len(got), len(bits))
	}
	for i := range bits {
		if got[i] != bits[i] {
			t.Fatalf("bit mismatch at %d", i)
		}
	}
}

func TestResampleLinearPreservesDurationAndEndpoints(t *testing.T) {
	input := make([]float32, 19200)
	for i := range input {
		input[i] = float32(math.Sin(2 * math.Pi * 1000 * float64(i) / 19200))
	}
	got, err := ResampleLinear(input, 19200, 48000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 48000 {
		t.Fatalf("got %d samples", len(got))
	}
	if got[0] != input[0] || got[len(got)-1] != input[len(input)-1] {
		t.Fatal("resampler endpoints differ")
	}
}
