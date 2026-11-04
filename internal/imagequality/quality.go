package imagequality

import (
	"errors"
	"image"
	"image/draw"
	"math"
)

type Preset string

const (
	Low    Preset = "low"
	Medium Preset = "medium"
	High   Preset = "high"
)

type Size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func Dimensions(p Preset) (Size, error) {
	switch p {
	case Low:
		return Size{320, 240}, nil
	case Medium:
		return Size{448, 336}, nil
	case High:
		return Size{640, 480}, nil
	default:
		return Size{}, errors.New("quality must be low, medium or high")
	}
}

// ResizeCover performs center-crop plus bilinear scaling into the exact Hamstation preset size.
func ResizeCover(src image.Image, p Preset) (*image.RGBA, error) {
	if src == nil {
		return nil, errors.New("source image is nil")
	}
	size, err := Dimensions(p)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return nil, errors.New("source image is empty")
	}
	scale := math.Max(float64(size.Width)/float64(sw), float64(size.Height)/float64(sh))
	cropW, cropH := float64(size.Width)/scale, float64(size.Height)/scale
	left := float64(b.Min.X) + (float64(sw)-cropW)/2
	top := float64(b.Min.Y) + (float64(sh)-cropH)/2

	// Reuse an existing RGBA source instead of copying a full-frame buffer.
	// Non-RGBA sources are converted once; the hot resampling loop below reads
	// packed pixels directly and performs fixed-point bilinear interpolation.
	source, ok := src.(*image.RGBA)
	if !ok || source.Bounds() != b {
		converted := image.NewRGBA(b)
		draw.Draw(converted, b, src, b.Min, draw.Src)
		source = converted
	}
	dst := image.NewRGBA(image.Rect(0, 0, size.Width, size.Height))
	type axisSample struct {
		a, b     int
		fraction uint32
	}
	xSamples := make([]axisSample, size.Width)
	for x := 0; x < size.Width; x++ {
		sx := left + (float64(x)+.5)/scale - .5
		x0 := int(math.Floor(sx))
		fx := sx - float64(x0)
		xSamples[x] = axisSample{clamp(x0, b.Min.X, b.Max.X-1), clamp(x0+1, b.Min.X, b.Max.X-1), uint32(math.Round(fx * 65536))}
	}
	for y := 0; y < size.Height; y++ {
		sy := top + (float64(y)+.5)/scale - .5
		y0 := int(math.Floor(sy))
		fy := uint32(math.Round((sy - float64(y0)) * 65536))
		ya, yb := clamp(y0, b.Min.Y, b.Max.Y-1), clamp(y0+1, b.Min.Y, b.Max.Y-1)
		row0, row1 := (ya-b.Min.Y)*source.Stride, (yb-b.Min.Y)*source.Stride
		dstRow := y * dst.Stride
		for x, xs := range xSamples {
			a := row0 + (xs.a-b.Min.X)*4
			bb := row0 + (xs.b-b.Min.X)*4
			c := row1 + (xs.a-b.Min.X)*4
			d := row1 + (xs.b-b.Min.X)*4
			o := dstRow + x*4
			for channel := 0; channel < 3; channel++ {
				dst.Pix[o+channel] = mix4(source.Pix[a+channel], source.Pix[bb+channel], source.Pix[c+channel], source.Pix[d+channel], xs.fraction, fy)
			}
			dst.Pix[o+3] = 255
		}
	}
	return dst, nil
}
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func mix4(a, b, c, d uint8, x, y uint32) uint8 {
	const one = uint32(1 << 16)
	top := (uint32(a)*(one-x) + uint32(b)*x + one/2) >> 16
	bottom := (uint32(c)*(one-x) + uint32(d)*x + one/2) >> 16
	v := (top*(one-y) + bottom*y + one/2) >> 16
	if v > 255 {
		v = 255
	}
	return uint8(v)
}
