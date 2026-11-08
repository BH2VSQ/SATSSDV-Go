#!/usr/bin/env python3
"""Static regression checks for M-ary soft FEC and packet erasure recovery."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FILES = {
    "mary": (ROOT / "internal/modem/mary/modem.go").read_text(encoding="utf-8"),
    "fec": (ROOT / "internal/fec/conv.go").read_text(encoding="utf-8"),
    "burst": (ROOT / "internal/fec/burst.go").read_text(encoding="utf-8"),
    "tx": (ROOT / "internal/transport/stream_encoder.go").read_text(encoding="utf-8"),
    "rx": (ROOT / "internal/transport/stream.go").read_text(encoding="utf-8"),
    "tests": (ROOT / "internal/transport/soft_fec_integration_test.go").read_text(encoding="utf-8"),
    "docs": (ROOT / "docs/SOFT_FEC_PACKET_RECOVERY.md").read_text(encoding="utf-8"),
}
checks = {
    "M-ary exposes soft bit LLRs": "func (d *StreamDemodulator) FeedSoft" in FILES["mary"] and "minOne - minZero" in FILES["mary"],
    "legacy hard-decision API wraps soft demapper": "llrs := d.FeedSoft(samples)" in FILES["mary"],
    "K=7 rate-1/2 convolutional encoder": "ConvPolyA" in FILES["fec"] and "ConvPolyB" in FILES["fec"] and "ConvMemory" in FILES["fec"],
    "soft-decision Viterbi consumes LLR": "func (d *ViterbiDecoder) Feed(llrs []float32)" in FILES["fec"],
    "64-byte block interleaver": "InterleaveBlockBytes" in FILES["fec"] and "DeinterleaveSoftBlock" in FILES["fec"],
    "SDF2 descriptor has repeated header and CRC": "RepeatBurstHeader" in FILES["burst"] and "ParseBurstHeader" in FILES["burst"] and "CRC16" in FILES["burst"],
    "TX uses inner FEC for M-ary stream": "NewConvStreamEncoder" in FILES["tx"] and "NewBlockInterleaver" in FILES["tx"],
    "TX inserts SPF2 8+2 packet parity": "BuildPacketParityPayloads" in FILES["tx"] and "packetParityMagic = [4]byte{'S', 'P', 'F', '2'}" in FILES["burst"],
    "RX uses soft demapping and Viterbi": "FeedSoft(native)" in FILES["rx"] and "NewViterbiDecoder" in FILES["rx"],
    "RX restores erased packets and rechecks SSDV": "RecoverPacketParity" in FILES["rx"] and "parseImagePacket" in FILES["rx"],
    "recovered packets are marked": "packet.FECRecovered = true" in FILES["rx"],
    "tests cover all constellations and both audio rates": "SSDVQPSK1600" in FILES["tests"] and "SSDV8PSK1600" in FILES["tests"] and "SSDV16QAM1600" in FILES["tests"] and "44100, 48000" in FILES["tests"],
    "tests cover AWGN and packet erasures": "RoundTripWithAWGN" in FILES["tests"] and "PacketErasureRecoveryThroughCCSDSFrameParser" in FILES["tests"],
    "docs state punctured SDF3 backward-compatibility limit": "predating the punctured SDF3 profiles" in FILES["docs"] and "balanced on-air coding profiles" in FILES["docs"],
    "docs avoid claiming simulated SNR is a real RF threshold": "do not define real-radio SNR thresholds" in FILES["docs"],
}
failed = [name for name, ok in checks.items() if not ok]
for name, ok in checks.items():
    print(("PASS: " if ok else "FAIL: ") + name)
if failed:
    raise SystemExit(f"{len(failed)} soft-FEC checks failed")
print(f"PASS: {len(checks)} soft-FEC regression checks")
