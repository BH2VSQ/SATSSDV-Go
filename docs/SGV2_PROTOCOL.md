# SATSSDV-Go GIF transport: SGV2

## Goals and compatibility

SGV2 adds MCU-level motion compensation to the existing SATSSDV-Go GIF extension. It does not change the 256-byte SSDV packet size, the ordinary SSDV data packet layout, AX.25/CCSDS framing, or the existing keyframe recovery policy.

The new receiver accepts both:

- **SGV1**: the original `SGV1` frame-control envelope with position-based sparse MCU updates.
- **SGV2**: the same envelope and base flags, plus a versioned motion-vector extension on frame-open controls.

The transmitter's legacy `gifmode.Encode` / `EncodeContext` entry points continue to produce SGV1. `EncodeContextV2` produces SGV2 and is used by the current application for GIF template transmission.

An old SGV1-only receiver can still identify the common SSDV/GIF control envelope, but it does not implement motion compensation. It must not be expected to reconstruct SGV2 delta frames correctly. To transmit to such a receiver, use the SGV1 encoder path. This is receiver-side backward compatibility, not a claim that old firmware can decode new motion semantics.

## Frame-control packet layout

Offsets are relative to the complete 256-byte SSDV FEC packet. Fields not listed remain the existing SSDV/GIF fields.

| Offset | Size | SGV2 meaning |
|---:|---:|---|
| 17 | 4 | ASCII `SGV1` base envelope magic (retained for compatibility) |
| 21 | 1 | Existing flags in bits 0..3; bit 4 means SGV2 extension present |
| 22 | 2 | Existing big-endian expected image-data packet count |
| 24 | 1 | Extension version, value `2` |
| 25 | 1 | Number of motion-vector records, 0..32 |
| 26 | 1 | Motion-vector record size, exactly `6` |
| 27 | N×6 | Motion-vector records |
| 220 | 4 | Existing CRC-32, recalculated over the extension |
| 224 | 32 | Existing RS8 parity, recalculated after the extension |

The extension is present only on frame-control packets. A non-SGV2 packet has bit 4 clear and is parsed as SGV1.

## Motion-vector record

Each record is six bytes:

| Byte | Field | Unit |
|---:|---|---|
| 0 | `SourceX` | MCU column in the previous composed frame |
| 1 | `SourceY` | MCU row in the previous composed frame |
| 2 | `Width` | MCU columns |
| 3 | `Height` | MCU rows |
| 4 | `DeltaX` | signed MCU columns |
| 5 | `DeltaY` | signed MCU rows |

One MCU is 16×16 pixels. The destination rectangle is:

`(SourceX + DeltaX, SourceY + DeltaY, Width, Height)`

The receiver copies source pixels from an immutable snapshot of the previous composed frame into the destination rectangle. The transmitted SSDV sparse residual packets are then applied to the predicted canvas. This allows the residual to clear the old location, update uncovered background, or repair imperfect matches.

Vectors are currently limited to a ±6 MCU search displacement, at most 32 records per frame, and must fit entirely inside the image. Destination rectangles may not overlap. Vectors are accepted only on non-keyframe frame-open controls. A keyframe always starts from a complete SSDV image and carries no motion vectors.

## Encoder behavior

The current motion estimator:

1. Divides the composed GIF frame into 16×16 MCU blocks.
2. Skips blocks sufficiently similar at their original coordinate.
3. Searches a ±6 MCU neighborhood in the previous frame for a source block.
4. Uses sampled RGB SAD as a low-cost match score.
5. Accepts only high-confidence matches with a decisive improvement over same-coordinate coding.
6. Merges adjacent blocks with equal motion into rectangular vectors.
7. Keeps at most 32 vectors, preferring larger predicted regions.
8. Encodes all remaining changed MCU segments as ordinary SSDV sparse residual data.

If sparse restart-point coding cannot be validated, the frame is promoted to a full keyframe and all motion vectors are discarded. Periodic keyframes and the existing changed-area keyframe threshold remain active.

## Loss and recovery

Control packets retain the existing three-copy repetition. If an SGV2 frame-open control is lost, its motion vectors are unavailable and that delta frame may not reconstruct correctly; periodic full keyframes restore the canvas. The receiver rejects malformed, out-of-bounds, overlapping, or keyframe-attached vectors rather than applying ambiguous copies.

## Streaming audio TX architecture

Image and GIF TX now use `transport.Encoder.StreamPackets` rather than `EncodePackets` + full-length PCM allocation. The streaming path maintains modulator phase, bit-packing, pulse-shaping/filter state, and resampler fractional position across all blocks.

The Malgo playback path uses a fixed-capacity, single-producer/single-consumer float32 ring (two seconds of mono PCM). A producer goroutine performs modulation, filtering and resampling. The audio callback only copies ready samples to the device buffer; it performs no modulation, FFT, file access, or dynamic allocation. The producer blocks when the ring is full, so memory does not grow with airtime.

Stream preparation fills at least 250 ms of PCM before PTT is asserted. The device callback starts only after the normal PTT lead time, avoiding a keyed silent start while the first block is being modulated. If the ring nevertheless underruns during playback, the burst is aborted instead of inserting a silent gap into the modulated stream. PTT remains asserted until the producer has finished, the ring has drained, and the configured tail delay has elapsed. Cancellation aborts the producer, discards queued PCM, and releases PTT through the existing emergency-release path.

`EstimateOutputSamplesForPackets` is checked against the actual sample count returned by the stream encoder. Streaming resampling is regression-tested against the prior batch resampler to preserve output length and waveform tolerance.
