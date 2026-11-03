# Round 13 interoperability findings

This round addresses the report that Hamstation decodes the application's SSDV CCSDS 1200 and PCSI CCSDS 1200 transmissions, while the other transmit profiles fail or produce incomplete PCSI images.

## Changes made

- CCSDS 2400 TX now uses the ARCSSD phase deviation of ±31 and reference bit polarity. The prior Go path used ±16 and the opposite bit-to-deviation mapping, so its waveform did not match the reference modem.
- CCSDS 2400 RX majority voting now uses the same polarity as the ARCSSD C receiver.
- CCSDS 1200 TX now uses the reference 1024-entry DDS sine table, 16 samples/bit, center phase increment 91, deviation 26 and output amplitude 0.9155273.
- CCSDS ASM scanning now tolerates a single bit error, as the reference receiver does, and restores the canonical ASM bytes before parsing.
- AX.25 TX uses 75 HDLC flags before and after a burst, equivalent to 500 ms each at 1200 bit/s. The AFSK waveform now follows the reference DDS lookup table, integer phase increments (64 for mark, 117 for the quantized space tone), phase-update order and 0.9155273 amplitude at 19.2 kHz.
- The TX editor allows text/image layers to be dragged beyond the canvas edges. Component coordinates are no longer clamped to the canvas; the final rendered canvas clips what lies outside. If a layer is moved completely off-canvas, select it in the layer list and drag from empty canvas space to bring it back. Layer list order determines occlusion.
- Native browser confirm/alert/prompt calls were removed from the frontend. Destructive actions use an in-app inline double-click confirmation, avoiding browser dialogs labelled with the Wails origin.

## Independent reference checks

- CCSDS 1200 and 2400 include sample-level golden tests generated from the ARCSSD DDS/GMSK reference formulas and coefficients.
- PCSI MMT's first 224-byte packet body for a deterministic 320x240 pattern matches a separately generated Python reference vector (SHA256 `168cd4636342ba18519ef9f52edb7a6d778e0c178689d5861782f829933b6db9`).
- PCSI RS8 parity matches a separate ARCSSD C reference vector exactly.
- CCSDS one-bit ASM correction, 2400 RX symbol majority polarity, arbitrary stream chunk boundaries and AX.25 burst framing have regression tests.

## Remaining validation

The workspace does not contain the user's actual Hamstation virtual-audio WAV capture, so this round has not independently confirmed a complete Hamstation decode. It also has not run the Windows Wails GUI or a real Windows audio device. In particular, the 2400-bit/s RX still assumes the nominal 1800 Hz center frequency; AFC/carrier-offset tracking and real capture noise tests remain. SSDV image reconstruction still requires a contiguous packet sequence because MCU resynchronization metadata is not implemented beyond the current sentinel layout. PCSI's iterative reconstruction solver is not claimed to be numerically identical to SciPy's optimizer.

For a definitive end-to-end diagnosis, capture lossless 48 kHz or 19.2 kHz mono PCM from the virtual audio loop for one short image per mode, preserving the complete preamble and trailer. Those files can be decoded offline against the same transmitted bytes and compared packet-by-packet without relying on a live device.
