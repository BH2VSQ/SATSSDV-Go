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
    "legacy audio playback borrows caller waveform": "samples: samples" in audio and "samples: append([]float32(nil), samples...)" not in audio,
    "GIF TX prepares archive before transmission": app.find("archiveGIF, prepareErr = gifmode.EncodeGIF(frames)") < app.find("duplex.TransmitStream(ctx, estimatedSamples"),
    "GIF TX releases RGBA frame references after packetization": "frames = nil" in app and "gifData, base, overlay = nil, nil, nil" in app,
    "GIF TX retains compressed packet data but not full PCM waveform": "packets = encodedPackets" in app and "encoder.StreamPackets(streamCtx, packets" in app and "waveform []float32" not in app,
    "GIF TX frees temporary heap before streaming playback": app.find("runtimedebug.FreeOSMemory()") < app.find("duplex.TransmitStream(ctx, estimatedSamples"),
    "TX archive uses pre-encoded GIF bytes": "archiveTXGIFData(archiveGIF, archiveWidth, archiveHeight" in app,
    "RX completed GIF releases PNG snapshots": "ReleaseGIFExport(update.SessionID)" in app and "stream.frames = nil" in rx and "stream.delays = nil" in rx,
    "stream PCM queue is fixed at two seconds": "ringCapacity := max(sampleRate*2, 8192)" in audio,
    "audio producer blocks on bounded PCM ring": "ring.Write(producerCtx, block)" in audio and "case <-r.space" in (ROOT / "internal" / "audio" / "stream_ring.go").read_text(encoding="utf-8"),
    "PCM prefill completes before Duplex asserts PTT": (lambda d: 0 <= d.find("playback.PrepareStream") < d.find("d.ptt.SetPTT(txCtx, true)"))(section((ROOT / "internal" / "audio" / "duplex.go").read_text(encoding="utf-8"), "func (d *Duplex) TransmitStream", "func (d *Duplex)")) and "Keep at least 250 ms queued before PTT" in audio,
    "audio underrun aborts instead of inserting silent gaps": "ErrPCMUnderrun" in audio and "Never insert silent gaps" in audio,
    "running audio device is gated until PTT lead expires": "playbackStarted" in audio and "if !job.playbackStarted" in audio,
    "TX packet modulation is streamed in bounded blocks": "streamPCMChunkSamples = 4096" in (ROOT / "internal" / "transport" / "stream_encoder.go").read_text(encoding="utf-8"),
    "stream resampler preserves batch output count": "func (r *StreamResampler) Flush()" in (ROOT / "internal" / "dsp" / "resampler_stream.go").read_text(encoding="utf-8"),
}
failed = [name for name, ok in checks.items() if not ok]
for name, ok in checks.items():
    print(("PASS" if ok else "FAIL") + ": " + name)
if failed:
    raise SystemExit("failed checks: " + ", ".join(failed))
