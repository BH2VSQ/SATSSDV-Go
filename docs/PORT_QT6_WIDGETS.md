# Qt 6 Widgets migration status

Date: 2026-11-05

## Current implementation

The application entry point is now split into two processes:

1. `SATSSDV-Go` is the C++17 Qt 6 Widgets desktop client (`qt6/`). It owns the native window, controls, menus, file dialogs, image previews and spectrum/waterfall painting.
2. `satssdv-backend` is the Go service process (`main.go`). It owns audio, DSP, modulation/demodulation, SSDV/PCSI reconstruction, BCSI experiments, PTT, settings, gallery, album and template storage.

The Qt process launches the Go process with `QProcess`. The Go process binds an ephemeral TCP port on `127.0.0.1`, generates a random 256-bit token, and writes a single-line handshake to its private child-process stdout. Qt uses the token for each RPC and event-feed request. No service is exposed on LAN interfaces. The backend has an explicit RPC allowlist and a bounded event history; spectrum emission is throttled in Go before the UI can consume it.

The old `frontend/` HTML/CSS/JavaScript tree and Wails-generated runtime bindings have been removed. Wails is no longer a Go dependency or a build/CI path. The Qt executable does not link Qt WebEngine and does not create a WebView/Chromium subprocess.

## Native UI implemented

- RX/TX profile selectors, including the three M-ary 1600 profiles.
- RX start/stop and live image/packet/coverage status.
- TX image template rendering, resolution selection, QSO fields, AX.25 destination and source SSID, repeat interval, progress and stop controls.
- Native Qt image preview, spectrum curve and scrolling waterfall paint widget.
- Device settings for sample rate, input/output device, persisted language preference, VOX/Hamlib/serial PTT, PTT polarity and lead/tail timing. Full native translation catalogs are still pending; the language selector is explicitly marked as a migration-in-progress setting.
- Native serial-port refresh and PTT test start/stop controls.
- Native image gallery import, album listing, preview, per-record export/delete and bulk export.
- Native template-pack import/export, text-component property editing, and a 4:3 interactive template canvas with component selection, drag-to-move, text-size/image-scale handles, add/delete controls, layer raise/lower controls, optional 5% snap grid, gallery image layers and last-RX image layers.

## RPC safety and lifecycle fixes

- `BackendClient::callSync` owns its own request and timeout lifecycle. It aborts the in-flight `QNetworkReply` on timeout and never leaves a callback referencing stack-local variables after return.
- Loopback RPC explicitly disables desktop proxy routing.
- The server accepts only allowlisted application methods and limits RPC request bodies.
- The server uses constant-time token comparison and only binds to IPv4 loopback.
- Go service logs go to stderr; stdout is reserved for the one-line Qt handshake.

## Remaining UI parity work

The shell and primary interactions are native Qt; no browser runtime remains. Remaining parity work includes image crop/fit controls, a complete localized text catalog across all pages, a richer waterfall palette/range editor and advanced album filters. The template canvas supports drag, resize, layer order, optional 5% snapping, add/delete and property editing; crop/fit modes are not yet exposed. The language preference is persisted for compatibility, but a QTranslator catalog and complete native translation coverage are not yet implemented.

## Build and verification

Qt build:

```sh
cmake -S . -B build-qt6 -DCMAKE_BUILD_TYPE=Release -DSATSSDV_WITH_MALGO=ON
cmake --build build-qt6 --parallel
```

Qt requires Qt 6.4+ `Widgets` and `Network`, CMake 3.21+ and a C++17 compiler. Native audio additionally requires the CGo toolchain and platform audio build dependencies.

The supplied execution container did not have Qt 6 headers, CMake package metadata or `qmake6`, and its package-manager index update timed out. Therefore this container cannot compile the Qt target. Go tests and backend RPC lifecycle tests are executable separately; Qt compilation is configured as a required CI job on Qt-enabled Linux, Windows and macOS runners.

No Windows/macOS desktop executable or installable package is claimed as locally tested until those CI jobs complete on their respective runners.
