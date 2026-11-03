package imagequality

import (
	"image"
	"image/color"
	"testing"
)

func TestPresets(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 100))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	cases := []struct {
		p    Preset
		w, h int
	}{{Low, 320, 240}, {Medium, 448, 336}, {High, 640, 480}}
	for _, c := range cases {
		img, e := ResizeCover(src, c.p)
		if e != nil {
			t.Fatal(e)
		}
		if img.Bounds().Dx() != c.w || img.Bounds().Dy() != c.h {
			t.Fatalf("%s dimensions=%v", c.p, img.Bounds())
		}
	}
}
func TestInvalidPreset(t *testing.T) {
	if _, e := Dimensions("ultra"); e == nil {
		t.Fatal("expected invalid preset")
	}
}
