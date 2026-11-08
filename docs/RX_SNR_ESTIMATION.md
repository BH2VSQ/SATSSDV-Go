# RX audio SNR estimation

## Purpose and measurement definition

The RX workbench SNR indicator reports an **in-channel audio SNR estimate**.
It is not calibrated RF input SNR, carrier-to-noise density, Eb/N0, receiver
sensitivity, or a substitute for a calibrated noise-source measurement.

The estimator integrates the selected modem receive passband and compares it
with the idle-noise power measured in that same passband. It does not estimate
the channel noise from a distant audio band, because sound-card and receiver
filter responses can make the noise power at those frequencies substantially
different.

For the selected receive channel, the calculation is:

```text
Pnoise  = sum of learned idle-noise power for the in-channel FFT bins
Ptotal  = sum of current power for the same FFT bins
Psignal = max(Ptotal - Pnoise, 0)
SNR(dB) = 10 * log10(Psignal / Pnoise)
```

`Spectrum()` reports a Hann-windowed magnitude in dBFS. The implementation
converts each bin to linear power before integrating it. The displayed result
therefore refers to the configured modem audio passband, not the whole audio
spectrum.

## Noise-floor tracking

`dsp.BandSNRTracker` maintains an idle-noise power estimate for every FFT bin.
When RX starts, the tracker learns at least 12 noise-only windows before it
publishes an SNR. The UI emits a 4096-sample spectrum at up to 15 updates per
second, so this warm-up is typically around 0.8 seconds. During this period,
the UI displays `SNR -- dB`.

A separate spectral-shape detector is used only as a warm-up guard. If a
recognizable modem-shaped signal is already present, that window is not added
to the idle-noise profile. Once the tracker is ready, the displayed SNR comes
from the temporal in-channel noise profile, not from the instantaneous
sideband/guard-band estimator.

During idle reception, the noise profile follows ordinary changes slowly
(4% exponential update per accepted idle window). A near-full-passband rise,
which is more consistent with broadband noise or an AGC/audio gain transition
than a modem signal, is rejected as a signal and followed faster (25% update).
When a modem-like signal is accepted, the idle profile is held so the received
signal is not learned as noise.

## Signal-presence safeguards

An SNR number is not displayed merely because current channel power exceeds
the learned noise estimate. The candidate must also satisfy these safeguards:

- At least 150 Hz of estimated 90%-occupied excess bandwidth.
- The occupied bandwidth must remain below 82% of the configured receive
  passband. A near-full-band rise is treated as broadband noise/AGC change.
- The estimated in-channel SNR must be at least 3 dB.
- The warm-up detector rejects random noise and single narrowband tones. For
  AFSK 1200 it looks for separated mark/space tone groups; other modes use a
  wider spectral feature.

These are conservative display gates. A real signal below about 3 dB may be
present and partially decodable while the UI still shows `SNR -- dB`. An
unusual modem waveform that occupies almost the entire configured passband
may also be rejected by the broadband-noise safeguard.

## Calibration and regression coverage

`rx_snr_test.go` checks:

- White and colored idle noise across the supported RX profiles.
- Single-tone interference and AFSK mark/space evidence.
- 1000 deterministic noise windows with the signal hint forcibly asserted.
- QPSK, 8PSK, 16QAM, AFSK 1200 and GMSK 2400 against directly measured
  in-channel signal/noise power.
- Linear audio gain invariance, a 26 dB idle-noise gain step, and recovery of a
  real modem SNR estimate after the idle-noise profile adapts to that gain step.
- Burst-present-at-start behavior, ensuring a recognized burst is not learned
  as the idle-noise profile.

In the deterministic M-ary calibration sweep, the measured estimates at
6/9/12/18/24 dB were:

| Mode | 6 dB | 9 dB | 12 dB | 18 dB | 24 dB |
|---|---:|---:|---:|---:|---:|
| QPSK | 5.67 | 8.51 | 12.07 | 17.73 | 23.68 |
| 8PSK | 5.03 | 8.13 | 11.27 | 17.77 | 23.95 |
| 16QAM | 5.84 | 8.86 | 11.94 | 17.87 | 23.49 |

These are synthetic baseband calibration tests. The expected value is computed
from the same selected receive channel for both signal and noise. A whole-audio
band injected SNR is not directly comparable to this channel-integrated result.
The 0–3 dB points are intentionally not treated as dependable display values
in the current regression policy.

## Operating recommendations and limitations

For a meaningful baseline, start RX while the channel is idle and allow the
noise profile to finish learning before the satellite signal arrives. If RX
starts during a weak or unusual burst that the shape detector cannot recognize,
that burst could contaminate the initial noise profile. Resetting RX, or
waiting for an idle interval before the next pass, relearns the profile.

The tracker handles common stationary colored-noise spectra and broad level
changes, but cannot perfectly distinguish every time-varying interferer,
narrowband noise source, compression effect, or receiver AGC behavior from a
modem signal. The 82% occupancy gate is a deliberate false-positive trade-off.

Before using the displayed value as an operational station metric, compare it
against recorded WAV/IQ captures and a calibrated RF/noise measurement. The UI
is intended for relative monitoring of the selected audio channel, not as an
absolute receiver sensitivity instrument.
