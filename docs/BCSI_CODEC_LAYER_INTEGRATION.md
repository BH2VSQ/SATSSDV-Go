# BCSI as an image-codec layer for existing radio modes

Date: 2026-11-05

## Correct role

BCSI is an optional image compression and reconstruction codec. It is **not** a modulation, symbol mapping, modem profile or radio PHY. The selected existing radio profile remains responsible for the current AX.25/HDLC, CCSDS framing, and QPSK/8PSK/16QAM/FSK/GMSK modulation and demodulation.

The intended pipeline is:

```text
source image
  -> resize / image preparation
  -> selected image codec (legacy SSDV / PCSI-MMT / optional BCSI)
  -> packet fragmentation and reassembly for the selected link framing
  -> existing AX.25 or CCSDS framing
  -> existing modem (QPSK / 8PSK / 16QAM / FSK / GMSK)
  -> audio device / radio
```

Receive performs the inverse: existing modem and link framing recover transport payloads, then a codec discriminator routes legacy SSDV/PCSI packets or BCSI fragments to the corresponding image decoder.

## Current implementation boundary

`internal/image/bcsi` implements block-compressive-sensing image blocks, adaptive measurement counts, quantization, optional DEFLATE, CRC validation, independent block reconstruction and progressive assembly. It remains an isolated codec module in this revision.

It is **not yet selected by `tximage.EncodeImageWithAX25`**, and the existing fixed-size image packet receiver does not yet dispatch/reassemble BCSI fragments. Consequently no current on-air profile uses BCSI and no new modulation mode has been added. Existing profiles continue to use their established SSDV or PCSI-MMT codecs unchanged.

## Required integration work before on-air use

1. Add a codec selection setting that is independent of `TXProfile` / `RXProfile`; do not create `bcsi-qpsk`, `bcsi-8psk` or `bcsi-16qam` modem profiles.
2. Define a versioned BCSI-over-transport fragment envelope that fits the existing 256-byte link payload. It needs image/session ID, fragment sequence, fragment count or bounded offset, codec version, payload length, and per-fragment integrity. The BCSI block packet itself may span more than one 256-byte transport payload and must be fragmented safely.
3. On RX, identify the BCSI envelope before attempting SSDV/PCSI packet parsing, reassemble fragments with bounded memory/time limits, and submit complete independent BCSI block packets to the BCSI assembler.
4. Preserve interoperation by keeping the legacy packet parsers and profile defaults unchanged. BCSI should be opt-in and identified in-band; it must never make a legacy receiver interpret BCSI bytes as SSDV/PCSI.
5. Compare at equal delivered image quality and equal radio payload budget against existing SSDV/JPEG/PCSI paths, measuring encode time, modulation time, total transmitted samples/airtime, reconstruction time, PSNR/SSIM and block loss recovery.

Do not enable the BCSI option for transmission until the complete TX-to-RX transport path passes round-trip, truncation, reorder, duplicate, loss, malformed-packet and cross-platform regression tests.
