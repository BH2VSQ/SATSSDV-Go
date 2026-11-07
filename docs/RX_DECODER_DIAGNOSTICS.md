# RX Decoder Liveness Diagnostics

The RX pane emits an `rx:diagnostics` event once per second while capture is active.
The compact line shows audio callback activity, decoder feed/output activity,
input queue pressure, queue drops, selected M-ary profile/lock, SDF3 FEC chunk
results, and the active GIF frame's packet/MCU progress. The tooltip provides
more detailed counters and the last decoder error.

## How to interpret a stalled display

- `AUD` stops increasing: the audio capture callback is no longer delivering
  blocks (device/backend/input problem).
- `AUD` increases but `DEC` stops: the RX decode worker is not consuming its
  queue (worker exit, panic, or scheduling blockage).
- `Q` rises continuously or `DROP` increases: capture is outrunning demodulation;
  audio blocks are being discarded and a discontinuity reset is inserted.
- M-ary shows `SEARCH` and reacquire count does not increase: symbol timing/carrier
  lock has not been reacquired after a packet guard or signal interruption.
- `FEC` bad count increases or the tooltip reports an SDF3 error: demodulation is
  producing symbols but packet framing/FEC/CRC is rejecting units.
- Decoder packet count increases while manager `processed` does not: the RX
  manager queue/worker is stalled.
- GIF frame packet count increases but decoded MCU count does not: packets are
  arriving, but the current MCU segment is incomplete or its JPEG decode failed.
- `KEY` means the GIF stream is waiting for a complete keyframe; it does not mean
  the audio or transport decoder has stopped.

## Failure containment

`runRXDecodeWorker` now catches panics from `StreamDecoder.Feed`, increments a
panic counter, emits an `rx:decoder-error` event with a stack trace, resets the
current decoder and continues listening for the next training word. This keeps a
single corrupt FEC group from silently killing the only RX worker.

The packet-erasure decoder also handles a single missing shard when only P0 or
only P1 has arrived. Previously the P0-only path allocated an empty P1 residual
and wrote through it while subtracting known shards, causing a panic. Regression
tests cover P0-only, P1-only and two-parity recovery.

The native Qt runtime and real RF/audio device are still required to validate
actual Windows UI behavior and on-air recovery. Go waveform tests do not replace
that test.
