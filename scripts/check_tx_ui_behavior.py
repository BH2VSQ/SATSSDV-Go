#!/usr/bin/env python3
"""Static regression checks for the SATSSDV-Go TX/UI repair."""
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
FILES = {
    "cpp": ROOT / "qt6" / "mainwindow.cpp",
    "header": ROOT / "qt6" / "mainwindow.h",
    "canvas": ROOT / "qt6" / "template_canvas.cpp",
    "app": ROOT / "app.go",
}
for key, path in FILES.items():
    if not path.is_file():
        print(f"FAIL: missing {path.relative_to(ROOT)}", file=sys.stderr)
        raise SystemExit(2)
text = {key: path.read_text(encoding="utf-8") for key, path in FILES.items()}
checks = {
    "countdown state declared": "bool m_txCountdownActive = false;" in text["header"],
    "repeat checkbox locked outside countdown while TX active": "const bool canToggleRepeat = !m_txPreparing && (!m_txActive || m_txCountdownActive);" in text["cpp"],
    "countdown state machine": "m_txCountdownActive = true;" in text["cpp"] and "m_txCountdownActive = false;" in text["cpp"],
    "cancel loop via StopTX during countdown": "cancelRepeatCountdown" in text["cpp"] and 'QStringLiteral("StopTX")' in text["cpp"],
    "input preserves base interval": "m_repeatIntervalSeconds" in text["cpp"] and "const int repeatInterval = m_repeat->isChecked() ? m_repeatIntervalSeconds" in text["cpp"],
    "repeat activity survives idle countdown": "txReserved := a.txReserved" in text["app"] and "s.TXActive || txReserved" in text["app"],
    "1500 Hz tone uses stronger output level": "gain := 0.70" in text["app"],
    "tone timeout includes PTT/output margin": "context.WithTimeout(parent, 75*time.Second)" in text["app"],
    "test tone remains exactly 1500 Hz": "const frequency = 1500.0" in text["app"],
    "repeat interval cancellation restores the user value": "m_repeatInterval->setValue(m_repeatIntervalSeconds);" in text["cpp"] and "cancelRepeatCountdown" in text["cpp"],
    "text hint is borderless": 'background:transparent; border:0;' in text["cpp"],
    "horizontal slider has no frame": "QSlider { border:0; background:transparent; padding:0; }" in text["cpp"] and "QSlider::groove:horizontal" in text["cpp"],
    "canvas no longer displays drag/resize caption": "拖动以移动" not in text["canvas"] and "右下角调整大小" not in text["canvas"],
}
failed = [name for name, ok in checks.items() if not ok]
if failed:
    print("FAIL: " + "; ".join(failed), file=sys.stderr)
    raise SystemExit(1)
for name in checks:
    print(f"PASS: {name}")
print(f"PASS: {len(checks)} TX/UI behavior regression checks")
