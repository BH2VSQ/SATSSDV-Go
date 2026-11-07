#!/usr/bin/env python3
"""Regression checks for automatic animated GIF playback in the RX preview."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CPP = (ROOT / "qt6" / "mainwindow.cpp").read_text(encoding="utf-8")
APP = (ROOT / "app.go").read_text(encoding="utf-8")
TEST = (ROOT / "app_gif_rx_test.go").read_text(encoding="utf-8")

checks = {
    "RX preview label owns a QMovie": "QMovie *m_movie = nullptr;" in CPP,
    "GIF preview data is opened through QBuffer": "new QBuffer(this)" in CPP and "QIODevice::ReadOnly" in CPP,
    "GIF preview playback starts automatically": "setMovie(m_movie);" in CPP and "m_movie->start();" in CPP,
    "GIF playback is resized with the preview widget": "if (m_movie) updateMovieScale();" in CPP,
    "data:image/gif is routed to animated preview": 'startsWith(QStringLiteral("data:image/gif")' in CPP and "target->setSourceGIF(gifData)" in CPP,
    "GIF completion forces RX preview refresh": 'payload.value(QStringLiteral("gifEnd")).toBool()' in CPP,
    "completed RX GIF replaces progressive PNG preview": "a.setLatestRXGIF(data)" in APP,
    "latest preview returns GIF MIME type": '"data:image/gif;base64,"' in APP,
    "live RX GIF completion is covered by integration test": "app.handleRXImageUpdate(finalUpdate)" in TEST and "completed GIF did not become the live RX preview" in TEST,
}

failed = [name for name, ok in checks.items() if not ok]
for name, ok in checks.items():
    print(f"{'PASS' if ok else 'FAIL'}: {name}")
if failed:
    raise SystemExit("RX GIF preview regression checks failed: " + "; ".join(failed))
print(f"PASS: {len(checks)} RX GIF preview regression checks")
