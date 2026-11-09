package fec

import (
	"bytes"
	"testing"
)

func TestRS8EncodeProducesValidCodeword(t *testing.T) {
	data := make([]byte, 223)
	for i := range data {
		data[i] = byte(i*17 + 3)
	}
	p, e := RS8Parity(data)
	if e != nil {
		t.Fatal(e)
	}
	code := append(append([]byte(nil), data...), p...)
	if !ValidateRS8(code) {
		t.Fatal("generated RS8 codeword did not validate")
	}
}
func TestRS8ParityMatchesARCSSDReferenceVector(t *testing.T) {
	data := make([]byte, RS8DataSymbols)
	for i := range data {
		data[i] = byte((i*37 + 11) & 0xff)
	}
	parity, err := RS8Parity(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0xad, 0x18, 0x12, 0x37, 0x72, 0xca, 0xc8, 0xe0,
		0xa8, 0x14, 0x58, 0xb1, 0xcd, 0xbe, 0x0c, 0x41,
		0xba, 0x95, 0xe2, 0x3e, 0x54, 0x31, 0xbd, 0xc3,
		0xf1, 0x58, 0x13, 0x5b, 0xb1, 0xd7, 0x78, 0xaa,
	}
	if !bytes.Equal(parity, want) {
		t.Fatalf("RS8 parity differs from ARCSSD C encoder: got %x want %x", parity, want)
	}
}

func TestRS8RejectsCorruptedCodeword(t *testing.T) {
	data := make([]byte, 223)
	p, _ := RS8Parity(data)
	code := append(append([]byte(nil), data...), p...)
	if !ValidateRS8(code) {
		t.Fatal("expected valid codeword")
	}
	code[10] ^= 0x80
	if ValidateRS8(code) {
		t.Fatal("corrupt codeword passed")
	}
}
func TestRS8ParityDeterministic(t *testing.T) {
	a := make([]byte, 223)
	b := make([]byte, 223)
	p1, _ := RS8Parity(a)
	p2, _ := RS8Parity(b)
	if !bytes.Equal(p1, p2) {
		t.Fatal("parity not deterministic")
	}
}

func TestRS8CorrectsUpTo16SymbolErrors(t *testing.T) {
	data := make([]byte, RS8DataSymbols)
	for i := range data {
		data[i] = byte((i*73 + i/7 + 19) & 0xff)
	}
	parity, err := RS8Parity(data)
	if err != nil {
		t.Fatal(err)
	}
	original := append(append([]byte(nil), data...), parity...)
	for _, count := range []int{1, 2, 8, 16} {
		corrupt := append([]byte(nil), original...)
		for i := 0; i < count; i++ {
			pos := (i*31 + 17) % len(corrupt)
			corrupt[pos] ^= byte(0x5b + i*7)
			if corrupt[pos] == original[pos] {
				corrupt[pos] ^= 0x80
			}
		}
		fixed, err := CorrectRS8(corrupt)
		if err != nil {
			t.Fatalf("count=%d: %v", count, err)
		}
		if fixed != count {
			t.Fatalf("count=%d corrected=%d", count, fixed)
		}
		if !bytes.Equal(corrupt, original) {
			t.Fatalf("count=%d did not restore original codeword", count)
		}
	}
}

func TestRS8CorrectionDoesNotMutateUncorrectableInput(t *testing.T) {
	data := make([]byte, RS8DataSymbols)
	parity, err := RS8Parity(data)
	if err != nil {
		t.Fatal(err)
	}
	code := append(append([]byte(nil), data...), parity...)
	for i := 0; i < 17; i++ {
		code[i*13] ^= byte(i + 1)
	}
	before := append([]byte(nil), code...)
	if _, err := CorrectRS8(code); err == nil {
		t.Fatal("expected 17-symbol corruption to be uncorrectable")
	}
	if !bytes.Equal(code, before) {
		t.Fatal("uncorrectable correction attempt modified the input")
	}
}

func TestGFDivRejectsZeroDenominatorWithoutPanic(t *testing.T) {
	if _, err := gfDiv(1, 0); err == nil {
		t.Fatal("GF(256) division by zero must return an error")
	}
	if got, err := gfDiv(0, 7); err != nil || got != 0 {
		t.Fatalf("0 divided by non-zero must be zero: got=%d err=%v", got, err)
	}
}
