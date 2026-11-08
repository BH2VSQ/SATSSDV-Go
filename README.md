# SATSSDV-Go

SATSSDV-Go is a native desktop application for amateur-satellite SSDV and PCSI image transmission/reception. The desktop UI is built with **Qt 6 Widgets**; Go hosts the audio, modem, image-codec, transport, PTT and storage services. There is no browser UI, WebView, Chromium process or Qt WebEngine dependency.

## Native architecture

- `qt6/` — C++17 Qt 6 Widgets desktop client. It uses native controls, native file dialogs, native image painting and a custom spectrum/waterfall widget.
- `main.go`, `backend_server.go`, `app.go` — Go service process. The Qt client launches it as a child process.
- `backend_server.go` — RPC allowlist and bounded event-feed endpoint. The service binds only to `127.0.0.1:0`; it generates a random 256-bit token and sends the ephemeral endpoint/token to Qt through the child process stdout pipe. RPC and event requests require that token.
- `internal/` — audio runtime, DSP, modulation/demodulation, FEC, image codecs, receive reconstruction, transmit queue, PTT and storage. These packages do not depend on Qt.

The local RPC is a process boundary, not a network service: it listens only on loopback, rejects calls not in an explicit method allowlist, and is shut down when the Qt client exits. High-frequency spectrum events are throttled before entering the bounded 512-event history. RX event entries contain metadata only; full PNG/packet payloads are not retained in the event ring, and the newest preview is held in a single bounded cache and fetched on demand.

## Transport profiles

- SSDV AX.25
- SSDV CCSDS 1200
- SSDV CCSDS 2400
- SSDV QPSK 1600
- SSDV 8PSK 1600
- SSDV 16QAM 1600
- PCSI CCSDS 1200
- PCSI CCSDS 2400

The three M-ary modes are project-specific experimental profiles. Their 1600 values are symbol rates (baud), with raw symbol bit rates of 3200/4800/6400 bit/s before framing overhead. They use a 48 kHz native modem sample rate, a 96-symbol acquisition word and Gray-labelled QPSK/8PSK/16QAM mapping. They are not claimed to interoperate with unrelated modems that happen to use the same symbol rate.

RX quality display uses AFSK 1200 in-channel audio SNR and synchronized post-demodulation EVM-SNR for QPSK/8PSK/16QAM (including Auto RX M-ary candidates). GMSK 2400 still shows `SNR --` until a validated estimator is available. EVM-SNR is measured at the symbol-decision aperture and is not calibrated RF SNR/Eb/N0 or directly comparable with full-band PCM SNR. The M-ary receive path estimates amplitude/phase from its known training sequence and produces max-log soft bit LLRs. New `StreamPackets` transmissions use grouped SDF3 envelopes with triplicated/CRC-protected metadata, a 64-byte block interleaver, soft-decision K=7 Viterbi decoding and group CRC32. The default balanced FEC policy adapts redundancy to the constellation: QPSK uses rate 5/6, 8PSK uses rate 3/4 plus one packet-erasure shard per eight source SSDV packets, and 16QAM uses rate 2/3 plus one parity shard. The Qt settings dialog also exposes a robust option (rate 1/2 plus 8+2 erasure recovery for all three modes). Existing per-packet SSDV RS8/CRC validation remains in place. `transport.MARYFECRobust` retains rate 1/2 plus two parity shards for all three modes when extra link margin is preferred over speed. The new receiver continues to accept earlier SDF3 rate-1/2 groups, SDF2 groups and legacy uncoded M-ary bursts; receivers predating the punctured SDF3 profiles cannot decode the new balanced modes. See `docs/SOFT_FEC_PACKET_RECOVERY.md` and `docs/MODEM_THROUGHPUT.md` for coding compatibility and airtime figures. Use the matching 1600 profile at both ends; 48 kHz remains the recommended sound-card setting.

## Image and receive pipeline

- SSDV JPEG coefficient transcoding and standard 256-byte FEC/no-FEC packets with CRC32/RS8 validation.
- PCSI-MMT packetization, RS8 correction and sparse-measurement image reconstruction.
- Independent experimental BCSI codec code under `internal/image/bcsi`. BCSI is intended to be an optional image compression/reconstruction layer *before* the existing packet/framing and modulation stages; it is not a modulation, symbol mapper or replacement radio profile. The packet fragmentation/reassembly and RX codec dispatch needed to carry BCSI through the current AX.25/CCSDS profiles are not yet integrated, so current on-air TX/RX continues to use the established SSDV/PCSI codec paths.
- Streaming receive demodulators preserve state across arbitrary audio callback boundaries. Packet validation and image reconstruction run outside the audio callback, and queue overload is bounded rather than allowed to freeze the capture thread.
- Progressive SSDV and PCSI previews, local image gallery, RX/TX album, portable template packs and repeat transmission.

## Build requirements

- Go 1.26.x for the current release toolchain.
- CMake 3.21 or newer.
- C++17 compiler.
- Qt 6.4 or newer, components `Widgets` and `Network`.
- For native shared-mode audio (`SATSSDV_WITH_MALGO=ON`): a working CGo compiler and the platform's audio development toolchain. On Linux install ALSA development headers (`libasound2-dev`).

### Build the desktop application

```sh
cmake -S . -B build-qt6 -DCMAKE_BUILD_TYPE=Release -DSATSSDV_WITH_MALGO=ON
cmake --build build-qt6 --parallel
```

The Qt executable and the `satssdv-backend` child process are placed under `build-qt6/bin`. On Windows CMake invokes `windeployqt` when it is available; on macOS it invokes `macdeployqt` when available. Linux deployment should be built/tested on the target distribution with the matching Qt runtime packages present.

### Build without native audio (development / CI fallback)

```sh
cmake -S . -B build-qt6-noaudio -DCMAKE_BUILD_TYPE=Release -DSATSSDV_WITH_MALGO=OFF
cmake --build build-qt6-noaudio --parallel
```

This produces the UI and backend with the audio runtime stub; it is useful for validating the Qt application shell but cannot access real audio devices or transmit/receive over sound hardware.

### Run tests

```sh
go test ./...
go vet ./...
python3 scripts/check_native_ui.py
python3 scripts/test_backend_rpc.py
```

The Qt build is separately validated through CMake on Qt-enabled CI runners. The Qt client can also be run with an already-built backend by placing `satssdv-backend` (or `satssdv-backend.exe`) beside the desktop executable.

## Notes on compatibility and limitations

- The dedicated `modem-characterization` CI job sweeps AWGN SNR and constant audio-IF frequency offset for all eight transport profiles, then publishes a Markdown report artifact. These are synthetic audio-baseband results, not calibrated RF sensitivity or Eb/N0 values. RX shows an in-channel audio SNR estimate for AFSK 1200 and a synchronized post-demodulation EVM-SNR for QPSK/8PSK/16QAM, including Auto RX candidates. EVM-SNR is measured at the symbol-decision aperture and is not directly comparable with full-band PCM SNR or calibrated RF SNR/Eb/N0. CCSDS 2400 GMSK remains `SNR --` until a validated demodulator-domain estimator is implemented. See [RX SNR estimation](docs/RX_SNR_ESTIMATION.md) for measurement definitions and limitations.

- Existing SSDV, PCSI-MMT, AX.25 and CCSDS packet/modulation paths remain independent of the experimental BCSI codec.
- QPSK/8PSK/16QAM 1600 are project-specific and require the matching decoder. Their on-air performance still needs validation against recorded sound-card/electrical loopback and real RF captures.
- The Qt client provides a native RX/TX workbench, profile selectors, QSO fields, an interactive 4:3 template canvas (component selection, drag-to-move, text/image resizing, layer ordering, optional 5% snap grid, add/delete image and text layers), text properties, gallery and album operations, template-pack import/export, audio/PTT configuration, and native spectrum/waterfall drawing. The language preference is stored but full QTranslator localization is pending. Image crop/fit controls, full localization catalogs, advanced album filters and waterfall palette/range editing remain parity work tracked in `docs/PORT_QT6_WIDGETS.md`.
- The Go heap soft limit is not a cap on total process RSS. Qt, image buffers, audio drivers and other native allocations are outside the Go runtime's heap limit.

See `docs/` for codec benchmarks, interoperability tests, protocol status and migration notes.
