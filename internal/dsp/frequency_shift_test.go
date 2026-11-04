package dsp

import (
	"math"
	"testing"
)

func TestShiftFrequencyMovesSineTone(t *testing.T) {
	const rate = 19200
	src := make([]float32, 19200)
	for i := range src {
		src[i] = float32(0.6 * math.Sin(2*math.Pi*1500*float64(i)/rate))
	}
	shifted, err := ShiftFrequency(src, rate, 125)
	if err != nil {
		t.Fatal(err)
	}
	bins, err := Spectrum(shifted[2048:18432], rate)
	if err != nil {
		t.Fatal(err)
	}
	peak := bins[0]
	for _, bin := range bins[1:] {
		if bin.MagnitudeDBFS > peak.MagnitudeDBFS {
			peak = bin
		}
	}
	if math.Abs(peak.FrequencyHz-1625) > 2 {
		t.Fatalf("frequency-shifted peak %.1f Hz, want 1625 Hz", peak.FrequencyHz)
	}
}

func TestShiftFrequencyRampChangesOffsetContinuously(t *testing.T) {
	const rate = 19200
	src := make([]float32, rate)
	for i := range src {
		src[i] = float32(0.5 * math.Sin(2*math.Pi*1500*float64(i)/rate))
	}
	shifted, err := ShiftFrequencyRamp(src, rate, 200, -200)
	if err != nil {
		t.Fatal(err)
	}
	if len(shifted) != len(src) {
		t.Fatalf("ramp length %d != source length %d", len(shifted), len(src))
	}
	if shifted[rate/4] == src[rate/4] || shifted[3*rate/4] == src[3*rate/4] {
		t.Fatal("frequency-ramped waveform is unchanged at expected offset checkpoints")
	}
}
