package dsp

import (
	"errors"
	"math"
	"testing"
)

func TestStreamResamplerIsIndependentOfInputBlockBoundaries(t *testing.T) {
	input := make([]float32, 48001)
	for i := range input {
		input[i] = float32(0.6*math.Sin(2*math.Pi*1200*float64(i)/48000) + 0.2*math.Sin(2*math.Pi*2200*float64(i)/48000))
	}
	whole, err := NewStreamResampler(48000, 19200)
	if err != nil {
		t.Fatal(err)
	}
	want := whole.Feed(input)
	stream, err := NewStreamResampler(48000, 19200)
	if err != nil {
		t.Fatal(err)
	}
	var got []float32
	for offset := 0; offset < len(input); {
		end := offset + 317
		if end > len(input) {
			end = len(input)
		}
		got = append(got, stream.Feed(input[offset:end])...)
		offset = end
	}
	if len(got) != len(want) {
		t.Fatalf("stream output length=%d, one-block output=%d", len(got), len(want))
	}
	for i := range want {
		if math.Abs(float64(got[i]-want[i])) > 1e-7 {
			t.Fatalf("sample %d differs: got %.8f want %.8f", i, got[i], want[i])
		}
	}
}

func TestFSKStreamDemodulatorPreservesClockAcrossBlocks(t *testing.T) {
	bits := make([]bool, 512)
	for i := range bits {
		bits[i] = (i*17+i/7)%11 < 5
	}
	wave, err := modulateTestFSK(bits, 19200, 1200, 1200, 2200)
	if err != nil {
		t.Fatal(err)
	}
	demod, err := NewFSKStreamDemodulator(19200, 1200, 1200, 2200)
	if err != nil {
		t.Fatal(err)
	}
	var got []bool
	for offset := 0; offset < len(wave); {
		end := offset + 173
		if end > len(wave) {
			end = len(wave)
		}
		got = append(got, demod.Feed(wave[offset:end])...)
		offset = end
	}
	if len(got) < len(bits)-64 {
		t.Fatalf("only %d recovered bits from %d input bits", len(got), len(bits))
	}
	matched := false
	for offset := 0; offset <= 64 && offset+len(got) <= len(bits); offset++ {
		match := true
		for i := range got {
			if got[i] != bits[offset+i] {
				match = false
				break
			}
		}
		if match {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("stream FSK output does not align with any input suffix: got %d bits", len(got))
	}
}

func modulateTestFSK(bits []bool, sampleRate, bitRate, markHz, spaceHz int) ([]float32, error) {
	if sampleRate%bitRate != 0 {
		return nil, errors.New("sample rate must be divisible by bit rate")
	}
	sps := sampleRate / bitRate
	out := make([]float32, len(bits)*sps)
	phase := 0.0
	for i, bit := range bits {
		freq := spaceHz
		if bit {
			freq = markHz
		}
		step := 2 * math.Pi * float64(freq) / float64(sampleRate)
		for j := 0; j < sps; j++ {
			out[i*sps+j] = float32(0.75 * math.Sin(phase))
			phase += step
			if phase >= 2*math.Pi {
				phase -= 2 * math.Pi
			}
		}
	}
	return out, nil
}
