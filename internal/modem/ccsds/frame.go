package ccsds

import (
	"errors"
	"fmt"
	"math"
)

const (
	ASM               uint32 = 0x1ACFFC1D
	PayloadLength            = 256
	DefaultSampleRate        = 19200
)

var randomizer = generateRandomizer(255)

func generateRandomizer(n int) []byte {
	seq := make([]byte, n)
	x := [9]int{1, 1, 1, 1, 1, 1, 1, 1, 1}
	for i := 0; i < n*8; i++ {
		seq[i/8] |= byte(x[1] << (7 - (i % 8)))
		next := (x[8] + x[6] + x[4] + x[1]) % 2
		old := x
		x[0] = next
		for j := 1; j < 8; j++ {
			x[j] = old[j+1]
		}
		x[8] = next
	}
	return seq
}
func RandomizerSequence() []byte { return append([]byte(nil), randomizer...) }

// BuildDataUnit follows the supplied ARCSSD CCSDS source: four-byte ASM followed
// by 256 bytes randomized with the 255-byte CCSDS PN sequence repeated.
func BuildDataUnit(payload []byte) ([]byte, error) {
	if len(payload) != PayloadLength {
		return nil, fmt.Errorf("CCSDS payload must be exactly %d bytes, got %d", PayloadLength, len(payload))
	}
	out := make([]byte, 4+PayloadLength)
	out[0] = byte(ASM >> 24)
	out[1] = byte((ASM >> 16) & 0xff)
	out[2] = byte((ASM >> 8) & 0xff)
	out[3] = byte(ASM & 0xff)
	for i, b := range payload {
		out[4+i] = b ^ randomizer[i%len(randomizer)]
	}
	return out, nil
}
func ParseDataUnit(data []byte) ([]byte, error) {
	if len(data) != 4+PayloadLength {
		return nil, fmt.Errorf("CCSDS data unit must be %d bytes", 4+PayloadLength)
	}
	sync := uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
	if sync != ASM {
		return nil, errors.New("CCSDS ASM mismatch")
	}
	out := make([]byte, PayloadLength)
	for i, b := range data[4:] {
		out[i] = b ^ randomizer[i%len(randomizer)]
	}
	return out, nil
}

func Modulate(data []byte, bitRate, markHz, spaceHz, sampleRate int) ([]float32, error) {
	if bitRate != 1200 && bitRate != 2400 {
		return nil, errors.New("CCSDS bit rate must be 1200 or 2400")
	}
	if sampleRate%bitRate != 0 {
		return nil, errors.New("sample rate must be divisible by bit rate")
	}
	if markHz <= 0 || spaceHz <= 0 || markHz >= sampleRate/2 || spaceHz >= sampleRate/2 {
		return nil, errors.New("invalid tone frequency")
	}
	// Match the supplied ARCSSD phase-accumulator modems at 19.2 kHz.
	if sampleRate == 19200 && bitRate == 2400 && markHz == 1200 && spaceHz == 2400 {
		return modulateARCSSD2400(data), nil
	}
	if sampleRate == 19200 && bitRate == 1200 && markHz == 1200 && spaceHz == 2200 {
		return modulateARCSSD1200(data), nil
	}
	sps := sampleRate / bitRate
	out := make([]float32, len(data)*8*sps)
	phase := 0.0
	pos := 0
	for _, b := range data {
		for k := 7; k >= 0; k-- {
			freq := spaceHz
			if b&(1<<k) != 0 {
				freq = markHz
			}
			inc := 2 * math.Pi * float64(freq) / float64(sampleRate)
			for j := 0; j < sps; j++ {
				out[pos] = float32(.9155273 * math.Sin(phase))
				phase += inc
				if phase >= 2*math.Pi {
					phase -= 2 * math.Pi
				}
				pos++
			}
		}
	}
	return out, nil
}

// AddGuardSequences reproduces ARCSSD's 150-byte PN preamble and trailer.
// Apply this once to a complete burst, not separately to each data unit.
func AddGuardSequences(data []byte, preambleBytes, trailerBytes int) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("CCSDS data unit is empty")
	}
	if preambleBytes < 0 || trailerBytes < 0 || preambleBytes > 4096 || trailerBytes > 4096 {
		return nil, errors.New("CCSDS guard sequence length must be 0..4096 bytes")
	}
	out := make([]byte, 0, preambleBytes+len(data)+trailerBytes)
	for i := 0; i < preambleBytes; i++ {
		out = append(out, randomizer[i%len(randomizer)])
	}
	out = append(out, data...)
	for i := 0; i < trailerBytes; i++ {
		out = append(out, randomizer[i%len(randomizer)])
	}
	return out, nil
}

// BuildGuardedDataUnits creates the complete CCSDS burst byte stream directly
// in its final backing array. It avoids allocating one temporary byte slice per
// 256-byte frame and then copying the complete stream again to add guards.
// The resulting bytes are identical to BuildDataUnit for every payload followed
// by AddGuardSequences around the concatenated units.
func BuildGuardedDataUnits(payloads [][]byte, preambleBytes, trailerBytes int) ([]byte, error) {
	if len(payloads) == 0 {
		return nil, errors.New("CCSDS payload batch is empty")
	}
	if preambleBytes < 0 || trailerBytes < 0 || preambleBytes > 4096 || trailerBytes > 4096 {
		return nil, errors.New("CCSDS guard sequence length must be 0..4096")
	}
	for i, payload := range payloads {
		if len(payload) != PayloadLength {
			return nil, fmt.Errorf("CCSDS payload %d must be exactly %d bytes, got %d", i, PayloadLength, len(payload))
		}
	}
	unitSize := 4 + PayloadLength
	total := preambleBytes + len(payloads)*unitSize + trailerBytes
	out := make([]byte, total)
	pos := 0
	for i := 0; i < preambleBytes; i++ {
		out[pos] = randomizer[i%len(randomizer)]
		pos++
	}
	for _, payload := range payloads {
		out[pos], out[pos+1], out[pos+2], out[pos+3] = byte((ASM>>24)&0xff), byte((ASM>>16)&0xff), byte((ASM>>8)&0xff), byte(ASM&0xff)
		pos += 4
		for i, value := range payload {
			out[pos+i] = value ^ randomizer[i%len(randomizer)]
		}
		pos += PayloadLength
	}
	for i := 0; i < trailerBytes; i++ {
		out[pos] = randomizer[i%len(randomizer)]
		pos++
	}
	return out, nil
}

// FindDataUnits scans an aligned byte stream for ASM and returns complete,
// de-randomized 256-byte user frames. It tolerates arbitrary bytes before ASM.
func FindDataUnits(stream []byte) ([][]byte, error) {
	var frames [][]byte
	for i := 0; i+4+PayloadLength <= len(stream); {
		if matchesASMBytes(stream[i : i+4]) {
			frame := append([]byte(nil), stream[i:i+4+PayloadLength]...)
			frame[0], frame[1], frame[2], frame[3] = 0x1a, 0xcf, 0xfc, 0x1d
			payload, err := ParseDataUnit(frame)
			if err == nil {
				frames = append(frames, payload)
				i += 4 + PayloadLength
				continue
			}
		}
		i++
	}
	if len(frames) == 0 {
		return nil, errors.New("no complete CCSDS data units found")
	}
	return frames, nil
}

// matchesASMBytes allows the single-bit ASM correction accepted by the
// reference ARCSSD receiver. The caller normalizes the accepted ASM before
// handing the data unit to ParseDataUnit.
func matchesASMBytes(candidate []byte) bool {
	if len(candidate) != 4 {
		return false
	}
	want := [...]byte{0x1a, 0xcf, 0xfc, 0x1d}
	different := 0
	for i := range want {
		v := candidate[i] ^ want[i]
		for v != 0 {
			different++
			if different > 1 {
				return false
			}
			v &= v - 1
		}
	}
	return true
}
