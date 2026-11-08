# QPSK / 8PSK / 16QAM FEC review (pre-implementation audit)

Date: 2026-11-07

> Historical audit. Its implementation-status statements describe the state at the time of review and are superseded by the current balanced/robust per-modulation FEC policy in `SOFT_FEC_PACKET_RECOVERY.md` and the airtime results in `MODEM_THROUGHPUT.md`.

This document records the baseline audit performed before the soft-decision
FEC implementation. The recommendations below have since been implemented in
the M-ary streaming path; see `SOFT_FEC_PACKET_RECOVERY.md` for the current
wire format, code parameters, compatibility rules and validation coverage.

## Current configuration

All three profiles use 1600 symbols/s and 48 kHz audio:

| Profile | Bits/symbol | Gross bit rate | Current image-packet FEC |
|---|---:|---:|---|
| QPSK | 2 | 3.2 kbit/s | SSDV RS8, RS(255,223), 32 parity symbols |
| 8PSK | 3 | 4.8 kbit/s | SSDV RS8, RS(255,223), 32 parity symbols |
| 16QAM | 4 | 6.4 kbit/s | SSDV RS8, RS(255,223), 32 parity symbols |

`internal/tximage/encode.go` and `internal/gifmode/gif.go` both request
`FEC: true`. For each 256-byte SSDV packet, bytes 1..255 form the shortened
RS8 codeword: 223 data symbols plus 32 parity symbols. The decoder corrects
up to 16 erroneous byte symbols in that codeword, then validates the packet
CRC32. The leading sync byte is outside that RS codeword. This packet-level
RS8 layer is common to all three M-ary modulation profiles.

At the time of this audit, the M-ary layer added no separate convolutional,
turbo or LDPC FEC. The current streaming M-ary path now adds the SDF2 K=7
rate-1/2 convolutional code, 64-byte block interleaver, and SPF2 8+2 packet
erasure parity. The CCSDS randomizer itself still adds no redundancy.

The baseline receiver used hard constellation decisions. The current M-ary
streaming receiver additionally exposes per-bit max-log LLR values and feeds
them directly to a soft-decision Viterbi decoder for SDF2 bursts. Legacy M-ary
bursts continue through the hard-decision compatibility path.

## What this means for the three modes

The packet FEC strength is identical in QPSK, 8PSK and 16QAM. Their RF/audio
robustness is not identical:

- QPSK has the widest constellation decision margin at equal average symbol
  energy and is expected to be the most tolerant of noise and residual phase
  error.
- 8PSK has closer angular decision boundaries than QPSK.
- 16QAM has closer normalized point spacing and additionally depends on
  amplitude accuracy, so it is the most sensitive to compression, clipping,
  gain changes and nonlinear audio paths.

The current receiver has training-based amplitude/phase/frequency acquisition
and decision-directed tracking, but this is synchronization/equalization, not
FEC. Existing software tests cover clean loopback and selected frequency
offset/gain/filter cases. They do not establish AWGN, burst-fade or real-radio
packet-error performance.

## Historical recommendations (implementation status)

### Priority 1: soft-decision demapping — max-log LLR implemented; noise calibration remains future work

The M-ary receive path retains the complex matched-filter symbol and produces
per-bit max-log LLRs rather than immediately slicing to hard bits. The current
implementation uses minimum squared constellation distances and clips the
result; it does not yet calibrate LLR magnitude against a measured noise
variance from training/residual symbols.
For 16QAM, per-bit LLRs are especially valuable because inner and outer points
have different reliability. This requires a compatible inner FEC code; merely
calculating LLRs while continuing to discard them before RS8 will not improve
correction.

### Priority 2: inner FEC baseline — implemented; selectable rates remain future work

Keep the existing per-packet RS8 unchanged as the outer packet-integrity layer.
Add an optional channel-code layer around the CCSDS randomized data payload,
with ASM/training acquisition kept separately recognizable. Candidate
experiments:

- Constraint-length-7 convolutional code with soft-decision Viterbi decoding:
  rate 1/2 as a robust baseline, then punctured 2/3 and 3/4 variants for
  throughput/robustness trade-offs.
- A modern LDPC option at a moderate code rate if a block decoder and its
  latency/CPU budget are acceptable.

Do not select code rates based on intuition alone. Compare packet success rate
at equal on-air duration and equal information payload. QPSK can reasonably be
offered with a lower code rate; 8PSK/16QAM should be measured with more
conservative margins, but the final mapping should be configurable and tested.

### Priority 3: symbol/byte interleaving — implemented (64-byte 8×8 transpose)

Add a bounded block interleaver across multiple transport data units, then
reverse it before RS8 packet parsing. Start with depths 4 and 8. This can turn a
short continuous fade or impulse burst into dispersed errors that individual
RS8 codewords can correct. Interleaving adds latency and buffering and must be
specified on-air. It does not recover an entire packet that was completely
lost/erased.

### Priority 4: packet-erasure recovery — implemented (8+2)

The existing RS8 code repairs corrupted symbols inside a received packet. It
cannot recreate a whole missing SSDV packet. For unacknowledged/simplex
transmission, add packet-level erasure coding across groups of source packets
(e.g. systematic K data + R parity) or a fountain-code layer. A small initial
experiment such as K=8 with R=2 can quantify the 20% parity overhead and
recover up to two erased packets per group when enough independent equations
arrive. Larger groups or fountain coding can handle less predictable loss at
higher implementation complexity. This is the feature most directly related
to whole-frame/packet loss rather than bit corruption.

### Priority 5: link adaptation and instrumentation

Record per profile: training quality, estimated noise variance, carrier/phase
tracking error, hard/soft BER estimate, RS8 corrected-symbol count, CRC failure,
ASM loss, missing packet count and reconstructed-image coverage. Use measured
curves to define QPSK/8PSK/16QAM operating recommendations. Avoid fixed SNR
switch thresholds until a repeatable AWGN and recorded-radio test matrix exists.

## Suggested validation matrix

1. AWGN sweeps per modulation and FEC rate; report BER and complete SSDV packet
   success, not only raw bit recovery.
2. Burst errors and short fades with/without interleaving depths 4 and 8.
3. Erasure simulation at 1%, 3%, 5%, 10% and burst loss patterns for packet
   parity/fountain candidates.
4. Carrier offset, sample-clock offset, audio gain variation and clipping.
5. Real WAV/audio loopback and actual radio/SDR captures once available.
6. Compare image completion/coverage and useful image bits per second at equal
   transmit airtime.

## Source inspection points

- `internal/domain/profile.go`: three 1600-baud M-ary profiles and raw rates.
- `internal/tximage/encode.go`: static SSDV packet generation with `FEC: true`.
- `internal/gifmode/gif.go`: GIF frame packet generation with `FEC: true`.
- `internal/fec/rs8.go`: RS8 parameters and correction implementation.
- `internal/image/ssdv/packet.go`: RS8 correction followed by CRC32 validation.
- `internal/modem/mary/modem.go`: constellation mapping, hard decisions and
  training/decision-directed receiver.
- `internal/modem/ccsds/frame.go`: ASM and PN randomization; no payload FEC.

The current implementation is described in `SOFT_FEC_PACKET_RECOVERY.md`. This
file is retained as the historical baseline audit that motivated that work.
