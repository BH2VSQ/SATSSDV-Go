package dsp

import (
	"math"
	"testing"
)

func TestSpectrumFinds1500HzTone(t *testing.T) {
	const n = 2048
	const rate = 48000
	in := make([]float32, n)
	for i := range in {
		in[i] = float32(.6 * math.Sin(2*math.Pi*1500*float64(i)/rate))
	}
	bins, e := Spectrum(in, rate)
	if e != nil {
		t.Fatal(e)
	}
	peak := 0
	for i := 1; i < len(bins); i++ {
		if bins[i].MagnitudeDBFS > bins[peak].MagnitudeDBFS {
			peak = i
		}
	}
	if d := math.Abs(bins[peak].FrequencyHz - 1500); d > rate/float64(n) {
		t.Fatalf("peak %.2f Hz", bins[peak].FrequencyHz)
	}
}
func TestSpectrumRejectsInvalidLength(t *testing.T) {
	if _, e := Spectrum([]float32{1, 2, 3}, 48000); e == nil {
		t.Fatal("expected error")
	}
}

func TestSpectrumExposesComplexFFTComponents(t *testing.T) {
	const n = 2048
	const rate = 48000
	in := make([]float32, n)
	for i := range in {
		in[i] = float32(.6 * math.Sin(2*math.Pi*1500*float64(i)/rate))
	}
	bins, err := Spectrum(in, rate)
	if err != nil {
		t.Fatal(err)
	}
	peak := 0
	for i := 1; i < len(bins); i++ {
		if bins[i].MagnitudeDBFS > bins[peak].MagnitudeDBFS {
			peak = i
		}
	}
	bin := bins[peak]
	if math.Hypot(bin.Real, bin.Imag) < .5 {
		t.Fatalf("complex FFT magnitude = %.3f, want > .5", math.Hypot(bin.Real, bin.Imag))
	}
	if math.IsNaN(bin.Real) || math.IsNaN(bin.Imag) || math.IsInf(bin.Real, 0) || math.IsInf(bin.Imag, 0) {
		t.Fatalf("invalid complex FFT coordinates: (%v, %v)", bin.Real, bin.Imag)
	}
}
