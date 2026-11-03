package dsp

import "errors"

// StreamResampler performs linear interpolation while preserving fractional
// source position between calls. One source sample is retained as the right
// interpolation neighbour, so arbitrary audio callback boundaries are safe.
type StreamResampler struct {
	sourceRate int
	targetRate int
	step       float64
	position   float64
	pending    []float32
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
}

func (r *StreamResampler) Feed(input []float32) []float32 {
	if r == nil || len(input) == 0 {
		return nil
	}
	if r.sourceRate == r.targetRate {
		return append([]float32(nil), input...)
	}
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
