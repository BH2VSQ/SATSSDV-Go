# M-ary soft-decision FEC and packet-erasure recovery

Date: 2026-11-07

This implementation adds a new M-ary transport envelope for QPSK, 8PSK and
16QAM. It is separate from the GIF frame-level SGV2 motion-vector extension
specified in `SGV2_PROTOCOL.md`.

## Scope and compatibility

- New M-ary transmissions produced by `Encoder.StreamPackets` use SDF3
  independently protected groups of up to ten CCSDS data units. Each group
  has one repeated descriptor, the selected K=7 convolutional-code profile and
  a 64-byte block interleaver; SPF2 packet-erasure parity depends on the
  balanced/robust FEC policy.
- The receiver probes for SDF3 first, then supports the earlier SDF2 burst
  format, and finally falls back to the original hard-decision CCSDS-MARY
  path. Previously recorded SGV1/uncoded M-ary bursts remain receivable.
- A receiver predating the punctured SDF3 profiles (5/6, 3/4 and 2/3) does not
  understand the balanced on-air coding profiles and must not be expected to
  decode them. The legacy `EncodePackets` API remains
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

## SDF3 group descriptor

The logical descriptor is 18 bytes. Byte 5 identifies the convolutional-code
profile: 2 = K=7 rate 1/2, 3 = punctured K=7 rate 3/4, 4 = punctured K=7 rate
2/3, and 5 = punctured K=7 rate 5/6. The receiver accepts all four profiles,
including earlier SDF3 rate-1/2 groups.

| Offset | Size | Meaning |
|---|---:|---|
| 0 | 4 | ASCII `SDF3` |
| 4 | 1 | Descriptor version, currently 1 |
| 5 | 1 | Inner-code profile: 2 = rate 1/2, 3 = rate 3/4, 4 = rate 2/3, 5 = rate 5/6; K=7 generators 171/133 octal |
| 6 | 2 | Interleaver block size, 64 bytes |
| 8 | 4 | Number of raw bytes after the descriptor |
| 12 | 4 | CRC32/IEEE of the complete raw byte stream |
| 16 | 2 | CRC16-CCITT of bytes 0..15 |

Every descriptor bit is transmitted three times. The receiver combines each
triplet in the LLR domain, then verifies CRC16 before trusting the length or
code profile. The descriptor follows the existing 96-symbol M-ary training
word and precedes the encoded data stream.

At the exact end of the advertised interleaved body, the receiver releases the
current carrier/timing lock and re-arms for the next group's 96-symbol training
word. It does not rely only on detecting digital silence: a real radio capture
continues to contain channel noise during the 200 ms transmit guard, and that
noise can prevent an audio-RMS silence detector from reaching its threshold.
This boundary-based re-acquisition is covered with noisy guards and multiple
callback sizes.

## Inner code and interleaver

The grouped CCSDS byte stream (ASM/data units) is protected by a terminated
K=7 convolutional mother code using generator polynomials 171 and 133 octal,
with six zero termination bits and MSB-first bit order. The encoder supports four on-air profiles:

- Profile 2: rate 1/2, no puncturing.
- Profile 3: rate 3/4, keep (A,B), then A, then B in a repeating three-input
  pattern.
- Profile 4: rate 2/3, keep (A,B), then A in a repeating two-input pattern.
- Profile 5: rate 5/6, keep (A,B), then A, A, B, B in a repeating five-input
  pattern. Punctured observations in all three punctured profiles are restored
  as zero-reliability LLRs.

The default **balanced** profile is selected by modulation:

| Modulation | Inner code | Packet parity after each 8 SSDV packets |
|---|---:|---:|
| QPSK | rate 5/6 | 0 shards (SSDV RS8 remains enabled) |
| 8PSK | rate 3/4 | 1 shard (recovers one whole lost packet) |
| 16QAM | rate 2/3 | 1 shard (recovers one whole lost packet) |

Applications that need the previous maximum-redundancy behavior can select
`transport.MARYFECRobust` in `transport.Config`; this uses rate 1/2 plus both
packet-erasure parity shards for every M-ary constellation. The default
`transport.MARYFECBalanced` trades some coding margin for higher useful image
throughput. The SSDV packet-level RS8 FEC remains enabled in both policies.

Encoded bytes are interleaved in 64-byte blocks using an 8x8 byte transpose.
The receiver reverses the transpose in the soft domain and feeds LLRs directly
to the bounded-traceback soft-decision Viterbi decoder. Punctured observations
are represented by zero LLRs rather than hard guesses. The burst CRC32
validates the full decoded CCSDS group after Viterbi decoding.

## SPF2 packet-erasure parity

M-ary packet-erasure parity is selected by the FEC policy and modulation.
The balanced defaults emit zero shards for QPSK, one shard for 8PSK, and two
shards for 16QAM after each group of up to eight adjacent ordinary FEC-enabled
SSDV data packets of the same callsign and image ID. `MARYFECRobust` always
emits both parity data units (`SPF2`), forming a systematic 8+2 group.

Each parity payload identifies the source packet IDs explicitly. It protects
SSDV bytes 2..223 (222 bytes); the fixed leading sync/type bytes are recreated,
and the existing RS8 parity bytes are regenerated after recovery.

With one parity shard the receiver can recover one missing data packet. With
two independent parity shards it can recover any two missing data packets. A
zero-parity group relies on each packet's own RS8 code and CRC, but cannot
recreate a completely erased packet. No configuration can recover more packet
erasures than the number of available parity shards. Reconstructed packets are
passed through the regular SSDV parser, including RS8 and packet CRC validation,
and are marked `fecRecovered` in the receive event.

Packet-cache memory is bounded to 512 packet IDs per image key; idle packet
caches expire after two minutes and incomplete parity groups after 30 seconds.

## Current test coverage

- noiseless streaming TX/RX round trips for QPSK, 8PSK and 16QAM at both
  44.1 kHz and 48 kHz audio rates;
- deterministic AWGN software loopback at 18 dB for all three balanced
  modulation-specific profiles;
- 6 dB AWGN across two SDF3 groups with noisy inter-group guards for QPSK,
  8PSK and 16QAM, tested with 480, 997, 1920 and 4096-sample callbacks;
- live M-ary EVM-SNR publication after synchronization, including Auto RX
  candidate forwarding and idle-noise rejection;
- fixed ±200 Hz carrier offset and ±150 Hz linear drift tests at 18 dB for
  balanced QPSK, 8PSK and 16QAM;
- actual encoder sample-count checks against the airtime estimator;
- old uncoded M-ary waveform reception through the legacy fallback;
- SDF3 header repetition and CRC validation for rate 1/2, rate 2/3, rate 3/4 and rate 5/6;
- one- and two-packet erasure recovery, including restoration accepted by the
  normal SSDV RS8 parser;
- re-acquisition of a second M-ary burst after an inter-burst low-energy gap;
- existing AX.25, CCSDS FSK, GIF, RX and TX regression suites.

The software tests do not replace real sound-card/Malgo tests or RF/SDR
measurements. In particular, the demodulator must be tested with measured
receiver noise floors, frequency offsets, sample-clock errors, multipath and
bursty fading before publishing operational SNR recommendations.
