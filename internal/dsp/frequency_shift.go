package dsp

import (
	"errors"
	"math"
)

// ShiftFrequency translates a real audio waveform by offsetHz using an
// analytic-signal FFT shift. It is intended for offline Doppler/IF tolerance
// testing and capture diagnostics, not the real-time audio callback.
func ShiftFrequency(samples []float32, sampleRate int, offsetHz float64) ([]float32, error) {
	return ShiftFrequencyRamp(samples, sampleRate, offsetHz, offsetHz)
}

// ShiftFrequencyRamp applies a linearly changing audio-IF offset. It is useful
// for testing the slow frequency drift seen over a satellite pass, whereas
// ShiftFrequency models a fixed receiver tuning error.
func ShiftFrequencyRamp(samples []float32, sampleRate int, startHz, endHz float64) ([]float32, error) {
	if len(samples) == 0 {
		return nil, errors.New("frequency shift input is empty")
	}
	if sampleRate <= 0 {
		return nil, errors.New("sample rate must be positive")
	}
	for _, offsetHz := range []float64{startHz, endHz} {
		if math.IsNaN(offsetHz) || math.IsInf(offsetHz, 0) || math.Abs(offsetHz) >= float64(sampleRate)/2 {
			return nil, errors.New("frequency shift endpoints must be finite and below Nyquist")
		}
	}
	if startHz == 0 && endHz == 0 {
		return append([]float32(nil), samples...), nil
	}

	pad := sampleRate / 4
	if pad > 8192 {
		pad = 8192
	}
	total := len(samples) + 2*pad
	n := 1
	for n < total {
		n <<= 1
	}
	re := make([]float64, n)
	im := make([]float64, n)
	copy(re[pad:pad+len(samples)], float64Samples(samples))
	fft(re, im)
	for k := 1; k < n/2; k++ {
		re[k] *= 2
		im[k] *= 2
	}
	for k := n/2 + 1; k < n; k++ {
		re[k], im[k] = 0, 0
	}
	for i := range im {
		im[i] = -im[i]
	}
	fft(re, im)
	invN := 1 / float64(n)
	for i := range re {
		re[i] *= invN
		im[i] *= -invN
	}

	out := make([]float32, len(samples))
	phase := 0.0
	for i := range out {
		t := 0.0
		if len(out) > 1 {
			t = float64(i) / float64(len(out)-1)
		}
		offsetHz := startHz + (endHz-startHz)*t
		if i > 0 {
			prevT := float64(i-1) / float64(max(1, len(out)-1))
			prevHz := startHz + (endHz-startHz)*prevT
			phase += 2 * math.Pi * (prevHz + offsetHz) * 0.5 / float64(sampleRate)
		}
		c, s := math.Cos(phase), math.Sin(phase)
		idx := pad + i
		out[i] = float32(re[idx]*c - im[idx]*s)
	}
	return out, nil
}

func float64Samples(src []float32) []float64 {
	out := make([]float64, len(src))
	for i, v := range src {
		out[i] = float64(v)
	}
	return out
}
