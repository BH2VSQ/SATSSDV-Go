package dsp

import (
	"errors"
	"math"
)

type SpectrumBin struct {
	FrequencyHz   float64 `json:"frequencyHz"`
	MagnitudeDBFS float64 `json:"magnitudeDbfs"`
	Real          float64 `json:"real"`
	Imag          float64 `json:"imag"`
}

// Spectrum computes a Hann-windowed real-input FFT. Input is never modified.
func Spectrum(samples []float32, sampleRate int) ([]SpectrumBin, error) {
	n := len(samples)
	if n < 2 || n&(n-1) != 0 {
		return nil, errors.New("FFT input length must be a power of two and at least 2")
	}
	if sampleRate <= 0 {
		return nil, errors.New("sample rate must be positive")
	}
	re := make([]float64, n)
	im := make([]float64, n)
	for i, v := range samples {
		w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
		re[i] = float64(v) * w
	}
	fft(re, im)
	out := make([]SpectrumBin, n/2+1)
	windowGain := float64(n) / 2
	for k := range out {
		scale := 1.0
		if k != 0 && k != n/2 {
			scale = 2
		}
		real := re[k] / windowGain * scale
		imag := im[k] / windowGain * scale
		mag := math.Hypot(real, imag)
		db := 20 * math.Log10(math.Max(mag, 1e-12))
		out[k] = SpectrumBin{
			FrequencyHz:   float64(k) * float64(sampleRate) / float64(n),
			MagnitudeDBFS: db, Real: real, Imag: imag,
		}
	}
	return out, nil
}

func fft(re, im []float64) {
	n := len(re)
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		angle := -2 * math.Pi / float64(size)
		wr0, wi0 := math.Cos(angle), math.Sin(angle)
		for start := 0; start < n; start += size {
			wr, wi := 1.0, 0.0
			for k := 0; k < half; k++ {
				even := start + k
				odd := even + half
				tr := wr*re[odd] - wi*im[odd]
				ti := wr*im[odd] + wi*re[odd]
				re[odd] = re[even] - tr
				im[odd] = im[even] - ti
				re[even] += tr
				im[even] += ti
				wr, wi = wr*wr0-wi*wi0, wr*wi0+wi*wr0
			}
		}
	}
}
