package ax25

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	UIControl   byte = 0x03
	NoLayer3PID byte = 0xF0
	Flag        byte = 0x7E

	// ARCSSD cfg_afsk.h configures 500 ms preamble and trailer at 1200 bit/s.
	// One HDLC flag is eight bits, so each guard contains 75 flags.
	DefaultBurstPreambleFlags = 75
	DefaultBurstTrailerFlags  = 75
)

type Address struct {
	Callsign string
	SSID     uint8
}

func BuildUIFrame(source, destination Address, digipeaters []Address, information []byte) ([]byte, error) {
	if err := validateAddress(source); err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	if err := validateAddress(destination); err != nil {
		return nil, fmt.Errorf("destination: %w", err)
	}
	if len(digipeaters) > 8 {
		return nil, errors.New("AX.25 supports at most 8 digipeater addresses in this profile")
	}
	frame := make([]byte, 0, 16+len(digipeaters)*7+len(information)+2)
	addresses := make([]Address, 0, 2+len(digipeaters))
	addresses = append(addresses, destination, source)
	addresses = append(addresses, digipeaters...)
	for i, a := range addresses {
		last := i == len(addresses)-1
		frame = append(frame, encodeAddress(a, last, i == 0)...)
	}
	frame = append(frame, UIControl, NoLayer3PID)
	frame = append(frame, information...)
	fcs := CRC16X25(frame)
	frame = append(frame, byte(fcs), byte(fcs>>8))
	return frame, nil
}
func validateAddress(a Address) error {
	call := strings.TrimSpace(strings.ToUpper(a.Callsign))
	if call == "" || len(call) > 6 {
		return errors.New("callsign must contain 1..6 characters")
	}
	for _, r := range call {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return fmt.Errorf("invalid callsign character %q", r)
		}
	}
	if a.SSID > 15 {
		return errors.New("SSID must be 0..15")
	}
	return nil
}
func encodeAddress(a Address, last, destination bool) []byte {
	call := strings.ToUpper(strings.TrimSpace(a.Callsign))
	call += strings.Repeat(" ", 6-len(call))
	out := make([]byte, 7)
	for i := 0; i < 6; i++ {
		out[i] = call[i] << 1
	}
	ssid := byte(0x60 | (a.SSID&0x0f)<<1)
	if destination {
		ssid |= 0x80
	}
	if last {
		ssid |= 1
	}
	out[6] = ssid
	return out
}
func CRC16X25(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

// HDLCBits wraps a frame in flags and performs AX.25 bit stuffing. Bits are LSB-first.
func HDLCBits(frame []byte, flagCount int) []bool {
	if flagCount < 1 {
		flagCount = 1
	}
	bits := make([]bool, 0, (len(frame)+2)*8+flagCount*8)
	appendByte := func(b byte) {
		for i := 0; i < 8; i++ {
			bits = append(bits, (b>>i)&1 != 0)
		}
	}
	for i := 0; i < flagCount; i++ {
		appendByte(Flag)
	}
	ones := 0
	for _, b := range frame {
		for i := 0; i < 8; i++ {
			v := (b>>i)&1 != 0
			bits = append(bits, v)
			if v {
				ones++
				if ones == 5 {
					bits = append(bits, false)
					ones = 0
				}
			} else {
				ones = 0
			}
		}
	}
	appendByte(Flag)
	return bits
}

// HDLCBurstBits creates one AFSK burst using the 500 ms ARCSSD preamble and
// trailer. Each UI frame has its own opening and closing flag; the guards are
// emitted once around the full image rather than once for every packet.
func HDLCBurstBits(frames [][]byte, preambleFlags, trailerFlags int) []bool {
	if preambleFlags < 0 {
		preambleFlags = 0
	}
	if trailerFlags < 0 {
		trailerFlags = 0
	}
	capacity := (preambleFlags + trailerFlags) * 8
	for _, frame := range frames {
		capacity += (len(frame)+2)*10 + 16
	}
	bits := make([]bool, 0, capacity)
	appendFlag := func() {
		for i := 0; i < 8; i++ {
			bits = append(bits, (Flag>>uint(i))&1 != 0)
		}
	}
	for i := 0; i < preambleFlags; i++ {
		appendFlag()
	}
	for _, frame := range frames {
		bits = append(bits, HDLCBits(frame, 1)...)
	}
	for i := 0; i < trailerFlags; i++ {
		appendFlag()
	}
	return bits
}

// NRZI encodes a zero as a transition and a one as no transition.
func NRZI(bits []bool, initialMark bool) []bool {
	out := make([]bool, len(bits))
	level := initialMark
	for i, b := range bits {
		if !b {
			level = !level
		}
		out[i] = level
	}
	return out
}

var arcssdAFSKSineTable = func() [1024]float64 {
	var table [1024]float64
	for i := range table {
		table[i] = math.Sin(2 * math.Pi * float64(i) / 1024)
	}
	return table
}()

// modulateARCSSD1200 reproduces the reference 1024-entry sine-table AFSK
// modulator. The integer DDS increments are 64 for 1200 Hz and 117 for the
// reference's quantized 2200 Hz tone at 19.2 kHz.
func modulateARCSSD1200(bits []bool) []float32 {
	out := make([]float32, len(bits)*16)
	phase := 0
	for i, bit := range bits {
		increment := 117
		if bit {
			increment = 64
		}
		for j := 0; j < 16; j++ {
			phase = (phase + increment) & 1023
			out[i*16+j] = float32(0.9155273 * arcssdAFSKSineTable[phase])
		}
	}
	return out
}

func ModulateAFSK(bits []bool, sampleRate, bitRate, markHz, spaceHz int) ([]float32, error) {
	if sampleRate <= 0 || bitRate <= 0 || sampleRate%bitRate != 0 {
		return nil, errors.New("sample rate must be an integer multiple of bit rate")
	}
	if markHz <= 0 || spaceHz <= 0 || markHz >= sampleRate/2 || spaceHz >= sampleRate/2 {
		return nil, errors.New("invalid AFSK tone frequencies")
	}
	if sampleRate == 19200 && bitRate == 1200 && markHz == 1200 && spaceHz == 2200 {
		return modulateARCSSD1200(bits), nil
	}
	sps := sampleRate / bitRate
	out := make([]float32, len(bits)*sps)
	phase := 0.0
	for i, b := range bits {
		freq := spaceHz
		if b {
			freq = markHz
		}
		inc := 2 * math.Pi * float64(freq) / float64(sampleRate)
		for j := 0; j < sps; j++ {
			out[i*sps+j] = float32(.9155273 * math.Sin(phase))
			phase += inc
			if phase >= 2*math.Pi {
				phase -= 2 * math.Pi
			}
		}
	}
	return out, nil
}

func EncodeUI(source, destination Address, digipeaters []Address, information []byte) ([]float32, error) {
	frame, err := BuildUIFrame(source, destination, digipeaters, information)
	if err != nil {
		return nil, err
	}
	bits := HDLCBurstBits([][]byte{frame}, DefaultBurstPreambleFlags, DefaultBurstTrailerFlags)
	nrzi := NRZI(bits, true)
	return ModulateAFSK(nrzi, 19200, 1200, 1200, 2200)
}
