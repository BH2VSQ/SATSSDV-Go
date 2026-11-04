package pcsi

import (
	"math"
	"testing"
)

func TestPCSIObjectiveUsesReferenceColumnMajorCoefficients(t *testing.T) {
	const width, height = 5, 3
	spatial := make([]float64, width*height)
	observations := make([]uint8, len(spatial))
	positions := make([]int, len(spatial))
	for i := range spatial {
		spatial[i] = float64((i*17)%251) + 0.25
		observations[i] = 0
		positions[i] = i
	}
	matrixCoefficients, err := DCT2(spatial, width, height)
	if err != nil {
		t.Fatal(err)
	}
	// PCSI.py uses x.reshape((width, height)).T, so x is the transpose
	// flattening of the row-major DCT matrix.
	coefficients := make([]float64, len(spatial))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			coefficients[x*height+y] = matrixCoefficients[y*width+x]
		}
	}
	objective, err := newPCSIObjective(width, height, positions, observations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := objective.evaluate(coefficients, true); err != nil {
		t.Fatal(err)
	}
	for i, want := range spatial {
		if delta := math.Abs(objective.spatial[i] - want); delta > 1e-8 {
			t.Fatalf("reference coefficient layout reconstructed wrong spatial sample %d: got %.9f want %.9f", i, objective.spatial[i], want)
		}
	}
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			idx := x*height + y
			want := 2 * matrixCoefficients[y*width+x]
			if delta := math.Abs(objective.gradient[idx] - want); delta > 1e-7 {
				t.Fatalf("reference gradient layout mismatch at %d: got %.9f want %.9f", idx, objective.gradient[idx], want)
			}
		}
	}
}

func TestOWLQNCoefficientCanMoveBackTowardZero(t *testing.T) {
	// For a one-sample 1x1 DCT, the exact minimizer of
	// (x-100)^2 + 5*|x| is x=97.5. The first gradient step overshoots to
	// 200; OWL-QN must then be allowed to move back toward zero.
	spatial, coefficients, err := reconstructChannel(1, 1, []int{0}, []uint8{100}, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(spatial) != 1 || len(coefficients) != 1 {
		t.Fatalf("unexpected optimizer output: spatial=%v coefficients=%v", spatial, coefficients)
	}
	if math.Abs(spatial[0]-97.5) > 0.1 {
		t.Fatalf("OWL-QN did not converge to the analytic solution: got %.6f want 97.5", spatial[0])
	}
}
