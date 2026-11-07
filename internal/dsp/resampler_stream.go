package dsp

import (
	"errors"
	"math"
)

// StreamResampler performs linear interpolation while preserving fractional
// source position between calls. One source sample is retained as the right
// interpolation neighbour, so arbitrary audio callback boundaries are safe.
type StreamResampler struct {
	sourceRate  int
	targetRate  int
	step        float64
	position    float64
	pending     []float32
	totalInput  int64
	totalOutput int64
}

func NewStreamResampler(sourceRate, targetRate int) (*StreamResampler, error) {
	if sourceRate <= 0 || targetRate <= 0 {
		return nil, errors.New("sample rates must be positive")
	}
	return &StreamResampler{
		sourceRate: sourceRate,
		targetRate: targetRate,
		step:       float64(sourceRate) / float64(targetRate),
	}, nil
}

func (r *StreamResampler) Reset() {
	if r == nil {
		return
	}
	r.position = 0
	r.pending = nil
	r.totalInput = 0
	r.totalOutput = 0
}

func (r *StreamResampler) Feed(input []float32) []float32 {
	if r == nil || len(input) == 0 {
		return nil
	}
	if r.sourceRate == r.targetRate {
		r.totalInput += int64(len(input))
		r.totalOutput += int64(len(input))
		return append([]float32(nil), input...)
	}
	r.totalInput += int64(len(input))
	r.pending = append(r.pending, input...)
	if len(r.pending) < 2 {
		return nil
	}
	capacity := int(float64(len(input))*float64(r.targetRate)/float64(r.sourceRate)) + 2
	out := make([]float32, 0, capacity)
	for r.position+1 < float64(len(r.pending)) {
		left := int(r.position)
		fraction := float32(r.position - float64(left))
		out = append(out, r.pending[left]*(1-fraction)+r.pending[left+1]*fraction)
		r.totalOutput++
		r.position += r.step
	}
	consumed := int(r.position)
	if consumed > 0 {
		if consumed >= len(r.pending) {
			consumed = len(r.pending) - 1
		}
		copy(r.pending, r.pending[consumed:])
		r.pending = r.pending[:len(r.pending)-consumed]
		r.position -= float64(consumed)
	}
	return out
}

// Flush emits the final edge-held samples so the output length is exactly
// round(totalInput*targetRate/sourceRate), matching ResampleLinear. It must be
// called once after the final Feed.
func (r *StreamResampler) Flush() []float32 {
	if r == nil || r.sourceRate <= 0 || r.targetRate <= 0 || len(r.pending) == 0 {
		return nil
	}
	want := int64(math.Round(float64(r.totalInput) * float64(r.targetRate) / float64(r.sourceRate)))
	remaining := want - r.totalOutput
	if remaining <= 0 {
		r.pending = nil
		return nil
	}
	last := r.pending[len(r.pending)-1]
	out := make([]float32, int(remaining))
	for i := range out {
		out[i] = last
	}
	r.totalOutput = want
	r.pending = nil
	return out
}
