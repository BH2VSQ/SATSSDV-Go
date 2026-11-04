package dsp

import "testing"

func BenchmarkFSKAcquisitionIdleAudio(b *testing.B) {
	demod, err := NewFSKStreamDemodulator(19200, 1200, 1200, 2200)
	if err != nil {
		b.Fatal(err)
	}
	block := make([]float32, 480)
	b.ReportAllocs()
	b.SetBytes(int64(len(block) * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		demod.Feed(block)
	}
}
