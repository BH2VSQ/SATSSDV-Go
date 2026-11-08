
# SATSSDV-Go modem noise and frequency-offset characterization

Generated: 2026-11-08T09:04:49Z

Synthetic audio-PCM/baseband test only; this is not calibrated RF SNR, Eb/N0, receiver sensitivity or an over-the-air result.

AWGN SNR is defined from generated waveform RMS and injected white-Gaussian-noise RMS over the PCM buffer. 2 deterministic seeds are tested per SNR point; success requires one valid packet with the expected packet ID.

The receiver uses the production streaming decoder. QPSK/8PSK/16QAM transmit SDF3 grouped convolutional soft-FEC units. Legacy AX.25/CCSDS modes use their production streaming paths.

Constant audio-IF frequency shift is tested separately from AWGN. Only the contiguous pass interval around zero is counted; isolated successes beyond a failing point are excluded.

## Method

- SNR points: 30, 24, 18, 12, 9, 6, 3 and 0 dB; 2 deterministic seeds per point.
- FSK CFO points: -600 to +600 Hz in 100 Hz steps.
- M-ary CFO points: -300 to +300 Hz in 100 Hz steps.
- Each result is an end-to-end streaming packet decode with normal parser/integrity validation.
- Legacy AX.25/CCSDS modes use their 19.2 kHz native rate; M-ary modes use 48 kHz.

## ssdv-ax25

- Modulation: `afsk-1200`; codec: `SSDV`; link layer: `AX.25 UI / HDLC`.
- Audio rate: 19200 Hz; bit rate: 1200 bit/s; symbol rate: 0 baud; bits/symbol: 0.
- FSK tones: mark 1200 Hz, space 2200 Hz.

### AWGN SNR sweep

| Injected SNR | Valid packets | Success |
|---:|---:|---:|
| 30 dB | 2/2 | 100% |
| 24 dB | 2/2 | 100% |
| 18 dB | 2/2 | 100% |
| 12 dB | 2/2 | 100% |
| 9 dB | 2/2 | 100% |
| 6 dB | 2/2 | 100% |
| 3 dB | 0/2 | 0% |
| 0 dB | 0/2 | 0% |

Lowest tested SNR with at least 2/2 successes at this and every higher point: **6 dB (coarse grid)**.

### Constant audio-IF frequency offset

| Offset | Result |
|---:|:---:|
| -600 Hz | PASS |
| -500 Hz | PASS |
| -400 Hz | PASS |
| -300 Hz | PASS |
| -200 Hz | PASS |
| -100 Hz | PASS |
| +0 Hz | PASS |
| +100 Hz | PASS |
| +200 Hz | PASS |
| +300 Hz | PASS |
| +400 Hz | PASS |
| +500 Hz | PASS |
| +600 Hz | PASS |

Contiguous interval: **-600 / +600 Hz**; symmetric verified interval: **±600 Hz** (100 Hz step).

## ssdv-ccsds-1200

- Modulation: `afsk-1200`; codec: `SSDV`; link layer: `CCSDS`.
- Audio rate: 19200 Hz; bit rate: 1200 bit/s; symbol rate: 0 baud; bits/symbol: 0.
- FSK tones: mark 1200 Hz, space 2200 Hz.

### AWGN SNR sweep

| Injected SNR | Valid packets | Success |
|---:|---:|---:|
| 30 dB | 2/2 | 100% |
| 24 dB | 2/2 | 100% |
| 18 dB | 2/2 | 100% |
| 12 dB | 2/2 | 100% |
| 9 dB | 2/2 | 100% |
| 6 dB | 2/2 | 100% |
| 3 dB | 2/2 | 100% |
| 0 dB | 0/2 | 0% |

Lowest tested SNR with at least 2/2 successes at this and every higher point: **3 dB (coarse grid)**.

### Constant audio-IF frequency offset

| Offset | Result |
|---:|:---:|
| -600 Hz | PASS |
| -500 Hz | PASS |
| -400 Hz | PASS |
| -300 Hz | PASS |
| -200 Hz | PASS |
| -100 Hz | PASS |
| +0 Hz | PASS |
| +100 Hz | PASS |
| +200 Hz | PASS |
| +300 Hz | PASS |
| +400 Hz | PASS |
| +500 Hz | PASS |
| +600 Hz | PASS |

Contiguous interval: **-600 / +600 Hz**; symmetric verified interval: **±600 Hz** (100 Hz step).

## ssdv-ccsds-2400

- Modulation: `afsk-2400`; codec: `SSDV`; link layer: `CCSDS`.
- Audio rate: 19200 Hz; bit rate: 2400 bit/s; symbol rate: 0 baud; bits/symbol: 0.
- FSK tones: mark 1200 Hz, space 2400 Hz.

### AWGN SNR sweep

| Injected SNR | Valid packets | Success |
|---:|---:|---:|
| 30 dB | 2/2 | 100% |
| 24 dB | 2/2 | 100% |
| 18 dB | 2/2 | 100% |
| 12 dB | 2/2 | 100% |
| 9 dB | 1/2 | 50% |
| 6 dB | 2/2 | 100% |
| 3 dB | 0/2 | 0% |
| 0 dB | 0/2 | 0% |

Lowest tested SNR with at least 2/2 successes at this and every higher point: **12 dB (coarse grid)**.

### Constant audio-IF frequency offset

| Offset | Result |
|---:|:---:|
| -600 Hz | PASS |
| -500 Hz | PASS |
| -400 Hz | PASS |
| -300 Hz | PASS |
| -200 Hz | PASS |
| -100 Hz | PASS |
| +0 Hz | PASS |
| +100 Hz | PASS |
| +200 Hz | PASS |
| +300 Hz | PASS |
| +400 Hz | PASS |
| +500 Hz | PASS |
| +600 Hz | PASS |

Contiguous interval: **-600 / +600 Hz**; symmetric verified interval: **±600 Hz** (100 Hz step).

## pcsi-ccsds-1200

- Modulation: `afsk-1200`; codec: `PCSI-MMT`; link layer: `CCSDS`.
- Audio rate: 19200 Hz; bit rate: 1200 bit/s; symbol rate: 0 baud; bits/symbol: 0.
- FSK tones: mark 1200 Hz, space 2200 Hz.

### AWGN SNR sweep

| Injected SNR | Valid packets | Success |
|---:|---:|---:|
| 30 dB | 2/2 | 100% |
| 24 dB | 2/2 | 100% |
| 18 dB | 2/2 | 100% |
| 12 dB | 2/2 | 100% |
| 9 dB | 2/2 | 100% |
| 6 dB | 2/2 | 100% |
| 3 dB | 2/2 | 100% |
| 0 dB | 0/2 | 0% |

Lowest tested SNR with at least 2/2 successes at this and every higher point: **3 dB (coarse grid)**.

### Constant audio-IF frequency offset

| Offset | Result |
|---:|:---:|
| -600 Hz | PASS |
| -500 Hz | PASS |
| -400 Hz | PASS |
| -300 Hz | PASS |
| -200 Hz | PASS |
| -100 Hz | PASS |
| +0 Hz | PASS |
| +100 Hz | PASS |
| +200 Hz | PASS |
| +300 Hz | PASS |
| +400 Hz | PASS |
| +500 Hz | PASS |
| +600 Hz | PASS |

Contiguous interval: **-600 / +600 Hz**; symmetric verified interval: **±600 Hz** (100 Hz step).

## pcsi-ccsds-2400

- Modulation: `afsk-2400`; codec: `PCSI-MMT`; link layer: `CCSDS`.
- Audio rate: 19200 Hz; bit rate: 2400 bit/s; symbol rate: 0 baud; bits/symbol: 0.
- FSK tones: mark 1200 Hz, space 2400 Hz.

### AWGN SNR sweep

| Injected SNR | Valid packets | Success |
|---:|---:|---:|
| 30 dB | 2/2 | 100% |
| 24 dB | 2/2 | 100% |
| 18 dB | 2/2 | 100% |
| 12 dB | 2/2 | 100% |
| 9 dB | 2/2 | 100% |
| 6 dB | 0/2 | 0% |
| 3 dB | 0/2 | 0% |
| 0 dB | 0/2 | 0% |

Lowest tested SNR with at least 2/2 successes at this and every higher point: **9 dB (coarse grid)**.

### Constant audio-IF frequency offset

| Offset | Result |
|---:|:---:|
| -600 Hz | PASS |
| -500 Hz | PASS |
| -400 Hz | PASS |
| -300 Hz | PASS |
| -200 Hz | PASS |
| -100 Hz | PASS |
| +0 Hz | PASS |
| +100 Hz | PASS |
| +200 Hz | PASS |
| +300 Hz | PASS |
| +400 Hz | PASS |
| +500 Hz | PASS |
| +600 Hz | PASS |

Contiguous interval: **-600 / +600 Hz**; symmetric verified interval: **±600 Hz** (100 Hz step).

## ssdv-qpsk-1600

- Modulation: `qpsk`; codec: `SSDV`; link layer: `CCSDS-MARY`.
- Audio rate: 48000 Hz; bit rate: 3200 bit/s; symbol rate: 1600 baud; bits/symbol: 2.
- TX format: SDF3 grouped (max 10 CCSDS units/group; this fixture carries one unit); K=7 rate-3/4 soft-decision Viterbi; 0 packet-erasure parity shards per 8 source SSDV packets.
- M-ary carrier: 1600 Hz; acquisition word: 96 symbols.

### AWGN SNR sweep

| Injected SNR | Valid packets | Success |
|---:|---:|---:|
| 30 dB | 2/2 | 100% |
| 24 dB | 2/2 | 100% |
| 18 dB | 2/2 | 100% |
| 12 dB | 2/2 | 100% |
| 9 dB | 2/2 | 100% |
| 6 dB | 2/2 | 100% |
| 3 dB | 0/2 | 0% |
| 0 dB | 0/2 | 0% |

Lowest tested SNR with at least 2/2 successes at this and every higher point: **6 dB (coarse grid)**.

### Constant audio-IF frequency offset

| Offset | Result |
|---:|:---:|
| -300 Hz | PASS |
| -200 Hz | PASS |
| -100 Hz | PASS |
| +0 Hz | PASS |
| +100 Hz | PASS |
| +200 Hz | PASS |
| +300 Hz | PASS |

Contiguous interval: **-300 / +300 Hz**; symmetric verified interval: **±300 Hz** (100 Hz step).

## ssdv-8psk-1600

- Modulation: `8psk`; codec: `SSDV`; link layer: `CCSDS-MARY`.
- Audio rate: 48000 Hz; bit rate: 4800 bit/s; symbol rate: 1600 baud; bits/symbol: 3.
- TX format: SDF3 grouped (max 10 CCSDS units/group; this fixture carries one unit); K=7 rate-2/3 soft-decision Viterbi; 1 packet-erasure parity shards per 8 source SSDV packets.
- M-ary carrier: 1600 Hz; acquisition word: 96 symbols.

### AWGN SNR sweep

| Injected SNR | Valid packets | Success |
|---:|---:|---:|
| 30 dB | 2/2 | 100% |
| 24 dB | 2/2 | 100% |
| 18 dB | 2/2 | 100% |
| 12 dB | 2/2 | 100% |
| 9 dB | 2/2 | 100% |
| 6 dB | 2/2 | 100% |
| 3 dB | 0/2 | 0% |
| 0 dB | 0/2 | 0% |

Lowest tested SNR with at least 2/2 successes at this and every higher point: **6 dB (coarse grid)**.

### Constant audio-IF frequency offset

| Offset | Result |
|---:|:---:|
| -300 Hz | PASS |
| -200 Hz | PASS |
| -100 Hz | PASS |
| +0 Hz | PASS |
| +100 Hz | PASS |
| +200 Hz | PASS |
| +300 Hz | PASS |

Contiguous interval: **-300 / +300 Hz**; symmetric verified interval: **±300 Hz** (100 Hz step).

## ssdv-16qam-1600

- Modulation: `16qam`; codec: `SSDV`; link layer: `CCSDS-MARY`.
- Audio rate: 48000 Hz; bit rate: 6400 bit/s; symbol rate: 1600 baud; bits/symbol: 4.
- TX format: SDF3 grouped (max 10 CCSDS units/group; this fixture carries one unit); K=7 rate-1/2 soft-decision Viterbi; 2 packet-erasure parity shards per 8 source SSDV packets.
- M-ary carrier: 1600 Hz; acquisition word: 96 symbols.

### AWGN SNR sweep

| Injected SNR | Valid packets | Success |
|---:|---:|---:|
| 30 dB | 2/2 | 100% |
| 24 dB | 2/2 | 100% |
| 18 dB | 2/2 | 100% |
| 12 dB | 2/2 | 100% |
| 9 dB | 2/2 | 100% |
| 6 dB | 2/2 | 100% |
| 3 dB | 0/2 | 0% |
| 0 dB | 0/2 | 0% |

Lowest tested SNR with at least 2/2 successes at this and every higher point: **6 dB (coarse grid)**.

### Constant audio-IF frequency offset

| Offset | Result |
|---:|:---:|
| -300 Hz | PASS |
| -200 Hz | PASS |
| -100 Hz | PASS |
| +0 Hz | PASS |
| +100 Hz | PASS |
| +200 Hz | PASS |
| +300 Hz | PASS |

Contiguous interval: **-300 / +300 Hz**; symmetric verified interval: **±300 Hz** (100 Hz step).

## Limitations

The SNR threshold is a coarse tested-grid estimate for one packet fixture and 2 deterministic noise seeds per point, not a statistically certified packet-error-rate curve. Refine transitions with multiple image payloads, 1 dB steps, at least 100 seeds, recorded receiver audio, RF filtering, AGC, clipping, gain variation and oscillator errors.

The CFO test translates the generated audio waveform. It does not exercise RF front-end selectivity, oscillator phase noise, fading, multipath or a complete satellite Doppler trajectory. The separate SDF3 regression tests add -200/0/+200 Hz at 18 dB SNR and +/-150 Hz linear CFO ramps.

