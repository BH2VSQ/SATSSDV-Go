package dsp

import (
	"math"
	"testing"
)

func filteredToneAmplitude(t *testing.T, frequency float64) float64 {
	t.Helper()
	const sampleRate = 19200
	const durationSamples = sampleRate
	samples := make([]float32, durationSamples)
	for i := range samples {
		samples[i] = float32(0.5 * math.Sin(2*math.Pi*frequency*float64(i)/sampleRate))
	}
	if err := FilterModulatedAudioInPlace(samples, sampleRate); err != nil {
		t.Fatal(err)
	}
	// Measure the final half-second so the causal filter's startup transient
	// does not influence the passband/stopband comparison.
	start := len(samples) / 2
	var sinSum, cosSum float64
	for i := start; i < len(samples); i++ {
		phase := 2 * math.Pi * frequency * float64(i) / sampleRate
		value := float64(samples[i])
		sinSum += value * math.Sin(phase)
		cosSum += value * math.Cos(phase)
	}
	n := float64(len(samples) - start)
	return 2 * math.Hypot(sinSum, cosSum) / n
}

func TestModulatedAudioFilterPreservesDataCarriers(t *testing.T) {
	for _, frequency := range []float64{1200, 1600, 2200, 2400} {
		got := filteredToneAmplitude(t, frequency)
		attenuationDB := 20 * math.Log10(got/0.5)
		if math.Abs(attenuationDB) > 0.2 {
			t.Fatalf("carrier %.0f Hz changed by %.3f dB (amplitude %.4f)", frequency, attenuationDB, got)
		}
	}
}

func TestModulatedAudioFilterAttenuatesOutOfBandEdges(t *testing.T) {
	low := filteredToneAmplitude(t, 60)
	high := filteredToneAmplitude(t, 8000)
	if low > 0.5*0.2 {
		t.Fatalf("60 Hz rumble amplitude %.4f was not sufficiently attenuated", low)
	}
	if high > 0.5*0.2 {
		t.Fatalf("8 kHz out-of-band amplitude %.4f was not sufficiently attenuated", high)
	}
}

func TestModulatedAudioFilterRejectsInvalidSampleRate(t *testing.T) {
	if err := FilterModulatedAudioInPlace([]float32{0, 1}, 8000); err == nil {
		t.Fatal("expected invalid Nyquist/passband error at 8 kHz")
	}
}
