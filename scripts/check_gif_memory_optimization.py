#!/usr/bin/env python3
"""Static regression checks for the first GIF memory optimization pass."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
qt = (ROOT / "qt6" / "mainwindow.cpp").read_text(encoding="utf-8")
audio = (ROOT / "internal" / "audio" / "runtime_malgo.go").read_text(encoding="utf-8")
app = (ROOT / "app.go").read_text(encoding="utf-8")
gif_tx = (ROOT / "app_gif_tx.go").read_text(encoding="utf-8")
rx = (ROOT / "internal" / "rx" / "gif.go").read_text(encoding="utf-8")
def section(source: str, start: str, end: str) -> str:
    a = source.find(start)
    if a < 0:
        raise AssertionError(f"missing section start: {start}")
    b = source.find(end, a + len(start))
    if b < 0:
        raise AssertionError(f"missing section end: {end}")
    return source[a:b]

gif_send = section(gif_tx, "func (s *gifTransmissionSession) send(", "func (s *gifTransmissionSession) prepare(")
gif_prepare = gif_tx[gif_tx.find("func (s *gifTransmissionSession) prepare("):]

checks = {
    "RX GIF decoder does not cache every frame": "QMovie::CacheNone" in qt and "QMovie::CacheAll" not in qt,
    "legacy audio playback borrows caller waveform": "samples: samples" in audio and "samples: append([]float32(nil), samples...)" not in audio,
    "GIF TX prepares archive before transmission": "s.archiveGIF, err = gifmode.EncodeGIF(frames)" in gif_prepare and gif_send.find("s.prepare(ctx)") < gif_send.find("s.duplex.TransmitStreamWithStart(ctx, s.estimatedSamples"),
    "GIF TX releases RGBA frame references after packetization": "frames = nil" in gif_tx and "s.gifData, s.base, s.overlay = nil, nil, nil" in gif_tx,
    "GIF TX retains compressed packet data but not full PCM waveform": "s.packets, s.stats, err = gifmode.EncodeContextV2" in gif_tx and "s.encoder.StreamPackets(streamCtx, s.packets" in gif_tx and "waveform []float32" not in gif_tx,
    "GIF TX frees temporary heap before streaming playback": "debug.FreeOSMemory()" in gif_prepare and gif_send.find("s.prepare(ctx)") < gif_send.find("s.duplex.TransmitStreamWithStart(ctx, s.estimatedSamples"),
    "TX archive uses pre-encoded GIF bytes": "archiveTXGIFData(s.archiveGIF, s.archiveWidth, s.archiveHeight" in gif_tx,
    "RX completed GIF releases PNG snapshots": "ReleaseGIFExport(update.SessionID)" in app and "stream.frames = nil" in rx and "stream.delays = nil" in rx,
    "stream PCM queue is fixed at two seconds": "ringCapacity := max(sampleRate*2, 8192)" in audio,
    "audio producer blocks on bounded PCM ring": "ring.Write(producerCtx, block)" in audio and "case <-r.space" in (ROOT / "internal" / "audio" / "stream_ring.go").read_text(encoding="utf-8"),
    "PCM prefill completes before Duplex asserts PTT": (lambda d: 0 <= d.find("playback.PrepareStream") < d.find("d.ptt.SetPTT(txCtx, true)") < d.find("onPlaybackStart()"))(section((ROOT / "internal" / "audio" / "duplex.go").read_text(encoding="utf-8"), "func (d *Duplex) TransmitStreamWithStart", "func (d *Duplex) StopTX")) and "Keep at least 250 ms queued before PTT" in audio,
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
