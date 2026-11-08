# RX audio SNR and demodulator-quality measurement

## What the live number represents

The RX panel now exposes two different, explicitly named measurements:

- **AFSK 1200 SNR** is an audio-channel power estimate. It uses two separated
  mark/space tone groups and a learned in-channel idle-noise reference.
- **M-ary EVM-SNR** is a post-demodulation constellation-quality estimate for
  QPSK, 8PSK and 16QAM. It is computed from synchronized payload symbols and
  follows the demodulator's carrier/phase correction.

Neither measurement is calibrated RF SNR, C/N0, Eb/N0, receiver sensitivity
or antenna-system noise figure. In particular, EVM-SNR is measured in the
symbol decision aperture and **must not be compared directly with full-band
PCM SNR**, an SDR's RF SNR readout or a calibrated S-meter.

## AFSK 1200 audio SNR

AFSK 1200 provides two separated mark/space tone groups with intervening
spectral regions that can serve as a noise reference. The live tracker requires
an idle-channel noise profile, current two-tone spectral evidence, decoder
synchronization and a fresh CRC-valid packet. Packet evidence is limited to
1.2 seconds.

The displayed estimate integrates linear FFT-bin power in the configured
AFSK channel and subtracts the corresponding per-bin idle-noise estimate:

```text
Pnoise  = sum of learned idle-noise power for the in-band FFT bins
Ptotal  = sum of current power in those same bins
Psignal = max(Ptotal - Pnoise, 0)
SNR(dB) = 10 * log10(Psignal / Pnoise)
```

`Spectrum()` provides Hann-windowed amplitude in dBFS. The tracker converts
each bin to linear power before integration. The UI shows `SNR --` until the
baseline and all evidence are available. Values below 3 dB are suppressed.

## M-ary EVM-SNR

Wideband QPSK/8PSK/16QAM occupy too much of the useful audio spectrum to obtain
an independent noise estimate from adjacent FFT bins. Their numeric estimate
therefore comes from the live synchronized demodulator, not from an idle FFT
noise floor.

For each payload symbol, the modem first performs carrier/timing correction,
then compares the received complex symbol `r[k]` with its nearest ideal
constellation point `s[k]`. The receiver tracks the normalized mean-square
error:

```text
EVM^2 = mean(|r[k] - s[k]|^2) / mean(|s[k]|^2)
EVM-SNR(dB) = 10 * log10(1 / EVM^2)
```

A value is published only while carrier/timing lock is valid and at least 48
payload symbols have been measured. The estimate is cleared on loss of lock or
when the decoder re-arms for a new independently trained group. Auto RX can
show the value when one of its M-ary candidates is synchronized.

This is a **demodulator-domain EVM-SNR**, reflecting residual noise, phase error
and amplitude distortion at the symbol decision aperture. Its effective noise
bandwidth differs from the full PCM bandwidth. For example, in the deterministic
software fixture with 12 dB AWGN measured across the full 48 kHz PCM signal,
the observed EVM-SNR values were approximately 20.5 dB for QPSK, 17.7 dB for
8PSK and 16.7 dB for 16QAM. The difference is expected from the distinct
measurement bandwidths; these numbers are not a calibration table for RF SNR.

GMSK 2400 does not yet have a validated demodulator-domain estimate and remains
`SNR --`. AFSK and M-ary values are deliberately labelled separately in the
Qt UI.

## Noise-baseline learning (AFSK 1200)

The AFSK tracker learns the receive-channel noise profile over 24 noise-only
FFT windows. At the current 4096-sample, 48 kHz diagnostic window size, each
window is about 85 ms. The UI emits the spectrum up to 15 times per second, so
the initial warm-up generally takes about 1.6 seconds after the first valid
window. Until then, the UI shows `SNR --`.

A current modem-like two-tone spectrum is never learned as idle noise. When
the current window no longer contains the feature, the tracker follows the idle
noise even if a legacy demodulator retains an old carrier-lock state. A coherent
gain step across the idle channel is followed after repeated confirmation. An
unknown gain change during active reception still invalidates the assumptions
of an idle-profile estimator; fixed gain during a pass is recommended.

## Tests and limits

The regression suite covers white/colored idle noise, digital silence,
narrowband tone interference, current-window return to noise after a valid
packet, fresh-packet age gating, AFSK two-tone evidence and known synthetic
audio SNR. M-ary EVM-SNR is checked end-to-end through the production
`StreamPackets` encoder and `StreamDecoder` receiver, including idle pre-roll,
AWGN, and symbol-lock gating. Pure idle noise must not produce an EVM-SNR.

Synthetic tests are software regressions only. Recorded satellite audio is
still needed to characterize actual receiver noise, AGC behavior, compression,
audio filtering and Doppler. Absolute RF SNR/Eb/N0 requires a defined
measurement bandwidth and RF calibration.

## Recommended operational use

Use the AFSK display as a relative audio-channel SNR indication. Use M-ary
EVM-SNR to compare demodulator quality within the same modulation/profile and
receiver configuration, not as an RF SNR value. For GMSK 2400, rely on decode
success, CRC/FEC statistics and acquisition diagnostics until a validated
estimator is implemented.
