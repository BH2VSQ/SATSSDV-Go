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

func BenchmarkResizeCoverPresets(b *testing.B) {
	src := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	for _, preset := range []Preset{Low, Medium, High} {
		b.Run(string(preset), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := ResizeCover(src, preset); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type genericImage struct{ *image.RGBA }

func TestRGBAFastPathMatchesGenericImageConversion(t *testing.T) {
	src := image.NewRGBA(image.Rect(-13, 5, 323, 245))
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x*3 + y), G: uint8(x - y*2), B: uint8(x ^ y), A: 255})
		}
	}
	for _, preset := range []Preset{Low, Medium, High} {
		fast, err := ResizeCover(src, preset)
		if err != nil {
			t.Fatal(err)
		}
		slow, err := ResizeCover(genericImage{src}, preset)
		if err != nil {
			t.Fatal(err)
		}
		if len(fast.Pix) != len(slow.Pix) {
			t.Fatalf("%s output length mismatch", preset)
		}
		for i := range fast.Pix {
			if fast.Pix[i] != slow.Pix[i] {
				t.Fatalf("%s fast RGBA path differs at byte %d: %d != %d", preset, i, fast.Pix[i], slow.Pix[i])
			}
		}
	}
}
