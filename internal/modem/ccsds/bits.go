package ccsds

import "errors"

// FindDataUnitsInBits searches an MSB-first demodulated bit stream for the
// 32-bit ASM and extracts complete 256-byte payloads without assuming byte
// alignment at the beginning of the capture.
func FindDataUnitsInBits(bits []bool) ([][]byte, error) {
	const frameBits = 32 + PayloadLength*8
	var frames [][]byte
	for i := 0; i+frameBits <= len(bits); {
		if !MatchASMBits(bits[i : i+32]) {
			i++
			continue
		}
		data := make([]byte, 4+PayloadLength)
		for j := 0; j < frameBits; j++ {
			if bits[i+j] {
				data[j/8] |= 1 << uint(7-j%8)
			}
		}
		data[0], data[1], data[2], data[3] = 0x1a, 0xcf, 0xfc, 0x1d
		payload, err := ParseDataUnit(data)
		if err == nil {
			frames = append(frames, payload)
			i += frameBits
		} else {
			i++
		}
	}
	if len(frames) == 0 {
		return nil, errors.New("no complete CCSDS data units found in bit stream")
	}
	return frames, nil
}

// MatchASMBits reports whether a 32-bit sequence is within one bit of ASM.
func MatchASMBits(bits []bool) bool {
	if len(bits) != 32 {
		return false
	}
	want := uint32(ASM)
	different := 0
	for i := 0; i < 32; i++ {
		bit := bits[i]
		if bit != (want&(1<<uint(31-i)) != 0) {
			different++
			if different > 1 {
				return false
			}
		}
	}
	return true
}
