# RX preview and AX.25 / CCSDS 1200 interoperability regression (round 14)

## Changes

### RX image viewport

- The RX image element now uses a height-first fit (`height: 100%; width: auto`) inside the fixed, clipped viewport.
- `max-width` and `max-height` remain bounded by the viewport, and `object-fit: contain` preserves the complete image without stretching or cropping.
- The same viewport is used by low, medium and high resolution images.

### Remove constellation display

- Removed RX/TX constellation canvases and side panels from the workbench.
- Removed frontend constellation drawing/history and Wails event subscriptions.
- Removed TX-side repeated waveform re-demodulation used only for constellation display.
- Removed unused FSK/GMSK constellation sample buffers and transport wrappers.
- Waterfall magnitude display remains unchanged and occupies the full spectrum panel width.

### FSK acquisition data-loss fix

`FSKStreamDemodulator.Feed` uses the trailing 64 symbols to estimate the symbol phase and common tone offset. Previously, after the estimate it sliced `pending` from `start + phase`, discarding every sample before the trailing acquisition window. When the first RX callback contained a large part of the burst (or the whole burst), valid preamble and complete image frames were thrown away before HDLC/CCSDS framing saw them.

The demodulator now converts the phase found relative to the trailing window into a sample offset relative to the beginning of the pending stream:

```go
globalPhase := (start + phase) % d.sps
d.pending = append([]float32(nil), d.pending[globalPhase:]...)
```

This preserves all aligned symbols while still using the last 64 symbols for acquisition. AX.25 HDLC framing and CCSDS ASM scanning can then process the whole burst. No FCS, ASM, RS8 or CRC validation was weakened.

## End-to-end regression

`internal/transport/codec_roundtrip_test.go` exercises the production path:

1. Generate a 320x240 image.
2. Encode it using the SSDV image codec into 17 FEC packets.
3. Encode the packets using the production AX.25 or CCSDS 1200 transport encoder.
4. Decode with both `DecodeAudioAtRate` and `StreamDecoder`.
5. Verify all 17 packet IDs, callsign and image ID.

Streaming callback sizes tested:

- whole waveform in one call
- 480 samples
- 997 samples (not a multiple of 16 samples/symbol)
- 1920 samples

All cases recover 17/17 packets for both AX.25 and CCSDS 1200. The regression specifically prevents the previous large-first-callback data loss.

## Verification boundary

These are deterministic production encoder/decoder loopback tests with synthetic audio waveforms. They prove the current Go transmit and receive implementations agree over the tested callback boundaries. They do not replace real radio/SDR recording tests with independent transmitters, audio device clock error, multipath, clipping, noise, or Doppler.
