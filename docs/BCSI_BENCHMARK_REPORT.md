# BCSI / JPEG / Haar codec benchmark

- Date: 2026-11-04 15:28:57 UTC
- Go: go1.23.2
- OS/Arch: linux/amd64
- CPUs visible: 3
- Reconstruction: 24 iterations, 3 workers
- Fixture: deterministic synthetic RGB image (smooth gradients + edges + texture)

All codec sizes include each codec's own headers/CRC/compressed payload. BCSI size is the sum of independent block packets; no existing CCSDS/AX.25 framing, link FEC, interleaving or modem waveform overhead is included. The JPEG target-size row uses Go's standard JPEG encoder with quality selected to approach the BCSI byte target; it is not a separate libjpeg-turbo optimized-Huffman implementation. JPEG and Haar candidates may be smaller/larger than the target when the tested quality/quantizer range cannot hit it exactly. Airtime is ideal payload-only time at 1200/2400 bit/s, not a modem/on-air measurement.

## Same-target-size comparison

| Image | Base luma / effective all-channel measurements | BCSI bytes | PSNR | SSIM | Encode ms | Decode ms | Encode alloc MB | Decode alloc MB | JPEG Q75 bytes / PSNR | Target-size JPEG Q / bytes / PSNR | Haar step / bytes / PSNR | Ideal payload time 1200 / 2400 bit/s |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 320x240 | 25.0% / 15.5% | 79203 | 18.52 dB | 0.8866 | 7.3 | 170.3 | 1.64 | 1.87 | Q75: 21421 / 19.43 dB | Q100: 80407 / 20.43 dB | step 6: 77936 / 41.43 dB | 528.0 s / 264.0 s |
<!-- Haar encode/decode 12792/6916 us; BCSI measurements 35735; BCSI received blocks 1200/1200 -->
| 320x240 | 37.5% / 22.9% | 97178 | 20.15 dB | 0.9344 | 8.1 | 154.1 | 1.69 | 2.44 | Q75: 21421 / 19.43 dB | Q100: 80407 / 20.43 dB | step 3: 93515 / 46.30 dB | 647.9 s / 323.9 s |
<!-- Haar encode/decode 9648/6986 us; BCSI measurements 52845; BCSI received blocks 1200/1200 -->
| 320x240 | 50.0% / 30.5% | 114834 | 21.31 dB | 0.9558 | 11.8 | 143.4 | 1.80 | 2.87 | Q75: 21421 / 19.43 dB | Q100: 80407 / 20.43 dB | step 2: 105959 / 48.26 dB | 765.6 s / 382.8 s |
<!-- Haar encode/decode 10097/6769 us; BCSI measurements 70259; BCSI received blocks 1200/1200 -->
| 448x336 | 25.0% / 15.5% | 155288 | 18.51 dB | 0.8852 | 15.1 | 285.5 | 2.08 | 3.64 | Q75: 41701 / 19.39 dB | Q100: 159945 / 20.41 dB | step 6: 157055 / 41.24 dB | 1035.3 s / 517.6 s |
<!-- Haar encode/decode 19784/14325 us; BCSI measurements 70075; BCSI received blocks 2352/2352 -->
| 448x336 | 37.5% / 23.0% | 190468 | 20.02 dB | 0.9290 | 18.4 | 318.4 | 3.36 | 4.69 | Q75: 41701 / 19.39 dB | Q100: 159945 / 20.41 dB | step 3: 186908 / 46.30 dB | 1269.8 s / 634.9 s |
<!-- Haar encode/decode 19872/12025 us; BCSI measurements 103691; BCSI received blocks 2352/2352 -->
| 448x336 | 50.0% / 30.5% | 224913 | 21.14 dB | 0.9510 | 22.9 | 289.4 | 2.41 | 5.74 | Q75: 41701 / 19.39 dB | Q100: 159945 / 20.41 dB | step 2: 212531 / 48.20 dB | 1499.4 s / 749.7 s |
<!-- Haar encode/decode 19702/14118 us; BCSI measurements 137748; BCSI received blocks 2352/2352 -->
| 640x480 | 25.0% / 15.6% | 317714 | 18.39 dB | 0.8891 | 23.6 | 614.2 | 4.21 | 7.37 | Q75: 85988 / 19.22 dB | Q99: 301469 / 20.26 dB | step 7: 314109 / 40.32 dB | 2118.1 s / 1059.0 s |
<!-- Haar encode/decode 36748/24856 us; BCSI measurements 143423; BCSI received blocks 4800/4800 -->
| 640x480 | 37.5% / 23.1% | 390057 | 19.84 dB | 0.9310 | 31.5 | 653.4 | 3.33 | 9.59 | Q75: 85988 / 19.22 dB | Q100: 335234 / 20.27 dB | step 3: 392583 / 46.18 dB | 2600.4 s / 1300.2 s |
<!-- Haar encode/decode 36916/29152 us; BCSI measurements 212621; BCSI received blocks 4800/4800 -->
| 640x480 | 50.0% / 30.6% | 460225 | 20.96 dB | 0.9529 | 48.2 | 644.0 | 3.74 | 11.82 | Q75: 85988 / 19.22 dB | Q100: 335234 / 20.27 dB | step 2: 441581 / 48.16 dB | 3068.2 s / 1534.1 s |
<!-- Haar encode/decode 40645/26663 us; BCSI measurements 281982; BCSI received blocks 4800/4800 -->

## BCSI packet-loss behavior (320×240, 37.5% base luma rate)

| Random block-packet loss | Blocks received | Blocks independently reconstructed | Blocks concealed | Output coverage | PSNR | SSIM |
|---:|---:|---:|---:|---:|---:|---:|
| 10% | 1080/1200 | 1080 | 120 | 90.0% | 17.56 dB | 0.8833 |
| 25% | 900/1200 | 900 | 300 | 75.0% | 15.36 dB | 0.8130 |
| 40% | 720/1200 | 720 | 480 | 60.0% | 14.14 dB | 0.7386 |

## Interpretation and limitations

- BCSI is a block-local experimental codec. Every block packet carries image geometry, block index, measurement counts, quantizer, deterministic matrix seed and CRC. A missing/corrupt block does not invalidate neighbouring blocks; the preview conceals missing blocks by nearest recovered block. Concealment improves canvas continuity but does not recover the original missing detail.
- The test input is deterministic synthetic RGB content (gradients, geometric edges and texture), not a photographic corpus. Results must not be generalized to all satellite imagery; repeat with representative user images before selecting production defaults.
- The Haar baseline is a deliberately simple block Haar + scalar quantization + DEFLATE reference, not JPEG 2000. The target-size JPEG/Haar values are the nearest candidate from the tested quality/step range; actual byte counts are shown because exact rate matching is not always possible.
- Allocation columns show cumulative Go heap bytes allocated during the respective BCSI encode/decode phase. They are allocation volume, not peak resident set size (RSS) and do not include native audio/GUI allocations.
- BCSI measurements and reconstruction use floating-point arithmetic. The current prototype prioritizes correctness and packet independence; it is not yet integrated into a radio profile/modem, so on-air compatibility and real RF packet loss are not claimed.
- Reported encode/decode time is one local run and is workload/CPU dependent. Run this command on Windows, Linux and macOS release builds before making cross-platform performance claims.
