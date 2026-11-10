package mary

import (
	"errors"
	"math"
)

// StreamModulator emits a single M-ary burst in bounded byte-sized writes.
// The training word is emitted once, while bit packing and carrier phase are
// retained between WriteBytes calls.
type StreamModulator struct {
	cfg            Config
	sps            int
	period         int
	cosLUT         []float64
	sinLUT         []float64
	sampleIndex    uint64
	carrierPhase   float64
	dynamicCarrier bool
	pending        [4]bool
	pendingN       int
	started        bool
	closed         bool
}

func NewStreamModulator(cfg Config) (*StreamModulator, error) {
	if cfg.SampleRate <= 0 || cfg.SymbolRate <= 0 || cfg.SampleRate%cfg.SymbolRate != 0 || cfg.BitsPerSymbol < 2 || cfg.BitsPerSymbol > 4 {
		return nil, errors.New("invalid M-ary modem configuration")
	}
	if cfg.CarrierHz <= 0 {
		cfg.CarrierHz = cfg.SymbolRate
	}
	effectiveCarrier := float64(cfg.CarrierHz) + cfg.CarrierOffsetHz
	if effectiveCarrier <= 0 || effectiveCarrier >= float64(cfg.SampleRate)/2 {
		return nil, errors.New("M-ary effective carrier must be above zero and below Nyquist")
	}
	if math.IsNaN(cfg.CarrierOffsetHz) || math.IsInf(cfg.CarrierOffsetHz, 0) || math.IsNaN(cfg.CarrierDriftHzPerSecond) || math.IsInf(cfg.CarrierDriftHzPerSecond, 0) {
		return nil, errors.New("M-ary carrier offset and drift must be finite")
	}
	period := cfg.SampleRate / gcd(cfg.SampleRate, cfg.CarrierHz)
	dynamicCarrier := cfg.CarrierOffsetHz != 0 || cfg.CarrierDriftHzPerSecond != 0
	m := &StreamModulator{cfg: cfg, sps: cfg.SampleRate / cfg.SymbolRate, period: period, cosLUT: make([]float64, period), sinLUT: make([]float64, period), dynamicCarrier: dynamicCarrier}
	for i := 0; i < period; i++ {
		phase := 2 * math.Pi * float64(cfg.CarrierHz) * float64(i) / float64(cfg.SampleRate)
		m.cosLUT[i], m.sinLUT[i] = math.Cos(phase), math.Sin(phase)
	}
	return m, nil
}

// Start emits the known preamble exactly once at the beginning of a burst.
func (m *StreamModulator) Start() ([]float32, error) {
	if m == nil || m.closed {
		return nil, errors.New("M-ary stream modulator is closed")
	}
	if m.started {
		return nil, errors.New("M-ary stream modulator has already started")
	}
	m.started = true
	out := make([]float32, trainingSymbols*m.sps)
	pos := 0
	for _, sign := range trainingSequence {
		value := complex(float32(sign), 0)
		m.writeSymbol(value, out, &pos)
	}
	return out, nil
}

// WriteTraining emits another known 96-symbol BPSK training word without
// resetting carrier phase. SDF3 inserts one before each independently coded
// CCSDS unit so a receiver that loses symbol lock during a long image burst can
// reacquire at the next unit. Any pending M-ary bit carry is zero-padded before
// the training word; SDF3 receivers search its following header bit-wise.
func (m *StreamModulator) WriteTraining() ([]float32, error) {
	if m == nil || m.closed {
		return nil, errors.New("M-ary stream modulator is closed")
	}
	if !m.started {
		return nil, errors.New("M-ary stream modulator must be started before writing training")
	}
	out := make([]float32, 0, (trainingSymbols+1)*m.sps)
	if m.pendingN > 0 {
		for m.pendingN < m.cfg.BitsPerSymbol {
			m.pending[m.pendingN] = false
			m.pendingN++
		}
		base := len(out)
		out = append(out, make([]float32, m.sps)...)
		m.writeSymbol(mapBits(m.pending[:m.cfg.BitsPerSymbol], m.cfg.BitsPerSymbol), out, &base)
		m.pendingN = 0
	}
	base := len(out)
	out = append(out, make([]float32, trainingSymbols*m.sps)...)
	for _, sign := range trainingSequence {
		value := complex(float32(sign), 0)
		m.writeSymbol(value, out, &base)
	}
	return out, nil
}

func (m *StreamModulator) WriteBytes(data []byte) ([]float32, error) {
	if m == nil || m.closed {
		return nil, errors.New("M-ary stream modulator is closed")
	}
	if !m.started {
		return nil, errors.New("M-ary stream modulator must be started before writing data")
	}
	// There are eight input bits per byte and up to four bits per symbol. The
	// carry is at most three bits, so this output bound is safely conservative.
	symbolCapacity := (len(data)*8 + m.pendingN) / m.cfg.BitsPerSymbol
	out := make([]float32, 0, symbolCapacity*m.sps)
	for _, value := range data {
		for bit := 7; bit >= 0; bit-- {
			m.pending[m.pendingN] = value&(1<<uint(bit)) != 0
			m.pendingN++
			if m.pendingN == m.cfg.BitsPerSymbol {
				symbol := mapBits(m.pending[:m.cfg.BitsPerSymbol], m.cfg.BitsPerSymbol)
				base := len(out)
				for i := 0; i < m.sps; i++ {
					out = append(out, 0)
				}
				m.writeSymbol(symbol, out, &base)
				m.pendingN = 0
			}
		}
	}
	return out, nil
}

// Close zero-pads the final partial constellation symbol, matching
// ModulateBytes' whole-buffer encoder.
func (m *StreamModulator) Close() ([]float32, error) {
	if m == nil || m.closed {
		return nil, nil
	}
	m.closed = true
	if !m.started {
		return nil, errors.New("M-ary stream modulator was not started")
	}
	if m.pendingN == 0 {
		return nil, nil
	}
	for m.pendingN < m.cfg.BitsPerSymbol {
		m.pending[m.pendingN] = false
		m.pendingN++
	}
	out := make([]float32, m.sps)
	pos := 0
	m.writeSymbol(mapBits(m.pending[:m.cfg.BitsPerSymbol], m.cfg.BitsPerSymbol), out, &pos)
	m.pendingN = 0
	return out, nil
}

func (m *StreamModulator) writeSymbol(symbol complex64, out []float32, pos *int) {
	iPart, qPart := float64(real(symbol)), float64(imag(symbol))
	for j := 0; j < m.sps; j++ {
		var cosine, sine float64
		if !m.dynamicCarrier {
			lut := int(m.sampleIndex % uint64(m.period))
			cosine, sine = m.cosLUT[lut], m.sinLUT[lut]
		} else {
			// Apply CFO in the complex transmitter before the real-audio
			// projection. This is a physically faithful carrier impairment for
			// wideband M-ary audio whose lower sideband approaches DC; shifting
			// the real waveform via a one-sided analytic-signal transform is not
			// equivalent in that case.
			cosine, sine = math.Cos(m.carrierPhase), math.Sin(m.carrierPhase)
			t := float64(m.sampleIndex) / float64(m.cfg.SampleRate)
			instantaneousHz := float64(m.cfg.CarrierHz) + m.cfg.CarrierOffsetHz + m.cfg.CarrierDriftHzPerSecond*t
			m.carrierPhase = math.Remainder(m.carrierPhase+2*math.Pi*instantaneousHz/float64(m.cfg.SampleRate), 2*math.Pi)
		}
		out[*pos] = float32(defaultAmplitude * (iPart*cosine - qPart*sine))
		(*pos)++
		m.sampleIndex++
	}
}
