# Protocol implementation status

This document distinguishes implemented Go paths from protocol components that still require reference-vector, capture-WAV, and on-air interoperability validation.

## Current implementation matrix

| Profile | Implemented in Go | Remaining limitations |
|---|---|---|
| SSDV AX.25 | Baseline 4:2:0 JPEG coefficient parsing/transcoding, SSDV 256-byte FEC/No-FEC packet generation, standard MCU offset/index restart metadata, CRC32/RS8, Base40, packet validation and MCU-block preview/complete JPEG reconstruction; AX.25 UI framing, FCS-16, HDLC, NRZI and ARCSSD-style 1200-baud AFSK TX/RX | Synthetic audio-IF sweep passes ±600 Hz symmetrically; ±1500 Hz wider sweep has non-monotonic isolated successes. RF/SDR Doppler, real WAV and Hamstation comparison remain. |
| SSDV CCSDS 1200 | Same SSDV JPEG codec with per-packet MCU restart metadata and block preview; CCSDS ASM with one-bit correction, PN randomization, frame extraction, 150-byte burst guards and reference phase-accumulator 1200-baud TX/RX | Synthetic audio-IF sweep passes ±500 Hz symmetrically; -600 Hz fails. RF/SDR Doppler and independent Hamstation/recorded-WAV validation remain. |
| SSDV CCSDS 2400 | Same SSDV JPEG codec with per-packet MCU restart metadata and block preview; CCSDS ASM with one-bit correction, PN framing, 19.2 kHz, 2400-bit/s Gaussian pulse-shaped TX and ARCSSD-compatible 61-tap quadrature RX filter/timing loop/AFC | Synthetic audio-IF sweep passes ±600 Hz symmetrically; wider offsets and RF/SDR Doppler require real capture validation. |
| PCSI CCSDS 1200 | MMT packet generation, deterministic pixel shuffle, Base40 callsign, 12-bit measurement layout, RS8 parity/correction, CCSDS framing and reference phase-accumulator 1200-baud TX/RX | Synthetic audio-IF sweep passes ±500 Hz symmetrically; -600 Hz fails. The optimizer is independent and not numerically identical to Python; full Hamstation comparison remains. |
| PCSI CCSDS 2400 | Same PCSI MMT packet generation/correction/reconstruction, with the corrected CCSDS 2400 TX/RX AFC path | Synthetic audio-IF sweep passes ±600 Hz symmetrically; real RF/SDR Doppler and Hamstation/recorded-WAV end-to-end validation remain. |

## SSDV JPEG codec and packet limitations

The Go SSDV path now implements image transmission, per-packet MCU-block preview and complete contiguous-image reconstruction:

- TX accepts Go `image.Image` values, creates an 8-bit baseline sequential 4:2:0 JPEG intermediate, parses the JPEG Huffman scan, and transcodes quantized DCT coefficients into the canonical SSDV quality tables.
- The default SSDV quality level 4 uses an intermediate JPEG quality-80 prefilter before the canonical SSDV quantizer. In the deterministic 320×240 regression fixture, this reduced the packet count from 53 to 50 (5.7% fewer packets) with a sampled reconstructed PSNR of 21.98 dB. Other SSDV quality levels use progressively weaker/stronger intermediate prefilters. These are lossy rate/quality tradeoffs; the SSDV on-air quality flag and quantization tables remain standard.
- Packetization emits 256-byte SSDV packets in FEC (`0x66`, 205-byte image payload plus CRC32 and RS32) or No-FEC (`0x67`, 237-byte image payload plus CRC32) layout. Callsigns use SSDV Base40. RS8 and CRC32 are validated by the Go receiver.
- TX writes the standard MCU offset/index fields. Each payload identifies the byte offset to its first complete MCU and that MCU's image index; the first MCU's component DC predictors restart relative to zero. The 320×240 regression fixture remains 53 FEC packets when comparing continuous entropy output with MCU-restart packetization (no packet-count overhead; about 26 extra alignment bytes).
- RX validates/corrects packets, de-duplicates identical packet IDs, and decodes complete MCU strips between adjacent packet restart markers. Each accepted packet can update a fixed-size full-resolution preview without waiting for the previous packet ID. Missing image regions are represented as local damaged 16×16 MCU blocks. A later packet can therefore fill a region beyond an earlier lost packet.
- The complete, bit-exact JPEG/PNG reconstruction still requires a complete contiguous packet sequence through EOI. Incomplete packet archives retain all received packet IDs so they can be merged/recovered later. Legacy transmitters that emit sentinel MCU metadata (`0xff`/`0xffff`) retain contiguous-prefix preview fallback.
- RS8 correction and CRC32 validation remain mandatory before an SSDV packet is accepted for preview. CRC-failed packets are not passed to the JPEG entropy decoder.
- The current codec accepts baseline sequential 8-bit 4:2:0 JPEG only. Progressive JPEG, grayscale JPEG, restart intervals and other sampling layouts are rejected. Source images must be at least 16×16, no larger than 4080×4080, and have dimensions divisible by 16.

Tests cover packet metadata, FEC/No-FEC layouts, all eight SSDV quantizer levels, packet reordering and duplicate handling, missing packet rejection, MCU block placement, local damage markers, complete preview coverage, JPEG decode validity, adaptive source prefilter packet reduction, and RX PNG reconstruction. These tests establish internal consistency; packet-for-packet comparison against the upstream SSDV CLI/Hamstation and real RF captures remains outstanding. The Go implementation does not bundle upstream SSDV C source; algorithm and table provenance still requires a licensing review before binary distribution.

## PCSI MMT path

The supplied `PCSI.py` entrypoint imports `pcsitximage_mmt` and `pcsidecoder_mmt`. The Go packetizer follows this MMT path and keeps it separate from the AX.25/KISS PCSI path. The 256-byte packet path includes Base40 callsign, image and packet identifiers, pixel measurement payload and RS8 parity. The receiver corrects up to 16 symbol errors before accepting the packet, accumulates out-of-order measurements, and produces periodic PNG previews.

The Go image reconstruction uses an orthonormal separable 2-D DCT/IDCT and a finite-memory OWL-QN/L-BFGS-style solver for the squared measurement residual plus L1 coefficient objective. The reference Python path flattens coefficients with `reshape((width, height)).T`; the Go implementation now explicitly converts between that column-major coefficient vector and the row-major DCT matrix. Its OWL-QN orthant projection also allows non-zero coefficients to move toward zero after a line-search overshoot (the previous implementation incorrectly masked those descent steps and could freeze with strongly wrong channel values). The final PCSI reconstruction budget is 96 iterations with a gradient-based early stop; progressive previews use a smaller budget.

DCT numeric vectors are checked against OpenCV orthonormal DCT output. A deterministic 320x240 image test checks the first 224-byte MMT packet body against a Python-reference-generated SHA256 vector; a separate test checks RS8 parity against a compiled ARCSSD C reference vector. Tests also cover packet correction, out-of-order and duplicate packet ingestion, full-sample reconstruction, RGB/chroma channel order, reference coefficient flattening, an analytic 1x1 OWL-QN optimum, a 64x64 red-image chroma/quality regression, worker coalescing and continued packet ingestion during reconstruction. The optimizer is independently implemented and is not claimed to be a bit-identical SciPy port. Full reconstructed-image comparison and Hamstation capture verification remain open.

## CCSDS modulation and demodulation

- The 1200-bit/s CCSDS profile uses 19.2 kHz samples, 1200/2200 Hz nominal mark/space tones, the reference 1024-entry sine table, integer DDS increments (center 91 and deviation 26) and amplitude 0.9155273.
- The 2400-bit/s profile uses 19.2 kHz samples and the 1200/2400 Hz mark/space pair. TX uses the supplied ARCSSD 25-tap BT=0.35 pulse-shaping sequence, reference phase deviation ±31, correct wire polarity and continuous phase accumulator.
- The 2400 RX now uses the supplied ARCSSD 61-tap quadrature low-pass filter, delayed I/Q phase discriminator, edge-assisted timing adjustment and three-sample majority decision. The receiver state persists across `Feed` chunks. Synthetic self-loopback tests cover arbitrary input block boundaries, startup sample offset, reduced amplitude and a small DC offset.
- Both 1200 and 2400 CCSDS guard sequences are applied once around the complete TX burst, not once per 256-byte data unit. This matches the supplied engine's FIFO burst behavior.
- The receiver accepts an ASM with up to one bit error, matching the supplied ARCSSD sync search behavior, and normalizes the sync word before PN de-randomization.
- The 2400 RX estimates and slowly tracks a common audio carrier offset from the balanced preamble. A production-path synthetic audio-IF sweep covers ±1500 Hz and confirms ±600 Hz symmetric reception for both 2400 profiles, plus ±100 Hz linear drift in both directions. This does not validate true RF Doppler, radio passband behavior or real Hamstation/ARCSSD WAV captures; see `docs/DOPPLER_TOLERANCE.md`.
- 1200 and 2400 TX now have fixed sample-level golden vectors derived from the ARCSSD reference DDS. The 2400 RX majority decision is matched to the C reference logic. These independent vectors improve conformance confidence but do not replace actual Hamstation end-to-end capture testing.

## Full-duplex audio and PTT

- The optional Malgo runtime owns independent shared-mode capture and playback devices. TX does not stop RX capture.
- RX audio is analyzed for spectrum and level independently of the packet worker. Stateful resampling, AFSK symbol acquisition with a bounded common-offset search, AX.25 HDLC extraction, CCSDS ASM search and packet validation persist across arbitrary audio callback chunks. Synthetic audio-IF offset tolerance is summarized in `docs/DOPPLER_TOLERANCE.md`; large RF Doppler excursions and independent radio captures remain unvalidated.
- PCSI measurement insertion is serialized in the RX manager. Image reconstruction runs on a dedicated single-worker queue using independent decoder snapshots, preventing a slow 640x480 numerical solve from blocking incoming packet ingestion. Newer snapshots are coalesced while a reconstruction is running, and solver coefficients are retained as the next warm start.
- PTT supports VOX, Hamlib `rigctld`, and serial RTS/DTR. Serial enumeration is available for Windows, Linux and macOS; actual RTS/DTR behavior depends on OS driver and adapter support.
- RTS, DTR and Hamlib each expose a backend-enforced PTT test that releases after 15 seconds or on manual stop. VOX does not expose a software PTT test.
- A real RF full-duplex session still requires radio hardware and RF/audio routing that support simultaneous uplink and downlink, plus suitable isolation.

## UI, gallery and single-image TX

- The top-level navigation contains only Workbench and Album. The local image library is opened from the TX editor rather than a separate top-level page.
- The TX editor follows the SATSSTV-Go layout: a fixed-size canvas preview, a dropdown preset selector with initial CQ/RST/73/blank templates, dynamic preset management (up to 32), and per-transmission QSO fields below the preview in the left column. The component editor fills the right column to the same bottom edge. The preview uses a fixed 4:3 CSS viewport regardless of the intrinsic low/medium/high encoding resolution.
- A single shared workbench background is stored in settings and remains independent of template presets. Each template stores only text/image/last-RX components and layout. The image-library dialog displays thumbnails and supports import, set as template background, add as image layer and delete. Deleting an asset clears its references from persisted templates.
- The album uses RX/TX thumbnail cards with prominent type chips, type filters, large-image preview, automatic archiving of partial previews and packet-only SSDV records, and export/deletion actions. Export invokes the native OS directory picker and copies PNG/SSDV data to the selected folder.
- Template packs use a versioned `.satssdv.json` format containing preset components plus embedded copies of referenced gallery assets and the shared TX background. Import validates image dimensions and bundle size, remaps asset identifiers and appends presets without overwriting the user's existing templates. Built-in CQ/RST/73/blank templates now contain useful initial layouts.
- GitHub Actions validates all three desktop operating systems and packages Windows x64, Linux x64 (`tar.gz`, `.deb`, AppImage), and macOS arm64/amd64 `.app` archives.
- Gallery import currently supports PNG, JPEG and GIF. Image files are size- and dimension-validated before storage.
- The selected TX template is rendered at 320x240, 448x336 or 640x480 and packetized as a single-image transmission.
- The TX title bar contains one combined Start/Stop button immediately to the left of TX IDLE/ACTIVE. The preview canvas has no text overlay for dimensions or quality. The four selectable template slots are always displayed above the canvas/editor, and component properties are arranged in grouped columns.
- A decoded RX callsign automatically fills the QSO HisCall field. Once the operator enters a value manually, automatic updates are suspended; clearing the field re-enables automatic fill.
- The Wails API accepts one rendered TX template per start request. Repeat mode resends that same image at the chosen interval; there is no multi-image TX queue API or gallery queue action.
- Single-template TX transmission is connected for all five profiles. SSDV uses the Go baseline-JPEG coefficient transcoder and 256-byte packetizer; PCSI uses the Go MMT packetizer.
- RX PCSI previews/final PNGs and reconstructed SSDV PNGs are archived. SSDV album entries also retain the corrected `.ssdv` packet stream as an attachment.
- Interface dictionaries support Simplified Chinese, English and Japanese.

## Licensing review gate

The supplied ARCSSD CCSDS C files carry GPL-3.0-or-later notices, Dire Wolf DSP code has GPL notices, and the RS reference utility carries LGPL terms. The Go modem's 2400 pulse-shaping and receive-filter implementation was translated from the supplied ARCSSD algorithm and coefficient sequences. Review the original archive's license and the resulting distribution obligations before publishing binaries or choosing a project-wide license. The full SSDV upstream implementation has its own GPL licensing requirements. This project does not include the upstream SSDV C source files.

## PCSI reference licensing note

The supplied PCSI MMT ZIP does not include a repository-level LICENSE/COPYING file or source-level licensing statement. The upstream `pcsiolw.py` comments attribute the referenced PyLBFGS implementation to MIT, but that does not license the complete PCSI transmitter/decoder archive. Confirm permission and licensing with the original author before distributing a binary that includes derived PCSI algorithm work.


## TX editor rendering and settings localization

- Component updates are drawn to an offscreen staging canvas. Only the newest completed frame is blitted to the visible preview, preventing the clear/redraw flicker seen during text and geometry edits. Image decode promises are cached by gallery asset ID.
- Template backgrounds are workbench-level state, migrated from legacy per-template values at startup. `SaveTemplates` always removes background IDs from all template records.
- The clear-background control is placed in the component editor heading. The former QSO-side background information block is removed. The QSO panel and component editor share a vertical column boundary and bottom edge.
- The settings dialog and dynamically generated serial/audio device options use the selected language. Locale changes re-render port placeholders, audio default labels and device status strings.
- RTS/DTR is selected with the PTT method dropdown; no separate signal selector is shown. The active-high checkbox only configures electrical polarity.

## RX queue and incomplete-image archival update

- The RX worker no longer discards a valid, already-decoded packet merely because the manager queue is at capacity; it waits for a free slot and exits promptly when the manager context is cancelled. The queue capacity is 2048 packets. This avoids the observed burst-loss mode where PCSI image reconstruction or callback load could overrun the former 128-packet queue.
- SSDV progress is emitted for each newly accepted valid packet. The standard MCU offset/index fields let RX decode the exact strip between adjacent packet restart points and place complete 16×16 MCU blocks at their image coordinates. A fixed-resolution canvas preserves viewport geometry, and gaps are shown as local damaged blocks. Disk archive writes remain batched separately from live UI updates.
- PCSI preview reconstruction uses 48 OWL-QN iterations (final reconstruction: 96, with early convergence exit). The earlier two-iteration preview was visibly under-converged and could show a strong green cast even with valid measurements. The corrected DCT coefficient indexing and OWL-QN orthant projection are covered by reference-layout and synthetic color regressions.
- PCSI preview output is archived as it becomes available. SSDV raw packet bundles are snapshotted periodically and once complete; an incomplete stream can be exported even if no valid JPEG preview has yet been reconstructed.
- Image sessions are no longer keyed only by the permanent `(profile, callsign, 8-bit image ID)` tuple. A changed packet-0 fingerprint, a completed previous image, or a longer idle gap can start a fresh session. Packet 0 arriving late in an out-of-order burst is not by itself treated as a new image. Because the wire protocols do not carry a unique transmission-session nonce, repeated identical incomplete images with no separating idle interval remain intrinsically ambiguous.
- AX.25 TX exposes a source SSID setting (0..15). The CQ template sends to `CQ` and clears HisCall; other templates use HisCall as the AX.25 destination when present, falling back to `CQ` when empty.
- Remaining interoperability tasks are real AX.25/CCSDS WAV-capture tests, comparison with Hamstation's output, PCSI pixel-level comparison against the reference Python decoder, and real-world RF Doppler testing. Synthetic audio-IF offset coverage is documented in `docs/DOPPLER_TOLERANCE.md`, but it does not replace recorded RF/Hamstation comparisons.


## RX refinement and AX.25 addressing (2026-11-04)

- PCSI preview iterations increased from 2 to 48; final reconstruction remains capped at 96 with convergence early-exit. Regression tests verify that a 320x240 red reference image is not rendered as green at the early preview checkpoint. This addresses under-converged preview output, but actual Hamstation capture equivalence is still unverified.
- SSDV previews update after every newly accepted packet using the standard MCU offset/index metadata. Each complete MCU strip is composited at its indexed 16×16 destination; packet gaps are displayed as local damage. The full-resolution canvas remains fixed and centered with `object-fit: contain`.
- SSDV/PCSI receive sessions retain a packet-0 payload fingerprint. A changed first-packet payload or a completed prior image can start a fresh session when the 8-bit image ID is reused; a late packet 0 from an out-of-order current burst no longer resets the image. A five-second idle boundary handles many incomplete-stream restarts. Same-content, incomplete retransmissions without a detectable gap remain ambiguous because the wire format has no unique session identifier.
- AX.25 TX settings now expose SSID 0..15 only while the AX.25 TX profile is selected. CQ template uses destination `CQ` and clears HisCall; other templates use the uppercase HisCall value as destination when available.
- Regression tests cover out-of-order packet 0, reused image IDs with changed first-packet contents, PCSI early color recovery, AX.25 SSID/destination encoding and front-end field visibility.
- Real RF capture validation remains necessary for AX.25 receive stalls, Hamstation interoperability, and quantitative PCSI image comparison.


## 2026-11-04 SSDV, TX timeout and frequency-offset tests

- The 1500 Hz test tone now passes through a backend-enforced 60-second context deadline, independent of frontend state. Album records are sorted newest-first, with RX completion and TX start timestamps.
- SSDV RX emits an update for every accepted packet and decodes 16×16 MCU strips using packet MCU offset/index restart points. Archive writes remain separately batched.
- SSDV source JPEG prefilter policy is tied to the selected wire quantizer; the level-4 regression fixture reduces 53 FEC packets to 50 with sampled PSNR 21.98 dB. Encoder benchmark: approximately 4.0–4.5 ms/op; full 320×240 block preview benchmark: approximately 5.7–6.6 ms/op on AMD EPYC 9V74.
- All five synthetic transport profiles pass a constant audio-IF sweep from -500 Hz to +500 Hz and both linear audio-IF ramps (+100 to -100 Hz and reverse). The observed conservative symmetric range is ±600 Hz for SSDV AX.25 and the two 2400 profiles; the 1200-bit/s CCSDS profiles are ±500 Hz. This is a synthetic audio-passband result, not real RF Doppler validation.
