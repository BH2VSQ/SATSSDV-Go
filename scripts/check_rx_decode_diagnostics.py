#!/usr/bin/env python3
"""Static guard for RX decoder liveness diagnostics and panic containment."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
app = (ROOT / "app.go").read_text(encoding="utf-8")
manager = (ROOT / "internal/rx/manager.go").read_text(encoding="utf-8")
transport = (ROOT / "internal/transport/stream.go").read_text(encoding="utf-8")
auto = (ROOT / "internal/transport/auto.go").read_text(encoding="utf-8")
modem = (ROOT / "internal/modem/mary/modem.go").read_text(encoding="utf-8")
qt = (ROOT / "qt6/mainwindow.cpp").read_text(encoding="utf-8")
header = (ROOT / "qt6/mainwindow.h").read_text(encoding="utf-8")
fec = (ROOT / "internal/fec/conv.go").read_text(encoding="utf-8")
fec_test = (ROOT / "internal/fec/conv_test.go").read_text(encoding="utf-8")

checks = {
    "RX decoder Feed is panic-contained": "func safeFeedRXDecoder" in app and "recover()" in app,
    "decoder is reset after a panic": "resetter.Reset()" in app,
    "panic count and stack are emitted": "decoderPanics" in app and '"stack": panicStack' in app,
    "audio callback blocks and samples are counted": "audioBlocks" in app and "audioSamples" in app,
    "capture discontinuities and queue drops are counted": "inputDiscontinuities" in app and "queueDrops" in app,
    "decoder block and packet output is counted": "decoderBlocks" in app and "decoderPackets" in app,
    "RX diagnostics are emitted periodically": "time.NewTicker(time.Second)" in app and 'a.emit("rx:diagnostics"' in app,
    "transport diagnostics include modem lock and SDF3 FEC": "MARYLocked" in transport and "FECChunksBad" in transport and "MARYReacquireCount" in transport,
    "automatic profile exposes M-ary candidates": "func (d *AutoStreamDecoder) Diagnostics() AutoDecoderDiagnostics" in auto,
    "GIF manager exposes queue and frame decode progress": "func (m *Manager) Diagnostics() ManagerDiagnostics" in manager and "DecodedMCUs" in manager and "LastDecodeAgeMS" in manager,
    "Qt has dedicated RX diagnostics line": "m_rxDiagnostics" in header and 'name==QStringLiteral("rx:diagnostics")' in qt,
    "Qt surfaces recovered decoder errors": 'name==QStringLiteral("rx:decoder-error")' in qt,
    "single-shard RS recovery branches before two-shard residuals": "if len(missing) == 1" in fec and fec.index("if len(missing) == 1") < fec.index("residual1 := append([]byte(nil), parity1...)"),
    "P0-only parity recovery regression exists": "TestPacketParityCanRecoverOneLossFromFirstParityShard" in fec_test,
    "SDF3 holds symbol lock through in-group fades and rearms on a declared group boundary": "SetPacketizedMode" in modem and "if !d.packetizedMode && d.signalRMS > 0" in modem and "if d.maryFECFinished" in transport and "d.mary.Rearm()" in transport and "maryFECRearmPending = true" in transport and "TestSDF3MARYReacquiresAcrossNoisyGroupGuards" in (ROOT / "internal/transport/soft_fec_integration_test.go").read_text(encoding="utf-8"),
}
failed = [name for name, ok in checks.items() if not ok]
for name, ok in checks.items():
    print(("PASS" if ok else "FAIL") + ": " + name)
if failed:
    raise SystemExit(1)
print(f"PASS: {len(checks)} RX liveness diagnostics checks")
