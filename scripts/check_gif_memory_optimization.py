#!/usr/bin/env python3
"""Static regression checks for the first GIF memory optimization pass."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
qt = (ROOT / "qt6" / "mainwindow.cpp").read_text(encoding="utf-8")
audio = (ROOT / "internal" / "audio" / "runtime_malgo.go").read_text(encoding="utf-8")
app = (ROOT / "app.go").read_text(encoding="utf-8")
rx = (ROOT / "internal" / "rx" / "gif.go").read_text(encoding="utf-8")

def section(source: str, start: str, end: str) -> str:
    a = source.find(start)
    if a < 0:
        raise AssertionError(f"missing section start: {start}")
    b = source.find(end, a + len(start))
    if b < 0:
        raise AssertionError(f"missing section end: {end}")
    return source[a:b]

checks = {
    "RX GIF decoder does not cache every frame": "QMovie::CacheNone" in qt and "QMovie::CacheAll" not in qt,
    "audio playback borrows caller waveform": "samples: samples" in audio and "samples: append([]float32(nil), samples...)" not in audio,
    "GIF TX prepares archive before transmission": app.find("archiveGIF, prepareErr = gifmode.EncodeGIF(frames)") < app.find("duplex.Transmit(ctx, waveform"),
    "GIF TX releases RGBA frame references after packetization": "frames = nil" in app and "gifData, base, overlay = nil, nil, nil" in app,
    "GIF TX releases packet references after modulation": "packets = nil" in app,
    "GIF TX frees temporary heap before playback": app.find("runtimedebug.FreeOSMemory()") < app.find("duplex.Transmit(ctx, waveform"),
    "TX archive uses pre-encoded GIF bytes": "archiveTXGIFData(archiveGIF, archiveWidth, archiveHeight" in app,
    "RX completed GIF releases PNG snapshots": "ReleaseGIFExport(update.SessionID)" in app and "stream.frames = nil" in rx and "stream.delays = nil" in rx,
}
failed = [name for name, ok in checks.items() if not ok]
for name, ok in checks.items():
    print(("PASS" if ok else "FAIL") + ": " + name)
if failed:
    raise SystemExit("failed checks: " + ", ".join(failed))
