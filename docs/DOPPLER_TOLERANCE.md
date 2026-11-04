# Doppler / frequency-offset tolerance test

This report records the reproducible **synthetic audio-IF test** added for the five transport profiles. It is not a claim of an over-the-air satellite pass test.

## Method

- Generate a valid SSDV packet or PCSI MMT packet with the production packet encoder.
- Generate the corresponding 19.2 kHz modem waveform with the production `transport.Encoder` for each profile.
- Apply a frequency translation to the real-valued audio passband using the analytic-signal frequency shifter, then feed the samples to the production streaming RX decoder in 1024-sample chunks.
- A test is counted as successful only when exactly one packet is recovered and its packet ID matches the transmitted packet. SSDV FEC/CRC and PCSI RS8 parsing therefore remain in the test path.
- Sweep constant audio-IF offsets from -1500 Hz to +1500 Hz in 100 Hz increments. Separately apply linear ramps of +100 to -100 Hz and -100 to +100 Hz over one packet waveform.

## Results

All five modes pass every tested constant offset from -500 Hz through +500 Hz, and both ±100 Hz linear ramps:

| Profile | -600 Hz | -500…+500 Hz | +600 Hz | Conservative symmetric range observed |
|---|---:|---:|---:|---:|
| SSDV AX.25 | Pass | Pass | Pass | ±600 Hz |
| SSDV CCSDS 1200 | Fail | Pass | Pass | ±500 Hz |
| SSDV CCSDS 2400 | Pass | Pass | Pass | ±600 Hz |
| PCSI CCSDS 1200 | Fail | Pass | Pass | ±500 Hz |
| PCSI CCSDS 2400 | Pass | Pass | Pass | ±600 Hz |

The wider ±1500 Hz scan is deliberately treated as a characterization, not as a continuous tolerance guarantee. It produced isolated, non-monotonic successes outside the conservative ranges, which can result from tone/phase acquisition aliases. For example, some +700/+900 Hz test points decoded while a neighbouring offset did not. Those isolated points must not be interpreted as a stable operating range.

The 1200-bit/s profiles use a bounded mark/space acquisition search. Its current acquisition sweep is ±600 Hz. The 2400-bit/s profiles estimate a common audio carrier offset from the balanced preamble and track it slowly during the burst. These results are a useful synthetic regression baseline, not a complete Doppler AFC specification.

## How to interpret this for a linear satellite

The test shifts the modem's **audio passband** after waveform generation. Actual satellite Doppler first shifts the RF carrier. How much of that shift reaches the decoder depends on the radio's tuning correction, passband/audio filtering, the transponder translation, audio output mode, and whether CAT/rotor software is already correcting Doppler. A radio that is correctly tracking the downlink may present only a small residual audio offset even when the RF Doppler is several kHz.

No user-supplied IQ recording, SDR capture, or Hamstation WAV replay was available for this work. Therefore there is no real linear-satellite reception result in this report. A proper on-air validation should replay one recorded pass through the actual radio/audio path and record residual audio offset over time, then compare packet success, RS corrections, CRC failures, packet loss, and reconstructed image coverage for the same capture.
