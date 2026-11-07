package mary

import (
	"errors"
	"math"
)

// StreamModulator emits a single M-ary burst in bounded byte-sized writes.
// The training word is emitted once, while bit packing and carrier phase are
// retained between WriteBytes calls.
type StreamModulator struct {
	cfg         Config
	sps         int
	period      int
	cosLUT      []float64
	sinLUT      []float64
	sampleIndex uint64
	pending     [4]bool
	pendingN    int
	started     bool
	closed      bool
}

func NewStreamModulator(cfg Config) (*StreamModulator, error) {
	if cfg.SampleRate <= 0 || cfg.SymbolRate <= 0 || cfg.SampleRate%cfg.SymbolRate != 0 || cfg.BitsPerSymbol < 2 || cfg.BitsPerSymbol > 4 {
		return nil, errors.New("invalid M-ary modem configuration")
	}
	if cfg.CarrierHz <= 0 {
		cfg.CarrierHz = cfg.SymbolRate
	}
	if cfg.CarrierHz >= cfg.SampleRate/2 {
		return nil, errors.New("M-ary carrier must be below Nyquist")
	}
	period := cfg.SampleRate / gcd(cfg.SampleRate, cfg.CarrierHz)
	m := &StreamModulator{cfg: cfg, sps: cfg.SampleRate / cfg.SymbolRate, period: period, cosLUT: make([]float64, period), sinLUT: make([]float64, period)}
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
		lut := int(m.sampleIndex % uint64(m.period))
		out[*pos] = float32(defaultAmplitude * (iPart*m.cosLUT[lut] - qPart*m.sinLUT[lut]))
		(*pos)++
		m.sampleIndex++
	}
}
