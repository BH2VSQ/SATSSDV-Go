# GIF memory optimization notes (2026-11-07)

## First-pass changes

- RX `QMovie` uses `QMovie::CacheNone` instead of `CacheAll`. Only the current decoded frame needs to remain available; long animations should not retain every Qt-decoded frame.
- After the application has successfully archived a completed RX GIF, `rx.Manager` releases its PNG frame snapshots, delay slice and composed RGBA canvas while retaining the small completed-session metadata needed for duplicate suppression. The live preview keeps the compressed GIF and drops the superseded final PNG snapshot.
- The synchronous audio playback job borrows the caller-owned `[]float32` waveform instead of cloning it. The caller must keep the slice alive and immutable until `PlayWithProgress` returns; the audio callback only reads it, and the job drops its reference on completion/cancellation.
- GIF TX creates the compressed album GIF immediately after frame preparation, then drops the RGBA frame slice, source GIF bytes, and decoded template layers after SSDV packetization. The prepared waveform is kept for repeat cycles, but the much larger full-resolution RGBA animation is not retained throughout radio airtime.
- The temporary packet collection is released after modulation and `runtime/debug.FreeOSMemory` runs once in the non-real-time TX preparation path before playback.

## Memory estimates

An RGBA frame occupies `width * height * 4` bytes, excluding slice/object overhead. At 640x480 this is 1,228,800 bytes (about 1.17 MiB); 120 fully materialized frames occupy about 140.6 MiB. A second playback copy of a 10-minute waveform occupies:

| Sample rate | One `float32` waveform for 10 min | Avoided duplicate |
|---:|---:|---:|
| 19,200 Hz | 43.9 MiB | 43.9 MiB |
| 44,100 Hz | 100.9 MiB | 100.9 MiB |
| 48,000 Hz | 109.9 MiB | 109.9 MiB |

This is why borrowing the playback slice and releasing RGBA frames before the actual transmission should materially lower peak and sustained memory.

## Remaining linear allocation

The full modulated waveform is still materialized once in `startTransmissionGIF` and retained for repeat cycles. Its size is `sampleRate * airtime * 4` bytes, so it is still linear in airtime. The next architecture step for a hard memory ceiling is a disk-backed PCM spool (or bounded producer/consumer stream), with packet/frame-at-a-time modulation. It must preserve PTT lead/tail timing and modem acquisition. A chunked implementation must be validated for all current profiles because independently modulating chunks resets modem phase and can require a reacquisition guard.

## Motion-prediction assessment

Current `gifmode.changedMCUs` compares each 16x16 MCU with the immediately previous source frame. `EncodeContext` then sends restart-point JPEG packet segments covering changed MCUs, promotes to a keyframe every 12 frames, or when at least 60% of MCUs changed. This is sparse residual transmission, not motion compensation.

Motion prediction is feasible, but it is not safe to add only on the TX side. The RX must receive and apply the same motion vectors before applying residual JPEG MCU updates. SGV1 currently defines frame open/close markers, frame delay, stream start/end, data-packet count and keyframe flags; it has no motion-vector semantics. Reinterpreting SGV1 reserved bytes would break compatibility or risk accidental legacy decoding.

Recommended SGV2 experiment:

1. Only run block matching for MCUs that differ at the same coordinates. Search a bounded neighborhood in the previous composed frame using a low-resolution luma SAD pass, then refine the best candidates at full resolution.
2. Accept a motion vector only when the candidate block is sufficiently close to the current target block. Otherwise send that MCU as normal JPEG residual data. Do not force a vector for every changed tile.
3. Store vectors as destination MCU index plus signed pixel displacement in distinct SGV2 motion packets. RX must copy from an immutable previous-frame snapshot, then apply normal JPEG residual segments. This avoids order-dependent overlapping copies.
4. Restrict prediction to regions that can safely move. Template background and overlay layers are static; global image translation would move them incorrectly. The GIF component ROI should be signaled at stream start or vectors must be explicitly limited to changed foreground tiles.
5. Compare total radio cost, not only changed-pixel count. A motion packet costs airtime too; fall back to ordinary SGV1-style delta or a keyframe whenever motion-vector overhead exceeds the saved JPEG packets.
6. Keep periodic intra refresh and add loss recovery. A lost motion packet must not silently corrupt all following frames; repeat motion metadata with FEC or force a keyframe after a bounded number of dependent frames.

The SGV2 motion experiment is not included in this first memory-focused change. It needs a wire-format revision, RX implementation, loss simulations and over-the-air interoperability tests before deployment.
