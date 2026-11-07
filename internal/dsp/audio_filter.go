package dsp

import (
	"errors"
	"math"
)

const (
	modulatedAudioHighPassHz = 200.0
	modulatedAudioLowPassHz  = 4800.0
)

type biquadCoefficients struct {
	b0, b1, b2 float64
	a1, a2     float64
}

type biquadState struct {
	z1, z2 float64
}

// FilterModulatedAudioInPlace applies a gentle Butterworth band-pass edge
// filter to a generated TX waveform. The passband deliberately contains every
// configured modem carrier (1200, 1600, 2200 and 2400 Hz) and the useful first
// modulation sidebands. It attenuates DC/low-frequency rumble and out-of-band
// switching energy without changing packet timing or resampling the waveform.
// The waveform is filtered in place to avoid allocating a second full burst.
func FilterModulatedAudioInPlace(samples []float32, sampleRate int) error {
	if sampleRate < 8000 || sampleRate > 192000 {
		return errors.New("modulated audio filter sample rate must be within 8000..192000 Hz")
	}
	if len(samples) == 0 {
		return nil
	}
	if modulatedAudioHighPassHz <= 0 || modulatedAudioLowPassHz >= float64(sampleRate)/2 || modulatedAudioHighPassHz >= modulatedAudioLowPassHz {
		return errors.New("modulated audio filter passband is invalid for the selected sample rate")
	}
	highPass := designButterworthBiquad(modulatedAudioHighPassHz, float64(sampleRate), true, 0.7071067811865476)
	// A fourth-order Butterworth low-pass gives substantially cleaner upper
	// skirts than one biquad while remaining essentially flat at every modem
	// carrier. Its two Q sections are the standard 4th-order Butterworth pair.
	lowPass1 := designButterworthBiquad(modulatedAudioLowPassHz, float64(sampleRate), false, 0.541196100146197)
	lowPass2 := designButterworthBiquad(modulatedAudioLowPassHz, float64(sampleRate), false, 1.306562964876377)
	var hpState, lpState1, lpState2 biquadState
	for i, sample := range samples {
		value := applyBiquad(float64(sample), highPass, &hpState)
		value = applyBiquad(value, lowPass1, &lpState1)
		value = applyBiquad(value, lowPass2, &lpState2)
		if value > 1 {
			value = 1
		} else if value < -1 {
			value = -1
		}
		samples[i] = float32(value)
	}
	return nil
}

func designButterworthBiquad(cutoff, sampleRate float64, highPass bool, q float64) biquadCoefficients {
	omega := 2 * math.Pi * cutoff / sampleRate
	cosine, sine := math.Cos(omega), math.Sin(omega)
	alpha := sine / (2 * q)
	a0 := 1 + alpha
	var b0, b1, b2 float64
	if highPass {
		b0 = (1 + cosine) / 2
		b1 = -(1 + cosine)
		b2 = (1 + cosine) / 2
	} else {
		b0 = (1 - cosine) / 2
		b1 = 1 - cosine
		b2 = (1 - cosine) / 2
	}
	return biquadCoefficients{
		b0: b0 / a0, b1: b1 / a0, b2: b2 / a0,
		a1: (-2 * cosine) / a0, a2: (1 - alpha) / a0,
	}
}

func applyBiquad(input float64, coeff biquadCoefficients, state *biquadState) float64 {
	output := coeff.b0*input + state.z1
	state.z1 = coeff.b1*input - coeff.a1*output + state.z2
	state.z2 = coeff.b2*input - coeff.a2*output
	return output
}

// StreamModulatedAudioFilter preserves the biquad delay state between PCM
// blocks. It is equivalent to FilterModulatedAudioInPlace on one concatenated
// waveform, but requires memory proportional only to the current block.
type StreamModulatedAudioFilter struct {
	highPass biquadCoefficients
	lowPass1 biquadCoefficients
	lowPass2 biquadCoefficients
	hpState  biquadState
	lp1State biquadState
	lp2State biquadState
}

func NewStreamModulatedAudioFilter(sampleRate int) (*StreamModulatedAudioFilter, error) {
	if sampleRate < 8000 || sampleRate > 192000 {
		return nil, errors.New("modulated audio filter sample rate must be within 8000..192000 Hz")
	}
	if modulatedAudioLowPassHz >= float64(sampleRate)/2 {
		return nil, errors.New("modulated audio filter passband is invalid for the selected sample rate")
	}
	return &StreamModulatedAudioFilter{
		highPass: designButterworthBiquad(modulatedAudioHighPassHz, float64(sampleRate), true, 0.7071067811865476),
		lowPass1: designButterworthBiquad(modulatedAudioLowPassHz, float64(sampleRate), false, 0.541196100146197),
		lowPass2: designButterworthBiquad(modulatedAudioLowPassHz, float64(sampleRate), false, 1.306562964876377),
	}, nil
}

func (f *StreamModulatedAudioFilter) ProcessInPlace(samples []float32) {
	if f == nil {
		return
	}
	for i, sample := range samples {
		value := applyBiquad(float64(sample), f.highPass, &f.hpState)
		value = applyBiquad(value, f.lowPass1, &f.lp1State)
		value = applyBiquad(value, f.lowPass2, &f.lp2State)
		if value > 1 {
			value = 1
		} else if value < -1 {
			value = -1
		}
		samples[i] = float32(value)
	}
}
