package gifmode

import (
	"image"
	"image/color"
	"testing"
)

func TestPredictMotionProducesCompactVectorForTranslatedSprite(t *testing.T) {
	previous := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			previous.SetRGBA(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 239), B: uint8((x*3 + y*7) % 241), A: 255})
		}
	}
	current := cloneRGBA(previous)
	const sx, sy, w, h, dx, dy = 4, 4, 4, 3, 2, 1
	// Draw a source-independent but spatially unique sprite pattern at the new
	// location in both frames via a separate image, then compare translated MCUs.
	sprite := image.NewRGBA(image.Rect(0, 0, w*16, h*16))
	for y := 0; y < h*16; y++ {
		for x := 0; x < w*16; x++ {
			sprite.SetRGBA(x, y, color.RGBA{R: uint8((x*11 + y*3) % 255), G: uint8((x*5 + y*17) % 255), B: uint8((x*7 + y*13) % 255), A: 255})
		}
	}
	// Previous frame has the sprite at its old position.
	for y := 0; y < h*16; y++ {
		for x := 0; x < w*16; x++ {
			previous.SetRGBA(sx*16+x, sy*16+y, sprite.RGBAAt(x, y))
			current.SetRGBA((sx+dx)*16+x, (sy+dy)*16+y, sprite.RGBAAt(x, y))
		}
	}
	_, vectors, predictedMCUs := PredictMotion(previous, current)
	if predictedMCUs == 0 || len(vectors) == 0 {
		t.Fatal("expected motion vectors for translated sprite")
	}
	found := false
	for _, vector := range vectors {
		if int(vector.SourceX) == sx && int(vector.SourceY) == sy && int(vector.Width) == w && int(vector.Height) == h && int(vector.DeltaX) == dx && int(vector.DeltaY) == dy {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected vector (%d,%d %dx%d -> +%d,+%d), got %+v", sx, sy, w, h, dx, dy, vectors)
	}
}
