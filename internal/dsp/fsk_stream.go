package dsp

import (
	"errors"
	"math"
)

// FSKStreamDemodulator acquires a symbol phase from a short signal window and
// then keeps its sample clock and residual samples across Feed calls. It
// returns one hard-decision mark/space level per recovered symbol.
//
// This is a bounded-complexity nominal-clock receiver. It is suitable for
// continuous packet capture where the radio and audio clocks are close; a
// frequency/timing tracking loop is still needed for large oscillator errors.
type FSKStreamDemodulator struct {
	sampleRate int
	bitRate    int
	markHz     int
	spaceHz    int
	sps        int
	pending    []float32
	locked     bool
	offsetHz   int
}

func NewFSKStreamDemodulator(sampleRate, bitRate, markHz, spaceHz int) (*FSKStreamDemodulator, error) {
	if sampleRate <= 0 || bitRate <= 0 || sampleRate%bitRate != 0 {
		return nil, errors.New("sample rate must be an integer multiple of bit rate")
	}
	if markHz <= 0 || spaceHz <= 0 || markHz >= sampleRate/2 || spaceHz >= sampleRate/2 || markHz == spaceHz {
		return nil, errors.New("invalid FSK tone frequencies")
	}
	return &FSKStreamDemodulator{
		sampleRate: sampleRate,
		bitRate:    bitRate,
		markHz:     markHz,
		spaceHz:    spaceHz,
		sps:        sampleRate / bitRate,
	}, nil
}

func (d *FSKStreamDemodulator) Reset() {
	if d == nil {
		return
	}
	d.pending = nil
	d.locked = false
	d.offsetHz = 0
}

func (d *FSKStreamDemodulator) Feed(samples []float32) []bool {
	if d == nil || len(samples) == 0 {
		return nil
	}
	d.pending = append(d.pending, samples...)
	if !d.locked {
		const acquireSymbols = 64
		need := acquireSymbols * d.sps
		if len(d.pending) < need {
			return nil
		}
		start := len(d.pending) - need
		phase, offset, score, power := d.findBestPhase(d.pending[start:])
		// Do not lock to silence or a nearly silent input. Retain a short tail
		// and continue searching as soon as signal energy arrives.
		if power < 1e-7 || score < 0.015 {
			keep := need * 2
			if len(d.pending) > keep {
				d.pending = append([]float32(nil), d.pending[len(d.pending)-keep:]...)
			}
			return nil
		}
		start += phase
		d.pending = append([]float32(nil), d.pending[start:]...)
		d.offsetHz = offset
		d.locked = true
	}

	count := len(d.pending) / d.sps
	if count == 0 {
		return nil
	}
	out := make([]bool, count)
	for i := 0; i < count; i++ {
		window := d.pending[i*d.sps : (i+1)*d.sps]
		out[i] = toneEnergyAt(window, d.sampleRate, d.markHz+d.offsetHz) >= toneEnergyAt(window, d.sampleRate, d.spaceHz+d.offsetHz)
	}
	consumed := count * d.sps
	if consumed == len(d.pending) {
		d.pending = d.pending[:0]
	} else {
		d.pending = append(d.pending[:0], d.pending[consumed:]...)
	}
	return out
}

func (d *FSKStreamDemodulator) findBestPhase(samples []float32) (int, int, float64, float64) {
	// Search a common audio-IF shift for both FSK tones. This models an
	// uncorrected passband/IF offset caused by Doppler or receiver tuning.
	// A coarse sweep finds the carrier, then a narrow sweep refines it.
	bestPhase, bestOffset := 0, 0
	bestScore, bestPower := math.Inf(-1), 0.0
	evaluate := func(offset int) {
		for phase := 0; phase < d.sps; phase++ {
			var score, power float64
			count := 0
			markHz, spaceHz := d.markHz+offset, d.spaceHz+offset
			if markHz <= 0 || spaceHz <= 0 || markHz >= d.sampleRate/2 || spaceHz >= d.sampleRate/2 {
				continue
			}
			for start := phase; start+d.sps <= len(samples); start += d.sps {
				window := samples[start : start+d.sps]
				mark := toneEnergyAt(window, d.sampleRate, markHz)
				space := toneEnergyAt(window, d.sampleRate, spaceHz)
				sum := mark + space
				power += sum / float64(d.sps*d.sps)
				score += math.Abs(mark-space) / (sum + 1e-18)
				count++
			}
			if count == 0 {
				continue
			}
			score /= float64(count)
			power /= float64(count)
			if score > bestScore {
				bestPhase, bestOffset, bestScore, bestPower = phase, offset, score, power
			}
		}
	}
	for offset := -600; offset <= 600; offset += 25 {
		evaluate(offset)
	}
	coarse := bestOffset
	for offset := coarse - 25; offset <= coarse+25; offset += 5 {
		evaluate(offset)
	}
	return bestPhase, bestOffset, bestScore, bestPower
}

// toneEnergyAt evaluates a single-frequency Goertzel bin. The frequency is
// integer hertz because the acquisition loop searches in 5 Hz increments.
func toneEnergyAt(samples []float32, sampleRate, frequency int) float64 {
	omega := 2 * math.Pi * float64(frequency) / float64(sampleRate)
	coefficient := 2 * math.Cos(omega)
	var previous, previous2 float64
	for _, sample := range samples {
		current := float64(sample) + coefficient*previous - previous2
		previous2, previous = previous, current
	}
	return previous*previous + previous2*previous2 - coefficient*previous*previous2
}
