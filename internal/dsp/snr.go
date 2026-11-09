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
type noisePSD struct {
	centre float64
	mean   float64
}

func EstimateBandSNRWithEvidence(bins []SpectrumBin, signalLowHz, signalHighHz float64, noiseBands []FrequencyBand, minClusterBins, minClusters int, minSeparationHz, minOccupiedBandwidthHz float64) BandSNR {
	if !validSNRDetectionParameters(signalLowHz, signalHighHz, noiseBands, minClusterBins, minClusters, minSeparationHz, minOccupiedBandwidthHz) {
		return BandSNR{}
	}
	valid := validSpectrumPowerBins(bins)
	if len(valid) < 16 {
		return BandSNR{}
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].frequency < valid[j].frequency })
	signal := selectSignalBins(valid, signalLowHz, signalHighHz)
	if len(signal) < 8 {
		return BandSNR{}
	}
	noiseEstimates := estimateGuardNoise(valid, noiseBands, signalLowHz, signalHighHz)
	if len(noiseEstimates) == 0 {
		return BandSNR{}
	}
	sort.Slice(noiseEstimates, func(i, j int) bool { return noiseEstimates[i].centre < noiseEstimates[j].centre })
	predictedNoise, excessPower, expectedNoise, observedSignal, ok := predictSignalBandPower(signal, noiseEstimates)
	if !ok {
		return BandSNR{}
	}
	longestRun, clusters := signalEvidenceClusters(signal, predictedNoise, medianBinSpacing(valid), minClusterBins)
	separatedClusters := separatedEvidenceClusters(clusters, minSeparationHz)

	result := BandSNR{
		EvidenceClusters:      len(clusters),
		EvidenceBins:          longestRun,
		OccupiedBandwidth90Hz: occupiedBandwidth90(signal, excessPower),
		NoiseFloorDBFS:        10 * math.Log10(math.Max(expectedNoise/float64(len(signal)), 1e-24)),
		SignalBandDBFS:        10 * math.Log10(math.Max(observedSignal, 1e-24)),
	}
	if len(clusters) < minClusters || (minClusters > 1 && separatedClusters < minClusters-1) ||
		result.OccupiedBandwidth90Hz < minOccupiedBandwidthHz || expectedNoise <= 0 || observedSignal <= expectedNoise {
		return result
	}
	result.SNRDB = 10 * math.Log10((observedSignal-expectedNoise)/expectedNoise)
	if !finiteSNR(result.SNRDB) || result.SNRDB < 3.0 {
		return result
	}
	result.Valid = true
	return result
}

func validSNRDetectionParameters(lowHz, highHz float64, noiseBands []FrequencyBand, minClusterBins, minClusters int, minSeparationHz, minOccupiedBandwidthHz float64) bool {
	return minClusterBins >= 1 && minClusters >= 1 && finiteSNR(minSeparationHz) && minSeparationHz >= 0 &&
		finiteSNR(minOccupiedBandwidthHz) && minOccupiedBandwidthHz >= 0 &&
		finiteSNR(lowHz) && finiteSNR(highHz) && lowHz >= 0 && highHz > lowHz && len(noiseBands) > 0
}

func validSpectrumPowerBins(bins []SpectrumBin) []snrPowerBin {
	valid := make([]snrPowerBin, 0, len(bins))
	for _, bin := range bins {
		if !finiteSNR(bin.FrequencyHz) || !finiteSNR(bin.MagnitudeDBFS) ||
			bin.MagnitudeDBFS < -200 || bin.MagnitudeDBFS > 40 || bin.FrequencyHz < 0 {
			continue
		}
		power := math.Pow(10, bin.MagnitudeDBFS/10)
		if finiteSNR(power) && power > 0 {
			valid = append(valid, snrPowerBin{frequency: bin.FrequencyHz, power: power})
		}
	}
	return valid
}

func selectSignalBins(valid []snrPowerBin, lowHz, highHz float64) []snrPowerBin {
	signal := make([]snrPowerBin, 0, len(valid)/4)
	for _, bin := range valid {
		if bin.frequency >= lowHz && bin.frequency <= highHz {
			signal = append(signal, bin)
		}
	}
	return signal
}

func estimateGuardNoise(valid []snrPowerBin, noiseBands []FrequencyBand, signalLowHz, signalHighHz float64) []noisePSD {
	estimates := make([]noisePSD, 0, len(noiseBands))
	for _, band := range noiseBands {
		if !finiteSNR(band.LowHz) || !finiteSNR(band.HighHz) || band.LowHz < 0 || band.HighHz <= band.LowHz {
			continue
		}
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
		median := powers[len(powers)/2]
		if len(powers)%2 == 0 {
			middle := len(powers) / 2
			median = (powers[middle-1] + powers[middle]) / 2
		}
		mean := median / math.Ln2
		if finiteSNR(mean) && mean > 1e-24 {
			estimates = append(estimates, noisePSD{centre: (band.LowHz + band.HighHz) / 2, mean: mean})
		}
	}
	return estimates
}

func predictSignalBandPower(signal []snrPowerBin, estimates []noisePSD) ([]float64, []float64, float64, float64, bool) {
	lower, upper := estimates[0], estimates[len(estimates)-1]
	if len(estimates) == 1 {
		upper = lower
	}
	if lower.centre > upper.centre {
		lower, upper = upper, lower
	}
	predictedNoise := make([]float64, len(signal))
	excessPower := make([]float64, len(signal))
	expectedNoise, observedSignal := 0.0, 0.0
	for i, bin := range signal {
		predicted := lower.mean
		if len(estimates) >= 2 && upper.centre > lower.centre {
			t := (bin.frequency - lower.centre) / (upper.centre - lower.centre)
			t = math.Max(-0.5, math.Min(1.5, t))
			predicted = math.Exp(math.Log(lower.mean) + t*(math.Log(upper.mean)-math.Log(lower.mean)))
		}
		if !finiteSNR(predicted) || predicted <= 0 {
			return nil, nil, 0, 0, false
		}
		predictedNoise[i] = predicted
		excessPower[i] = math.Max(0, bin.power-predicted)
		expectedNoise += predicted
		observedSignal += bin.power
	}
	return predictedNoise, excessPower, expectedNoise, observedSignal, true
}

func signalEvidenceClusters(signal []snrPowerBin, predictedNoise []float64, spacing float64, minClusterBins int) (int, []float64) {
	longestRun, currentRun := 0, 0
	clusterStart, clusterEnd := 0.0, 0.0
	clusters := make([]float64, 0, 8)
	appendCluster := func() {
		if currentRun >= minClusterBins {
			clusters = append(clusters, (clusterStart+clusterEnd)/2)
		}
	}
	for i, bin := range signal {
		above := bin.power >= predictedNoise[i]*6.3095734448
		if above {
			if currentRun == 0 || (i > 0 && bin.frequency-signal[i-1].frequency > spacing*1.5) {
				appendCluster()
				currentRun = 0
				clusterStart = bin.frequency
			}
			currentRun++
			clusterEnd = bin.frequency
			if currentRun > longestRun {
				longestRun = currentRun
			}
		} else if currentRun > 0 {
			appendCluster()
			currentRun = 0
		}
	}
	appendCluster()
	return longestRun, clusters
}

func separatedEvidenceClusters(centres []float64, minSeparationHz float64) int {
	separated := 0
	for i := range centres {
		for j := i + 1; j < len(centres); j++ {
			if math.Abs(centres[i]-centres[j]) >= minSeparationHz {
				separated++
				break
			}
		}
	}
	return separated
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
	frequencies     []float64
	noisePower      []float64
	frames          int
	gainShiftFrames int
	gainShiftDB     float64
}

const (
	snrNoiseLearningFrames = 24
	snrNoiseAdaptAlpha     = 0.04
	snrDisplayMinDB        = 3.0
	snrGainShiftThreshold  = 4.5
	snrGainShiftMaxMAD     = 6.5
	snrGainShiftConfirm    = 3
	// A passband whose total FFT power is below this floor is effectively
	// digital silence for this estimator. Never learn a near-zero baseline:
	// doing so makes the next ordinary receiver-noise sample look like an
	// enormous SNR improvement.
	snrMinimumBaselinePower = 1e-18
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
	t.gainShiftFrames = 0
	t.gainShiftDB = 0
}

// Estimate returns an audio SNR measured only inside [signalLowHz, signalHighHz].
// This compatibility entry point treats signalHint as a confirmed-signal flag;
// live RX code must use EstimateWithLock and pass an actual modem acquisition
// state. A spectral shape alone is never sufficient to publish live SNR.
func (t *BandSNRTracker) Estimate(bins []SpectrumBin, signalLowHz, signalHighHz float64, signalHint bool) BandSNR {
	return t.EstimateWithLock(bins, signalLowHz, signalHighHz, signalHint, signalHint)
}

// EstimateWithLock measures audio in-channel SNR against a temporally learned
// noise profile. The profile is learned whenever the current FFT window has no
// independent modem-like feature, even if a legacy FSK/GMSK lock flag remains
// latched. `signalLocked` must come from the production demodulator, not from
// this spectrum estimator. A value is published only when both current-window
// spectral evidence and decoder lock are present. A real RF-calibrated SNR
// still requires a calibrated receiver path.
func (t *BandSNRTracker) EstimateWithLock(bins []SpectrumBin, signalLowHz, signalHighHz float64, signalHint, signalLocked bool) BandSNR {
	return t.EstimateWithEvidence(bins, signalLowHz, signalHighHz, signalHint, signalLocked, signalLocked)
}

// EstimateWithEvidence separates three different conditions:
//   - signalHint: spectrum looks modem-like, so do not learn it as idle noise;
//   - signalAcquired: the production demodulator has acquired timing/carrier;
//   - packetValidated: a full frame passed protocol/CRC checks, permitting a
//     numeric SNR value to be shown to the operator.
//
// Carrier lock alone is insufficient for display because CW interference and
// impulsive noise can occasionally produce a front-end lock. Conversely, a
// valid but not-yet-decoded preamble must freeze the noise baseline.
func (t *BandSNRTracker) EstimateWithEvidence(bins []SpectrumBin, signalLowHz, signalHighHz float64, signalHint, signalAcquired, packetValidated bool) BandSNR {
	if t == nil || len(bins) < 16 || !finiteSNR(signalLowHz) || !finiteSNR(signalHighHz) ||
		signalLowHz < 0 || signalHighHz <= signalLowHz {
		return BandSNR{}
	}
	frequencies, powers, bandPower, bandBins, valid := t.spectrumPowers(bins, signalLowHz, signalHighHz)
	if !valid || bandBins < 8 {
		return BandSNR{}
	}
	if bandPower < snrMinimumBaselinePower {
		if !signalAcquired {
			t.Reset()
		}
		return BandSNR{}
	}
	if !t.prepareNoiseProfile(frequencies, powers, signalHint, signalAcquired) {
		return BandSNR{}
	}
	if t.frames < snrNoiseLearningFrames {
		if signalHint {
			return BandSNR{}
		}
		t.learnNoiseFrame(powers)
		return BandSNR{}
	}
	if !signalHint {
		t.updateIdleNoise(powers, frequencies, signalLowHz, signalHighHz)
		return BandSNR{}
	}
	if !signalAcquired || !packetValidated {
		return BandSNR{}
	}
	return t.calculateTrackedSNR(frequencies, powers, signalLowHz, signalHighHz)
}

func (t *BandSNRTracker) spectrumPowers(bins []SpectrumBin, lowHz, highHz float64) ([]float64, []float64, float64, int, bool) {
	frequencies := make([]float64, len(bins))
	powers := make([]float64, len(bins))
	bandPower, bandBins := 0.0, 0
	for i, bin := range bins {
		if !finiteSNR(bin.FrequencyHz) || !finiteSNR(bin.MagnitudeDBFS) ||
			bin.MagnitudeDBFS < -200 || bin.MagnitudeDBFS > 40 || bin.FrequencyHz < 0 {
			t.Reset()
			return nil, nil, 0, 0, false
		}
		frequencies[i] = bin.FrequencyHz
		powers[i] = math.Pow(10, bin.MagnitudeDBFS/10)
		if !finiteSNR(powers[i]) || powers[i] <= 0 || (i > 0 && frequencies[i] <= frequencies[i-1]) {
			t.Reset()
			return nil, nil, 0, 0, false
		}
		if frequencies[i] >= lowHz && frequencies[i] <= highHz {
			bandPower += powers[i]
			bandBins++
		}
	}
	return frequencies, powers, bandPower, bandBins, true
}

func (t *BandSNRTracker) prepareNoiseProfile(frequencies, powers []float64, signalHint, signalAcquired bool) bool {
	if len(t.frequencies) != len(frequencies) || len(t.noisePower) != len(powers) {
		t.resetNoiseProfile(frequencies, powers, signalHint, signalAcquired)
		return false
	}
	for i := range frequencies {
		if math.Abs(frequencies[i]-t.frequencies[i]) > 1e-6 {
			t.resetNoiseProfile(frequencies, powers, signalHint, signalAcquired)
			return false
		}
	}
	return true
}

func (t *BandSNRTracker) resetNoiseProfile(frequencies, powers []float64, signalHint, signalAcquired bool) {
	t.frequencies = append(t.frequencies[:0], frequencies...)
	t.noisePower = make([]float64, len(powers))
	if !signalHint && !signalAcquired {
		copy(t.noisePower, powers)
		t.frames = 1
	} else {
		t.frames = 0
	}
}

func (t *BandSNRTracker) learnNoiseFrame(powers []float64) {
	t.frames++
	for i, value := range powers {
		t.noisePower[i] += (value - t.noisePower[i]) / float64(t.frames)
	}
}

func (t *BandSNRTracker) calculateTrackedSNR(frequencies, powers []float64, lowHz, highHz float64) BandSNR {
	signal := make([]snrPowerBin, 0, len(powers)/4)
	noise := make([]snrPowerBin, 0, len(powers)/4)
	for i, frequency := range frequencies {
		if frequency >= lowHz && frequency <= highHz {
			signal = append(signal, snrPowerBin{frequency: frequency, power: powers[i]})
			noise = append(noise, snrPowerBin{frequency: frequency, power: t.noisePower[i]})
		}
	}
	if len(signal) < 8 || len(noise) != len(signal) {
		return BandSNR{}
	}

	const noiseGain = 1.0
	expectedNoise, observedPower := 0.0, 0.0
	excess := make([]float64, len(signal))
	for i := range signal {
		expected := noise[i].power * noiseGain
		expectedNoise += expected
		observedPower += signal[i].power
		excess[i] = math.Max(0, signal[i].power-expected)
	}
	result := BandSNR{
		OccupiedBandwidth90Hz: occupiedBandwidth90(signal, excess),
		NoiseFloorDBFS:        10 * math.Log10(math.Max(expectedNoise/float64(len(signal)), 1e-24)),
		SignalBandDBFS:        10 * math.Log10(math.Max(observedPower, 1e-24)),
	}
	if expectedNoise <= 0 || observedPower <= expectedNoise {
		return result
	}
	result.SNRDB = 10 * math.Log10((observedPower-expectedNoise)/expectedNoise)
	if !finiteSNR(result.SNRDB) || result.SNRDB < snrDisplayMinDB {
		return result
	}
	result.Valid = true
	return result
}

// updateIdleNoise follows the noise floor while the current FFT window has
// no modem-like signal feature. If the median per-bin level moves coherently,
// treat it as an audio/AGC gain change and scale the learned noise spectrum
// after three consecutive confirmations. This avoids displaying an enormous
// SNR against a noise floor learned at a much lower receiver gain.
func (t *BandSNRTracker) updateIdleNoise(powers, frequencies []float64, lowHz, highHz float64) {
	shiftDB, madDB, ok := t.noiseGainShift(powers, frequencies, lowHz, highHz)
	if ok && math.Abs(shiftDB) >= snrGainShiftThreshold && madDB <= snrGainShiftMaxMAD {
		if t.gainShiftFrames == 0 || math.Abs(shiftDB-t.gainShiftDB) > 3.0 {
			t.gainShiftDB = shiftDB
			t.gainShiftFrames = 1
		} else {
			t.gainShiftDB += (shiftDB - t.gainShiftDB) / float64(t.gainShiftFrames+1)
			t.gainShiftFrames++
		}
		if t.gainShiftFrames >= snrGainShiftConfirm {
			scale := math.Pow(10, t.gainShiftDB/10)
			for i := range t.noisePower {
				t.noisePower[i] *= scale
			}
			t.gainShiftFrames = 0
			t.gainShiftDB = 0
		}
		return
	}
	t.gainShiftFrames = 0
	t.gainShiftDB = 0
	t.updateNoise(powers)
}

func (t *BandSNRTracker) noiseGainShift(powers, frequencies []float64, lowHz, highHz float64) (medianDB, madDB float64, ok bool) {
	if t == nil || len(powers) != len(t.noisePower) || len(frequencies) != len(powers) {
		return 0, 0, false
	}
	deltas := make([]float64, 0, len(powers)/4)
	for i, frequency := range frequencies {
		if frequency < lowHz || frequency > highHz || t.noisePower[i] <= 0 || powers[i] <= 0 {
			continue
		}
		delta := 10 * math.Log10(powers[i]/t.noisePower[i])
		if finiteSNR(delta) {
			deltas = append(deltas, delta)
		}
	}
	if len(deltas) < 16 {
		return 0, 0, false
	}
	sort.Float64s(deltas)
	medianDB = medianFloat64(deltas)
	deviations := make([]float64, len(deltas))
	for i, value := range deltas {
		deviations[i] = math.Abs(value - medianDB)
	}
	sort.Float64s(deviations)
	madDB = medianFloat64(deviations)
	return medianDB, madDB, true
}

func medianFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	middle := len(values) / 2
	if len(values)%2 == 0 {
		return (values[middle-1] + values[middle]) * 0.5
	}
	return values[middle]
}

func (t *BandSNRTracker) updateNoise(powers []float64) {
	t.updateNoiseAlpha(powers, snrNoiseAdaptAlpha)
}

func (t *BandSNRTracker) updateNoiseAlpha(powers []float64, alpha float64) {
	if t == nil || len(powers) != len(t.noisePower) {
		return
	}
	for i, value := range powers {
		t.noisePower[i] = t.noisePower[i]*(1-alpha) + value*alpha
	}
}
