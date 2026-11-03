package ax25

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

type UIFrame struct {
	Destination Address
	Source      Address
	Digipeaters []Address
	Information []byte
}

var hdlcFlagBits = []bool{false, true, true, true, true, true, true, false}

// DecodeHDLCFrames extracts AX.25 frames from an LSB-first, already NRZI-decoded
// bit stream. It performs bit unstuffing, FCS validation and UI-frame parsing.
func DecodeHDLCFrames(bits []bool) ([]UIFrame, error) {
	var frames []UIFrame
	var start int = -1
	for i := 0; i+8 <= len(bits); i++ {
		if equalFlag(bits[i : i+8]) {
			if start >= 0 && i > start+8 {
				frameBits := bits[start+8 : i]
				if raw, err := unstuff(frameBits); err == nil && len(raw) >= 18 {
					frame, parseErr := parseUIFrame(raw)
					if parseErr == nil {
						frames = append(frames, frame)
					}
				}
			}
			start = i
			i += 7
		}
	}
	if len(frames) == 0 {
		return nil, errors.New("no valid AX.25 UI frames found")
	}
	return frames, nil
}

func equalFlag(bits []bool) bool {
	if len(bits) != 8 {
		return false
	}
	for i := range hdlcFlagBits {
		if bits[i] != hdlcFlagBits[i] {
			return false
		}
	}
	return true
}

func unstuff(bits []bool) ([]byte, error) {
	clean := make([]bool, 0, len(bits))
	ones := 0
	for i := 0; i < len(bits); i++ {
		b := bits[i]
		clean = append(clean, b)
		if b {
			ones++
			if ones == 5 {
				if i+1 >= len(bits) || bits[i+1] {
					return nil, errors.New("AX.25 bit-stuffing violation")
				}
				i++
				ones = 0
			}
		} else {
			ones = 0
		}
	}
	if len(clean)%8 != 0 {
		return nil, errors.New("AX.25 frame is not byte aligned")
	}
	out := make([]byte, len(clean)/8)
	for i, b := range clean {
		if b {
			out[i/8] |= 1 << uint(i%8)
		}
	}
	return out, nil
}

func parseUIFrame(raw []byte) (UIFrame, error) {
	var out UIFrame
	if len(raw) < 18 {
		return out, errors.New("AX.25 frame too short")
	}
	fcsAt := len(raw) - 2
	got := binary.LittleEndian.Uint16(raw[fcsAt:])
	want := CRC16X25(raw[:fcsAt])
	if got != want {
		return out, fmt.Errorf("AX.25 FCS mismatch: got %04x want %04x", got, want)
	}
	addressEnd := -1
	var addresses []Address
	for off := 0; off+7 <= fcsAt; off += 7 {
		a := raw[off : off+7]
		callBytes := make([]byte, 6)
		for i := 0; i < 6; i++ {
			callBytes[i] = a[i] >> 1
		}
		call := strings.TrimRight(string(callBytes), " ")
		addresses = append(addresses, Address{Callsign: call, SSID: (a[6] >> 1) & 0x0f})
		if a[6]&1 != 0 {
			addressEnd = off + 7
			break
		}
	}
	if addressEnd < 14 || len(addresses) < 2 || addressEnd+2 > fcsAt {
		return out, errors.New("AX.25 address chain is invalid")
	}
	if raw[addressEnd] != UIControl || raw[addressEnd+1] != NoLayer3PID {
		return out, errors.New("AX.25 frame is not a UI frame")
	}
	out.Destination, out.Source = addresses[0], addresses[1]
	if len(addresses) > 2 {
		out.Digipeaters = append([]Address(nil), addresses[2:]...)
	}
	out.Information = append([]byte(nil), raw[addressEnd+2:fcsAt]...)
	return out, nil
}

// NRZIDecode converts demodulated mark/space levels back to HDLC bits.
func NRZIDecode(levels []bool, initialMark bool) []bool {
	out := make([]bool, len(levels))
	previous := initialMark
	for i, level := range levels {
		out[i] = level == previous
		previous = level
	}
	return out
}
