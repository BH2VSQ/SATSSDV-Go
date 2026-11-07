// Package ssdv implements SSDV packet validation, JPEG coefficient transcoding,
// packet generation and contiguous packet-set reconstruction.
package ssdv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"

	"github.com/BH2VSQ/SATSSDV-Go/internal/fec"
)

const (
	PacketSize = 256
	SyncWord   = 0x55
	TypeFEC    = 0x66
	TypeNoFEC  = 0x67
)

// GIFMotionVector describes one rectangular MCU region copied from the prior
// composed frame. DeltaX/DeltaY move the source region to its destination, in
// 16x16 MCU units. It is carried only by SGV2 frame-open control packets.
type GIFMotionVector struct {
	SourceX uint8
	SourceY uint8
	Width   uint8
	Height  uint8
	DeltaX  int8
	DeltaY  int8
}

type Packet struct {
	Raw            [PacketSize]byte
	Callsign       string
	ImageID        uint8
	PacketID       uint16
	Width          int
	Height         int
	Quality        uint8
	EOI            bool
	Subsampling    uint8
	MCUOffset      uint8
	MCUIndex       uint16
	MCUCount       uint32
	FEC            bool
	GIFMode        bool
	GIFKeyframe    bool
	GIFControl     bool
	GIFFrameStart  bool
	GIFFrameEnd    bool
	GIFDataPackets uint16
	GIFDelayCS     uint16
	GIFVersion     uint8
	GIFMotion      []GIFMotionVector
	GIFStart       bool // animation stream start
	GIFEnd         bool // animation stream end
}

func ParsePacket(raw []byte) (Packet, error) {
	var p Packet
	if len(raw) != PacketSize {
		return p, fmt.Errorf("SSDV packet must be 256 bytes")
	}
	copy(p.Raw[:], raw)
	if p.Raw[0] != SyncWord {
		return p, errors.New("SSDV sync byte mismatch")
	}
	typ := p.Raw[1]
	var crcAt, crcLen int
	if typ == TypeNoFEC {
		// A damaged FEC type byte can turn 0x66 into 0x67. Accept the
		// no-FEC layout only when its longer CRC validates; otherwise try RS.
		crcAt, crcLen = 252, 251
		if binary.BigEndian.Uint32(p.Raw[crcAt:crcAt+4]) != crc32.ChecksumIEEE(p.Raw[1:1+crcLen]) {
			if _, err := fec.CorrectRS8(p.Raw[1:]); err != nil {
				return p, fmt.Errorf("SSDV packet is neither valid no-FEC nor correctable FEC: %w", err)
			}
			typ = p.Raw[1]
			if typ != TypeFEC {
				return p, fmt.Errorf("unsupported SSDV packet type 0x%02x after RS8 correction", typ)
			}
			crcAt, crcLen = 220, 219
		}
	} else {
		// The type byte belongs to the protected RS codeword. Attempt FEC
		// correction before interpreting it, so a damaged 0x66 can recover.
		if _, err := fec.CorrectRS8(p.Raw[1:]); err != nil {
			return p, fmt.Errorf("SSDV RS8 correction failed: %w", err)
		}
		typ = p.Raw[1]
		if typ != TypeFEC {
			return p, fmt.Errorf("unsupported SSDV packet type 0x%02x after RS8 correction", typ)
		}
		crcAt, crcLen = 220, 219
	}
	got := binary.BigEndian.Uint32(p.Raw[crcAt : crcAt+4])
	want := crc32.ChecksumIEEE(p.Raw[1 : 1+crcLen])
	if got != want {
		return p, fmt.Errorf("SSDV CRC mismatch: got %08x want %08x", got, want)
	}
	p.Callsign = DecodeBase40(binary.BigEndian.Uint32(p.Raw[2:6]))
	p.ImageID = p.Raw[6]
	p.PacketID = binary.BigEndian.Uint16(p.Raw[7:9])
	p.Width = int(p.Raw[9]) * 16
	p.Height = int(p.Raw[10]) * 16
	flags := p.Raw[11]
	p.Quality = ((flags >> 3) & 0x07) ^ 0x04
	p.EOI = flags&0x04 != 0
	p.Subsampling = flags & 0x03
	p.MCUOffset = p.Raw[12]
	p.MCUIndex = binary.BigEndian.Uint16(p.Raw[13:15])
	p.FEC = typ == TypeFEC
	p.GIFMode = flags&0x80 != 0
	p.GIFKeyframe = flags&0x40 != 0
	p.GIFControl = p.GIFMode && (p.PacketID == 0xfffe || p.PacketID == 0xffff) && p.MCUOffset == 0xff && p.MCUIndex == 0xffff
	if p.GIFControl {
		p.GIFDelayCS = binary.BigEndian.Uint16(p.Raw[15:17])
		// SGV2 deliberately retains the SGV1 control envelope magic and legacy
		// low flag bits. SGV1 receivers ignore the extension bytes; SGV2 receivers
		// additionally parse the versioned motion-vector payload.
		if string(p.Raw[17:21]) == "SGV1" {
			controlFlags := p.Raw[21]
			p.GIFStart = controlFlags&0x01 != 0
			p.GIFEnd = controlFlags&0x02 != 0
			p.GIFFrameStart = controlFlags&0x04 != 0
			p.GIFFrameEnd = controlFlags&0x08 != 0
			p.GIFDataPackets = binary.BigEndian.Uint16(p.Raw[22:24])
			p.GIFVersion = 1
			if controlFlags&0x10 != 0 {
				if p.Raw[24] != 2 || p.Raw[26] != 6 || p.Raw[25] > 32 || 27+int(p.Raw[25])*6 > 219 {
					return Packet{}, errors.New("invalid SGV2 motion extension header")
				}
				p.GIFVersion = 2
				for i := 0; i < int(p.Raw[25]); i++ {
					base := 27 + i*6
					v := GIFMotionVector{SourceX: p.Raw[base], SourceY: p.Raw[base+1], Width: p.Raw[base+2], Height: p.Raw[base+3], DeltaX: int8(p.Raw[base+4]), DeltaY: int8(p.Raw[base+5])}
					p.GIFMotion = append(p.GIFMotion, v)
				}
				if len(p.GIFMotion) > 0 && (!p.GIFFrameStart || p.GIFKeyframe || !validGIFMotionSet(p.GIFMotion, p.Width, p.Height)) {
					return Packet{}, errors.New("invalid SGV2 motion vector set for this frame control")
				}
			}
		}
		if !p.GIFFrameStart && !p.GIFFrameEnd {
			p.GIFFrameEnd = true // legacy SGV1 delimiter compatibility
		}
	}
	if p.Width == 0 || p.Height == 0 || p.Width > 4080 || p.Height > 4080 || p.Width%16 != 0 || p.Height%16 != 0 {
		return Packet{}, errors.New("SSDV packet has invalid image dimensions")
	}
	baseMCUs := uint32(p.Width/16) * uint32(p.Height/16)
	switch p.Subsampling {
	case 1, 2:
		p.MCUCount = baseMCUs * 2
	case 3:
		p.MCUCount = baseMCUs * 4
	default:
		p.MCUCount = baseMCUs
	}
	if p.MCUCount == 0 || p.MCUCount > 65535 {
		return Packet{}, errors.New("SSDV MCU count exceeds the 16-bit header range")
	}
	if p.MCUIndex != 0xffff && uint32(p.MCUIndex) >= p.MCUCount {
		return Packet{}, errors.New("SSDV MCU index exceeds image dimensions")
	}
	payloadLength := PacketSize - 15 - 4
	if p.FEC {
		payloadLength -= 32
	}
	if p.MCUOffset != 0xff && int(p.MCUOffset) >= payloadLength {
		return Packet{}, errors.New("SSDV MCU offset exceeds packet payload")
	}
	return p, nil
}
func validGIFMotionSet(vectors []GIFMotionVector, width, height int) bool {
	for i, vector := range vectors {
		if vector.Width == 0 || vector.Height == 0 || !validGIFMotionVector(vector, width, height) {
			return false
		}
		left := int(vector.SourceX) + int(vector.DeltaX)
		top := int(vector.SourceY) + int(vector.DeltaY)
		right, bottom := left+int(vector.Width), top+int(vector.Height)
		for _, other := range vectors[:i] {
			otherLeft := int(other.SourceX) + int(other.DeltaX)
			otherTop := int(other.SourceY) + int(other.DeltaY)
			otherRight, otherBottom := otherLeft+int(other.Width), otherTop+int(other.Height)
			if left < otherRight && otherLeft < right && top < otherBottom && otherTop < bottom {
				return false // destination rectangles must not overlap
			}
		}
	}
	return true
}

func validGIFMotionVector(v GIFMotionVector, width, height int) bool {
	w, h := width/16, height/16
	sx, sy := int(v.SourceX), int(v.SourceY)
	dx, dy := sx+int(v.DeltaX), sy+int(v.DeltaY)
	return sx >= 0 && sy >= 0 && dx >= 0 && dy >= 0 &&
		sx+int(v.Width) <= w && sy+int(v.Height) <= h &&
		dx+int(v.Width) <= w && dy+int(v.Height) <= h &&
		(v.DeltaX != 0 || v.DeltaY != 0)
}

func DecodeBase40(v uint32) string {
	if v > 0xF423FFFF {
		return ""
	}
	out := make([]byte, 0, 6)
	for v > 0 {
		d := v % 40
		v /= 40
		switch {
		case d == 0:
			out = append(out, '-')
		case d < 11:
			out = append(out, byte('0'+d-1))
		case d < 14:
			out = append(out, '-')
		default:
			out = append(out, byte('A'+d-14))
		}
	}
	for len(out) < 6 {
		out = append(out, ' ')
	}
	return strings.TrimRight(string(out), " ")
}
