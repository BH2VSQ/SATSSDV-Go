package dsp

import (
    "math"
    "sort"
)

// BandSNR is an audio-baseband estimate from one spectrum window. It is not
// calibrated RF SNR, Eb/N0 or a standards-compliance measurement.
type BandSNR struct {
    SNRDB          float64
    NoiseFloorDBFS float64 // estimated mean noise power per FFT bin
    SignalBandDBFS float64 // integrated energy in the configured signal band
    Valid          bool
}

// EstimateBandSNR estimates integrated in-band SNR. The median guard-band
// periodogram power is divided by ln(2) to estimate its mean and is then
// scaled to the number of bins in the signal band. Frequency arguments are Hz.
func EstimateBandSNR(bins []SpectrumBin, signalLowHz, signalHighHz, noiseLowHz, noiseHighHz float64) BandSNR {
    result := BandSNR{}
    if len(bins) < 8 || !finiteSNR(signalLowHz) || !finiteSNR(signalHighHz) ||
        !finiteSNR(noiseLowHz) || !finiteSNR(noiseHighHz) || signalLowHz < 0 || noiseLowHz < 0 ||
        signalHighHz <= signalLowHz || noiseHighHz <= noiseLowHz {
        return result
    }
    signalPower := 0.0
    signalCount := 0
    noise := make([]float64, 0, len(bins)/4)
    for _, bin := range bins {
        if !finiteSNR(bin.FrequencyHz) || !finiteSNR(bin.MagnitudeDBFS) || bin.MagnitudeDBFS < -200 || bin.MagnitudeDBFS > 40 {
            continue
        }
        power := math.Pow(10, bin.MagnitudeDBFS/10)
        if bin.FrequencyHz >= signalLowHz && bin.FrequencyHz <= signalHighHz {
            signalPower += power
            signalCount++
        }
        if bin.FrequencyHz >= noiseLowHz && bin.FrequencyHz <= noiseHighHz {
            noise = append(noise, power)
        }
    }
    if signalCount < 3 || len(noise) < 5 {
        return result
    }
    sort.Float64s(noise)
    mid := len(noise)/2
    median := noise[mid]
    if len(noise)%2 == 0 {
        median = (noise[mid-1] + noise[mid]) / 2
    }
    noisePerBin := median / math.Ln2 // exponential periodogram median -> mean
    if !finiteSNR(noisePerBin) || noisePerBin <= 0 || !finiteSNR(signalPower) {
        return result
    }
    noiseInBand := noisePerBin * float64(signalCount)
    result.NoiseFloorDBFS = 10 * math.Log10(math.Max(noisePerBin, 1e-20))
    result.SignalBandDBFS = 10 * math.Log10(math.Max(signalPower, 1e-20))
    signalOnly := signalPower - noiseInBand
    if signalOnly <= 0 {
        return result
    }
    result.SNRDB = 10 * math.Log10(signalOnly / noiseInBand)
    result.Valid = finiteSNR(result.SNRDB)
    return result
}

func finiteSNR(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
