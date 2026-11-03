package imagequality

import (
	"errors"
	"image"
	"image/color"
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
	size, e := Dimensions(p)
	if e != nil {
		return nil, e
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return nil, errors.New("source image is empty")
	}
	scale := math.Max(float64(size.Width)/float64(sw), float64(size.Height)/float64(sh))
	cropW := float64(size.Width) / scale
	cropH := float64(size.Height) / scale
	left := float64(b.Min.X) + (float64(sw)-cropW)/2
	top := float64(b.Min.Y) + (float64(sh)-cropH)/2
	dst := image.NewRGBA(image.Rect(0, 0, size.Width, size.Height))
	for y := 0; y < size.Height; y++ {
		sy := top + (float64(y)+.5)/scale - .5
		y0 := int(math.Floor(sy))
		fy := sy - float64(y0)
		for x := 0; x < size.Width; x++ {
			sx := left + (float64(x)+.5)/scale - .5
			x0 := int(math.Floor(sx))
			fx := sx - float64(x0)
			c00 := color.RGBAModel.Convert(src.At(clamp(x0, b.Min.X, b.Max.X-1), clamp(y0, b.Min.Y, b.Max.Y-1))).(color.RGBA)
			c10 := color.RGBAModel.Convert(src.At(clamp(x0+1, b.Min.X, b.Max.X-1), clamp(y0, b.Min.Y, b.Max.Y-1))).(color.RGBA)
			c01 := color.RGBAModel.Convert(src.At(clamp(x0, b.Min.X, b.Max.X-1), clamp(y0+1, b.Min.Y, b.Max.Y-1))).(color.RGBA)
			c11 := color.RGBAModel.Convert(src.At(clamp(x0+1, b.Min.X, b.Max.X-1), clamp(y0+1, b.Min.Y, b.Max.Y-1))).(color.RGBA)
			dst.SetRGBA(x, y, color.RGBA{R: mix4(c00.R, c10.R, c01.R, c11.R, fx, fy), G: mix4(c00.G, c10.G, c01.G, c11.G, fx, fy), B: mix4(c00.B, c10.B, c01.B, c11.B, fx, fy), A: 255})
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
func mix4(a, b, c, d uint8, x, y float64) uint8 {
	v0 := float64(a)*(1-x) + float64(b)*x
	v1 := float64(c)*(1-x) + float64(d)*x
	v := v0*(1-y) + v1*y
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(math.Round(v))
}
