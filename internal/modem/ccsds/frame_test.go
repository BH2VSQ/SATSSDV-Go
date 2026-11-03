package ccsds

import (
	"bytes"
	"math"
	"testing"
)

func TestRandomizerMatchesARCSSDReferencePrefix(t *testing.T) {
	s := RandomizerSequence()
	want := []byte{0xff, 0x48, 0x0e, 0xc0, 0x9a, 0x0d, 0x70, 0xbc}
	if !bytes.Equal(s[:len(want)], want) {
		t.Fatalf("sequence prefix % x", s[:len(want)])
	}
}
func TestFrameRoundTrip(t *testing.T) {
	p := make([]byte, PayloadLength)
	for i := range p {
		p[i] = byte(i)
	}
	f, e := BuildDataUnit(p)
	if e != nil {
		t.Fatal(e)
	}
	got, e := ParseDataUnit(f)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(got, p) {
		t.Fatal("payload mismatch")
	}
}
func TestModulateCCSDS2400(t *testing.T) {
	w, e := Modulate([]byte{0xaa}, 2400, 1200, 2400, 19200)
	if e != nil {
		t.Fatal(e)
	}
	if len(w) != 64 {
		t.Fatalf("samples=%d", len(w))
	}
}

func TestARCSSD2400UsesReferenceGaussianShaping(t *testing.T) {
	wave, err := Modulate([]byte{0xaa}, 2400, 1200, 2400, 19200)
	if err != nil {
		t.Fatal(err)
	}
	if len(wave) != 64 {
		t.Fatalf("samples=%d, want 64", len(wave))
	}
	want := []float32{0.50863963, 0.84583688, 0.89793569, 0.64737558, 0.17861049, -0.35035709, -0.75809783, -0.91537219}
	for i, expected := range want {
		if delta := math.Abs(float64(wave[i] - expected)); delta > 2e-6 {
			t.Fatalf("sample %d = %.8f, want %.8f (delta %.8f)", i, wave[i], expected, delta)
		}
	}
	// A pulse-shaped modulator is continuous-phase and does not jump between
	// two independent sinusoids at every symbol boundary.
	for i := 1; i < len(wave); i++ {
		if math.Abs(float64(wave[i]-wave[i-1])) > 0.8 {
			t.Fatalf("unexpected phase discontinuity at sample %d", i)
		}
	}
}

func TestAddGuardSequences(t *testing.T) {
	unit := []byte{1, 2, 3}
	guarded, err := AddGuardSequences(unit, 150, 150)
	if err != nil {
		t.Fatal(err)
	}
	if len(guarded) != 303 {
		t.Fatalf("got %d bytes", len(guarded))
	}
	seq := RandomizerSequence()
	if guarded[0] != seq[0] || guarded[149] != seq[149] || guarded[150] != 1 || guarded[152] != 3 || guarded[153] != seq[0] {
		t.Fatalf("guard sequence mismatch: %x", guarded[:8])
	}
}

func TestFindDataUnits(t *testing.T) {
	payload := make([]byte, PayloadLength)
	for i := range payload {
		payload[i] = byte(i*17 + 3)
	}
	unit, err := BuildDataUnit(payload)
	if err != nil {
		t.Fatal(err)
	}
	stream := append([]byte{0, 1, 2, 3, 4}, unit...)
	stream = append(stream, 9, 8)
	frames, err := FindDataUnits(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || string(frames[0]) != string(payload) {
		t.Fatal("CCSDS stream extraction mismatch")
	}
}

func TestFindDataUnitsInBitsUnaligned(t *testing.T) {
	payload := make([]byte, PayloadLength)
	for i := range payload {
		payload[i] = byte(i * 9)
	}
	unit, err := BuildDataUnit(payload)
	if err != nil {
		t.Fatal(err)
	}
	bits := make([]bool, 3, 3+len(unit)*8)
	for _, b := range unit {
		for i := 7; i >= 0; i-- {
			bits = append(bits, b&(1<<uint(i)) != 0)
		}
	}
	frames, err := FindDataUnitsInBits(bits)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || string(frames[0]) != string(payload) {
		t.Fatal("bit-level CCSDS extraction mismatch")
	}
}

func TestARCSSD2400ReferenceReceiverRoundTrip(t *testing.T) {
	payload := make([]byte, PayloadLength)
	for i := range payload {
		payload[i] = byte(i*37 + 11)
	}
	unit, err := BuildDataUnit(payload)
	if err != nil {
		t.Fatal(err)
	}
	burst, err := AddGuardSequences(unit, 150, 150)
	if err != nil {
		t.Fatal(err)
	}
	wave, err := Modulate(burst, 2400, 1200, 2400, 19200)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a late-opened capture stream, lower audio level and a small
	// DC/audio-path offset. The synchronization word must remain discoverable.
	shifted := make([]float32, 23, len(wave)+43)
	for _, sample := range wave {
		shifted = append(shifted, sample*0.42+0.001)
	}
	shifted = append(shifted, make([]float32, 20)...)
	wave = shifted
	demod := &ARCSSD2400Demodulator{}
	var bits []bool
	for start := 0; start < len(wave); {
		end := start + 257
		if end > len(wave) {
			end = len(wave)
		}
		part, err := demod.Feed(wave[start:end], 19200)
		if err != nil {
			t.Fatal(err)
		}
		bits = append(bits, part...)
		start = end
	}
	frames, err := FindDataUnitsInBits(bits)
	if err != nil {
		t.Fatalf("reference 2400 receiver did not recover ASM: %v (recovered %d bits)", err, len(bits))
	}
	if len(frames) != 1 || !bytes.Equal(frames[0], payload) {
		t.Fatalf("reference 2400 receiver payload mismatch: frames=%d", len(frames))
	}
}

func TestARCSSD1200WaveformMatchesReferencePhaseAccumulator(t *testing.T) {
	wave, err := Modulate([]byte{0xaa}, 1200, 1200, 2200, 19200)
	if err != nil {
		t.Fatal(err)
	}
	if len(wave) != 128 {
		t.Fatalf("samples=%d, want 128", len(wave))
	}
	want := []float32{0.35554045, 0.6552711, 0.8521426, 0.91525155, 0.8346917, 0.6231088, 0.31371522, -0.04492279}
	for i, expected := range want {
		if delta := math.Abs(float64(wave[i] - expected)); delta > 2e-6 {
			t.Fatalf("sample %d = %.8f, want %.8f (delta %.8f)", i, wave[i], expected, delta)
		}
	}
}

func TestFindDataUnitsInBitsCorrectsOneBitASMError(t *testing.T) {
	payload := make([]byte, PayloadLength)
	for i := range payload {
		payload[i] = byte(i*19 + 7)
	}
	unit, err := BuildDataUnit(payload)
	if err != nil {
		t.Fatal(err)
	}
	bits := make([]bool, 5, 5+len(unit)*8)
	for _, value := range unit {
		for bit := 7; bit >= 0; bit-- {
			bits = append(bits, value&(1<<uint(bit)) != 0)
		}
	}
	bits[5+17] = !bits[5+17]
	frames, err := FindDataUnitsInBits(bits)
	if err != nil || len(frames) != 1 || !bytes.Equal(frames[0], payload) {
		t.Fatalf("one-bit ASM error was not corrected: frames=%d err=%v", len(frames), err)
	}
	bits[5+21] = !bits[5+21]
	if frames, err := FindDataUnitsInBits(bits); err == nil || len(frames) != 0 {
		t.Fatalf("two-bit ASM error should be rejected: frames=%d err=%v", len(frames), err)
	}
}
