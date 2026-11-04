# AX.25 / CCSDS 1200 FSK acquisition fix

## Failure mode

The 1200-bit/s AX.25 and CCSDS profiles share `dsp.FSKStreamDemodulator`. Before this fix it treated one 64-symbol window with even a very small mark/space discrimination score as a valid acquisition. During an active RX session, broadband idle audio noise could satisfy that weak threshold. The demodulator then remained locked to the noise-derived symbol phase and common tone offset, so later AX.25 HDLC flags or CCSDS ASM bits were sampled incorrectly. This affected both 1200-bit/s modes while the separate 2400-bit/s GMSK path was unaffected.

## Fix

- Acquisition now requires a meaningful mark/space discrimination score.
- Marginal acquisition candidates must repeat over two overlapping windows with stable symbol phase and common tone offset.
- A clean, high-energy burst can still lock immediately to preserve single-buffer/capture decoding.
- Samples before the confirmed acquisition window are retained, so the start of the burst and its preamble are not discarded.
- The AFC search remains enabled; this does not replace the carrier offset search with a hard-coded nominal frequency.

## Regression coverage

`internal/transport/fsk_noise_acquisition_test.go` feeds 500 ms of deterministic idle audio noise before a valid image burst and asserts exactly one valid SSDV packet is recovered for both AX.25 and CCSDS 1200. It covers native 19.2 kHz and 48 kHz input rates, and callback sizes 480, 997, 1920 samples plus a single full-buffer input.

The existing production-path round-trip test continues to validate all packets across arbitrary chunks, and the Doppler audio-IF sweep continues to exercise offsets from -1500 Hz to +1500 Hz, requiring the established ±500 Hz interval to pass.

These are deterministic synthetic waveform/capture tests. They do not replace replay of a real satellite SDR/electrical audio recording.
