# RX/TX resource and waveform notes (2026-11-04)

## Memory strategy

- `internal/rx` uses a 512-packet bounded ingest queue, instead of retaining a much larger backlog when the image worker falls behind.
- PCSI image-session retention is bounded to six sessions and inactive sessions older than three minutes are pruned.
- PCSI Y, Cb and Cr OWL-QN reconstruction is sequential. Only one full optimizer workspace is active at a time, rather than three concurrent DCT/line-search workspaces.
- The Go backend startup sets `runtime/debug.SetMemoryLimit(640 << 20)` and `SetGCPercent(80)`. `SetMemoryLimit` is a soft Go runtime target. Native Qt, GPU, audio-driver and other allocations are outside this limit.
- SSDV incremental preview keeps the decoded canvas and only decodes restart-marker neighbours for each new packet, avoiding repeated full-session parsing and temporary image allocations.

## PCSI progressive preview initialization

The old zero-valued initial DCT coefficient vector gave early previews a poor neutral/chroma baseline. When no warm-start vector exists, the solver now seeds coefficient `[0]` with the mean of received samples multiplied by `sqrt(width*height)`, which is the orthonormal DCT DC coefficient for a constant image. Remaining coefficients are still optimized by the same OWL-QN objective. This is an initialization improvement; it does not change PCSI packet bytes or the sparse measurements.

The preview uses 16 OWL-QN iterations and final reconstruction uses 96 iterations. The 320×240 red-image regression now passes at the 16-packet preview checkpoint; the earlier 8-iteration setting produced a visibly incorrect green preview and was rejected.

## TX audio output filter

The generated waveform is filtered in place without resampling:

- 200 Hz, 2nd-order Butterworth high-pass.
- 4.8 kHz, 4th-order Butterworth low-pass (two standard Butterworth biquad sections).

Tone-response tests at 19.2 kHz sample rate require less than 0.2 dB gain change at 1200, 1600, 2200 and 2400 Hz, and at least about 14 dB attenuation at 60 Hz and 8 kHz. TX waveform loopback tests in `internal/tximage` remain part of the normal package test suite.

## Validation caveats

The full non-race Go test suite and `go vet ./...` pass in the Linux validation environment. The full `internal/rx` race suite exceeded the execution time limit. A reduced RX race set covering queue backpressure, session expiry/reuse, SSDV gap preview and deduplication passes; an SSDV end-to-end race test with a fixed 3-second test deadline can time out under race instrumentation. Windows Qt 6 production build and real radio tests still need to be run on the target Windows machine.


## Native UI event-path overhead

- The native UI event broker uses a fixed 512-entry circular buffer. Appending an event overwrites the oldest slot in O(1) rather than shifting the full history after every 512 events.
- TX progress events are limited to 15 Hz (with an explicit final 100% update), matching the native display refresh budget. RX/TX spectrum FFT work remains limited to 15 Hz. The audio callback and demodulator input are not rate-limited by these UI display throttles.
