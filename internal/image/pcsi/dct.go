package pcsi

import (
	"errors"
	"math"
	"math/cmplx"
)

// dctPlan reuses every work buffer across all rows/columns. PCSI's reference
// image sizes are large enough that allocating recursive FFT trees for every
// line would otherwise create gigabytes of short-lived garbage per preview.
type dctPlan struct {
	n         int
	fftSize   int
	roots     []complex128
	rotation  []complex128
	fftInput  []complex128
	fftOutput []complex128
	scratch   []complex128
	lineIn    []float64
	lineOut   []float64
}

func newDCTPlan(n int) *dctPlan {
	p := &dctPlan{n: n, fftSize: 2 * n}
	p.roots = make([]complex128, p.fftSize)
	for k := range p.roots {
		p.roots[k] = cmplx.Rect(1, -2*math.Pi*float64(k)/float64(p.fftSize))
	}
	p.rotation = make([]complex128, n)
	for k := range p.rotation {
		p.rotation[k] = cmplx.Rect(1, -math.Pi*float64(k)/(2*float64(n)))
	}
	p.fftInput = make([]complex128, p.fftSize)
	p.fftOutput = make([]complex128, p.fftSize)
	p.scratch = make([]complex128, p.fftSize)
	p.lineIn = make([]float64, n)
	p.lineOut = make([]float64, n)
	return p
}

func (p *dctPlan) transform(src, dst []float64, inverse bool) {
	if !inverse {
		for i, v := range src {
			p.fftInput[i] = complex(v, 0)
			p.fftInput[p.fftSize-1-i] = complex(v, 0)
		}
		fftInto(p.fftInput, 0, 1, p.fftSize, p.fftOutput, 0, p.scratch, p.roots, p.fftSize, false)
		for k := 0; k < p.n; k++ {
			value := real(p.fftOutput[k]*p.rotation[k]) * 0.5
			if k == 0 {
				dst[k] = value / math.Sqrt(float64(p.n))
			} else {
				dst[k] = value * math.Sqrt(2/float64(p.n))
			}
		}
		return
	}

	clear(p.fftInput)
	p.fftInput[0] = complex(2*src[0]*math.Sqrt(float64(p.n)), 0)
	for k := 1; k < p.n; k++ {
		dctValue := src[k] * math.Sqrt(float64(p.n)/2)
		v := 2 * complex(dctValue, 0) * cmplx.Conj(p.rotation[k])
		p.fftInput[k] = v
		p.fftInput[p.fftSize-k] = cmplx.Conj(v)
	}
	p.fftInput[p.n] = 0
	fftInto(p.fftInput, 0, 1, p.fftSize, p.fftOutput, 0, p.scratch, p.roots, p.fftSize, true)
	for i := 0; i < p.n; i++ {
		dst[i] = real(p.fftOutput[i]) / float64(p.fftSize)
	}
}

// dct1 and idct1 remain small convenience wrappers for unit tests and
// reference-vector checks. Hot 2-D reconstruction uses reusable plans.
func dct1(src []float64) []float64 {
	if len(src) == 0 {
		return nil
	}
	plan := newDCTPlan(len(src))
	out := make([]float64, len(src))
	plan.transform(src, out, false)
	return out
}

func idct1(src []float64) []float64 {
	if len(src) == 0 {
		return nil
	}
	plan := newDCTPlan(len(src))
	out := make([]float64, len(src))
	plan.transform(src, out, true)
	return out
}

// DCT2 computes a separable orthonormal 2-D DCT-II in row-major order.
func DCT2(src []float64, width, height int) ([]float64, error) {
	return transform2D(src, width, height, false)
}

// IDCT2 computes a separable inverse orthonormal 2-D DCT-III in row-major order.
func IDCT2(src []float64, width, height int) ([]float64, error) {
	return transform2D(src, width, height, true)
}

func transform2D(src []float64, width, height int, inverse bool) ([]float64, error) {
	if width <= 0 || height <= 0 || len(src) != width*height {
		return nil, errors.New("DCT input dimensions do not match the supplied data")
	}
	out := append([]float64(nil), src...)
	rowPlan, colPlan := newDCTPlan(width), newDCTPlan(height)
	for y := 0; y < height; y++ {
		start := y * width
		copy(rowPlan.lineIn, out[start:start+width])
		rowPlan.transform(rowPlan.lineIn, rowPlan.lineOut, inverse)
		copy(out[start:start+width], rowPlan.lineOut)
	}
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			colPlan.lineIn[y] = out[y*width+x]
		}
		colPlan.transform(colPlan.lineIn, colPlan.lineOut, inverse)
		for y := 0; y < height; y++ {
			out[y*width+x] = colPlan.lineOut[y]
		}
	}
	return out, nil
}

// fftInto is a mixed-radix decimation-in-time transform. All output and
// scratch storage is owned by dctPlan, so its recursion performs no allocation.
func fftInto(input []complex128, inputOffset, stride, n int, output []complex128, outputOffset int, scratch, roots []complex128, baseN int, inverse bool) {
	if n <= 1 {
		output[outputOffset] = input[inputOffset]
		return
	}
	radix := smallestFactor(n)
	if radix == n {
		for k := 0; k < n; k++ {
			var sum complex128
			for j := 0; j < n; j++ {
				rootIndex := (j * k * (baseN / n)) % baseN
				root := roots[rootIndex]
				if inverse {
					root = cmplx.Conj(root)
				}
				sum += input[inputOffset+j*stride] * root
			}
			output[outputOffset+k] = sum
		}
		return
	}

	m := n / radix
	for j := 0; j < radix; j++ {
		fftInto(input, inputOffset+j*stride, stride*radix, m, output, outputOffset+j*m, scratch, roots, baseN, inverse)
	}
	for k := 0; k < n; k++ {
		k0 := k % m
		var sum complex128
		for j := 0; j < radix; j++ {
			rootIndex := (j * k * (baseN / n)) % baseN
			root := roots[rootIndex]
			if inverse {
				root = cmplx.Conj(root)
			}
			sum += output[outputOffset+j*m+k0] * root
		}
		scratch[k] = sum
	}
	copy(output[outputOffset:outputOffset+n], scratch[:n])
}

func smallestFactor(n int) int {
	if n%2 == 0 {
		return 2
	}
	for p := 3; p*p <= n; p += 2 {
		if n%p == 0 {
			return p
		}
	}
	return n
}
