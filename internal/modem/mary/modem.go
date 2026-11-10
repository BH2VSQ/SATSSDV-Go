// Package mary implements experimental audio-band M-ary SSDV modulation.
// It carries standard CCSDS ASM + randomized 256-byte data units, but the
// QPSK/8PSK/16QAM symbol mapping and acquisition preamble are SATSSDV-Go's
// own profile format and are not claimed to be Hamstation/ARCSSD compatible.
package mary

import (
	"errors"
	"math"
	"math/cmplx"
)

const trainingSymbols = 96
const defaultAmplitude = 0.72

type Config struct {
	SampleRate    int
	SymbolRate    int
	BitsPerSymbol int
	CarrierHz     int
}

type StreamDemodulator struct {
	cfg              Config
	sps              int
	samplesPerSymbol float64
	baseband         []complex64
	sampleCount      int64
	searchPos        int
	locked           bool
	cursor           float64
	phase            float64
	phaseStep        float64
	symbolIndex      int64
	phaseRotation    complex128
	stepRotation     complex128
	amplitudeScale   float64
	silenceSamples   int
	signalRMS        float64
	burstGeneration  uint64
	reacquireCount   uint64
	packetizedMode   bool
	evmSignalPower   float64
	evmErrorPower    float64
	evmSymbols       uint64
	cosLUT           []float64
	sinLUT           []float64
}

func NewStreamDemodulator(cfg Config) (*StreamDemodulator, error) {
	if cfg.SampleRate <= 0 || cfg.SymbolRate <= 0 || cfg.SampleRate%cfg.SymbolRate != 0 {
		return nil, errors.New("M-ary sample rate must be an integer multiple of symbol rate")
	}
	if cfg.BitsPerSymbol < 2 || cfg.BitsPerSymbol > 4 {
		return nil, errors.New("M-ary modulation must use 2, 3 or 4 bits per symbol")
	}
	if cfg.CarrierHz <= 0 || cfg.CarrierHz >= cfg.SampleRate/2 {
		return nil, errors.New("M-ary carrier must be below Nyquist")
	}
	sps := cfg.SampleRate / cfg.SymbolRate
	period := cfg.SampleRate / gcd(cfg.SampleRate, cfg.CarrierHz)
	cosLUT := make([]float64, period)
	sinLUT := make([]float64, period)
	for i := 0; i < period; i++ {
		angle := 2 * math.Pi * float64(cfg.CarrierHz) * float64(i) / float64(cfg.SampleRate)
		cosLUT[i], sinLUT[i] = math.Cos(angle), math.Sin(angle)
	}
	return &StreamDemodulator{cfg: cfg, sps: sps, samplesPerSymbol: float64(sps), cosLUT: cosLUT, sinLUT: sinLUT}, nil
}

func (d *StreamDemodulator) Reset() {
	if d == nil {
		return
	}
	d.baseband = nil
	d.sampleCount = 0
	d.searchPos = 0
	d.locked = false
	d.cursor = 0
	d.samplesPerSymbol = float64(d.sps)
	d.phase = 0
	d.phaseStep = 0
	d.symbolIndex = 0
	d.phaseRotation = 1
	d.stepRotation = 1
	d.amplitudeScale = 1
	d.silenceSamples = 0
	d.signalRMS = 0
	d.evmSignalPower = 0
	d.evmErrorPower = 0
	d.evmSymbols = 0
	d.burstGeneration++
}

// ModulateBytes prepends a known 96-symbol BPSK training sequence, then
// transmits the bit stream MSB-first using Gray-labelled M-ary symbols.
// The shipped profiles center the audio carrier at the symbol rate, giving
// an integer carrier cycle per symbol at the supported 48 kHz audio rate.
func ModulateBytes(data []byte, cfg Config) ([]float32, error) {
	if cfg.SampleRate <= 0 || cfg.SymbolRate <= 0 || cfg.SampleRate%cfg.SymbolRate != 0 || cfg.BitsPerSymbol < 2 || cfg.BitsPerSymbol > 4 {
		return nil, errors.New("invalid M-ary modem configuration")
	}
	if cfg.CarrierHz <= 0 {
		cfg.CarrierHz = cfg.SymbolRate
	}
	if cfg.CarrierHz >= cfg.SampleRate/2 {
		return nil, errors.New("M-ary carrier must be below Nyquist")
	}
	sps := cfg.SampleRate / cfg.SymbolRate
	bits := bytesToBits(data)
	pad := (cfg.BitsPerSymbol - len(bits)%cfg.BitsPerSymbol) % cfg.BitsPerSymbol
	for i := 0; i < pad; i++ {
		bits = append(bits, false)
	}
	symbols := make([]complex64, 0, trainingSymbols+len(bits)/cfg.BitsPerSymbol)
	for _, sign := range trainingSequence {
		if sign > 0 {
			symbols = append(symbols, complex(1, 0))
		} else {
			symbols = append(symbols, complex(-1, 0))
		}
	}
	for i := 0; i < len(bits); i += cfg.BitsPerSymbol {
		symbols = append(symbols, mapBits(bits[i:i+cfg.BitsPerSymbol], cfg.BitsPerSymbol))
	}
	wave := make([]float32, len(symbols)*sps)
	period := cfg.SampleRate / gcd(cfg.SampleRate, cfg.CarrierHz)
	cosLUT, sinLUT := make([]float64, period), make([]float64, period)
	for i := 0; i < period; i++ {
		phase := 2 * math.Pi * float64(cfg.CarrierHz) * float64(i) / float64(cfg.SampleRate)
		cosLUT[i], sinLUT[i] = math.Cos(phase), math.Sin(phase)
	}
	for si, symbol := range symbols {
		iPart, qPart := float64(real(symbol)), float64(imag(symbol))
		base := si * sps
		for j := 0; j < sps; j++ {
			n := base + j
			lut := n % period
			wave[n] = float32(defaultAmplitude * (iPart*cosLUT[lut] - qPart*sinLUT[lut]))
		}
	}
	return wave, nil
}

func (d *StreamDemodulator) Feed(samples []float32) []bool {
	llrs := d.FeedSoft(samples)
	bits := make([]bool, len(llrs))
	for i, llr := range llrs {
		bits[i] = llr < 0
	}
	return bits
}

// FeedSoft returns one max-log LLR per payload bit. Positive values favor zero
// and negative values favor one. Carrier/timing acquisition and decision-
// directed tracking are shared with the legacy hard-decision path.
func (d *StreamDemodulator) FeedSoft(samples []float32) []float32 {
	if d == nil || len(samples) == 0 {
		return nil
	}
	// A sustained low-energy interval marks the end of an M-ary burst. Do not
	// feed silence into the symbol slicer: drain any complete symbols already
	// buffered, then release acquisition state so a later training word can
	// lock the same long-running RX stream again.
	blockRMS := audioBlockRMS(samples)
	// Packetized SDF3 bursts carry an exact coded-bit count and CRC boundary.
	// Amplitude fades are therefore not safe evidence of an inter-burst gap:
	// releasing the carrier lock during a fade discards the remaining symbols
	// of a valid FEC group. StreamDecoder explicitly rearms after each declared
	// group; retain the lock until that protocol boundary is reached.
	silenceThreshold := math.Max(1e-5, d.signalRMS*0.10)
	if !d.packetizedMode && d.signalRMS > 0 && blockRMS < silenceThreshold {
		// Audio callbacks can straddle the final symbol and the receiver's
		// trailing noise/silence. Do not discard the complete callback just
		// because its *average* RMS is low: retain every sample up to the last
		// active sample, then count only the quiet suffix as an inter-burst gap.
		sampleThreshold := silenceThreshold
		activeSamples := trailingActivePrefix(samples, sampleThreshold, 5*d.sps)
		if activeSamples > 0 {
			d.appendAudioSamples(samples[:activeSamples])
			if !d.locked {
				d.tryAcquire()
			}
			if activeRMS := audioBlockRMS(samples[:activeSamples]); activeRMS > d.signalRMS {
				d.signalRMS = activeRMS
			}
		}
		quietSamples := len(samples) - activeSamples
		d.silenceSamples += quietSamples
		var ready []float32
		if d.locked {
			ready = d.decodeAvailableSoft()
		}
		if d.silenceSamples >= d.cfg.SampleRate/10 && (d.locked || len(d.baseband) > 0) {
			d.releaseAcquisition()
		}
		return ready
	}
	d.silenceSamples = 0
	if blockRMS > d.signalRMS {
		d.signalRMS = blockRMS
	}
	d.appendAudioSamples(samples)
	if !d.locked {
		d.tryAcquire()
		if !d.locked {
			maxKeep := 3 * trainingSymbols * d.sps
			if len(d.baseband) > maxKeep {
				drop := len(d.baseband) - maxKeep
				d.baseband = append([]complex64(nil), d.baseband[drop:]...)
				d.searchPos -= drop
				if d.searchPos < 0 {
					d.searchPos = 0
				}
			}
			return nil
		}
	}
	// SDF3 uses explicit low-energy guards between independent packet units.
	// Keep the generic symbol slicer on its legacy path here; the guard releases
	// acquisition and the following 96-symbol word performs a clean re-lock.
	return d.decodeAvailableSoft()
}

// appendAudioSamples performs quadrature downconversion while preserving the
// absolute audio sample index across arbitrary Feed boundaries.
func (d *StreamDemodulator) appendAudioSamples(samples []float32) {
	for _, sample := range samples {
		lutIndex := int(d.sampleCount % int64(len(d.cosLUT)))
		v := complex(2*float64(sample)*d.cosLUT[lutIndex], -2*float64(sample)*d.sinLUT[lutIndex])
		d.baseband = append(d.baseband, complex64(v))
		d.sampleCount++
	}
}

// trailingActivePrefix returns the number of leading samples before the final
// contiguous low-amplitude suffix. The suffix is treated as receiver gap/noise;
// the prefix is retained even when the whole callback RMS falls below the gap
// threshold.
func trailingActivePrefix(samples []float32, threshold float64, windowSize int) int {
	if windowSize < 1 {
		windowSize = 1
	}
	end := len(samples)
	for end > 0 {
		start := end - windowSize
		if start < 0 {
			start = 0
		}
		// An RMS decision over one symbol-sized window rejects isolated noise
		// peaks that would otherwise make a per-sample silence detector think
		// the 200 ms SDF3 guard is still active RF.
		if audioBlockRMS(samples[start:end]) >= threshold {
			break
		}
		end = start
	}
	return end
}

// SetPacketizedMode raises the low-energy guard threshold for SDF3 packetized
// bursts. A packet boundary includes an explicit 200 ms quiet interval; after
// 100 ms below the adaptive threshold the demodulator releases symbol lock so
// the next per-packet 96-symbol training word can reacquire timing and carrier.
func (d *StreamDemodulator) SetPacketizedMode(enabled bool) {
	if d == nil {
		return
	}
	d.packetizedMode = enabled
}

// BurstGeneration increases whenever a continuous receiver releases its
// current symbol lock after a 100 ms low-energy gap. StreamDecoder uses this
// signal to re-arm SGV2 burst framing without clearing completed image IDs.
// Rearm releases the current symbol lock so the same long-running receiver
// can acquire the training word of the next independent M-ary burst.
func (d *StreamDemodulator) Rearm() {
	if d == nil {
		return
	}
	d.releaseAcquisition()
}

func (d *StreamDemodulator) BurstGeneration() uint64 {
	if d == nil {
		return 0
	}
	return d.burstGeneration
}

func audioBlockRMS(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var energy float64
	for _, value := range samples {
		energy += float64(value) * float64(value)
	}
	return math.Sqrt(energy / float64(len(samples)))
}

func (d *StreamDemodulator) releaseAcquisition() {
	d.baseband = nil
	d.searchPos = 0
	d.locked = false
	d.cursor = 0
	d.samplesPerSymbol = float64(d.sps)
	d.phase = 0
	d.phaseStep = 0
	d.symbolIndex = 0
	d.phaseRotation = 1
	d.stepRotation = 1
	d.amplitudeScale = 1
	d.sampleCount = 0
	d.silenceSamples = 0
	d.evmSignalPower = 0
	d.evmErrorPower = 0
	d.evmSymbols = 0
	// A released burst no longer defines the receive level. Clearing the
	// envelope lets the next SDF3 unit reacquire even when its downlink level
	// is much lower than the preceding unit.
	d.signalRMS = 0
	d.burstGeneration++
}

type trainingCandidate struct {
	phase     float64
	phaseStep float64
	amplitude float64
	score     float64
}

func (d *StreamDemodulator) evaluateTraining(start int) (trainingCandidate, bool) {
	return d.evaluateTrainingAt(start, false)
}

func (d *StreamDemodulator) evaluateTrainingRefined(start int) (trainingCandidate, bool) {
	return d.evaluateTrainingAt(start, true)
}

func (d *StreamDemodulator) evaluateTrainingAt(start int, sampleAccurate bool) (trainingCandidate, bool) {
	window := trainingSymbols * d.sps
	if start < 0 || start+window > len(d.baseband) {
		return trainingCandidate{}, false
	}
	var means [trainingSymbols]complex128
	energy := 0.0
	levels := [trainingSymbols]float64{}
	for k := 0; k < trainingSymbols; k++ {
		means[k] = symbolMean(d.baseband, start+k*d.sps, d.sps)
		levels[k] = cmplx.Abs(means[k])
		energy += levels[k]
	}
	meanLevel := energy / trainingSymbols
	// Do not impose an absolute PCM amplitude floor: satellite downlink audio
	// levels vary substantially with receiver gain and squelch/AGC settings.
	// Instead, require energy above the numerical floor and check the training
	// word's normalized correlation below. The per-symbol activity test is
	// relative to the measured preamble level so low-gain bursts remain usable.
	activeSymbols := 0
	for _, level := range levels {
		if level > math.Max(1e-5, meanLevel*0.12) {
			activeSymbols++
		}
	}
	if meanLevel < 1e-4 || activeSymbols < trainingSymbols*9/10 {
		return trainingCandidate{}, false
	}
	var differential complex128
	for k := 1; k < trainingSymbols; k++ {
		sign := trainingSequence[k] * trainingSequence[k-1]
		differential += means[k] * cmplx.Conj(means[k-1]) * complex(float64(sign), 0)
	}
	stepPhase := cmplx.Phase(differential)
	frequencyOffset := stepPhase * float64(d.cfg.SymbolRate) / (2 * math.Pi)
	// The adjacent-symbol phase difference is unambiguous over +/-0.5 baud.
	// Leave a small guard at the Nyquist boundary where estimator noise and
	// symbol-clock error can make the sign ambiguous.
	if math.Abs(frequencyOffset) > float64(d.cfg.SymbolRate)*0.48 {
		return trainingCandidate{}, false
	}

	var correlation complex128
	magnitude := 0.0
	if !sampleAccurate {
		// Cheap coarse score: central apertures are enough to identify a
		// promising training window. Sample-accurate timing is refined only
		// around these candidates below.
		rotation := complex(1.0, 0.0)
		stepRotation := cmplx.Rect(1, -stepPhase)
		for k := 0; k < trainingSymbols; k++ {
			correlation += means[k] * complex(float64(trainingSequence[k]), 0) * rotation
			magnitude += cmplx.Abs(means[k])
			rotation *= stepRotation
		}
	} else {
		// Use the coarse differential estimate while comparing candidate symbol
		// boundaries. The maximum-likelihood CFO refinement is performed only
		// once, after the best boundary has been selected; doing the 25-point
		// frequency search for every sample candidate multiplied acquisition CPU
		// cost and could cause the live audio queue to fall behind.
		rotation := complex(1.0, 0.0)
		sampleRotation := cmplx.Rect(1, -stepPhase/float64(d.sps))
		for k := 0; k < trainingSymbols; k++ {
			sign := complex(float64(trainingSequence[k]), 0)
			for j := 0; j < d.sps; j++ {
				sample := complex128(d.baseband[start+k*d.sps+j])
				correlation += sample * rotation * sign
				magnitude += cmplx.Abs(sample)
				rotation *= sampleRotation
			}
		}
	}
	score := cmplx.Abs(correlation) / (magnitude + 1e-15)
	return trainingCandidate{phase: cmplx.Phase(correlation), phaseStep: stepPhase, amplitude: energy / trainingSymbols, score: score}, true
}

// refineTrainingCandidate performs the expensive carrier refinement once at
// the selected symbol boundary and recomputes the coherent phase estimate.
// estimateTrainingTiming derives the symbol period from the known BPSK
// transition edges in the 96-symbol acquisition word. It is deliberately
// bounded: weak or ambiguous edges fall back to nominal integer timing rather
// than poisoning an otherwise usable burst.
func (d *StreamDemodulator) estimateTrainingTiming(start int, candidate trainingCandidate) (float64, float64, bool) {
	nominal := float64(d.sps)
	coarseHz := candidate.phaseStep * float64(d.cfg.SymbolRate) / (2 * math.Pi)
	initialPhase := candidate.phase
	type edgePoint struct{ symbol, sample float64 }
	points := make([]edgePoint, 0, trainingSymbols/2)
	searchRadius := math.Max(3, nominal*0.30)
	for k := 0; k < trainingSymbols-1; k++ {
		if trainingSequence[k] == trainingSequence[k+1] {
			continue
		}
		expected := float64(start) + float64(k+1)*nominal
		lo := int(math.Floor(expected - searchRadius))
		hi := int(math.Ceil(expected + searchRadius))
		if lo < 0 {
			lo = 0
		}
		if hi >= len(d.baseband)-1 {
			hi = len(d.baseband) - 2
		}
		bestDist := math.Inf(1)
		bestEdge := 0.0
		found := false
		oldSign := float64(trainingSequence[k])
		for n := lo; n <= hi; n++ {
			t0 := float64(n - start)
			t1 := t0 + 1
			phase0 := -initialPhase - 2*math.Pi*coarseHz*t0/float64(d.cfg.SampleRate)
			phase1 := -initialPhase - 2*math.Pi*coarseHz*t1/float64(d.cfg.SampleRate)
			v0 := real(complex128(d.baseband[n])*cmplx.Rect(1, phase0)) * oldSign
			v1 := real(complex128(d.baseband[n+1])*cmplx.Rect(1, phase1)) * oldSign
			if v0 < 0 || v1 > 0 || v0 == v1 {
				continue
			}
			fraction := v0 / (v0 - v1)
			edge := float64(n) + fraction
			dist := math.Abs(edge - expected)
			if dist < bestDist {
				bestDist, bestEdge, found = dist, edge, true
			}
		}
		if found {
			points = append(points, edgePoint{symbol: float64(k + 1), sample: bestEdge})
		}
	}
	if len(points) < 20 {
		return float64(start), nominal, false
	}
	// Fit y = phase + symbol*period. Reject outliers using a median absolute
	// residual gate, then refit; this prevents one noisy edge from shifting
	// every data symbol in a long SDF3 group.
	fit := func(in []edgePoint) (float64, float64, float64, bool) {
		if len(in) < 2 {
			return 0, 0, 0, false
		}
		var mx, my float64
		for _, p := range in {
			mx += p.symbol
			my += p.sample
		}
		mx /= float64(len(in))
		my /= float64(len(in))
		var numerator, denominator float64
		for _, p := range in {
			dx := p.symbol - mx
			numerator += dx * (p.sample - my)
			denominator += dx * dx
		}
		if denominator <= 0 {
			return 0, 0, 0, false
		}
		period := numerator / denominator
		phase := my - period*mx
		var sumSq float64
		for _, p := range in {
			residual := p.sample - (phase + p.symbol*period)
			sumSq += residual * residual
		}
		return phase, period, math.Sqrt(sumSq / float64(len(in))), true
	}
	phase, period, rms, ok := fit(points)
	if !ok {
		return float64(start), nominal, false
	}
	filtered := make([]edgePoint, 0, len(points))
	gate := math.Max(0.28, math.Min(0.75, rms*2.5))
	for _, p := range points {
		if math.Abs(p.sample-(phase+p.symbol*period)) <= gate {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) >= 20 && len(filtered) < len(points) {
		phase, period, rms, ok = fit(filtered)
	}
	if !ok || len(filtered) < 20 || math.IsNaN(period) || math.IsInf(period, 0) {
		return float64(start), nominal, false
	}
	ppm := (period/nominal - 1) * 1e6
	if math.Abs(ppm) > 2000 || rms > 0.65 || phase < float64(start)-2 || phase > float64(start)+2 {
		return float64(start), nominal, false
	}
	return phase, period, true
}

func sampleComplexLinear(samples []complex64, position float64) complex128 {
	if len(samples) == 0 || position < 0 || position >= float64(len(samples)-1) {
		return 0
	}
	index := int(position)
	fraction := position - float64(index)
	a, b := complex128(samples[index]), complex128(samples[index+1])
	return a + (b-a)*complex(fraction, 0)
}

// refineTrainingCandidateTimed performs a bounded CFO search using the
// fractional symbol period measured from the training edges. This separates
// sample-clock error from carrier-frequency offset.
func (d *StreamDemodulator) refineTrainingCandidateTimed(start, period float64, candidate trainingCandidate) trainingCandidate {
	coarseHz := candidate.phaseStep * float64(d.cfg.SymbolRate) / (2 * math.Pi)
	const searchSpanHz = 12.0
	const searchStepHz = 1.0
	minimumHz := math.Max(-float64(d.cfg.SymbolRate)*0.49, coarseHz-searchSpanHz)
	maximumHz := math.Min(float64(d.cfg.SymbolRate)*0.49, coarseHz+searchSpanHz)
	bestHz := coarseHz
	bestScore := -1.0
	var bestCorrelation complex128
	bestMagnitude := 0.0
	bestAmplitude := 0.0
	for frequencyHz := minimumHz; frequencyHz <= maximumHz+searchStepHz*0.25; frequencyHz += searchStepHz {
		var correlation complex128
		magnitude := 0.0
		amplitudeSum := 0.0
		sampleRotation := cmplx.Rect(1, -2*math.Pi*frequencyHz/float64(d.cfg.SampleRate))
		for symbol := 0; symbol < trainingSymbols; symbol++ {
			symbolStart := start + float64(symbol)*period
			rotation := cmplx.Rect(1, -2*math.Pi*frequencyHz*float64(symbol)*period/float64(d.cfg.SampleRate))
			sign := complex(float64(trainingSequence[symbol]), 0)
			var symbolCorrelation complex128
			samplesUsed := 0
			for sampleOffset := 0.0; sampleOffset < period; sampleOffset += 1 {
				sample := sampleComplexLinear(d.baseband, symbolStart+sampleOffset)
				corrected := sample * rotation
				correlation += corrected * sign
				symbolCorrelation += corrected
				magnitude += cmplx.Abs(sample)
				rotation *= sampleRotation
				samplesUsed++
			}
			if samplesUsed > 0 {
				amplitudeSum += cmplx.Abs(symbolCorrelation / complex(float64(samplesUsed), 0))
			}
		}
		score := cmplx.Abs(correlation) / (magnitude + 1e-15)
		if score > bestScore {
			bestScore, bestHz, bestCorrelation, bestMagnitude = score, frequencyHz, correlation, magnitude
			bestAmplitude = amplitudeSum / trainingSymbols
		}
	}
	if bestMagnitude <= 1e-15 {
		return candidate
	}
	candidate.phase = cmplx.Phase(bestCorrelation)
	candidate.phaseStep = 2 * math.Pi * bestHz * period / float64(d.cfg.SampleRate)
	candidate.amplitude = bestAmplitude
	candidate.score = bestScore
	return candidate
}

func (d *StreamDemodulator) applyTrainingLockTimed(start, period float64, candidate trainingCandidate) {
	if candidate.amplitude > 1e-6 {
		d.amplitudeScale = 1 / candidate.amplitude
	} else {
		d.amplitudeScale = 1
	}
	d.phase = candidate.phase
	d.phaseStep = candidate.phaseStep
	d.samplesPerSymbol = period
	d.phaseRotation = cmplx.Rect(1, -(d.phase + d.phaseStep*(trainingSymbols+0.5)))
	d.stepRotation = cmplx.Rect(1, -d.phaseStep)
	d.cursor = start + float64(trainingSymbols)*period
	d.symbolIndex = 0
	d.evmSignalPower = 0
	d.evmErrorPower = 0
	d.evmSymbols = 0
	d.reacquireCount++
}

func (d *StreamDemodulator) tryAcquire() {
	window := trainingSymbols * d.sps
	if len(d.baseband) < window {
		return
	}
	step := d.sps / 8
	if step < 1 {
		step = 1
	}
	last := len(d.baseband) - window
	for start := d.searchPos; start <= last; start += step {
		candidate, valid := d.evaluateTraining(start)
		if !valid || candidate.score < 0.18 {
			continue
		}
		bestStart := -1
		best := trainingCandidate{}
		lo := start - step + 1
		if lo < d.searchPos {
			lo = d.searchPos
		}
		hi := start + step - 1
		if hi > last {
			hi = last
		}
		for refinedStart := lo; refinedStart <= hi; refinedStart++ {
			refined, ok := d.evaluateTrainingRefined(refinedStart)
			if ok && refined.score > best.score {
				bestStart, best = refinedStart, refined
			}
		}
		if bestStart < 0 || best.score < 0.70 {
			continue
		}
		timingStart, timingPeriod, timingOK := d.estimateTrainingTiming(bestStart, best)
		if !timingOK {
			timingStart = float64(bestStart)
			timingPeriod = float64(d.sps)
		}
		best = d.refineTrainingCandidateTimed(timingStart, timingPeriod, best)
		if best.score < 0.70 {
			continue
		}
		d.applyTrainingLockTimed(timingStart, timingPeriod, best)
		d.locked = true
		return
	}
	d.searchPos = last + 1
}

func (d *StreamDemodulator) compactBaseband() {
	if d.cursor > float64(len(d.baseband))/2 {
		drop := int(d.cursor)
		if drop > len(d.baseband) {
			drop = len(d.baseband)
		}
		d.baseband = append([]complex64(nil), d.baseband[drop:]...)
		d.cursor -= float64(drop)
		if d.cursor < 0 {
			d.cursor = 0
		}
	}
}

func (d *StreamDemodulator) decodeSymbolCount(count int) []float32 {
	period := d.samplesPerSymbol
	if period <= 0 {
		period = float64(d.sps)
	}
	if count <= 0 || d.cursor+float64(count-1)*period+period*0.75 >= float64(len(d.baseband)-1) {
		return nil
	}
	out := make([]float32, 0, count*d.cfg.BitsPerSymbol)
	stepPhase := cmplx.Phase(d.stepRotation)
	for n := 0; n < count; n++ {
		symbolStart := d.cursor
		first := symbolStart + period*0.25
		last := symbolStart + period*0.75
		center := symbolStart + period*0.5
		rotationStep := cmplx.Rect(1, stepPhase/period)
		localRotation := cmplx.Rect(1, stepPhase*(first-center)/period)
		symbolRotation := d.phaseRotation
		var corrected complex128
		samplesUsed := 0
		for samplePosition := first; samplePosition < last; samplePosition += 1 {
			corrected += sampleComplexLinear(d.baseband, samplePosition) * symbolRotation * localRotation
			localRotation *= rotationStep
			samplesUsed++
		}
		if samplesUsed == 0 {
			return out
		}
		z := corrected / complex(float64(samplesUsed), 0)
		z *= complex(d.amplitudeScale, 0)
		decision := nearestConstellationPoint(z, d.cfg.BitsPerSymbol)
		errorVector := z - decision
		errorPower := real(errorVector)*real(errorVector) + imag(errorVector)*imag(errorVector)
		referencePower := real(decision)*real(decision) + imag(decision)*imag(decision)
		const evmAlpha = 0.04
		if d.evmSymbols == 0 {
			d.evmErrorPower, d.evmSignalPower = errorPower, referencePower
		} else {
			d.evmErrorPower += evmAlpha * (errorPower - d.evmErrorPower)
			d.evmSignalPower += evmAlpha * (referencePower - d.evmSignalPower)
		}
		d.evmSymbols++
		phaseError := cmplx.Phase(z * cmplx.Conj(decision))
		d.phaseRotation *= cmplx.Rect(1, -0.12*phaseError)
		d.stepRotation *= cmplx.Rect(1, -0.0015*phaseError)
		d.phaseRotation *= d.stepRotation
		out = appendConstellationLLR(out, z, d.cfg.BitsPerSymbol)
		d.symbolIndex++
		d.cursor += period
	}
	return out
}

func (d *StreamDemodulator) decodeAvailableSoft() []float32 {
	period := d.samplesPerSymbol
	if period <= 0 {
		period = float64(d.sps)
	}
	lastSafeStart := float64(len(d.baseband)-1) - period*0.75
	count := int(math.Floor((lastSafeStart-d.cursor)/period)) + 1
	out := d.decodeSymbolCount(count)
	d.compactBaseband()
	return out
}

func appendConstellationLLR(dst []float32, z complex128, bitsPerSymbol int) []float32 {
	const clamp = 32.0
	if bitsPerSymbol < 2 || bitsPerSymbol > 4 {
		return dst
	}
	points := maryConstellationPoints[bitsPerSymbol]
	var minZero, minOne [4]float64
	for bit := 0; bit < bitsPerSymbol; bit++ {
		minZero[bit], minOne[bit] = math.Inf(1), math.Inf(1)
	}
	for label, point := range points {
		delta := z - point
		distance := real(delta)*real(delta) + imag(delta)*imag(delta)
		for bit := 0; bit < bitsPerSymbol; bit++ {
			mask := 1 << uint(bitsPerSymbol-1-bit)
			if label&mask == 0 {
				if distance < minZero[bit] {
					minZero[bit] = distance
				}
			} else if distance < minOne[bit] {
				minOne[bit] = distance
			}
		}
	}
	for bit := 0; bit < bitsPerSymbol; bit++ {
		llr := minOne[bit] - minZero[bit]
		if llr > clamp {
			llr = clamp
		}
		if llr < -clamp {
			llr = -clamp
		}
		dst = append(dst, float32(llr))
	}
	return dst
}

func buildMARYConstellationPoints() [5][]complex128 {
	var constellations [5][]complex128
	for bitsPerSymbol := 2; bitsPerSymbol <= 4; bitsPerSymbol++ {
		count := 1 << uint(bitsPerSymbol)
		points := make([]complex128, count)
		var bits [4]bool
		for label := 0; label < count; label++ {
			for bit := 0; bit < bitsPerSymbol; bit++ {
				bits[bit] = label&(1<<uint(bitsPerSymbol-1-bit)) != 0
			}
			points[label] = complex128(mapBits(bits[:bitsPerSymbol], bitsPerSymbol))
		}
		constellations[bitsPerSymbol] = points
	}
	return constellations
}

func symbolMean(samples []complex64, base, sps int) complex128 {
	// The unfiltered quadrature mixer contains an image at approximately
	// 2*CarrierHz. Averaging the complete symbol interval is intentional: at
	// the supported 48 kHz / 1600 baud profile the 3200 Hz image completes
	// exactly two cycles per symbol and largely cancels, while the desired
	// symbol component remains coherent.
	start := base
	count := sps
	var sum complex128
	for i := 0; i < count; i++ {
		sum += complex128(samples[start+i])
	}
	return sum / complex(float64(count), 0)
}

func DemodulateBytes(samples []float32, cfg Config) ([]byte, error) {
	d, err := NewStreamDemodulator(cfg)
	if err != nil {
		return nil, err
	}
	bits := d.Feed(samples)
	if len(bits) < 8 {
		return nil, errors.New("M-ary preamble or payload not detected")
	}
	return bitsToBytes(bits), nil
}

func bytesToBits(data []byte) []bool {
	out := make([]bool, 0, len(data)*8)
	for _, b := range data {
		for k := 7; k >= 0; k-- {
			out = append(out, b&(1<<uint(k)) != 0)
		}
	}
	return out
}
func bitsToBytes(bits []bool) []byte {
	out := make([]byte, len(bits)/8)
	for i := 0; i < len(out)*8; i++ {
		if bits[i] {
			out[i/8] |= 1 << uint(7-i%8)
		}
	}
	return out
}

var trainingSequence = makeTrainingSequence()

func makeTrainingSequence() [trainingSymbols]int {
	var out [trainingSymbols]int
	state := uint32(0x4d595df4)
	for i := range out {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		if state&1 == 0 {
			out[i] = 1
		} else {
			out[i] = -1
		}
	}
	return out
}

var constellation8 = func() [8]complex64 {
	var points [8]complex64
	for gray := 0; gray < 8; gray++ {
		angle := 2 * math.Pi * float64(grayToBinary(gray)) / 8
		points[gray] = complex(float32(math.Cos(angle)), float32(math.Sin(angle)))
	}
	return points
}()

// maryConstellationPoints is immutable after package initialization. Keeping the
// bit-labelled points out of the symbol loop avoids per-symbol slice creation.
var maryConstellationPoints = buildMARYConstellationPoints()

func mapBits(bits []bool, n int) complex64 {
	value := 0
	for _, bit := range bits {
		value <<= 1
		if bit {
			value++
		}
	}
	switch n {
	case 2:
		switch value {
		case 0:
			return complex(.70710677, .70710677)
		case 1:
			return complex(-.70710677, .70710677)
		case 3:
			return complex(-.70710677, -.70710677)
		default:
			return complex(.70710677, -.70710677)
		}
	case 3:
		return constellation8[value]
	case 4:
		i := grayPAM((value >> 2) & 3)
		q := grayPAM(value & 3)
		const norm = 0.31622776601683794 // 1/sqrt(10)
		return complex(float32(float64(i)*norm), float32(float64(q)*norm))
	}
	return 0
}

func nearestConstellationPoint(z complex128, n int) complex128 {
	switch n {
	case 2:
		iBit := real(z) < 0
		qBit := imag(z) < 0
		bits := []bool{false, false}
		if iBit && !qBit {
			bits = []bool{false, true}
		}
		if iBit && qBit {
			bits = []bool{true, true}
		}
		if !iBit && qBit {
			bits = []bool{true, false}
		}
		return complex128(mapBits(bits, 2))
	case 3:
		best := constellation8[0]
		bestDist := math.Inf(1)
		for _, point := range constellation8 {
			delta := z - complex128(point)
			dist := real(delta)*real(delta) + imag(delta)*imag(delta)
			if dist < bestDist {
				best, bestDist = point, dist
			}
		}
		return complex128(best)
	case 4:
		i := float64(grayPAM(nearestGrayPAM(real(z)))) / math.Sqrt(10)
		q := float64(grayPAM(nearestGrayPAM(imag(z)))) / math.Sqrt(10)
		return complex(i, q)
	default:
		return 1
	}
}

func appendSymbolBits(dst []bool, z complex128, n int) []bool {
	switch n {
	case 2:
		i, q := real(z), imag(z)
		var v int
		if i < 0 && q >= 0 {
			v = 1
		} else if i < 0 && q < 0 {
			v = 3
		} else if i >= 0 && q < 0 {
			v = 2
		}
		return append(dst, v&2 != 0, v&1 != 0)
	case 3:
		best, bestDist := 0, math.Inf(1)
		for gray := 0; gray < 8; gray++ {
			p := complex128(constellation8[gray])
			delta := z - p
			dist := real(delta)*real(delta) + imag(delta)*imag(delta)
			if dist < bestDist {
				best, bestDist = gray, dist
			}
		}
		return append(dst, best&4 != 0, best&2 != 0, best&1 != 0)
	case 4:
		i := nearestGrayPAM(real(z))
		q := nearestGrayPAM(imag(z))
		return append(dst, i&2 != 0, i&1 != 0, q&2 != 0, q&1 != 0)
	}
	return dst
}

func grayToBinary(v int) int {
	b := v
	for shift := 1; shift < 8; shift <<= 1 {
		b ^= b >> shift
	}
	return b
}
func grayPAM(v int) int {
	switch v {
	case 0:
		return -3
	case 1:
		return -1
	case 3:
		return 1
	default:
		return 3
	}
}
func nearestGrayPAM(v float64) int {
	levels := []float64{-3, -1, 1, 3}
	labels := []int{0, 1, 3, 2}
	best, bestD := 0, math.Inf(1)
	for i, l := range levels {
		d := math.Abs(v - l/math.Sqrt(10))
		if d < bestD {
			best, bestD = labels[i], d
		}
	}
	return best
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

// StreamDiagnostics exposes bounded read-only acquisition telemetry so the RX
// UI can distinguish a modem that is still receiving samples from one that has
// lost symbol lock or is waiting for a fresh training word.
type StreamDiagnostics struct {
	ReacquireCount   uint64
	PacketizedMode   bool
	Locked           bool
	BufferedSamples  int
	SignalRMS        float64
	SilenceSamples   int
	BurstGeneration  uint64
	SamplesPerSymbol float64
	ClockOffsetPPM   float64
	EVMRMS           float64
	SNRDB            float64
	SNRSamples       uint64
	SNRValid         bool
}

func finiteEVM(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func (d *StreamDemodulator) Diagnostics() StreamDiagnostics {
	if d == nil {
		return StreamDiagnostics{}
	}
	period := d.samplesPerSymbol
	if period <= 0 {
		period = float64(d.sps)
	}
	status := StreamDiagnostics{
		ReacquireCount: d.reacquireCount, PacketizedMode: d.packetizedMode, Locked: d.locked, BufferedSamples: len(d.baseband), SignalRMS: d.signalRMS,
		SilenceSamples: d.silenceSamples, BurstGeneration: d.burstGeneration, SamplesPerSymbol: period,
		ClockOffsetPPM: (period/float64(d.sps) - 1) * 1e6, SNRSamples: d.evmSymbols,
	}
	if d.locked && d.evmSymbols >= 48 && d.evmSignalPower > 1e-8 {
		evm2 := d.evmErrorPower / d.evmSignalPower
		if evm2 < 1e-6 {
			evm2 = 1e-6 // cap display at 60 dB for the numerical noise floor.
		}
		if finiteEVM(evm2) && evm2 > 0 {
			status.EVMRMS = math.Sqrt(evm2)
			status.SNRDB = 10 * math.Log10(1/evm2)
			if status.SNRDB > 60 {
				status.SNRDB = 60
			}
			status.SNRValid = finiteEVM(status.SNRDB)
		}
	}
	return status
}
