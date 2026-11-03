package dsp

import (
	"errors"
	"math"
)

// ResampleLinear converts mono PCM between rates with a linear interpolation
// resampler. It is intended for adapting the fixed modem rates to the selected
// shared audio-device rate; modem decoding should use its native profile rate.
func ResampleLinear(input []float32, sourceRate, targetRate int) ([]float32, error) {
	if sourceRate <= 0 || targetRate <= 0 {
		return nil, errors.New("sample rates must be positive")
	}
	if len(input) == 0 {
		return nil, errors.New("input PCM is empty")
	}
	if sourceRate == targetRate {
		return append([]float32(nil), input...), nil
	}
	if len(input) == 1 {
		return []float32{input[0]}, nil
	}
	outLen := int(math.Round(float64(len(input)) * float64(targetRate) / float64(sourceRate)))
	if outLen < 1 {
		outLen = 1
	}
	out := make([]float32, outLen)
	step := float64(sourceRate) / float64(targetRate)
	for i := range out {
		pos := float64(i) * step
		left := int(pos)
		if left >= len(input)-1 {
			out[i] = input[len(input)-1]
			continue
		}
		frac := float32(pos - float64(left))
		out[i] = input[left]*(1-frac) + input[left+1]*frac
	}
	return out, nil
}
