package dsp

import (
	"math"
	"math/rand"
	"testing"
)

func TestStreamResamplerMatchesLinear(t *testing.T) {
	for _, rates := range [][2]int{{19200, 44100}, {19200, 48000}, {48000, 44100}, {44100, 48000}} {
		n := 100003
		src := make([]float32, n)
		r := rand.New(rand.NewSource(1))
		for i := range src {
			src[i] = r.Float32()*2 - 1
		}
		want, _ := ResampleLinear(src, rates[0], rates[1])
		s, _ := NewStreamResampler(rates[0], rates[1])
		var got []float32
		for p := 0; p < n; p += 4096 {
			e := p + 4096
			if e > n {
				e = n
			}
			got = append(got, s.Feed(src[p:e])...)
		}
		got = append(got, s.Flush()...)
		if len(got) != len(want) {
			t.Fatalf("rates %v length %d != %d", rates, len(got), len(want))
		}
		var max float64
		at := 0
		for i := range got {
			d := math.Abs(float64(got[i] - want[i]))
			if d > max {
				max = d
				at = i
			}
		}
		if max > 1e-6 {
			t.Fatalf("rates %v max=%g at %d got=%f want=%f", rates, max, at, got[at], want[at])
		}
	}
}
