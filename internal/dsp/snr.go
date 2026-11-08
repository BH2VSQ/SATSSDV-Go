package dsp

import (
	"math"
	"sort"
)

// FrequencyBand describes a frequency interval in hertz. Noise bands should
// sit immediately outside the modem's occupied band, rather than several kHz
// away where the receiver/audio-chain noise spectrum may have a different
// slope or filtering response.
type FrequencyBand struct {
	LowHz  float64
	HighHz float64
}

// BandSNR is an audio-baseband estimate from one spectrum window. It is not
// calibrated RF SNR, Eb/N0 or a standards-compliance measurement. Valid is
// false until the spectrum contains a multi-bin signal-like feature; a
// broadband noise floor or a single-bin spur must not be displayed as a
// high-confidence modem SNR.
type BandSNR struct {
	SNRDB                 float64
	NoiseFloorDBFS        float64 // estimated mean noise power per FFT bin at signal-band centre
	SignalBandDBFS        float64 // integrated energy in the configured signal band
	EvidenceBins          int     // longest consecutive run of bins significantly above noise
	EvidenceClusters      int     // number of separated signal-like spectral clusters
	OccupiedBandwidth90Hz float64 // 90% of estimated excess signal power lies within this width
	Valid                 bool
}

type snrPowerBin struct {
	frequency float64
	power     float64
}

// EstimateBandSNR estimates a single-window, integrated in-band SNR using one
// reference noise band. It is useful for offline spectra and signal-shape
// detection, but it has no temporal noise history. The live RX UI therefore
// uses BandSNRTracker instead of displaying this instantaneous estimate.
func EstimateBandSNR(bins []SpectrumBin, signalLowHz, signalHighHz, noiseLowHz, noiseHighHz float64) BandSNR {
	return EstimateBandSNRWithGuards(bins, signalLowHz, signalHighHz,
		[]FrequencyBand{{LowHz: noiseLowHz, HighHz: noiseHighHz}})
}

// EstimateBandSNRWithGuards estimates the signal-to-noise ratio in a modem
// passband. It uses the median periodogram power in each adjacent guard band,
// converts the median to mean power (exponential FFT-bin model), and linearly
// interpolates the noise PSD in log-power across the signal band. Using guards
// on both sides is substantially more robust against the frequency-dependent
// response of sound cards, receiver filters and audio baseband paths.
//
// A second, independent signal-presence check is applied. The default policy
// requires at least three connected FFT bins 8 dB above the interpolated noise
// PSD and at least 150 Hz of occupied excess bandwidth. The AFSK policy can
// require two separated mark/space clusters. This reduces false detections
// from random noise fluctuations and single narrowband spurs. This stateless
// method is not used as the displayed live SNR; see BandSNRTracker.
func EstimateBandSNRWithGuards(bins []SpectrumBin, signalLowHz, signalHighHz float64, noiseBands []FrequencyBand) BandSNR {
	return EstimateBandSNRWithEvidence(bins, signalLowHz, signalHighHz, noiseBands, 3, 1, 0, 150)
}

// EstimateBandSNRWithTonePairEvidence is for 1200-baud AFSK profiles. A valid
// signal must contain two independently elevated spectral clusters separated
// by at least minSeparationHz, corresponding to the mark and space tones. This
// rejects a single continuous whistle/interferer as a high-SNR modem signal.
func EstimateBandSNRWithTonePairEvidence(bins []SpectrumBin, signalLowHz, signalHighHz float64, noiseBands []FrequencyBand, minSeparationHz float64) BandSNR {
	return EstimateBandSNRWithEvidence(bins, signalLowHz, signalHighHz, noiseBands, 3, 2, minSeparationHz, 0)
}

// EstimateBandSNRWithEvidence allows the caller to select the signal-shape
// evidence expected for the modulation: wide occupied spectrum (one cluster)
// or separated tone groups (e.g. AFSK mark/space).
func EstimateBandSNRWithEvidence(bins []SpectrumBin, signalLowHz, signalHighHz float64, noiseBands []FrequencyBand, minClusterBins, minClusters int, minSeparationHz, minOccupiedBandwidthHz float64) BandSNR {
	result := BandSNR{}
	if minClusterBins < 1 || minClusters < 1 || !finiteSNR(minSeparationHz) || minSeparationHz < 0 || !finiteSNR(minOccupiedBandwidthHz) || minOccupiedBandwidthHz < 0 {
		return result
	}
	if len(bins) < 16 || !finiteSNR(signalLowHz) || !finiteSNR(signalHighHz) ||
		signalLowHz < 0 || signalHighHz <= signalLowHz || len(noiseBands) == 0 {
		return result
	}

	valid := make([]snrPowerBin, 0, len(bins))
	for _, bin := range bins {
		if !finiteSNR(bin.FrequencyHz) || !finiteSNR(bin.MagnitudeDBFS) ||
			bin.MagnitudeDBFS < -200 || bin.MagnitudeDBFS > 40 || bin.FrequencyHz < 0 {
			continue
		}
		// Spectrum() reports amplitude in dBFS (20*log10(amplitude));
		// converting with 10^(dB/10) yields the corresponding linear power.
		power := math.Pow(10, bin.MagnitudeDBFS/10)
		if !finiteSNR(power) || power <= 0 {
			continue
		}
		valid = append(valid, snrPowerBin{frequency: bin.FrequencyHz, power: power})
	}
	if len(valid) < 16 {
		return result
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].frequency < valid[j].frequency })

	signal := make([]snrPowerBin, 0, len(valid)/4)
	for _, bin := range valid {
		if bin.frequency >= signalLowHz && bin.frequency <= signalHighHz {
			signal = append(signal, bin)
		}
	}
	if len(signal) < 8 {
		return result
	}

	type noiseEstimate struct {
		centre float64
		mean   float64
	}
	noiseEstimates := make([]noiseEstimate, 0, len(noiseBands))
	for _, band := range noiseBands {
		if !finiteSNR(band.LowHz) || !finiteSNR(band.HighHz) || band.LowHz < 0 || band.HighHz <= band.LowHz {
			continue
		}
		// Guard bands must not overlap the modem passband.
		if band.LowHz < signalHighHz && band.HighHz > signalLowHz {
			continue
		}
		powers := make([]float64, 0, len(valid)/8)
		for _, bin := range valid {
			if bin.frequency >= band.LowHz && bin.frequency <= band.HighHz {
				powers = append(powers, bin.power)
			}
		}
		if len(powers) < 5 {
			continue
		}
		sort.Float64s(powers)
		mid := len(powers) / 2
		median := powers[mid]
		if len(powers)%2 == 0 {
			median = (powers[mid-1] + powers[mid]) / 2
		}
		mean := median / math.Ln2
		if finiteSNR(mean) && mean > 1e-24 {
			noiseEstimates = append(noiseEstimates, noiseEstimate{centre: (band.LowHz + band.HighHz) / 2, mean: mean})
		}
	}
	if len(noiseEstimates) == 0 {
		return result
	}
	sort.Slice(noiseEstimates, func(i, j int) bool { return noiseEstimates[i].centre < noiseEstimates[j].centre })

	// With one guard, assume locally flat noise. With two or more guards,
	// interpolate the log PSD between the nearest lower/upper guard estimates.
	lower, upper := noiseEstimates[0], noiseEstimates[len(noiseEstimates)-1]
	if len(noiseEstimates) == 1 {
		upper = lower
	}
	if lower.centre > upper.centre {
		lower, upper = upper, lower
	}

	expectedNoise := 0.0
	observedSignal := 0.0
	longestRun, currentRun := 0, 0
	clusters := make([]struct{ centre float64 }, 0, 8)
	spacing := medianBinSpacing(valid)
	predictedNoise := make([]float64, len(signal))
	excessPower := make([]float64, len(signal))
	for i, bin := range signal {
		predicted := lower.mean
		if len(noiseEstimates) >= 2 && upper.centre > lower.centre {
			t := (bin.frequency - lower.centre) / (upper.centre - lower.centre)
			// Extrapolation is deliberately bounded: a guard band far from the
			// occupied channel must not create an unbounded PSD correction.
			t = math.Max(-0.5, math.Min(1.5, t))
			predicted = math.Exp(math.Log(lower.mean) + t*(math.Log(upper.mean)-math.Log(lower.mean)))
		}
		if !finiteSNR(predicted) || predicted <= 0 {
			return BandSNR{}
		}
		predictedNoise[i] = predicted
		excessPower[i] = math.Max(0, bin.power-predicted)
		expectedNoise += predicted
		observedSignal += bin.power
	}

	// Build connected groups of bins that are at least 8 dB above the
	// interpolated noise PSD. Windowed narrow tones have a short main lobe;
	// they are rejected by the profile-specific minimum cluster width below.
	currentRun = 0
	clusterStart, clusterEnd := 0.0, 0.0
	for i, bin := range signal {
		above := bin.power >= predictedNoise[i]*6.3095734448
		if above {
			if currentRun == 0 || (i > 0 && bin.frequency-signal[i-1].frequency > spacing*1.5) {
				if currentRun >= minClusterBins {
					clusters = append(clusters, struct{ centre float64 }{centre: (clusterStart + clusterEnd) / 2})
				}
				currentRun = 0
				clusterStart = bin.frequency
			}
			currentRun++
			clusterEnd = bin.frequency
			if currentRun > longestRun {
				longestRun = currentRun
			}
		} else if currentRun > 0 {
			if currentRun >= minClusterBins {
				clusters = append(clusters, struct{ centre float64 }{centre: (clusterStart + clusterEnd) / 2})
			}
			currentRun = 0
		}
	}
	if currentRun >= minClusterBins {
		clusters = append(clusters, struct{ centre float64 }{centre: (clusterStart + clusterEnd) / 2})
	}

	separatedClusters := 0
	for i := range clusters {
		for j := i + 1; j < len(clusters); j++ {
			if math.Abs(clusters[i].centre-clusters[j].centre) >= minSeparationHz {
				separatedClusters++
				break
			}
		}
	}
	result.EvidenceClusters = len(clusters)
	result.EvidenceBins = longestRun
	result.OccupiedBandwidth90Hz = occupiedBandwidth90(signal, excessPower)
	result.NoiseFloorDBFS = 10 * math.Log10(math.Max(expectedNoise/float64(len(signal)), 1e-24))
	result.SignalBandDBFS = 10 * math.Log10(math.Max(observedSignal, 1e-24))

	// A single spectral spike or unstructured noise is not enough to declare
	// that a modem signal is present, even if the integrated ratio is large.
	if len(clusters) < minClusters || (minClusters > 1 && separatedClusters < minClusters-1) ||
		result.OccupiedBandwidth90Hz < minOccupiedBandwidthHz || expectedNoise <= 0 || observedSignal <= expectedNoise {
		return result
	}
	signalOnly := observedSignal - expectedNoise
	result.SNRDB = 10 * math.Log10(signalOnly/expectedNoise)
	if !finiteSNR(result.SNRDB) {
		return BandSNR{}
	}
	// Below roughly 3 dB, broad random noise fluctuations are too easily
	// confused with a modem-shaped in-band feature in a single 85 ms FFT
	// window. Keep the numeric estimate hidden until the signal is at least
	// measurably above its interpolated local noise power.
	if result.SNRDB < 3.0 {
		return result
	}
	result.Valid = true
	return result
}

func occupiedBandwidth90(signal []snrPowerBin, excess []float64) float64 {
	if len(signal) == 0 || len(signal) != len(excess) {
		return 0
	}
	total := 0.0
	for _, value := range excess {
		total += value
	}
	if total <= 0 {
		return 0
	}
	lowTarget, highTarget := total*0.05, total*0.95
	cumulative := 0.0
	lowHz, highHz := signal[0].frequency, signal[len(signal)-1].frequency
	lowFound := false
	for i, value := range excess {
		cumulative += value
		if !lowFound && cumulative >= lowTarget {
			lowHz = signal[i].frequency
			lowFound = true
		}
		if cumulative >= highTarget {
			highHz = signal[i].frequency
			break
		}
	}
	return math.Max(0, highHz-lowHz)
}

func medianBinSpacing(bins []snrPowerBin) float64 {
	if len(bins) < 2 {
		return 1
	}
	spacings := make([]float64, 0, len(bins)-1)
	for i := 1; i < len(bins); i++ {
		delta := bins[i].frequency - bins[i-1].frequency
		if delta > 0 {
			spacings = append(spacings, delta)
		}
	}
	if len(spacings) == 0 {
		return 1
	}
	sort.Float64s(spacings)
	return spacings[len(spacings)/2]
}

func finiteSNR(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// BandSNRTracker learns the receiver's noise spectrum in the actual modem
// channel while the receiver is idle. This avoids assuming that noise several
// kHz outside the modem band has the same level as in-band noise, and prevents
// transmitter sidebands from contaminating an adjacent guard-band estimate.
type BandSNRTracker struct {
	frequencies []float64
	noisePower  []float64
	frames      int
}

const (
	snrNoiseLearningFrames = 12
	snrNoiseAdaptAlpha     = 0.04
	snrSignalHoldDB        = 2.0
	snrDisplayMinDB        = 3.0
	snrMinOccupiedHz       = 150.0
	snrMaxOccupiedFraction = 0.82
)

// Reset discards the learned idle-noise spectrum. Call it when the RX audio
// stream is discontinuous or its FFT sample rate/profile changes.
// Ready reports whether the tracker has collected enough idle-noise frames
// to publish a channel SNR estimate.
func (t *BandSNRTracker) Ready() bool {
	return t != nil && t.frames >= snrNoiseLearningFrames
}

func (t *BandSNRTracker) Reset() {
	if t == nil {
		return
	}
	t.frequencies = nil
	t.noisePower = nil
	t.frames = 0
}

// Estimate returns an audio SNR measured only inside [signalLowHz, signalHighHz].
// It compares each in-channel FFT-bin power with the corresponding bin in a
// temporally learned idle-noise profile. signalHint is a separate shape-based
// detector result used during warm-up: a recognizable modem-shaped burst must
// not be learned as idle noise. Until 12 noise frames have been learned, Valid
// remains false rather than publishing an uncalibrated guess. The estimate is
// relative to the current audio input path; it is not RF SNR or Eb/N0.
func (t *BandSNRTracker) Estimate(bins []SpectrumBin, signalLowHz, signalHighHz float64, signalHint bool) BandSNR {
	result := BandSNR{}
	if t == nil || len(bins) < 16 || !finiteSNR(signalLowHz) || !finiteSNR(signalHighHz) ||
		signalLowHz < 0 || signalHighHz <= signalLowHz {
		return result
	}

	frequencies := make([]float64, len(bins))
	powers := make([]float64, len(bins))
	for i, bin := range bins {
		if !finiteSNR(bin.FrequencyHz) || !finiteSNR(bin.MagnitudeDBFS) ||
			bin.MagnitudeDBFS < -200 || bin.MagnitudeDBFS > 40 || bin.FrequencyHz < 0 {
			t.Reset()
			return result
		}
		frequencies[i] = bin.FrequencyHz
		powers[i] = math.Pow(10, bin.MagnitudeDBFS/10)
		if !finiteSNR(powers[i]) || powers[i] <= 0 {
			t.Reset()
			return result
		}
		if i > 0 && frequencies[i] <= frequencies[i-1] {
			t.Reset()
			return result
		}
	}

	if len(t.frequencies) != len(frequencies) || len(t.noisePower) != len(powers) {
		t.frequencies = append(t.frequencies[:0], frequencies...)
		t.noisePower = make([]float64, len(powers))
		if !signalHint {
			copy(t.noisePower, powers)
			t.frames = 1
		} else {
			t.frames = 0
		}
		return result
	}
	for i := range frequencies {
		if math.Abs(frequencies[i]-t.frequencies[i]) > 1e-6 {
			t.frequencies = append(t.frequencies[:0], frequencies...)
			t.noisePower = make([]float64, len(powers))
			if !signalHint {
				copy(t.noisePower, powers)
				t.frames = 1
			} else {
				t.frames = 0
			}
			return result
		}
	}

	if t.frames < snrNoiseLearningFrames {
		if signalHint {
			// Do not let an already-present burst become the learned noise floor.
			return result
		}
		t.frames++
		for i, value := range powers {
			t.noisePower[i] += (value - t.noisePower[i]) / float64(t.frames)
		}
		return result
	}

	signal := make([]snrPowerBin, 0, len(bins)/4)
	noise := make([]snrPowerBin, 0, len(bins)/4)
	for i, frequency := range frequencies {
		if frequency >= signalLowHz && frequency <= signalHighHz {
			signal = append(signal, snrPowerBin{frequency: frequency, power: powers[i]})
			noise = append(noise, snrPowerBin{frequency: frequency, power: t.noisePower[i]})
		}
	}
	if len(signal) < 8 || len(noise) != len(signal) {
		return result
	}

	// Compare every in-channel bin against its own learned idle-noise power.
	// This preserves the receiver/audio frequency response instead of assuming
	// that a distant guard band has the same absolute noise PSD.
	expectedNoise := 0.0
	observedPower := 0.0
	excess := make([]float64, len(signal))
	longestRun, currentRun := 0, 0
	for i := range signal {
		expected := noise[i].power
		expectedNoise += expected
		observedPower += signal[i].power
		excess[i] = math.Max(0, signal[i].power-expected)
		if signal[i].power >= expected*6.3095734448 {
			currentRun++
			if currentRun > longestRun {
				longestRun = currentRun
			}
		} else {
			currentRun = 0
		}
	}
	occupied := occupiedBandwidth90(signal, excess)
	result.EvidenceBins = longestRun
	result.OccupiedBandwidth90Hz = occupied
	result.NoiseFloorDBFS = 10 * math.Log10(math.Max(expectedNoise/float64(len(signal)), 1e-24))
	result.SignalBandDBFS = 10 * math.Log10(math.Max(observedPower, 1e-24))
	if expectedNoise <= 0 || observedPower <= expectedNoise {
		t.updateNoise(powers)
		return result
	}
	result.SNRDB = 10 * math.Log10((observedPower-expectedNoise)/expectedNoise)
	if !finiteSNR(result.SNRDB) {
		t.updateNoise(powers)
		return BandSNR{}
	}

	// Modem waveforms occupy a bounded portion of the configured passband.
	// A sudden broadband noise/AGC rise tends to fill almost the whole channel;
	// treating that as a modem would produce a false high-SNR report. Reject
	// near-full-band excess and let the idle floor follow it more quickly.
	maxOccupied := (signalHighHz - signalLowHz) * snrMaxOccupiedFraction
	if result.OccupiedBandwidth90Hz > maxOccupied {
		result.SNRDB = 0
		t.updateNoiseFast(powers)
		return result
	}
	candidate := occupied >= snrMinOccupiedHz && result.SNRDB >= snrSignalHoldDB
	if !candidate {
		t.updateNoise(powers)
		return result
	}
	if result.SNRDB < snrDisplayMinDB {
		return result
	}
	result.Valid = true
	return result
}

func (t *BandSNRTracker) updateNoise(powers []float64) {
	t.updateNoiseAlpha(powers, snrNoiseAdaptAlpha)
}

// updateNoiseFast is used only after a near-full-band noise/AGC transition has
// been rejected as a modem. The receiver should relearn the changed idle floor
// faster than the normal slow background adaptation.
func (t *BandSNRTracker) updateNoiseFast(powers []float64) {
	t.updateNoiseAlpha(powers, 0.25)
}

func (t *BandSNRTracker) updateNoiseAlpha(powers []float64, alpha float64) {
	if t == nil || len(powers) != len(t.noisePower) {
		return
	}
	for i, value := range powers {
		t.noisePower[i] = t.noisePower[i]*(1-alpha) + value*alpha
	}
}
