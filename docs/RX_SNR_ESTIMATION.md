# RX audio SNR measurement and validity

## What the live number represents

The live RX SNR number is currently enabled only for the AFSK 1200 profiles (SSDV AX.25, SSDV CCSDS 1200 and PCSI CCSDS 1200). It is an **audio-channel estimate**, not calibrated RF SNR, C/N0, Eb/N0, receiver sensitivity, or an antenna-system noise figure. It must not be compared directly with a calibrated S-meter or an SDR RF SNR readout.

AFSK 1200 provides two separated mark/space tone groups with intervening spectral regions that can serve as a noise reference. The stateless detector requires two separated spectral clusters; the live tracker also requires an idle-channel noise profile, current decoder synchronization and a fresh CRC-valid packet. The packet evidence used for SNR is limited to 1.2 seconds, even though the general RX diagnostics retain their separate three-second recent-packet indicator.

The displayed audio estimate integrates linear FFT-bin power in the configured AFSK channel and subtracts the corresponding per-bin idle-noise estimate:

```text
Pnoise  = sum of learned idle-noise power for the in-band FFT bins
Ptotal  = sum of current power in those same bins
Psignal = max(Ptotal - Pnoise, 0)
SNR(dB) = 10 * log10(Psignal / Pnoise)
```

`Spectrum()` provides Hann-windowed amplitude in dBFS. The tracker converts each bin to linear power before integration. The UI shows `SNR --` until the baseline and all evidence are available. Values below 3 dB are suppressed.

## Why M-ary, GMSK and Auto currently show `SNR --`

QPSK, 8PSK, 16QAM and GMSK occupy most of their configured audio analysis bands. There are not enough reliably signal-free FFT bins to estimate the current noise floor from the same window. An idle-noise profile learned before the pass can also become invalid if the receiver AGC/input gain changes while a signal occupies the band. The resulting signal/noise ratio can be grossly overstated.

For this reason, live numeric SNR is deliberately disabled for QPSK, 8PSK, 16QAM, CCSDS 2400 GMSK and Auto RX. The UI explains that the estimate is unavailable instead of presenting an attractive but unsupported number. Re-enabling those profiles requires a modem-derived estimate from known training/pilot symbols or a calibrated receiver noise measurement; simply changing a frequency guard or gain coefficient is not sufficient.

## Noise-baseline learning (AFSK 1200)

The AFSK tracker learns the actual receive-channel noise profile over 24 noise-only FFT windows. At the current 4096-sample, 48 kHz diagnostic window size, each window is about 85 ms. The UI emits the spectrum up to 15 times per second, so the initial warm-up generally takes about 1.6 seconds after the first valid window. Until then, the UI shows `SNR --`.

A current modem-like two-tone spectrum is never learned as idle noise. When the current window no longer contains the feature, the tracker follows the idle noise even if a legacy demodulator retains an old carrier-lock state. A coherent gain step across the idle channel is followed after repeated confirmation. An unknown gain change during active reception still invalidates the assumptions of an idle-profile estimator; fixed gain during a pass is recommended.

## Tests and limits

The regression suite covers white/colored idle noise, digital silence, narrowband tone interference, current-window return to noise after a valid packet, fresh-packet age gating, AFSK two-tone evidence and known synthetic audio SNR. Generic DSP tracker tests also exercise synthetic broad-band signals, but those do **not** authorize the live UI to show M-ary/GMSK SNR.

Synthetic tests are useful for software regression only. Recorded satellite audio is still needed to characterize actual receiver noise, AGC behavior, compression, audio filtering and Doppler. Absolute RF SNR/Eb/N0 requires a defined measurement bandwidth and RF calibration.

## Recommended operational use

Use the AFSK live estimate only as a relative audio-channel indication and keep receiver gain fixed during a pass. For M-ary/GMSK, rely on decode success, CRC/FEC statistics and acquisition diagnostics until a training-symbol/pilot-based estimator is implemented and validated against recordings.
