# M-ary soft-decision FEC and packet-erasure recovery

Date: 2026-11-07

This implementation adds a new M-ary transport envelope for QPSK, 8PSK and
16QAM. It is separate from the GIF frame-level SGV2 motion-vector extension
specified in `SGV2_PROTOCOL.md`.

## Scope and compatibility

- New M-ary transmissions produced by `Encoder.StreamPackets` use the SDF2
  soft-FEC burst descriptor, convolutional inner code, byte interleaver and
  SPF2 packet-erasure parity frames.
- The new M-ary receiver first probes for SDF2. If no SDF2 marker is found, it
  falls back to the original hard-decision CCSDS-MARY path, so previously
  recorded SGV1/uncoded M-ary bursts remain receivable.
- An older receiver does not understand the SDF2 envelope and must not be
  expected to decode new M-ary FEC bursts. The legacy `EncodePackets` API remains
  available for code that explicitly needs the old uncoded format, but the
  current Qt transmit path uses `StreamPackets` and does not expose a GUI
  compatibility toggle. Interoperation with an old receiver therefore requires
  an older application build or a separately added legacy transmit option.
- AX.25/AFSK and non-M-ary CCSDS FSK profiles are unchanged by this extension.
- The existing per-packet SSDV RS8 code remains enabled and is still validated
  after packet-level reconstruction.

## Soft-decision demapper

`mary.StreamDemodulator.FeedSoft` emits one max-log LLR per payload bit:

- positive LLR favours bit 0;
- negative LLR favours bit 1;
- values are clipped to a bounded range before entering the decoder.

For each constellation bit, the demapper calculates the minimum squared
Euclidean distance to all constellation points labelled 0 and 1. Their
 difference is the bit reliability metric. The legacy `Feed` API remains as a
hard-decision wrapper around these LLRs.

The receiver's carrier/timing acquisition and decision-directed tracking are
shared by both APIs. This is soft-decision demapping of symbol reliability, not
an estimate of a calibrated RF noise variance. The AWGN tests are reproducible
software simulations and do not define real-radio SNR thresholds.

## SDF2 burst descriptor

The logical descriptor is 18 bytes:

| Offset | Size | Meaning |
|---|---:|---|
| 0 | 4 | ASCII `SDF2` |
| 4 | 1 | Descriptor version, currently 1 |
| 5 | 1 | Inner-code profile, 1 = K=7 rate 1/2, generators 171/133 octal |
| 6 | 2 | Interleaver block size, 64 bytes |
| 8 | 4 | Number of raw bytes after the descriptor |
| 12 | 4 | CRC32/IEEE of the complete raw byte stream |
| 16 | 2 | CRC16-CCITT of bytes 0..15 |

Every descriptor bit is transmitted three times. The receiver combines each
triplet in the LLR domain, then verifies CRC16 before trusting the length or
code profile. The descriptor follows the existing 96-symbol M-ary training
word and precedes the encoded data stream.

## Inner code and interleaver

The complete randomized CCSDS byte stream (preamble, ASM/data units and
trailer) is protected by a terminated convolutional code:

- constraint length K=7;
- rate 1/2;
- generator polynomials 171 and 133 octal;
- six zero termination bits;
- MSB-first input and output bit order.

Encoded bytes are interleaved in 64-byte blocks using an 8x8 byte transpose.
The receiver reverses the transpose in the soft domain and feeds LLRs directly
to a bounded-traceback soft-decision Viterbi decoder. The burst CRC32 validates
the full decoded CCSDS stream after Viterbi decoding.

At rate 1/2, the inner code approximately doubles the coded bit count before
interleaver padding. This is a robustness/throughput trade-off; the current
implementation does not yet expose selectable punctured rates.

## SPF2 packet-erasure parity

For M-ary transmissions, each group of up to eight adjacent ordinary
FEC-enabled SSDV data packets of the same callsign and image ID is followed by
two parity data units (`SPF2`). These are systematic 8+2 packet-erasure groups.

Each parity payload identifies the source packet IDs explicitly. It protects
SSDV bytes 2..223 (222 bytes); the fixed leading sync/type bytes are recreated,
and the existing RS8 parity bytes are regenerated after recovery.

The receiver can recover:

- any one missing data packet when at least one valid parity shard arrives;
- any two missing data packets when both independent parity shards arrive.

It cannot recover more than two erasures in a group, nor can it recover a
missing parity shard and two missing data packets. Reconstructed packets are
passed through the regular SSDV parser, including RS8 and packet CRC validation,
and are marked `fecRecovered` in the receive event.

Packet-cache memory is bounded to 512 packet IDs per image key; idle packet
caches expire after two minutes and incomplete parity groups after 30 seconds.

## Current test coverage

- noiseless streaming TX/RX round trips for QPSK, 8PSK and 16QAM at both
  44.1 kHz and 48 kHz audio rates;
- deterministic AWGN software loopback at 18 dB for all three profiles;
- old uncoded M-ary waveform reception through the legacy fallback;
- SDF2 header repetition and CRC validation;
- one- and two-packet erasure recovery, including restoration accepted by the
  normal SSDV RS8 parser;
- re-acquisition of a second M-ary burst after an inter-burst low-energy gap;
- existing AX.25, CCSDS FSK, GIF, RX and TX regression suites.

The software tests do not replace real sound-card/Malgo tests or RF/SDR
measurements. In particular, the demodulator must be tested with measured
receiver noise floors, frequency offsets, sample-clock errors, multipath and
bursty fading before publishing operational SNR recommendations.
