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
	cfg            Config
	sps            int
	baseband       []complex64
	sampleCount    int64
	searchPos      int
	locked         bool
	cursor         int
	phase          float64
	phaseStep      float64
	symbolIndex    int64
	phaseRotation  complex128
	stepRotation   complex128
	amplitudeScale float64
	cosLUT         []float64
	sinLUT         []float64
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
	return &StreamDemodulator{cfg: cfg, sps: sps, cosLUT: cosLUT, sinLUT: sinLUT}, nil
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
	d.phase = 0
	d.phaseStep = 0
	d.symbolIndex = 0
	d.phaseRotation = 1
	d.stepRotation = 1
	d.amplitudeScale = 1
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
	if d == nil || len(samples) == 0 {
		return nil
	}
	for _, sample := range samples {
		lutIndex := int(d.sampleCount % int64(len(d.cosLUT)))
		v := complex(2*float64(sample)*d.cosLUT[lutIndex], -2*float64(sample)*d.sinLUT[lutIndex])
		d.baseband = append(d.baseband, complex64(v))
		d.sampleCount++
	}
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
	return d.decodeAvailable()
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
	activeSymbols := 0
	for k := 0; k < trainingSymbols; k++ {
		means[k] = symbolMean(d.baseband, start+k*d.sps, d.sps)
		level := cmplx.Abs(means[k])
		energy += level
		if level > 0.01 {
			activeSymbols++
		}
	}
	// A complete, energized training word is required. This rejects a
	// preamble fragment at a callback boundary and prevents acquisition on
	// idle receiver noise before a real burst arrives.
	if energy/trainingSymbols < 0.008 || activeSymbols < trainingSymbols*9/10 {
		return trainingCandidate{}, false
	}
	var differential complex128
	for k := 1; k < trainingSymbols; k++ {
		sign := trainingSequence[k] * trainingSequence[k-1]
		differential += means[k] * cmplx.Conj(means[k-1]) * complex(float64(sign), 0)
	}
	stepPhase := cmplx.Phase(differential)
	frequencyOffset := stepPhase * float64(d.cfg.SymbolRate) / (2 * math.Pi)
	if math.Abs(frequencyOffset) > float64(d.cfg.SymbolRate)*0.35 {
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
		// Correlate every sample in the known training word, not only the
		// central half-symbol aperture. A center-only metric has a broad flat
		// maximum for rectangular symbols and can lock several samples away
		// from the true symbol edge after an arbitrary idle pre-roll.
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

func (d *StreamDemodulator) tryAcquire() {
	window := trainingSymbols * d.sps
	if len(d.baseband) < window {
		return
	}
	// First search at 1/8-symbol resolution to keep the idle receiver cheap.
	// Once a promising preamble is found, refine its start sample-by-sample.
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
		// A high-confidence match is required before locking. The sample-level
		// metric rejects the broad off-boundary plateau of the coarse score.
		if bestStart < 0 || best.score < 0.76 {
			continue
		}
		d.phase = best.phase
		d.phaseStep = best.phaseStep
		// The BPSK training word has unit constellation amplitude. Use it as an
		// AGC reference so all payload constellations (especially 16-QAM outer
		// points) are sliced independently of sound-card gain and TX filtering.
		if best.amplitude > 1e-6 {
			d.amplitudeScale = 1 / best.amplitude
		} else {
			d.amplitudeScale = 1
		}
		// The full-sample training correlation references carrier phase at
		// the first sample of the preamble. Advance through all 96 training
		// symbols and to the center of the first payload symbol before making
		// its decision. The half-symbol term is important under residual CFO.
		d.phaseRotation = cmplx.Rect(1, -(d.phase + d.phaseStep*(trainingSymbols+0.5)))
		d.stepRotation = cmplx.Rect(1, -d.phaseStep)
		d.cursor = bestStart + window
		d.symbolIndex = 0
		d.locked = true
		return
	}
	d.searchPos = last + 1
}

func (d *StreamDemodulator) decodeAvailable() []bool {
	count := (len(d.baseband) - d.cursor) / d.sps
	if count <= 0 {
		return nil
	}
	out := make([]bool, 0, count*d.cfg.BitsPerSymbol)
	for n := 0; n < count; n++ {
		base := d.cursor + n*d.sps
		z := symbolMean(d.baseband, base, d.sps)
		z *= d.phaseRotation
		z *= complex(d.amplitudeScale, 0)
		decision := nearestConstellationPoint(z, d.cfg.BitsPerSymbol)
		phaseError := cmplx.Phase(z * cmplx.Conj(decision))
		// Decision-directed carrier tracking removes the small residual CFO
		// left by the finite training word. The proportional term corrects
		// phase immediately; the integral term tracks slow frequency drift.
		d.phaseRotation *= cmplx.Rect(1, -0.12*phaseError)
		d.stepRotation *= cmplx.Rect(1, -0.0015*phaseError)
		d.phaseRotation *= d.stepRotation
		out = appendSymbolBits(out, z, d.cfg.BitsPerSymbol)
		d.symbolIndex++
	}
	d.cursor += count * d.sps
	if d.cursor > len(d.baseband)/2 {
		d.baseband = append([]complex64(nil), d.baseband[d.cursor:]...)
		d.cursor = 0
	}
	return out
}

func symbolMean(samples []complex64, base, sps int) complex128 {
	start := base + sps/4
	count := sps / 2
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
