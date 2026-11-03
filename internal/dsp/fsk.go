package dsp

import (
	"errors"
	"math"
)

// DemodulateFSK converts a mono FSK waveform to hard-decision mark/space bits.
// It searches the integer samples-per-symbol phase and is intended as the
// first receive layer; production streaming still needs AFC and clock tracking.
func DemodulateFSK(samples []float32, sampleRate, bitRate, markHz, spaceHz int) ([]bool, int, error) {
	if sampleRate <= 0 || bitRate <= 0 || sampleRate%bitRate != 0 {
		return nil, 0, errors.New("sample rate must be divisible by bit rate")
	}
	if markHz <= 0 || spaceHz <= 0 || markHz >= sampleRate/2 || spaceHz >= sampleRate/2 || markHz == spaceHz {
		return nil, 0, errors.New("invalid FSK tone frequencies")
	}
	sps := sampleRate / bitRate
	if len(samples) < 2*sps {
		return nil, 0, errors.New("not enough samples for FSK demodulation")
	}
	bestPhase, bestScore := 0, math.Inf(-1)
	for phase := 0; phase < sps && phase+sps <= len(samples); phase++ {
		var score float64
		count := 0
		for start := phase; start+sps <= len(samples); start += sps {
			mark, space := toneEnergy(samples[start:start+sps], sampleRate, markHz), toneEnergy(samples[start:start+sps], sampleRate, spaceHz)
			score += math.Abs(mark-space) / (mark + space + 1e-18)
			count++
		}
		if count > 0 && score/float64(count) > bestScore {
			bestScore, bestPhase = score/float64(count), phase
		}
	}
	bits := make([]bool, 0, (len(samples)-bestPhase)/sps)
	for start := bestPhase; start+sps <= len(samples); start += sps {
		window := samples[start : start+sps]
		bits = append(bits, toneEnergy(window, sampleRate, markHz) >= toneEnergy(window, sampleRate, spaceHz))
	}
	return bits, bestPhase, nil
}

func toneEnergy(samples []float32, sampleRate, frequency int) float64 {
	var real, imag float64
	for i, sample := range samples {
		phase := 2 * math.Pi * float64(frequency*i) / float64(sampleRate)
		x := float64(sample)
		real += x * math.Cos(phase)
		imag -= x * math.Sin(phase)
	}
	return real*real + imag*imag
}
