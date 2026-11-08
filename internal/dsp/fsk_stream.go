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
	sampleRate           int
	bitRate              int
	markHz               int
	spaceHz              int
	sps                  int
	pending              []float32
	locked               bool
	offsetHz             int
	candidatePhase       int
	candidateOffset      int
	candidateCount       int
	samplesSinceAcquire  int
	acquisitionAttempted bool
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
	d.candidatePhase = 0
	d.candidateOffset = 0
	d.candidateCount = 0
	d.samplesSinceAcquire = 0
	d.acquisitionAttempted = false
}

func (d *FSKStreamDemodulator) Feed(samples []float32) []bool {
	if d == nil || len(samples) == 0 {
		return nil
	}
	d.pending = append(d.pending, samples...)
	if !d.locked {
		const acquireSymbols = 64
		need := acquireSymbols * d.sps
		d.samplesSinceAcquire += len(samples)
		if len(d.pending) < need {
			return nil
		}
		// The acquisition search is relatively expensive (a phase/frequency
		// sweep over 64 symbols).  Retry after half a window of new samples,
		// rather than repeating the same search on every small audio callback.
		// The 50% overlap still leaves ample preamble for stable confirmation.
		if d.acquisitionAttempted && d.samplesSinceAcquire < need/2 {
			return nil
		}
		d.acquisitionAttempted = true
		d.samplesSinceAcquire = 0
		start := len(d.pending) - need
		phase, offset, score, power := d.findBestPhase(d.pending[start:])
		globalPhase := (start + phase) % d.sps
		// Idle receiver noise may have substantial RMS energy and can produce
		// isolated mark/space imbalance peaks. Never lock on one marginal
		// window: require two overlapping windows with a stable timing phase
		// and carrier estimate. A very strong clean burst may lock immediately
		// so single-buffer capture decoding remains supported.
		const minAcquireScore = 0.68
		const immediateAcquireScore = 0.90
		const minAcquirePower = 1e-7
		if power < minAcquirePower || score < minAcquireScore {
			d.candidateCount = 0
			keep := need * 2
			if len(d.pending) > keep {
				d.pending = append([]float32(nil), d.pending[len(d.pending)-keep:]...)
			}
			return nil
		}
		if score >= immediateAcquireScore && power > 1e-4 {
			d.candidateCount = 2
		} else if d.candidateCount > 0 && absInt(globalPhase-d.candidatePhase) <= 1 && absInt(offset-d.candidateOffset) <= 25 {
			d.candidateCount++
		} else {
			d.candidatePhase = globalPhase
			d.candidateOffset = offset
			d.candidateCount = 1
		}
		if d.candidateCount < 2 {
			keep := need * 2
			if len(d.pending) > keep {
				d.pending = append([]float32(nil), d.pending[len(d.pending)-keep:]...)
			}
			return nil
		}
		// The acquisition window is only used to estimate symbol phase and
		// common tone offset. Do not discard the samples preceding that window:
		// they contain the packet preamble and often the first complete frames.
		d.pending = append([]float32(nil), d.pending[globalPhase:]...)
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
		markEnergy := toneEnergyAt(window, d.sampleRate, d.markHz+d.offsetHz)
		spaceEnergy := toneEnergyAt(window, d.sampleRate, d.spaceHz+d.offsetHz)
		out[i] = markEnergy >= spaceEnergy

	}
	consumed := count * d.sps
	if consumed == len(d.pending) {
		d.pending = d.pending[:0]
	} else {
		d.pending = append(d.pending[:0], d.pending[consumed:]...)
	}
	return out
}

// Locked reports whether the streaming receiver acquired and is currently
// tracking a symbol phase/frequency hypothesis. It is used only as independent
// signal-presence evidence for RX diagnostics; it does not certify a frame.
func (d *FSKStreamDemodulator) Locked() bool {
	return d != nil && d.locked
}

func (d *FSKStreamDemodulator) findBestPhase(samples []float32) (int, int, float64, float64) {
	bestPhase, bestOffset := 0, 0
	bestScore, bestPower := math.Inf(-1), 0.0
	evaluate := func(offset, phaseFirst, phaseStep, phaseLast int) {
		markHz, spaceHz := d.markHz+offset, d.spaceHz+offset
		if markHz <= 0 || spaceHz <= 0 || markHz >= d.sampleRate/2 || spaceHz >= d.sampleRate/2 {
			return
		}
		markCoefficient := 2 * math.Cos(2*math.Pi*float64(markHz)/float64(d.sampleRate))
		spaceCoefficient := 2 * math.Cos(2*math.Pi*float64(spaceHz)/float64(d.sampleRate))
		for phase := phaseFirst; phase <= phaseLast; phase += phaseStep {
			var score, power float64
			count := 0
			for start := phase; start+d.sps <= len(samples); start += d.sps {
				window := samples[start : start+d.sps]
				mark := toneEnergyCoefficient(window, markCoefficient)
				space := toneEnergyCoefficient(window, spaceCoefficient)
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

	// Coarse pass: 100 Hz carrier steps and every second sample phase.
	// This is sufficient to identify the local acquisition basin while
	// reducing the old 49*16 full search candidates to 13*8.
	for offset := -600; offset <= 600; offset += 100 {
		evaluate(offset, 0, 2, d.sps-1)
	}

	// Fine pass around the best coarse estimate. The 20 Hz frequency grid
	// is finer than the 64-symbol window's effective resolution, and the
	// adjacent sample phases cover the timing error left by the coarse pass.
	coarseOffset, coarsePhase := bestOffset, bestPhase
	bestScore, bestPower = math.Inf(-1), 0
	phaseStart, phaseEnd := coarsePhase-1, coarsePhase+1
	for offset := coarseOffset - 100; offset <= coarseOffset+100; offset += 20 {
		for phase := phaseStart; phase <= phaseEnd; phase++ {
			wrapped := phase % d.sps
			if wrapped < 0 {
				wrapped += d.sps
			}
			evaluate(offset, wrapped, d.sps, wrapped)
		}
	}
	return bestPhase, bestOffset, bestScore, bestPower
}

// toneEnergyAt evaluates a single-frequency Goertzel bin. The frequency is
// integer hertz because the acquisition offset grid is quantized in hertz.
func toneEnergyAt(samples []float32, sampleRate, frequency int) float64 {
	coefficient := 2 * math.Cos(2*math.Pi*float64(frequency)/float64(sampleRate))
	return toneEnergyCoefficient(samples, coefficient)
}

func toneEnergyCoefficient(samples []float32, coefficient float64) float64 {
	var previous, previous2 float64
	for _, sample := range samples {
		current := float64(sample) + coefficient*previous - previous2
		previous2, previous = previous, current
	}
	return previous*previous + previous2*previous2 - coefficient*previous*previous2
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
