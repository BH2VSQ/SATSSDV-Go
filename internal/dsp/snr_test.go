package dsp

import (
    "math"
    "testing"
)

func TestEstimateBandSNRFindsKnownTone(t *testing.T) {
    bins := make([]SpectrumBin, 0, 31)
    for i := 0; i <= 30; i++ {
        hz := float64(1000 + i*100)
        db := -90.0
        if hz == 1500 { db = -60 }
        bins = append(bins, SpectrumBin{FrequencyHz: hz, MagnitudeDBFS: db})
    }
    got := EstimateBandSNR(bins, 1000, 2000, 3000, 4000)
    if !got.Valid { t.Fatal("expected valid SNR") }
    if math.Abs(got.SNRDB-17.97) > 0.3 { t.Fatalf("SNR %.2f dB, want about 17.97 dB", got.SNRDB) }
}

func TestEstimateBandSNRRejectsNoiseOnly(t *testing.T) {
    bins := make([]SpectrumBin, 0, 31)
    for i := 0; i <= 30; i++ {
        bins = append(bins, SpectrumBin{FrequencyHz: float64(1000+i*100), MagnitudeDBFS: -90})
    }
    if got := EstimateBandSNR(bins, 1000, 2000, 3000, 4000); got.Valid {
        t.Fatalf("noise-only input returned valid SNR: %+v", got)
    }
}
