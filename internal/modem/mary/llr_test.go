package mary

import (
	"math"
	"math/rand"
	"testing"
)

func referenceConstellationLLR(z complex128, bitsPerSymbol int) []float32 {
	const clamp = 32.0
	out := make([]float32, 0, bitsPerSymbol)
	for bit := 0; bit < bitsPerSymbol; bit++ {
		minZero, minOne := math.Inf(1), math.Inf(1)
		mask := 1 << uint(bitsPerSymbol-1-bit)
		for label := 0; label < 1<<uint(bitsPerSymbol); label++ {
			var bits [4]bool
			for index := 0; index < bitsPerSymbol; index++ {
				bits[index] = label&(1<<uint(bitsPerSymbol-1-index)) != 0
			}
			point := complex128(mapBits(bits[:bitsPerSymbol], bitsPerSymbol))
			delta := z - point
			distance := real(delta)*real(delta) + imag(delta)*imag(delta)
			if label&mask == 0 {
				if distance < minZero {
					minZero = distance
				}
			} else if distance < minOne {
				minOne = distance
			}
		}
		llr := minOne - minZero
		if llr > clamp {
			llr = clamp
		}
		if llr < -clamp {
			llr = -clamp
		}
		out = append(out, float32(llr))
	}
	return out
}

func TestAppendConstellationLLRMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(0x4d415259))
	for _, bitsPerSymbol := range []int{2, 3, 4} {
		t.Run(map[int]string{2: "QPSK", 3: "8PSK", 4: "16QAM"}[bitsPerSymbol], func(t *testing.T) {
			for sample := 0; sample < 1000; sample++ {
				z := complex(rng.NormFloat64()*1.2, rng.NormFloat64()*1.2)
				want := referenceConstellationLLR(z, bitsPerSymbol)
				got := appendConstellationLLR(make([]float32, 0, bitsPerSymbol), z, bitsPerSymbol)
				for bit := range want {
					if math.Abs(float64(got[bit]-want[bit])) > 1e-6 {
						t.Fatalf("sample %d bit %d LLR mismatch: got %.8f want %.8f", sample, bit, got[bit], want[bit])
					}
				}
			}
		})
	}
}

func BenchmarkAppendConstellationLLR(b *testing.B) {
	for _, bitsPerSymbol := range []int{2, 3, 4} {
		b.Run(map[int]string{2: "QPSK", 3: "8PSK", 4: "16QAM"}[bitsPerSymbol], func(b *testing.B) {
			z := complex(0.37, -0.42)
			out := make([]float32, 0, bitsPerSymbol)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out = out[:0]
				out = appendConstellationLLR(out, z, bitsPerSymbol)
			}
		})
	}
}
