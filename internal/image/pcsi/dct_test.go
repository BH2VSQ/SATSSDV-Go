package pcsi

import (
	"math"
	"testing"
)

func TestDCT1RoundTrip(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 8, 15, 16, 24, 32, 48, 64, 80, 96, 112, 160, 224, 320, 448, 480, 640} {
		src := make([]float64, n)
		for i := range src {
			src[i] = math.Sin(float64(i)*0.17)*83 + math.Cos(float64(i)*0.071)*29
		}
		got := idct1(dct1(src))
		for i := range src {
			if math.Abs(src[i]-got[i]) > 1e-8 {
				t.Fatalf("n=%d index=%d src=%g got=%g", n, i, src[i], got[i])
			}
		}
	}
}

func TestDCT2RoundTrip(t *testing.T) {
	for _, size := range [][2]int{{16, 16}, {32, 16}, {48, 32}, {64, 48}} {
		w, h := size[0], size[1]
		src := make([]float64, w*h)
		for i := range src {
			src[i] = float64((i*37)%251) - 125
		}
		coeff, err := DCT2(src, w, h)
		if err != nil {
			t.Fatal(err)
		}
		got, err := IDCT2(coeff, w, h)
		if err != nil {
			t.Fatal(err)
		}
		for i := range src {
			if math.Abs(src[i]-got[i]) > 1e-7 {
				t.Fatalf("size=%dx%d index=%d src=%g got=%g", w, h, i, src[i], got[i])
			}
		}
	}
}

func TestDCT2MatchesOpenCVOrthonormalReference(t *testing.T) {
	src := []float64{
		0, 1, 2, 3,
		4, 5, 6, 7,
		8, 9, 10, 11,
		12, 13, 14, 15,
	}
	want := []float64{
		30, -4.460884994775328, 0, -0.31702533556221463,
		-17.843539979101308, 0, 0, 0,
		0, 0, 0, 0,
		-1.2681013422488601, 0, 0, 0,
	}
	got, err := DCT2(src, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-8 {
			t.Fatalf("OpenCV DCT reference mismatch at %d: got %.12f want %.12f", i, got[i], want[i])
		}
	}
}
