# Modem weak-signal and frequency-offset characterization

The opt-in `TestModeNoiseAndOffsetCharacterization` test exercises all eight
transport modes using `Encoder.StreamPackets()` and the production streaming
receiver. In particular, QPSK/8PSK/16QAM are measured using the current SDF3
grouped training and descriptor, modulation-specific K=7 FEC profile
(rate 5/6 QPSK, rate 3/4 8PSK, rate 2/3 16QAM), interleaver and
soft-decision Viterbi receive path. The legacy `EncodePackets()`
M-ary waveform is not used for this report.

The AWGN sweep uses 30, 24, 18, 12, 9, 6, 3 and 0 dB. The default is ten
deterministic trials per point. Set `SATSSDV_CHARACTERIZATION_TRIALS` to a
smaller number for a quick CI screening report or a larger number for a more
repeatable estimate (up to 1000). CI currently uses two trials per point to
keep the dedicated job bounded; this is a regression screen, not a statistically
certified packet-error-rate curve. A threshold is reported only when at least
90% of trials pass at that point and at every higher SNR point.

The constant audio-IF CFO sweep uses +/-600 Hz in 100 Hz steps for FSK modes and
+/-300 Hz in 100 Hz steps for M-ary modes. Additional production-path tests
exercise M-ary SDF3 with 18 dB AWGN and +/-200 Hz constant CFO, +/-150 Hz linear
CFO ramps, and PCM gain levels down to 0.001. A separate 6 dB regression sends
two consecutive SDF3 groups with continuous channel noise in the 200 ms guard;
all 16 source packets must be recovered for all three modulations at callback
sizes 480, 997, 1920 and 4096 samples. The receiver re-arms at the exact group
body boundary rather than requiring the RF noise floor to resemble digital
silence. The report is written after each mode completes, so partial results
remain available if a long run is interrupted.

These are synthetic audio-baseband tests. They are not calibrated RF SNR, Eb/N0,
receiver sensitivity or an over-the-air performance guarantee. They do not
fully reproduce RF front-end selectivity, oscillator phase noise, fading,
multipath, time-varying multipath or hardware AGC/compression. Validate with
recorded receiver WAV/IQ captures and actual satellite passes before publishing
operational sensitivity or Doppler limits.

The live RX quality display uses an in-channel audio SNR estimate for AFSK 1200 and a synchronized constellation EVM-SNR estimate for QPSK/8PSK/16QAM, including M-ary candidates under Auto RX. GMSK 2400 still shows `SNR --` until a validated demodulator-domain estimator is implemented. EVM-SNR is not calibrated RF SNR/Eb/N0 and is not directly comparable with full-band PCM SNR. See [RX SNR estimation](RX_SNR_ESTIMATION.md) for the measurement definition and limitations.

Manual run from the repository root (PowerShell):

```powershell
$env:SATSSDV_CHARACTERIZATION = "1"
$env:SATSSDV_CHARACTERIZATION_TRIALS = "10"
$env:MODEM_REPORT_PATH = (Join-Path (Get-Location).Path "modem-characterization-report.md")
go test -count=1 -v ./internal/transport -run "^TestModeNoiseAndOffsetCharacterization$"
```
