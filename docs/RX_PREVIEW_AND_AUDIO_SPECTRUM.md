# RX Preview and Audio Spectrum Refinement

## RX preview geometry

The RX image view now consumes the flexible space between the pane title and the metadata/waterfall controls. Its image element fills the view box and uses `object-fit: contain` with centered positioning. The intrinsic aspect ratio is preserved at 320x240, 448x336 and 640x480; no crop/cover operation is used. Because the RX image view is the flexible child, the metadata and waterfall/footer stay anchored to the bottom of the pane and align with the TX waterfall/footer.

## Last RX template component

`GetAlbum` returns records newest-first. The previous `GetLatestRXImageData` implementation iterated that list backwards and consequently selected the oldest RX record, which could make a newer medium/high-resolution capture appear to be ignored. `album.NewestRXImages` now returns RX records with image paths in newest-first order without filtering on the quality label or dimensions. Missing/unreadable files are skipped until the newest available one is found. A unit test covers 320x240, 448x336 and 640x480 records mixed with TX entries.

## RX queue overflow handling

The RX decode worker queue capacity is increased from 256 to 512 capture blocks. More importantly, overflow no longer drains every queued block. The non-blocking audio callback records a pending discontinuity and drops only incoming blocks while the queue is full. When at least two queue slots are available, it inserts a single `nil` reset marker followed by the next audio block. The decoder therefore processes the already-buffered audio in order and only resets at the actual capture gap. This cannot prevent loss if DSP processing remains slower than capture indefinitely, but it avoids throwing away several seconds of buffered frames in one overflow event.



- 1200-bit/s AX.25/CCSDS FSK: matched-tone mark/space coordinates are collected per recovered symbol.
- CCSDS 2400 GMSK: the phase-discriminator soft decision is sampled at the existing recovered symbol clock after carrier-offset correction.


## Waterfall frequency labels

Waterfall vertical frequency ticks are generated at 500 Hz intervals. Labels are plain integer Hz values (0, 500, 1000, ...), not abbreviated decimal kHz values. The 500 Hz grid is anchored to absolute multiples of 500 Hz and clipped to the configured visible frequency range.

## Validation

- `go test ./...` passed using the locally installed Go 1.23.2 compiler with the `go` directive temporarily lowered for the test command; the checked-in `go.mod` was restored to Go 1.26.0 immediately afterwards.
- `go vet ./...` passed under the same temporary local-toolchain workaround.
- `go test -race ./internal/dsp ./internal/album` passed.
- Frontend JavaScript syntax and UI regression tests passed, including functional canvas checks for square 2-FSK/GMSK symbol-space plots and exact 500 Hz labels.
- The former Wails desktop entry has been replaced by the Qt 6 Widgets client. The Qt target still requires a Qt-enabled build environment; see PORT_QT6_WIDGETS.md for the current build status.
- No real radio capture was available, so improved packet retention under queue overload is code-path tested/static-reviewed but not an over-the-air measurement.
