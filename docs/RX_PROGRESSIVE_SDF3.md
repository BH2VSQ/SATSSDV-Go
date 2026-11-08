# SDF3 progressive RX delivery and M-ary SNR holdover

## What changed

- SDF3 soft bits are deinterleaved in 64-byte blocks and fed into a single
  stateful depuncturer/Viterbi decoder. The receive path no longer waits for
  the complete SDF3 group's interleaved body before decoding its first CCSDS
  frames.
- Complete CCSDS data units are passed to the normal CCSDS/SSDV integrity
  parser as soon as the Viterbi traceback makes their bits available. The
  enclosing SDF3 CRC32 is still checked at the group boundary. A group CRC
  failure is reported, but packets which already passed their own CCSDS and
  SSDV integrity checks are not thrown away.
- The depuncturer retains puncture phase and partial puncture steps across
  arbitrary decoder callback boundaries for rate 1/2, 3/4, 2/3 and 5/6.
- M-ary EVM statistics survive the short acquisition releases between SDF3
  groups. They are cleared on an explicit modem reset, and the estimate is
  valid for at most two seconds after the last decoded symbol. The UI/API
  treats this short unlocked interval as an SNR holdover rather than dropping
  the value at every 200 ms group guard.
- Each `rx:image` event contains its own bounded JPEG `previewDataUrl`. Qt
  displays this event-local snapshot rather than asynchronously fetching a
  mutable `GetLatestRXImageData` slot, avoiding skipped intermediate snapshots
  when several image events are polled together. The full-resolution PNG is
  still retained in the latest-preview slot and is still used for archive work.

## Verification

The following synthetic, production-stream-path test passed for QPSK, 8PSK and
16QAM using 8 SSDV source packets, an SDF3 group size of 10, 18 dB AWGN and
+200 Hz constant audio-IF offset:

| Modulation | First packet emitted | Final packets | SDF3 group |
|---|---:|---:|---|
| QPSK 1600 | 15.4% of PCM stream | 8/8 | CRC passed |
| 8PSK 1600 | 15.2% of PCM stream | 8/8 | CRC passed |
| 16QAM 1600 | 15.8% of PCM stream | 8/8 | CRC passed |

The full Go test suite (`go test ./... -count=1`) and `go vet ./...` passed
before the final Auto-RX candidate-selection guard. After that guard, the root
package tests and `go vet .` passed, and the complete `internal/transport`
test suite passed separately with a 90-second test timeout. A combined final
`go test ./...` rerun timed out at the execution-environment level before
printing the transport-package result; no Go test failure stack was produced.
All Go test runs used the locally installed Go 1.23.2 compiler with a temporary
local-only `go.mod` version adjustment. The checked-in `go.mod` remains at the
project's required Go 1.26.0. Native Go 1.26 verification was not possible
because the environment could not download that toolchain.

Qt6 Widgets development files are not installed in this environment. The Qt
source changes were reviewed but a Qt desktop build was not run.

## Measurement caveat

In Auto RX, the UI selects the EVM estimate only from the profile which has
produced a validated image packet. It does not take the first candidate that
merely acquired the shared training sequence.

The displayed M-ary value is **EVM-SNR**, measured from synchronized symbol
samples. It is not RF-calibrated SNR, Eb/N0, or full-band 48 kHz audio SNR. In
the existing synthetic 12 dB full-band AWGN test, observed EVM-SNR values were
20.5 dB (QPSK), 17.7 dB (8PSK) and 16.7 dB (16QAM). This difference is due in
part to the symbol-decision aperture processing gain; these values must not be
presented as calibrated full-band audio SNR. The estimator holds the latest
value for no more than two seconds across an SDF3 guard/reacquisition interval;
it then returns to unavailable. Calibration to a specific receiver's IF/audio
bandwidth, AGC and filtering remains a separate task.
