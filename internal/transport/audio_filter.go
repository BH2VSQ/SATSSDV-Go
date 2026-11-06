package transport

import (
	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
)

// FilterTXWaveformInPlace applies generic audio-band cleanup only to
// constant-envelope FSK waveforms. M-ary profiles encode information in
// amplitude and must bypass this filter to preserve their constellation.
func FilterTXWaveformInPlace(profileID domain.TransportMode, samples []float32, sampleRate int) error {
	profile, err := domain.ParseProfile(string(profileID))
	if err != nil {
		return err
	}
	if profile.LinkLayer == "CCSDS-MARY" {
		return nil
	}
	return dsp.FilterModulatedAudioInPlace(samples, sampleRate)
}
