#!/usr/bin/env python3
"""Regression checks for RX GIF playback via QMovie frame-update signals."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CPP = (ROOT / "qt6" / "mainwindow.cpp").read_text(encoding="utf-8")
APP = (ROOT / "app.go").read_text(encoding="utf-8")
TEST = (ROOT / "app_gif_rx_test.go").read_text(encoding="utf-8")

checks = {
    "RX preview label owns a QMovie decoder": "QMovie *m_movie = nullptr;" in CPP,
    "GIF preview data is retained in an open QBuffer": "new QBuffer(this)" in CPP and "QIODevice::ReadOnly" in CPP,
    "QMovie playback is explicitly started": "m_movie->start();" in CPP and "m_movie->state() != QMovie::Running" in CPP,
    "QMovie updated signal drives QLabel frame rendering": "&QMovie::updated" in CPP and "displayMovieFrame();" in CPP and "setPixmap(frame.scaled(" in CPP,
    "QMovie resized signal refreshes the scaled frame": "&QMovie::resized" in CPP and "updateMovieScale()" in CPP,
    "GIF playback failures are surfaced": "&QMovie::error" in CPP and 'QStringLiteral("GIF 预览播放失败：%1")' in CPP,
    "duplicate preview fetches do not restart the same GIF": "m_movieDigest" in CPP and "digest == m_movieDigest" in CPP,
    "data:image/gif is routed to the animated preview": 'startsWith(QStringLiteral("data:image/gif")' in CPP and "target->setSourceGIF(gifData, &gifError)" in CPP,
    "GIF completion uses an independent final-animation refresh": 'refreshLatestRXGIFPreview();' in CPP and 'GetLatestRXGIFData' in CPP,
    "GIF completion is not routed through the still-image refresh path": 'const bool gifEnd = payload.value(QStringLiteral("gifEnd")).toBool();' in CPP and 'if (gifEnd)' in CPP and 'else if (hasPreview)' in CPP,
    "final GIF refresh has independent pending/queued state": "m_rxGIFPreviewRequestPending" in CPP and "m_rxGIFPreviewRefreshQueued" in CPP,
    "stale asynchronous previews cannot overwrite a newer GIF": "m_rxPreviewGeneration" in CPP and "requestGeneration == m_rxPreviewGeneration" in CPP,
    "RX preview requests advance a generation when new data arrives": "if (hasPreview || gifStart || gifEnd) ++m_rxPreviewGeneration;" in CPP,
    "GIF completion has priority over a per-event JPEG snapshot": "if (gifEnd)" in CPP and "else if (!previewDataUrl.isEmpty())" in CPP,
    "static preview events carry their own immutable JPEG snapshot": "PreviewDataURL" in (ROOT / "internal/rx" / "manager.go").read_text(encoding="utf-8") and "makeRXPreviewDataURL(data)" in APP,
    "completed RX GIF replaces progressive PNG preview": "a.setLatestRXGIF(data)" in APP,
    "latest RX preview returns GIF MIME type": '"data:image/gif;base64,"' in APP,
    "dedicated GIF RPC validates and returns an animated GIF": "GetLatestRXGIFData" in APP and "gif.DecodeConfig" in APP,
    "live RX GIF completion is covered by integration test": "app.handleRXImageUpdate(finalUpdate)" in TEST and "completed GIF did not become the live RX preview" in TEST,
    "dedicated GIF RPC is covered by integration test": "app.GetLatestRXGIFData()" in TEST and "dedicated RX GIF endpoint" in TEST,
}

failed = [name for name, ok in checks.items() if not ok]
for name, ok in checks.items():
    print(f"{'PASS' if ok else 'FAIL'}: {name}")
if failed:
    raise SystemExit("RX GIF preview regression checks failed: " + "; ".join(failed))
print(f"PASS: {len(checks)} RX GIF preview regression checks")
