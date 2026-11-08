package dsp

import (
	"math"
	"math/rand"
	"testing"
)

func makeSyntheticSpectrum(seed int64, noiseDBPerBin func(float64) float64, signalLow, signalHigh float64, signal func(float64) float64) []SpectrumBin {
	rng := rand.New(rand.NewSource(seed))
	const step = 10.0
	bins := make([]SpectrumBin, 0, 1001)
	for hz := 0.0; hz <= 10000; hz += step {
		// Exponential periodogram power is the expected distribution of a
		// single FFT periodogram bin for Gaussian receiver noise.
		meanNoiseDB := noiseDBPerBin(hz)
		meanNoisePower := math.Pow(10, meanNoiseDB/10)
		power := rng.ExpFloat64() * meanNoisePower
		if hz >= signalLow && hz <= signalHigh && signal != nil {
			power += signal(hz)
		}
		magDB := 10 * math.Log10(math.Max(power, 1e-24))
		bins = append(bins, SpectrumBin{FrequencyHz: hz, MagnitudeDBFS: magDB})
	}
	return bins
}

func noiseGuards() []FrequencyBand {
	return []FrequencyBand{{LowHz: 300, HighHz: 550}, {LowHz: 3300, HighHz: 3900}}
}

func TestEstimateBandSNRTracksKnownBroadbandSignal(t *testing.T) {
	// 1800 Hz-wide signal band, with a 15-bin occupied cluster carrying
	// 10 dB more integrated power than the estimated in-band noise.
	bins := makeSyntheticSpectrum(1234, func(float64) float64 { return -100 }, 650, 3100, func(hz float64) float64 {
		if hz >= 1300 && hz <= 1800 {
			return math.Pow(10, -100.0/10) * 60
		}
		return 0
	})
	got := EstimateBandSNRWithGuards(bins, 650, 3100, noiseGuards())
	if !got.Valid {
		t.Fatalf("expected valid signal estimate, got %+v", got)
	}
	if math.Abs(got.SNRDB-10) > 2.5 {
		t.Fatalf("SNR %.2f dB, want about 10 dB", got.SNRDB)
	}
	if got.EvidenceBins < 5 {
		t.Fatalf("signal evidence run too short: %+v", got)
	}
}

func TestEstimateBandSNRRejectsWhiteReceiverNoise(t *testing.T) {
	for seed := int64(0); seed < 500; seed++ {
		bins := makeSyntheticSpectrum(seed, func(float64) float64 { return -90 }, 650, 3100, nil)
		got := EstimateBandSNRWithGuards(bins, 650, 3100, noiseGuards())
		if got.Valid {
			t.Fatalf("noise-only spectrum seed %d produced valid/high SNR: %+v", seed, got)
		}
	}
}

func TestEstimateBandSNRRejectsColoredNoiseWithoutModemFeature(t *testing.T) {
	// Model a receiver/audio chain whose noise PSD slopes by 24 dB across the
	// passband. A distant single high-frequency noise reference would make the
	// lower-frequency noise look like a huge positive SNR.
	for seed := int64(0); seed < 100; seed++ {
		bins := makeSyntheticSpectrum(seed+9000, func(hz float64) float64 {
			return -75 - 24*(hz/10000)
		}, 650, 3100, nil)
		got := EstimateBandSNRWithGuards(bins, 650, 3100, noiseGuards())
		if got.Valid {
			t.Fatalf("colored-noise-only spectrum seed %d produced valid/high SNR: %+v", seed, got)
		}
	}
}

func TestEstimateBandSNRRejectsSingleBinInterference(t *testing.T) {
	bins := makeSyntheticSpectrum(22, func(float64) float64 { return -95 }, 650, 3100, func(hz float64) float64 {
		if hz == 1500 {
			return math.Pow(10, -95.0/10) * 1e8
		}
		return 0
	})
	got := EstimateBandSNRWithGuards(bins, 650, 3100, noiseGuards())
	if got.Valid {
		t.Fatalf("single-bin spur should not be treated as modem signal: %+v", got)
	}
}

func TestEstimateBandSNRIsGainInvariant(t *testing.T) {
	bins := makeSyntheticSpectrum(77, func(float64) float64 { return -96 }, 650, 3100, func(hz float64) float64 {
		if hz >= 1450 && hz <= 1600 {
			return math.Pow(10, -96.0/10) * 100
		}
		return 0
	})
	base := EstimateBandSNRWithGuards(bins, 650, 3100, noiseGuards())
	if !base.Valid {
		t.Fatalf("base spectrum not recognized: %+v", base)
	}
	shifted := append([]SpectrumBin(nil), bins...)
	for i := range shifted {
		shifted[i].MagnitudeDBFS -= 18 // 18 dB lower audio gain
	}
	lowerGain := EstimateBandSNRWithGuards(shifted, 650, 3100, noiseGuards())
	if !lowerGain.Valid || math.Abs(lowerGain.SNRDB-base.SNRDB) > 0.1 {
		t.Fatalf("SNR changed with common gain: base=%+v lower-gain=%+v", base, lowerGain)
	}
}

func TestEstimateBandSNRRejectsNoiseOnlyLegacySingleGuard(t *testing.T) {
	bins := makeSyntheticSpectrum(987, func(float64) float64 { return -90 }, 1000, 2000, nil)
	got := EstimateBandSNR(bins, 1000, 2000, 3000, 4000)
	if got.Valid {
		t.Fatalf("single-guard noise-only input returned valid SNR: %+v", got)
	}
}
