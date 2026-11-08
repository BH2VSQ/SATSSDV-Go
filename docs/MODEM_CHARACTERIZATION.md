# Modem noise and frequency-offset characterization

The optional `TestModeNoiseAndOffsetCharacterization` test covers all eight
transport modes through the production encoder and streaming decoder. CI runs
it in a dedicated job and stores `modem-characterization-report.md` as an
artifact.

It sweeps AWGN SNR at 30, 24, 18, 12, 9, 6, 3 and 0 dB with ten deterministic
seeds per point, and constant audio-IF offset from -600 to +600 Hz in 100 Hz
steps. The report records modulation/rate parameters, decode success rate,
the lowest coarse-grid SNR with at least 9/10 successes at that and every
higher point, and the contiguous CFO interval around zero.

These results are synthetic audio-baseband measurements, not calibrated RF
SNR, Eb/N0, receiver sensitivity or an over-the-air Doppler test. The SNR
threshold is not a statistically certified PER curve. Before treating it as a
hardware specification, repeat with multiple image payloads, 1 dB steps, at
least 100 seeds, actual receiver recordings, RF filtering, AGC, clipping, gain
variation and oscillator errors.

The live RX SNR indicator is an audio-band estimate. Median guard-band noise
power is used to estimate and subtract in-band noise from the configured signal
band. It is useful for relative monitoring and loopback comparison, but is not
an RF-calibrated measurement.

Manual run:

```sh
SATSSDV_CHARACTERIZATION=1 MODEM_REPORT_PATH=modem-characterization-report.md \
  go test -count=1 -v ./internal/transport -run '^TestModeNoiseAndOffsetCharacterization$'
```
