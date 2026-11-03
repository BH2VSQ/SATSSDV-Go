# Protocol implementation status

This document distinguishes implemented Go paths from protocol components that still require reference-vector, capture-WAV, and on-air interoperability validation.

## Current implementation matrix

| Profile | Implemented in Go | Remaining limitations |
|---|---|---|
| SSDV AX.25 | Baseline 4:2:0 JPEG coefficient parsing/transcoding, SSDV 256-byte FEC/No-FEC packet generation, CRC32/RS8, Base40, packet validation and contiguous packet-set JPEG/PNG reconstruction; AX.25 UI framing, FCS-16, HDLC, NRZI and ARCSSD-style 1200-baud AFSK TX/RX | TX now uses the reference DDS phase table, 500 ms preamble/trailer and 19.2 kHz profile. Packet MCU resynchronization metadata is sentinel-only, so a missing SSDV packet prevents JPEG reconstruction. Independent Hamstation/recorded-WAV validation and continuous AFC/clock recovery remain. |
| SSDV CCSDS 1200 | Same SSDV JPEG codec and packet assembly; CCSDS ASM with one-bit correction, PN randomization, frame extraction, 150-byte burst guards and reference phase-accumulator 1200-baud TX | Packet loss concealment, streaming AFC and independent Hamstation/recorded-WAV validation remain. |
| SSDV CCSDS 2400 | Same SSDV JPEG codec and packet assembly; CCSDS ASM with one-bit correction, PN framing, 19.2 kHz, 2400-bit/s Gaussian pulse-shaped TX and ARCSSD-compatible 61-tap quadrature RX filter/timing loop | Packet loss concealment, carrier-frequency offset tracking and independent Hamstation/recorded-WAV validation remain. |
| PCSI CCSDS 1200 | MMT packet generation, deterministic pixel shuffle, Base40 callsign, 12-bit measurement layout, RS8 parity/correction, CCSDS framing and reference phase-accumulator 1200-baud TX/RX | The 224-byte MMT packet body matches a deterministic vector generated independently from the supplied Python MMT path; the RS8 parity matches an ARCSSD C reference vector. The Go reconstruction optimizer is independently implemented and is not asserted to be numerically identical to the Python optimizer. Complete Hamstation capture/image comparison remains. |
| PCSI CCSDS 2400 | Same PCSI MMT packet generation/correction/reconstruction, with the corrected CCSDS 2400 TX/RX paths | MMT body and RS8 vectors are independently checked; carrier-frequency offset tracking and Hamstation/recorded-WAV end-to-end validation remain. |

## SSDV JPEG codec and packet limitations

The Go SSDV path now implements both image transmission and complete contiguous-image reconstruction:

- TX accepts Go `image.Image` values, emits an 8-bit baseline sequential 4:2:0 JPEG intermediate, parses the JPEG Huffman scan, and transcodes quantized DCT coefficients into SSDV's quality-level quantization tables and standard Huffman coding.
- Packetization emits 256-byte SSDV packets in FEC (`0x66`, 205-byte image payload plus CRC32 and RS32) or No-FEC (`0x67`, 237-byte image payload plus CRC32) layout. Callsigns use SSDV Base40. RS8 and CRC32 are validated by the Go receiver.
- RX validates/corrects packets, de-duplicates identical packet IDs, reorders out-of-order arrivals, verifies a contiguous sequence through EOI, reconstructs a baseline JPEG and converts it to PNG. The corrected packet bytes are retained as the `.ssdv` album attachment.
- The current packetizer splits entropy data at fixed payload-byte boundaries and uses the SSDV MCU metadata sentinel (`MCU ID = 0xffff`, offset `0xff`). Therefore a complete contiguous packet sequence is reconstructed, but an image with a missing packet cannot yet resynchronize at the next MCU or conceal the missing region.
- The current codec accepts baseline sequential 8-bit 4:2:0 JPEG only. Progressive JPEG, grayscale JPEG, restart intervals and other sampling layouts are rejected with explicit errors. Source images must be at least 16x16, no larger than 4080x4080, and have dimensions divisible by 16.

Tests cover packet metadata, FEC/No-FEC layouts, all eight SSDV quantizer levels, packet reordering and duplicate handling, missing packet rejection, JPEG decode validity and RX PNG reconstruction. These tests establish internal consistency. Independent packet-for-packet and decoded-image comparison against upstream SSDV/Hamstation reference fixtures and real RF captures remains outstanding. The Go implementation does not bundle upstream SSDV C source; algorithm and table provenance still requires a licensing review before binary distribution.

## PCSI MMT path

The supplied `PCSI.py` entrypoint imports `pcsitximage_mmt` and `pcsidecoder_mmt`. The Go packetizer follows this MMT path and keeps it separate from the AX.25/KISS PCSI path. The 256-byte packet path includes Base40 callsign, image and packet identifiers, pixel measurement payload and RS8 parity. The receiver corrects up to 16 symbol errors before accepting the packet, accumulates out-of-order measurements, and produces periodic PNG previews.

The Go image reconstruction uses an orthonormal separable 2-D DCT/IDCT and a finite-memory OWL-QN/L-BFGS-style solver for the squared measurement residual plus L1 coefficient objective. DCT numeric vectors are checked against OpenCV orthonormal DCT output. A deterministic 320x240 image test checks the first 224-byte MMT packet body against a Python-reference-generated SHA256 vector; a separate test checks RS8 parity against a compiled ARCSSD C reference vector. Tests also cover packet correction, out-of-order and duplicate packet ingestion, full-sample reconstruction, RGB/chroma channel order, worker coalescing and continued packet ingestion during reconstruction. The optimizer is independently implemented and is not claimed to be a bit-identical SciPy port. Full reconstructed-image comparison and Hamstation capture verification remain open.

## CCSDS modulation and demodulation

- The 1200-bit/s CCSDS profile uses 19.2 kHz samples, 1200/2200 Hz nominal mark/space tones, the reference 1024-entry sine table, integer DDS increments (center 91 and deviation 26) and amplitude 0.9155273.
- The 2400-bit/s profile uses 19.2 kHz samples and the 1200/2400 Hz mark/space pair. TX uses the supplied ARCSSD 25-tap BT=0.35 pulse-shaping sequence, reference phase deviation ±31, correct wire polarity and continuous phase accumulator.
- The 2400 RX now uses the supplied ARCSSD 61-tap quadrature low-pass filter, delayed I/Q phase discriminator, edge-assisted timing adjustment and three-sample majority decision. The receiver state persists across `Feed` chunks. Synthetic self-loopback tests cover arbitrary input block boundaries, startup sample offset, reduced amplitude and a small DC offset.
- Both 1200 and 2400 CCSDS guard sequences are applied once around the complete TX burst, not once per 256-byte data unit. This matches the supplied engine's FIFO burst behavior.
- The receiver accepts an ASM with up to one bit error, matching the supplied ARCSSD sync search behavior, and normalizes the sync word before PN de-randomization.
- The 2400 RX currently assumes a nominal 1800 Hz center carrier. Carrier-frequency offset/AFC, noisy real-world captures, and recorded Hamstation/ARCSSD WAV fixtures still require validation. Stateful resampling and arbitrary audio callback boundaries are covered by deterministic synthetic tests.
- 1200 and 2400 TX now have fixed sample-level golden vectors derived from the ARCSSD reference DDS. The 2400 RX majority decision is matched to the C reference logic. These independent vectors improve conformance confidence but do not replace actual Hamstation end-to-end capture testing.

## Full-duplex audio and PTT

- The optional Malgo runtime owns independent shared-mode capture and playback devices. TX does not stop RX capture.
- RX audio is analyzed for spectrum and level independently of the packet worker. Stateful resampling, nominal-clock AFSK symbol acquisition, AX.25 HDLC extraction, CCSDS ASM search and packet validation persist across arbitrary audio callback chunks. Large oscillator offsets, continuous AFC and independent radio captures remain unvalidated.
- PCSI measurement insertion is serialized in the RX manager. Image reconstruction runs on a dedicated single-worker queue using independent decoder snapshots, preventing a slow 640x480 numerical solve from blocking incoming packet ingestion. Newer snapshots are coalesced while a reconstruction is running, and solver coefficients are retained as the next warm start.
- PTT supports VOX, Hamlib `rigctld`, and serial RTS/DTR. Serial enumeration is available for Windows, Linux and macOS; actual RTS/DTR behavior depends on OS driver and adapter support.
- RTS, DTR and Hamlib each expose a backend-enforced PTT test that releases after 15 seconds or on manual stop. VOX does not expose a software PTT test.
- A real RF full-duplex session still requires radio hardware and RF/audio routing that support simultaneous uplink and downlink, plus suitable isolation.

## UI, gallery and single-image TX

- The top-level navigation contains only Workbench and Album. The local image library is opened from the TX editor rather than a separate top-level page.
- The TX editor follows the SATSSTV-Go layout: a fixed-size canvas preview, a dropdown preset selector with initial CQ/RST/73/blank templates, dynamic preset management (up to 32), and per-transmission QSO fields below the preview in the left column. The component editor fills the right column to the same bottom edge. The preview uses a fixed 4:3 CSS viewport regardless of the intrinsic low/medium/high encoding resolution.
- A single shared workbench background is stored in settings and remains independent of template presets. Each template stores only text/image/last-RX components and layout. The image-library dialog displays thumbnails and supports import, set as template background, add as image layer and delete. Deleting an asset clears its references from persisted templates.
- The album uses RX/TX thumbnail cards, type filters, large-image preview, timestamped PNG export and deletion.
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
