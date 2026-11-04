# SATSSDV-Go native Qt 6 desktop client

The desktop client is implemented with Qt 6 Widgets and Qt Network. It does not use Qt WebEngine, QWebView, a Chromium runtime, Wails, or embedded HTML.

## Build requirements

- Qt 6.4 or newer, components `Widgets` and `Network`
- CMake 3.21 or newer
- Go toolchain matching the `go` directive in the repository's `go.mod`
- A C/C++ compiler and platform audio development dependencies when `SATSSDV_WITH_MALGO=ON`

## Build

```sh
cmake -S . -B build-qt6 -DCMAKE_BUILD_TYPE=Release -DSATSSDV_WITH_MALGO=ON
cmake --build build-qt6 --parallel
```

The build places the `satssdv-backend` Go service next to the Qt executable. The Qt process starts it with `QProcess`; the service binds only to `127.0.0.1`, generates a random 256-bit token, and reports its ephemeral port/token over the private child-process stdout pipe. Every RPC and event request carries the token. The HTTP server is not exposed on LAN interfaces.

## Separation

- `qt6/mainwindow.*`: native Widgets UI, previews and native file dialogs.
- `qt6/backend_client.*`: local authenticated RPC, event polling, process lifecycle.
- root Go package: radio/audio/modem/image services; no browser runtime.
- `events.go`: bounded event history so a UI stall cannot backpressure RX/audio workers.

The current native client implements the main RX/TX workbench, QSO fields, template selection/rendering and an interactive template canvas. The canvas supports component selection, drag-to-move, text-size/image-scale adjustment, adding text/gallery/last-RX image layers, deletion and a property table. Native gallery import, album listing/preview/export/delete, template-pack import/export, audio/PTT settings, RX previews, and spectrum/waterfall rendering are also included. Layer ordering, snap/alignment guides, full translation catalogs and an advanced waterfall palette/range editor remain follow-up parity work.
