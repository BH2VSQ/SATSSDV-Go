# v16 Qt 6 continuation and RX event-memory hardening

Date: 2026-11-05

## Native template editor

The Qt 6 template dialog now contains a 4:3 native canvas with component selection and mouse manipulation. It supports dragging text/image/last-RX-image layers, changing text size or image scale with the lower-right handle, adding text and image layers, deleting selected layers with the toolbar or Delete key, renaming a preset, and editing text, coordinates, font size and color in a property table. The editor fetches its image assets through the Go RPC boundary and renders the canvas with QPainter; it does not embed HTML or a browser engine.

Remaining canvas parity work is explicit image crop/fit controls and complete localization catalog coverage. The 5% grid is optional and disabled by default. The waterfall view is a native QPainter widget; a full palette/range editor is not yet implemented.

## RX event memory reduction

Previously each `rx:image` event retained `PNGBase64` (and potentially `PacketDataBase64`) in the bounded 512-event history. A count-bounded event ring is not a sufficient memory bound when entries contain full images. The backend now decodes/validates a preview once, keeps only the newest PNG in a single 16 MiB-limited cache, and strips image/packet payload strings before publishing the event. The event includes `hasPreview`; Qt fetches the latest preview through `GetLatestRXImageData` and coalesces requests while a fetch is in flight. The album archive path remains unchanged.

A regression test verifies that the retained event has preview metadata but neither inline PNG nor packet Base64, and that the newest cached image remains available through the RPC method.

## Qt responsiveness and child lifecycle

- The periodic audio status refresh is asynchronous and guarded against overlapping RPC requests, so a slow backend cannot recursively queue status calls through nested event loops.
- The Qt process drains the Go child's stderr pipe and retains only a 16 KiB diagnostic tail. This prevents a repeatedly logging child from blocking on a full OS pipe.
- The template-canvas source is an explicit part of the CMake Qt target.

## Race-test threshold adjustment (v17 continuation)

The PCSI receive-pressure test keeps its independent 5-second queue-ingest assertion, but its eventual final-reconstruction deadline is now 45 seconds. A focused `-race` run takes about 17 seconds on this container, and a parallel full-package race invocation previously exceeded the 20-second test deadline. This is test scheduling tolerance only; it does not change production RX queue limits or worker behavior.

## Verification

The local Go test suite, Go vet, the loopback RPC lifecycle test, native-UI architecture checks, the backend-only CMake target, and the 1600-baud filtered-path race tests are the executable checks for this work. The test container uses Go 1.23.2; for local verification only, the `go` directive is temporarily lowered to 1.23 and restored to 1.26.0 afterward.

Qt 6 headers/config packages and `qmake6` are not present in the supplied container (`Qt6_DIR` is `NOTFOUND`), so the C++ Widgets target and GUI launch are **not** claimed as locally compiled or runtime-tested. The configured CI jobs remain the required Qt-enabled compilation checks on Linux, Windows and macOS. Real sound-card, PTT and RF tests remain separate hardware acceptance steps.

## Scope boundary

The 1600-baud QPSK, 8PSK and 16QAM receive calibration remains in the existing modem. The BCSI encoder remains an experimental codec layer and is not yet connected to the live transport TX/RX framing path. It is not a new modulation mode and does not replace existing profiles.
