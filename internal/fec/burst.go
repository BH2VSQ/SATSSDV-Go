package fec

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
)

const (
	BurstHeaderSize      = 18
	BurstVersion         = 1
	BurstHeaderRepeat    = 3
	BurstHeaderWireBits  = BurstHeaderSize * 8 * BurstHeaderRepeat
	BurstHeaderWireBytes = (BurstHeaderWireBits + 7) / 8
	BurstInterleave      = InterleaveBlockBytes
	maxBurstRawBytes     = 64 << 20
)

var burstMagic = [4]byte{'S', 'D', 'F', '2'}

type BurstHeader struct {
	RawBytes uint32
	CRC32    uint32
}

func NewBurstHeader(raw []byte) BurstHeader {
	return BurstHeader{RawBytes: uint32(len(raw)), CRC32: crc32.ChecksumIEEE(raw)}
}

func (h BurstHeader) MarshalBinary() ([]byte, error) {
	if h.RawBytes == 0 || h.RawBytes > maxBurstRawBytes {
		return nil, errors.New("FEC burst payload length is outside 1..64 MiB")
	}
	out := make([]byte, BurstHeaderSize)
	copy(out[:4], burstMagic[:])
	out[4] = BurstVersion
	out[5] = 1 // K=7, rate 1/2, generators 171/133 octal.
	binary.BigEndian.PutUint16(out[6:8], InterleaveBlockBytes)
	binary.BigEndian.PutUint32(out[8:12], h.RawBytes)
	binary.BigEndian.PutUint32(out[12:16], h.CRC32)
	binary.BigEndian.PutUint16(out[16:18], burstHeaderCRC16(out[:16]))
	return out, nil
}

func ParseBurstHeader(data []byte) (BurstHeader, error) {
	if len(data) < BurstHeaderSize || string(data[:4]) != string(burstMagic[:]) {
		return BurstHeader{}, errors.New("soft-FEC burst marker not found")
	}
	if data[4] != BurstVersion || data[5] != 1 || binary.BigEndian.Uint16(data[6:8]) != InterleaveBlockBytes {
		return BurstHeader{}, errors.New("unsupported soft-FEC burst profile")
	}
	if got, want := binary.BigEndian.Uint16(data[16:18]), burstHeaderCRC16(data[:16]); got != want {
		return BurstHeader{}, errors.New("soft-FEC burst header CRC mismatch")
	}
	h := BurstHeader{RawBytes: binary.BigEndian.Uint32(data[8:12]), CRC32: binary.BigEndian.Uint32(data[12:16])}
	if h.RawBytes == 0 || h.RawBytes > maxBurstRawBytes {
		return BurstHeader{}, errors.New("soft-FEC burst length is invalid")
	}
	return h, nil
}

// RepeatBurstHeader repeats every header bit three times before modulation.
// It makes the uncoded bootstrap descriptor robust enough to select the inner
// decoder; the receiver soft-combines the three LLR observations and then
// validates the CRC16 before trusting the advertised payload length.
func RepeatBurstHeader(header []byte) ([]byte, error) {
	if len(header) != BurstHeaderSize {
		return nil, errors.New("invalid soft-FEC header size")
	}
	out := make([]byte, BurstHeaderWireBytes)
	bitPos := 0
	for _, value := range header {
		for shift := 7; shift >= 0; shift-- {
			bit := value&(1<<uint(shift)) != 0
			for repeat := 0; repeat < BurstHeaderRepeat; repeat++ {
				if bit {
					out[bitPos/8] |= 1 << uint(7-bitPos%8)
				}
				bitPos++
			}
		}
	}
	return out, nil
}

// CombineRepeatedBurstHeader performs soft majority combining for each
// triplicated bit. Positive combined LLR favors zero; negative favors one.
func CombineRepeatedBurstHeader(llrs []float32) ([]byte, error) {
	if len(llrs) != BurstHeaderWireBits {
		return nil, errors.New("invalid repeated soft-FEC header bit count")
	}
	out := make([]byte, BurstHeaderSize)
	bitPos := 0
	for byteIndex := 0; byteIndex < BurstHeaderSize; byteIndex++ {
		for bit := 0; bit < 8; bit++ {
			combined := llrs[bitPos] + llrs[bitPos+1] + llrs[bitPos+2]
			if combined < 0 {
				out[byteIndex] |= 1 << uint(7-bit)
			}
			bitPos += BurstHeaderRepeat
		}
	}
	return out, nil
}

func burstHeaderCRC16(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, value := range data {
		crc ^= uint16(value) << 8
		for bit := 0; bit < 8; bit++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func (h BurstHeader) CodedBits() int64  { return 2 * (int64(h.RawBytes)*8 + ConvMemory) }
func (h BurstHeader) CodedBytes() int64 { return (h.CodedBits() + 7) / 8 }
func (h BurstHeader) InterleavedBytes() int64 {
	return ((h.CodedBytes() + InterleaveBlockBytes - 1) / InterleaveBlockBytes) * InterleaveBlockBytes
}

const (
	PacketParityHeaderSize = 28
	PacketParityDataSize   = 222 // SSDV bytes [2:224], excluding fixed sync/type and RS8 parity.
	PacketParityCRCOffset  = PacketParityHeaderSize + PacketParityDataSize
)

var packetParityMagic = [4]byte{'S', 'P', 'F', '2'}

type PacketParityPayload struct {
	Shard    uint8
	Count    uint8
	ImageID  uint8
	Callsign uint32
	IDs      [8]uint16
	Data     []byte
}

// BuildPacketParityPayloads creates two parity control payloads for a group of
// one to eight ordinary FEC-enabled SSDV packets. The SSDV RS8 parity bytes
// are intentionally omitted: the receiver regenerates them after recovering
// the protected packet data bytes.
func BuildPacketParityPayloads(packets [][]byte) ([][]byte, error) {
	if len(packets) < 1 || len(packets) > 8 {
		return nil, errors.New("packet parity group must contain 1..8 SSDV packets")
	}
	data := make([][]byte, len(packets))
	var callsign uint32
	var imageID uint8
	var ids [8]uint16
	for i, raw := range packets {
		if len(raw) != 256 || raw[0] != 0x55 || raw[1] != 0x66 {
			return nil, errors.New("packet parity requires standard FEC-enabled SSDV packets")
		}
		call := binary.BigEndian.Uint32(raw[2:6])
		id := binary.BigEndian.Uint16(raw[7:9])
		if i == 0 {
			callsign, imageID = call, raw[6]
		} else if call != callsign || raw[6] != imageID {
			return nil, errors.New("packet parity group must use one callsign and image ID")
		}
		ids[i] = id
		data[i] = raw[2:224]
	}
	p0, p1, err := EncodeErasureParity(data)
	if err != nil {
		return nil, err
	}
	return [][]byte{
		marshalPacketParity(PacketParityPayload{Shard: 0, Count: uint8(len(packets)), ImageID: imageID, Callsign: callsign, IDs: ids, Data: p0}),
		marshalPacketParity(PacketParityPayload{Shard: 1, Count: uint8(len(packets)), ImageID: imageID, Callsign: callsign, IDs: ids, Data: p1}),
	}, nil
}

func marshalPacketParity(p PacketParityPayload) []byte {
	out := make([]byte, 256)
	copy(out[:4], packetParityMagic[:])
	out[4], out[5], out[6], out[7] = 1, p.Shard, p.Count, p.ImageID
	binary.BigEndian.PutUint32(out[8:12], p.Callsign)
	for i, id := range p.IDs {
		binary.BigEndian.PutUint16(out[12+i*2:14+i*2], id)
	}
	copy(out[PacketParityHeaderSize:PacketParityCRCOffset], p.Data)
	binary.BigEndian.PutUint32(out[PacketParityCRCOffset:PacketParityCRCOffset+4], crc32.ChecksumIEEE(out[:PacketParityCRCOffset]))
	binary.BigEndian.PutUint16(out[254:256], 0xA55A)
	return out
}

func IsPacketParityPayload(data []byte) bool {
	return len(data) == 256 && string(data[:4]) == string(packetParityMagic[:])
}

func ParsePacketParityPayload(data []byte) (PacketParityPayload, error) {
	var p PacketParityPayload
	if !IsPacketParityPayload(data) {
		return p, errors.New("packet parity marker not found")
	}
	if data[4] != 1 || data[5] > 1 || data[6] < 1 || data[6] > 8 || binary.BigEndian.Uint16(data[254:256]) != 0xA55A {
		return p, errors.New("packet parity header is invalid")
	}
	if got, want := binary.BigEndian.Uint32(data[PacketParityCRCOffset:PacketParityCRCOffset+4]), crc32.ChecksumIEEE(data[:PacketParityCRCOffset]); got != want {
		return p, errors.New("packet parity payload CRC mismatch")
	}
	p.Shard, p.Count, p.ImageID = data[5], data[6], data[7]
	p.Callsign = binary.BigEndian.Uint32(data[8:12])
	for i := range p.IDs {
		p.IDs[i] = binary.BigEndian.Uint16(data[12+i*2 : 14+i*2])
	}
	p.Data = append([]byte(nil), data[PacketParityHeaderSize:PacketParityCRCOffset]...)
	for i := 0; i < int(p.Count); i++ {
		for j := 0; j < i; j++ {
			if p.IDs[i] == p.IDs[j] {
				return PacketParityPayload{}, errors.New("packet parity IDs are duplicated")
			}
		}
	}
	return p, nil
}

// RecoverPacketParity restores missing SSDV packet bodies from one or two
// parity payloads. recovered packets are full 256-byte SSDV packets with their
// canonical outer RS8 parity regenerated and validated by the caller.
func RecoverPacketParity(parities []PacketParityPayload, available map[uint16][]byte) (map[uint16][]byte, error) {
	if len(parities) < 1 || len(parities) > 2 {
		return nil, errors.New("one or two packet parity shards are required")
	}
	base := parities[0]
	var p0, p1 []byte
	for _, p := range parities {
		if p.Count != base.Count || p.ImageID != base.ImageID || p.Callsign != base.Callsign || p.IDs != base.IDs {
			return nil, errors.New("packet parity group metadata mismatch")
		}
		if p.Shard == 0 {
			p0 = p.Data
		} else {
			p1 = p.Data
		}
	}
	shards := make([][]byte, int(base.Count))
	for i := range shards {
		if raw := available[base.IDs[i]]; len(raw) == 256 && raw[0] == 0x55 && raw[1] == 0x66 && binary.BigEndian.Uint32(raw[2:6]) == base.Callsign && raw[6] == base.ImageID && binary.BigEndian.Uint16(raw[7:9]) == base.IDs[i] {
			shards[i] = append([]byte(nil), raw[2:224]...)
		}
	}
	missing := 0
	for _, shard := range shards {
		if shard == nil {
			missing++
		}
	}
	if missing == 0 {
		return map[uint16][]byte{}, nil
	}
	if missing > 2 {
		return nil, errors.New("more than two SSDV packets are missing in parity group")
	}
	if len(p0) == 0 && len(p1) == 0 {
		return nil, errors.New("packet parity group has no valid parity shard")
	}
	if len(p0) == 0 && missing == 1 {
		index := -1
		for i, shard := range shards {
			if shard == nil {
				index = i
				break
			}
		}
		if index >= 0 {
			residual := append([]byte(nil), p1...)
			for i, shard := range shards {
				if shard == nil {
					continue
				}
				for pos, value := range shard {
					residual[pos] ^= gfMul(alphaTo[i], value)
				}
			}
			coef := alphaTo[index]
			for pos := range residual {
				residual[pos] = gfDiv(residual[pos], coef)
			}
			shards[index] = residual
		}
	} else {
		if len(p0) == 0 {
			return nil, errors.New("two missing packets require both parity shards")
		}
		if len(p1) == 0 && missing == 2 {
			return nil, errors.New("two missing packets require both parity shards")
		}
		if _, err := RecoverErasureShards(shards, p0, p1); err != nil {
			return nil, err
		}
	}
	out := make(map[uint16][]byte, missing)
	for i, rawBody := range shards {
		if _, exists := available[base.IDs[i]]; exists {
			continue
		}
		if len(rawBody) != PacketParityDataSize {
			return nil, errors.New("recovered packet body has invalid size")
		}
		raw := make([]byte, 256)
		raw[0], raw[1] = 0x55, 0x66
		copy(raw[2:224], rawBody)
		parity, err := RS8Parity(raw[1:224])
		if err != nil {
			return nil, err
		}
		copy(raw[224:], parity)
		out[base.IDs[i]] = raw
	}
	return out, nil
}

func ExpectedInterleavedBytes(rawBytes uint32) (int, error) {
	if rawBytes == 0 || rawBytes > maxBurstRawBytes {
		return 0, errors.New("invalid raw burst size")
	}
	codedBits := 2 * (int64(rawBytes)*8 + ConvMemory)
	codedBytes := (codedBits + 7) / 8
	blocks := int64(math.Ceil(float64(codedBytes) / InterleaveBlockBytes))
	return int(blocks * InterleaveBlockBytes), nil
}
