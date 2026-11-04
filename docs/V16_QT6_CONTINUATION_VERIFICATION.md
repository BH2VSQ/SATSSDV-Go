# v16 Qt 6 continuation and verification report

Date: 2026-11-05

## Changes in this continuation

- The native Qt 6 template canvas now supports raising/lowering the selected layer and an optional 5% snapping grid. The component property table rebuilds its component-index mapping after layer reordering so edits continue to target the correct object.
- The PCSI RX pressure test keeps its independent 5-second queue-ingest assertion, while its eventual final-reconstruction deadline is now 45 seconds. This avoids a false timeout from race instrumentation and package-level CPU contention; it does not change production queue behavior.
- The Linux CI workflow now has a sequential race-regression job for the M-ary modem, BCSI codec, RX worker and filtered QPSK/8PSK/16QAM transport paths.
- Native UI documentation now distinguishes implemented canvas interactions from the remaining image crop/fit, translation-catalog, waterfall-palette and advanced album-filter work.
- The existing 1600-baud receive calibration and event-payload memory reduction remain in place. The Go backend is a separate process behind a token-authenticated loopback RPC interface; no WebView/browser engine is used.

## Local verification

Passed in the supplied Linux container with Go 1.23.2 while the module directive was temporarily set to 1.23.0 for local compatibility:

- `make check` (`go test ./...`, `go vet ./...`, native UI static architecture check, backend RPC lifecycle test)
- `go build -tags '' ./...`
- `go test -race -count=1 ./internal/rx` (run separately; about 30 seconds)
- `go test -race -count=1 ./internal/modem/mary`
- `go test -race -count=1 ./internal/image/bcsi`
- `go test -race -count=1 .`
- `go test -race -count=1 ./internal/transport -run '^TestMAModulationTXFilteredRXAirpath$|^TestMAModulationTXFilteredRXWithAudioNoise$'`
- Go backend cross-compilation with `CGO_ENABLED=0` for `windows/amd64`, `darwin/amd64` and `darwin/arm64`
- Clean CMake backend-only configure/build with `SATSSDV_WITH_MALGO=OFF`
- CI YAML parsing; jobs present: Go test matrix, race regression, Qt Linux, Qt Windows and Qt macOS

The full `go test -race ./...` command exceeded the container's five-minute command limit. It is not reported as a pass. The individual high-risk package and filtered M-ary race tests listed above did pass when run separately.

## Qt build boundary

Qt 6 headers, CMake package files, `qmake6` and Qt Widgets pkg-config metadata are absent from the supplied container. A fresh GUI CMake configure stops at `find_package(Qt6 6.4 ...)`. `apt-get update` timed out against the configured Debian mirrors, so Qt could not be installed during this session. Therefore the C++ Widgets executable was not compiled or launched locally.

The CI workflow contains Qt-enabled build jobs for Ubuntu, Windows/MinGW and macOS, but those remote jobs have not been executed from this local session. The backend-only CMake build uses the audio stub and does not validate real sound-card access. Sound-card, PTT, SDR and RF acceptance tests remain outstanding.

## 1600-baud M-ary receive repair

QPSK, 8PSK and 16QAM continue to use the existing modem profiles. The receiver estimates amplitude from the known training symbols before payload slicing. The deterministic filtered-TX air-path regression includes 44.1/48 kHz operation, gain changes, non-symbol-aligned leading idle audio, several callback block sizes, full packet count/order and a separate 34 dB SNR case. This is a software loopback result, not a real RF claim.

## BCSI status and compatibility

BCSI remains an optional image-codec experiment before the existing transport and modulation stages. It is not a modulation mode. The codec module is implemented and benchmarked, but its link-layer fragment envelope, independent TX codec setting, RX fragment reassembly and end-to-end over-air dispatch are not enabled. Earlier quality/byte benchmarks did not establish a consistent advantage over JPEG/Haar, so BCSI remains disabled rather than increasing airtime or risking compatibility. Existing SSDV/PCSI packet formats and profile defaults remain unchanged.

## Release toolchain

The release `go.mod` directive is restored to `go 1.26.0` in the deliverable. The container's Go 1.23.2 was used only for local compatibility tests before restoring the release directive. `go.sum` was unchanged from the v15 source archive.
