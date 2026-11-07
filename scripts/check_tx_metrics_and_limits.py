#!/usr/bin/env python3
"""Static regression checks for TX timing metrics and GIF airtime policy."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
app = (ROOT / "app.go").read_text(encoding="utf-8")
cpp = (ROOT / "qt6" / "mainwindow.cpp").read_text(encoding="utf-8")
header = (ROOT / "qt6" / "mainwindow.h").read_text(encoding="utf-8")
duplex = (ROOT / "internal" / "audio" / "duplex.go").read_text(encoding="utf-8")
docs = (ROOT / "docs" / "GIF_TX_MODE.md").read_text(encoding="utf-8")
checks = {
    "bottom protocol development warning is removed": "协议编解码仍在开发验证中" not in cpp,
    "GIF-specific ten-minute hard cap is removed": "airtime > 10*time.Minute" not in app and "safety limit is 10 minutes" not in app,
    "GIF docs no longer advertise ten-minute limit": "at most 10 minutes per burst" not in docs,
    "stream has real playback-start lifecycle callback": "TransmitStreamWithStart" in duplex and "onPlaybackStart()" in duplex,
    "TX emits estimated elapsed and remaining time": all(k in app for k in ("estimatedSeconds", "elapsedSeconds", "remainingSeconds")),
    "TX emits total and sent data frame counts": all(k in app for k in ("totalDataFrames", "sentDataFrames")),
    "GIF counter includes each full transport frame": "totalDataFrames := stats.TotalPackets" in app,
    "static image counter uses packet count": "totalDataFrames := len(prepared.Packets)" in app,
    "Qt has dedicated metrics label": "QLabel *m_txMetrics = nullptr;" in header and 'setObjectName(QStringLiteral("txMetrics"))' in cpp,
    "Qt displays all requested TX metrics": all(k in cpp for k in ("预计 %1", "已发射 %2", "剩余 %3", "数据帧 %4 / %5")),
    "Qt formats hours without 24-hour wrap": "const qint64 hours = seconds / 3600;" in cpp,
}
failed = [name for name, ok in checks.items() if not ok]
for name, ok in checks.items():
    print(("PASS: " if ok else "FAIL: ") + name)
if failed:
    raise SystemExit(1)
print(f"PASS: {len(checks)} TX metrics/limit checks")
