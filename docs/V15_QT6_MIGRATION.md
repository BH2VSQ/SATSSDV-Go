# v15 Qt 6 migration and M-ary receive hardening

Date: 2026-11-05

## Changes in this worktree

- Added a native Qt 6 Widgets desktop target and a separate Go backend process.
- Removed the old HTML/CSS/JavaScript frontend and Wails-generated runtime tree.
- Removed Wails from the Go module requirements and the active CI/build workflow.
- Replaced Wails build instructions with CMake/Qt 6 instructions and a cross-platform Qt build matrix.
- Added a token-authenticated loopback RPC protocol and bounded event feed for the Qt client.
- Fixed synchronous Qt RPC timeout lifetime handling: an expired request is aborted and no longer holds references to stack variables after the call returns.
- Disabled proxy routing for the private loopback RPC channel.
- Added the AX.25 source SSID control, Japanese language selection, serial-port refresh and PTT test start/stop controls to the native settings UI.
- TX/RX profile persistence is committed before start operations so the backend cannot race against an asynchronous settings write and use the previous profile.
- Retained the 1600-symbol-rate QPSK/8PSK/16QAM amplitude calibration derived from the known training word. The modulation remains an existing transport profile, not a new codec or radio layer.

## M-ary regression coverage

The existing modem tests cover all three constellations, 1600 baud, arbitrary sample offsets, leading idle audio and varied callback chunk lengths. The transport air-path tests additionally exercise the actual encoder output after the configured TX band-edge filter, different sound-card gain factors, 44.1/48 kHz audio paths, noise, and full SSDV packet validation.

These are deterministic software loopback tests. They do not replace recordings from the intended sound card, virtual audio cable, transceiver/SDR or RF path. A real on-air acceptance test must include the actual TX/RX audio-device settings and at least one capture containing frequency error, level variation and RF/channel noise.

## Verification boundaries

- `go test ./...` and `go vet ./...` are run in the local Linux environment with Go 1.23.2 after temporarily using a 1.23-compatible `go` directive. The release module directive is restored to Go 1.26.0 in the final source.
- Qt 6 is not installed in the local container. The CMake target and CI jobs are configured, but this container cannot truthfully report a Qt C++ compile or GUI launch as passed.
- Windows/macOS audio hardware, PTT keying and RF transmission are not tested in this environment.
- The native Qt window implements core RX/TX workflows but has documented visual/editor feature-parity gaps in `PORT_QT6_WIDGETS.md`.
