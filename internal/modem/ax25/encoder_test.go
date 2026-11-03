package ax25

import (
	"bytes"
	"math"
	"testing"
)

func TestAX25FrameAddressesAndFCS(t *testing.T) {
	f, e := BuildUIFrame(Address{"N0CALL", 1}, Address{"CQ", 0}, nil, []byte("SSDV"))
	if e != nil {
		t.Fatal(e)
	}
	if len(f) != 18+4 {
		t.Fatalf("frame length=%d", len(f))
	}
	if f[14] != UIControl || f[15] != NoLayer3PID {
		t.Fatalf("control/PID=%x %x", f[14], f[15])
	}
	if got := CRC16X25(f[:len(f)-2]); !bytes.Equal(f[len(f)-2:], []byte{byte(got), byte(got >> 8)}) {
		t.Fatal("FCS does not match")
	}
}
func TestBitStuffingAddsZeroAfterFiveOnes(t *testing.T) {
	bits := HDLCBits([]byte{0xF8}, 1)
	found := false
	for i := 0; i+5 < len(bits); i++ {
		if bits[i] && bits[i+1] && bits[i+2] && bits[i+3] && bits[i+4] && !bits[i+5] {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing stuffed zero")
	}
}
func TestHDLCBurstUsesARCSSD500msPreambleAndTrailer(t *testing.T) {
	information := []byte{0x01, 0x7e, 0xf8, 0xff}
	frame, err := BuildUIFrame(Address{Callsign: "N0CALL", SSID: 1}, Address{Callsign: "CQ"}, nil, information)
	if err != nil {
		t.Fatal(err)
	}
	bits := HDLCBurstBits([][]byte{frame}, DefaultBurstPreambleFlags, DefaultBurstTrailerFlags)
	wantLength := (DefaultBurstPreambleFlags+DefaultBurstTrailerFlags)*8 + len(HDLCBits(frame, 1))
	if len(bits) != wantLength {
		t.Fatalf("burst bits=%d want=%d", len(bits), wantLength)
	}
	for region, start := range map[string]int{"preamble": 0, "trailer": len(bits) - DefaultBurstTrailerFlags*8} {
		for i := 0; i < DefaultBurstPreambleFlags*8; i++ {
			at := start + i
			want := (Flag>>uint(i%8))&1 != 0
			if region == "trailer" && i >= DefaultBurstTrailerFlags*8 {
				break
			}
			if bits[at] != want {
				t.Fatalf("%s flag bit %d mismatch", region, i)
			}
		}
	}
	decoded, err := DecodeHDLCFrames(bits)
	if err != nil || len(decoded) != 1 || !bytes.Equal(decoded[0].Information, information) {
		t.Fatalf("burst did not preserve HDLC UI frame: frames=%d err=%v", len(decoded), err)
	}
}

func TestModulatorLength(t *testing.T) {
	out, e := ModulateAFSK([]bool{true, false}, 48000, 1200, 1200, 2200)
	if e != nil {
		t.Fatal(e)
	}
	if len(out) != 80 {
		t.Fatalf("samples=%d", len(out))
	}
}

func TestHDLCDecodeRoundTrip(t *testing.T) {
	source := Address{Callsign: "N0CALL", SSID: 1}
	destination := Address{Callsign: "CQ"}
	info := []byte{0x55, 0x66, 0x00, 0xff, 0x7e, 0xf8}
	frame, err := BuildUIFrame(source, destination, nil, info)
	if err != nil {
		t.Fatal(err)
	}
	bits := HDLCBits(frame, 20)
	decoded, err := DecodeHDLCFrames(bits)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 {
		t.Fatalf("got %d frames", len(decoded))
	}
	got := decoded[0]
	if got.Source.Callsign != source.Callsign || got.Source.SSID != source.SSID || got.Destination.Callsign != destination.Callsign {
		t.Fatalf("address mismatch: %+v", got)
	}
	if string(got.Information) != string(info) {
		t.Fatalf("information mismatch %x", got.Information)
	}
	levels := NRZI(bits, true)
	back := NRZIDecode(levels, true)
	for i := range bits {
		if bits[i] != back[i] {
			t.Fatalf("NRZI mismatch at %d", i)
		}
	}
}

func TestARCSSDAFSK1200WaveformMatchesReferenceDDS(t *testing.T) {
	wave, err := ModulateAFSK([]bool{true, false}, 19200, 1200, 1200, 2200)
	if err != nil {
		t.Fatal(err)
	}
	if len(wave) != 32 {
		t.Fatalf("samples=%d want=32", len(wave))
	}
	mark := []float32{0.35035713, 0.64737556, 0.84583693, 0.9155273, 0.84583693, 0.64737556, 0.35035713, 0}
	space := []float32{0.60223999, 0.90719841, 0.76433975, 0.24418281, -0.39650921, -0.84147382, -0.87106474, -0.4706751}
	for i, want := range mark {
		if diff := math.Abs(float64(wave[i] - want)); diff > 2e-6 {
			t.Fatalf("mark sample %d=%g want=%g", i, wave[i], want)
		}
	}
	for i, want := range space {
		if diff := math.Abs(float64(wave[16+i] - want)); diff > 2e-6 {
			t.Fatalf("space sample %d=%g want=%g", i, wave[16+i], want)
		}
	}
}
