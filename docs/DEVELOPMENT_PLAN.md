# Development plan and current progress

## Phase 1: application/control foundation

- [x] Five independent RX/TX transport profiles.
- [x] Hamlib Net Rigctl, serial RTS/DTR and VOX PTT controllers.
- [x] Windows, Linux and macOS serial-port discovery with a manual refresh API.
- [x] Backend-enforced 15-second PTT test with early stop and release-on-close.
- [x] Independent RX capture / TX playback contract and optional Malgo implementation.
- [x] FFT spectrum generation and Wails RX/TX spectrum event hooks.
- [x] Simplified Chinese, English and Japanese dictionaries and UI selector.
- [x] Initial CQ/RST/73/blank TX presets, dynamic preset management (up to 32), image gallery and RX/TX album persistence models.
- [x] Top-level navigation restricted to Workbench and Album; gallery is embedded in the TX template editor as a modal resource library.
- [x] Hamstation 320x240, 448x336 and 640x480 presets.
- [x] Repeat-send queue with configurable interval, cycle count and cancellation.

## Phase 2: link-layer foundation

- [x] AX.25 UI frame, FCS, bit stuffing, NRZI and baseline AFSK1200 modulation.
- [x] AX.25 HDLC bitstream extraction and FCS validation.
- [x] CCSDS ASM, PN randomization, de-randomization and 150-byte PN guards.
- [x] CCSDS byte- and bit-aligned ASM extraction.
- [x] PCSI MMT packet transmit layout and RS8 parity generation.
- [x] Stateful resampling, AFSK/HDLC/CCSDS framing and packet extraction across arbitrary audio callback chunks.
- [ ] Carrier-frequency/AFC tracking and timing-loop tolerance against independent radio captures.
- [ ] Reference WAV and binary interoperability fixtures for all five profiles.

## Phase 3: image codecs

- [x] Baseline 4:2:0 JPEG coefficient transcoding into SSDV 256-byte FEC/No-FEC packets.
- [x] Contiguous SSDV packet-set reconstruction to baseline JPEG/PNG, including out-of-order arrival and FEC correction.
- [ ] SSDV MCU-boundary resynchronization and missing-packet concealment.
- [x] SSDV packet header, CRC and RS8 validation primitives.
- [x] RS8 error correction, up to 16 symbol errors.
- [x] PCSI OWL-QN / L-BFGS image reconstruction worker, coalesced previews and cancellation.
- [ ] Compare PCSI reference Python packet streams and reconstructed images against golden Hamstation fixtures.
- [ ] Validate low/medium/high settings against Hamstation output images.

## Phase 4: complete workbench

- [x] RX/TX pane layout and independent profile selectors.
- [x] RX/TX spectrum rendering hooks and audio-level telemetry hooks.
- [x] PTT controls and test UI, with the 15-second safety timer in Go.
- [x] Template and gallery data models plus persistence APIs.
- [x] SATSSTV-Go-style visual template component editor, dropdown preset selection, create/duplicate/delete preset actions, shared workbench background independent of saved presets, QSO fields and image composition pipeline.
- [x] Image-library modal with thumbnail previews, import, set-background, add-layer, add-to-queue and delete actions.
- [x] Join SSDV/PCSI packetization, transport modulation and the multi-image TX scheduler into the image-send GUI.
- [x] Album thumbnail grid, RX/TX filters, large preview, timestamped PNG export and delete interactions.
- [ ] Progressive RX image updates.
- [x] Reconstructed SSDV RX PNG and original `.ssdv` archive attached to one album record.

## Phase 5: release validation

- [x] Core Go tests and vet.
- [x] Frontend JavaScript syntax check.
- [x] Cross-platform serial control source files.
- [ ] Build Wails GUI on Windows, Linux and macOS with CGO dependencies.
- [ ] Hardware tests with dummy load and reference radios.
- [ ] Interoperability review with the original Hamstation / ARCSSD-Go software.
