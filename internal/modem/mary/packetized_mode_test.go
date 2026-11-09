package mary

import "testing"

func TestPacketizedModeDoesNotReleaseLockOnAmplitudeFade(t *testing.T) {
	demod, err := NewStreamDemodulator(Config{SampleRate: 48000, SymbolRate: 1600, BitsPerSymbol: 2, CarrierHz: 1600})
	if err != nil {
		t.Fatal(err)
	}
	demod.SetPacketizedMode(true)
	demod.locked = true
	demod.signalRMS = 0.5
	demod.phaseRotation = 1
	demod.stepRotation = 1

	// A 200 ms fade is longer than the former 100 ms silence-release timeout
	// and is below the old 0.36 packetized threshold. SDF3's declared coded
	// length, not this amplitude dip, owns the packet boundary.
	fade := make([]float32, 48000/5)
	for i := range fade {
		fade[i] = 0.12
	}
	demod.FeedSoft(fade)
	if !demod.locked {
		t.Fatal("packetized receiver released symbol lock during an in-group fade")
	}
	if demod.BurstGeneration() != 0 {
		t.Fatalf("packetized fade advanced burst generation to %d", demod.BurstGeneration())
	}
}
