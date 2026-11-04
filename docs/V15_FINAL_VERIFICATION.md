# v15 Qt migration, M-ary receive and BCSI status

Date: 2026-11-05

## 1. Qt 6 migration state

The desktop architecture is now source-level native Qt 6 Widgets:

- The legacy `frontend/` HTML/CSS/JavaScript tree and generated Wails runtime files are removed.
- Wails is removed from the Go module dependency list and active build/CI workflow.
- `qt6/` contains the Widgets window, native dialogs, native image preview and spectrum/waterfall paint widget.
- The Go application runs as a separate backend process. Qt launches it with `QProcess`; the backend binds only to `127.0.0.1` and exchanges RPC/events through a random 256-bit token.
- RPC methods are explicitly allowlisted. Synchronous requests have bounded timeouts; asynchronous RPC and event polling now abort after 30 seconds and 5 seconds respectively. The event history is an O(1) circular buffer, and TX progress publication is throttled to 15 Hz alongside spectrum display work.
- `scripts/check_native_ui.py` checks the absence of the WebView frontend/dependency and checks that every RPC method called by Qt is exposed by the server allowlist.
- `scripts/test_backend_rpc.py` builds and runs the backend and tests its handshake, loopback-only address, token authentication, profile enumeration, allowlist rejection and shutdown.

The main RX/TX workbench, profile controls, QSO/AX.25 fields, image previews, text-template editing, template-pack import/export, album/gallery operations, audio device and PTT settings, and native spectrum/waterfall rendering are implemented. Advanced drag-and-drop template layout, a full localized Qt translation catalog, waterfall palette/range editing and advanced album filters remain UI parity work. The language preference is persisted but native translation catalogs are not complete; the settings label indicates this.

## 2. 1600-baud modem repair and verification

The QPSK/8PSK/16QAM receive path estimates signal amplitude from the known 96-symbol training sequence before slicing the payload constellation. This addresses the 16QAM failure seen after the actual TX filter, where the outer constellation points were shifted by the transmit gain and the receiver assumed ideal unit amplitude.

`TestMAModulationTXFilteredRXAirpath` runs all three M-ary profiles through the same TX transport encoding and configured audio band-edge filter, then exercises:

- output audio rates: 44.1 kHz and 48 kHz;
- linear audio gains: 0.20, 0.45 and 1.0;
- leading idle audio: 2,413 samples (not symbol aligned);
- callback chunk sizes: 480, 960, 1,920 and 4,096 samples;
- complete recovered SSDV packet count and packet sequence validation.

`TestMAModulationTXFilteredRXWithAudioNoise` additionally checks all three profiles with deterministic additive noise at 34 dB SNR. These software loopback tests pass. They do not replace a real sound-card, transceiver, SDR or RF channel test.

## 3. RX overload handling

The RX manager no longer waits indefinitely to submit into a saturated bounded packet queue. `TestManagerSubmitDoesNotBlockWhenQueueSaturates` verifies the non-blocking overload contract. This prevents the decode worker from holding up the upstream streaming decoder when reconstruction is overloaded; rejected packets are counted rather than silently blocking the real-time path.

## 4. BCSI codec role and integration boundary

BCSI is an image codec/reconstruction layer, not a radio modulation. It is intended to run before the existing transport framing and existing modem. The selected QPSK/8PSK/16QAM/FSK/GMSK profile remains unchanged.

The independent `internal/image/bcsi` codec currently implements block measurements, adaptive measurement counts, quantization, optional DEFLATE, per-block CRC, independent reconstruction and progressive block assembly. Its over-the-air fragment envelope, TX selection setting, RX dispatch/reassembly and full profile integration are not yet implemented. Therefore current on-air SSDV/PCSI paths remain unchanged and BCSI is not yet used for radio transmission. The detailed interface and no-new-modulation integration plan is in `BCSI_CODEC_LAYER_INTEGRATION.md`.

The previous experimental benchmark showed BCSI quality/bytes were not yet competitive with JPEG/Haar across the tested synthetic images. It is not enabled by default and must not be presented as a proven reduction in airtime.

## 5. Verification run in the supplied container

Passed:

```text
go test -count=1 ./...
go vet ./...
go build -tags "" ./...
go test -race ./internal/transport -run '^TestMAModulationTXFilteredRXAirpath$' -count=1
go test -race ./internal/transport -run '^TestMAModulationTXFilteredRXWithAudioNoise$' -count=1
go test -race ./internal/rx -run '^TestManagerSubmitDoesNotBlockWhenQueueSaturates$' -count=1
python3 scripts/check_native_ui.py
python3 scripts/test_backend_rpc.py
cmake --build build-backend-check --parallel 2
```

The backend-only CMake build was made with `SATSSDV_WITH_MALGO=OFF`, so it verifies the Go backend build and CMake integration but does not verify real audio-device support.

Not verified in this container:

- Full Qt C++ compile and GUI launch: Qt 6 development packages/config files are absent (`Qt6Config.cmake` not found). The package-manager index update timed out, so Qt could not be installed here.
- The `SATSSDV_WITH_MALGO=ON` native audio backend build: the local container does not provide a validated native audio toolchain/dependency download.
- Actual Windows and macOS Qt build, packaging, audio devices, PTT and RF transmission. CI jobs are defined for Linux, Windows and macOS but were not executed from this local session.
- The Go backend service (audio stub, `CGO_ENABLED=0`) was additionally cross-compiled for `windows/amd64`, `darwin/amd64` and `darwin/arm64`. This verifies the Go service code paths for those targets, not their Qt desktop clients or native audio support.
- Recorded hardware/SDR loopback and over-the-air acceptance of the M-ary profiles.

The release `go.mod` directive is Go 1.26.0. Local Go tests were run with the container's Go 1.23.2 after temporarily lowering that directive; the release directive was restored to 1.26.0 in the delivered source. The local environment could not run `go mod tidy` against the remote module proxy, so dependency tidiness is represented by the cleaned direct `malgo` requirement and minimal `go.sum`, not by a successful tidy command.

The final rerun also passed `go test -count=1 ./...`, `go vet ./...`, `go build -tags "" ./...`, the native-UI source check, the backend RPC lifecycle script, CI YAML validation and the CMake backend-only build. Race-enabled isolated tests passed for the M-ary modem unit package, the noisy filtered M-ary transport path and RX queue saturation. A subsequent race-enabled filtered transport rerun exceeded the outer command execution time limit; this is not recorded as a pass from that rerun. The ordinary full transport tests passed in the full Go test suite.
