# Symbol-space constellation monitoring

## Why the supplied GRC graph is not copied block-for-block

The supplied GRC flow forms its constellation after a receiver chain:

1. audio is converted to a complex stream and mixed from the selected IF to baseband;
2. a low-pass filter and AGC condition the signal;
3. an FLL band-edge loop acquires carrier-frequency offset;
4. PFB clock synchronization performs matched filtering and symbol timing recovery;
5. a second-order Costas loop recovers carrier phase;
6. an LMS decision-directed equalizer reduces channel distortion;
7. the QT GUI constellation sink plots the equalized complex symbol samples.

Its `variable_constellation` contains `[-1, +1]` with two-fold rotational symmetry, i.e. a BPSK decision constellation. Plotting the input audio FFT's `Real` and `Imag` fields is not equivalent: those are complex Fourier coefficients indexed by frequency bins, not recovered modulation symbols. Their phase changes with the FFT window and tone frequency, so plotting them tends to form radial or circular clouds.

SATSSDV-Go's supported on-air profiles are not that BPSK waveform. AX.25 and CCSDS 1200 use two audio-tone FSK paths; CCSDS 2400 uses the ARCSSD Gaussian-shaped continuous-phase/GMSK modem. Applying the GRC BPSK Costas/equalizer chain indiscriminately would make the constellation look plausible while displaying the wrong signal model.

## Mode-specific signal-space points

- **AX.25 AFSK 1200 and CCSDS 1200:** after the stream receiver's symbol-phase acquisition, each recovered symbol is passed through the same mark- and space-tone quadrature correlators used for the hard decision. The displayed coordinates are the matched-tone amplitudes `(MARK, SPACE)`. MARK symbols cluster on the positive horizontal axis; SPACE symbols cluster on the positive vertical axis. A slow display-only amplitude reference keeps the plot readable without feeding back into bit decisions.
- **CCSDS 2400 GMSK:** points are collected from the ARCSSD receiver's phase discriminator after its carrier-offset correction and symbol-clock loop. The soft decision `sin(corrected phase difference)` is plotted on the real symbol axis. This is a one-dimensional differential symbol decision, not the constant-envelope RF/IF carrier circle.

RX points are emitted from the same persistent stream decoder that handles packet reception. TX monitoring decodes a trailing waveform window with a fresh instance of the corresponding receiver, so the two panes use the same symbol decision plane. The waterfall still consumes FFT magnitude bins; its data is no longer reused as constellation input.

## Notes and limits

The current 1200-bit/s FSK receiver still uses a fixed nominal samples-per-symbol clock after acquisition; it does not yet implement GNU Radio's PFB clock synchronizer or a continuously tracking FLL. The 2400-bit/s path uses its existing ARCSSD phase/timing loop. The present display is therefore a standards-consistent symbol-space view for the application's actual modulations, but it is not a port of GNU Radio's BPSK receiver. If a BPSK profile is added later, the GRC sequence (FLL -> matched/PFB timing recovery -> Costas -> adaptive equalizer -> symbol sink) is an appropriate reference pipeline for that profile.
