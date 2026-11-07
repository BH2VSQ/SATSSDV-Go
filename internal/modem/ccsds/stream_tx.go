package ccsds

import (
	"errors"
	"math"
)

// AFSKStreamModulator keeps the DDS and Gaussian-FIR state across arbitrary
// input blocks. WriteBytes and WriteBits therefore produce one continuous
// phase-coherent waveform without retaining the whole transmission in RAM.
type AFSKStreamModulator struct {
	sampleRate        int
	bitRate           int
	markHz            int
	spaceHz           int
	sps               int
	phase             int
	phaseFloat        float64
	delay             [25]int32
	arcssd1200        bool
	arcssd2400        bool
	ccsds1200Polarity bool
}

func NewAFSKStreamModulator(sampleRate, bitRate, markHz, spaceHz int) (*AFSKStreamModulator, error) {
	if sampleRate <= 0 || bitRate <= 0 || sampleRate%bitRate != 0 {
		return nil, errors.New("sample rate must be an integer multiple of bit rate")
	}
	if markHz <= 0 || spaceHz <= 0 || markHz >= sampleRate/2 || spaceHz >= sampleRate/2 {
		return nil, errors.New("invalid AFSK tone frequencies")
	}
	return &AFSKStreamModulator{
		sampleRate: sampleRate, bitRate: bitRate, markHz: markHz, spaceHz: spaceHz,
		sps:        sampleRate / bitRate,
		arcssd1200: sampleRate == 19200 && bitRate == 1200 && markHz == 1200 && spaceHz == 2200,
		arcssd2400: sampleRate == 19200 && bitRate == 2400 && markHz == 1200 && spaceHz == 2400,
	}, nil
}

// NewCCSDSAFSKStreamModulator selects the original CCSDS phase-accumulator
// polarity. CCSDS 1200 uses 65 phase steps for mark while AX.25 AFSK uses 64;
// keeping the paths explicit preserves the established on-air waveforms.
func NewCCSDSAFSKStreamModulator(sampleRate, bitRate, markHz, spaceHz int) (*AFSKStreamModulator, error) {
	m, err := NewAFSKStreamModulator(sampleRate, bitRate, markHz, spaceHz)
	if err != nil {
		return nil, err
	}
	m.ccsds1200Polarity = true
	return m, nil
}

// WriteBytes modulates MSB-first octets. It returns only the samples generated
// for this call; phase, pulse-shaping delay and symbol timing remain in m.
func (m *AFSKStreamModulator) WriteBytes(data []byte) []float32 {
	if m == nil || len(data) == 0 {
		return nil
	}
	out := make([]float32, len(data)*8*m.sps)
	pos := 0
	for _, value := range data {
		for bit := 7; bit >= 0; bit-- {
			m.writeBit(value&(1<<uint(bit)) != 0, out, &pos)
		}
	}
	return out
}

// WriteBits modulates an already line-coded bit stream. It is used by AX.25,
// whose caller carries NRZI state between frames.
func (m *AFSKStreamModulator) WriteBits(bits []bool) []float32 {
	if m == nil || len(bits) == 0 {
		return nil
	}
	out := make([]float32, len(bits)*m.sps)
	pos := 0
	for _, bit := range bits {
		m.writeBit(bit, out, &pos)
	}
	return out
}

func (m *AFSKStreamModulator) writeBit(mark bool, out []float32, pos *int) {
	if m.arcssd1200 {
		increment := 117
		if mark {
			increment = 64
			if m.ccsds1200Polarity {
				increment = 65
			}
		}
		for i := 0; i < m.sps; i++ {
			m.phase = (m.phase + increment) & 1023
			out[*pos] = float32(0.9155273 * arcssdSineTable[m.phase])
			*pos++
		}
		return
	}
	if m.arcssd2400 {
		deviation := int32(31)
		if mark {
			deviation = -31
		}
		taps := [...]int32{2, 6, 19, 52, 132, 299, 607, 1105, 1804, 2643, 3471, 4087, 4316, 4087, 3471, 2643, 1804, 1105, 607, 299, 132, 52, 19, 6, 2}
		for i := 0; i < m.sps; i++ {
			copy(m.delay[:], m.delay[1:])
			m.delay[len(m.delay)-1] = deviation
			var accumulator int32
			for j := range taps {
				accumulator += m.delay[j] * taps[j]
			}
			shaped := (accumulator + 16384) >> 15
			m.phase = (m.phase + int(shaped) + 96) & 1023
			out[*pos] = float32(0.9155273 * arcssdSineTable[m.phase])
			*pos++
		}
		return
	}
	frequency := m.spaceHz
	if mark {
		frequency = m.markHz
	}
	increment := 2 * math.Pi * float64(frequency) / float64(m.sampleRate)
	for i := 0; i < m.sps; i++ {
		out[*pos] = float32(.9155273 * math.Sin(m.phaseFloat))
		m.phaseFloat += increment
		if m.phaseFloat >= 2*math.Pi {
			m.phaseFloat -= 2 * math.Pi
		}
		*pos++
	}
}
