# Modem weak-signal and frequency-offset characterization

The opt-in `TestModeNoiseAndOffsetCharacterization` test exercises all eight
transport modes using `Encoder.StreamPackets()` and the production streaming
receiver. In particular, QPSK/8PSK/16QAM are measured using the current SDF3
per-data-unit training, repeated descriptor, K=7 rate-1/2 convolutional FEC,
interleaver and soft-decision Viterbi receive path. The legacy `EncodePackets()`
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
CFO ramps, and PCM gain levels down to 0.001. The report is written after each
mode completes, so partial results remain available if a long run is interrupted.

These are synthetic audio-baseband tests. They are not calibrated RF SNR, Eb/N0,
receiver sensitivity or an over-the-air performance guarantee. They do not
fully reproduce RF front-end selectivity, oscillator phase noise, fading,
multipath, time-varying multipath or hardware AGC/compression. Validate with
recorded receiver WAV/IQ captures and actual satellite passes before publishing
operational sensitivity or Doppler limits.

The live RX SNR value is an audio-band estimate, not an RF-calibrated metric.

Manual run from the repository root (PowerShell):

```powershell
$env:SATSSDV_CHARACTERIZATION = "1"
$env:SATSSDV_CHARACTERIZATION_TRIALS = "10"
$env:MODEM_REPORT_PATH = (Join-Path (Get-Location).Path "modem-characterization-report.md")
go test -count=1 -v ./internal/transport -run "^TestModeNoiseAndOffsetCharacterization$"
```
