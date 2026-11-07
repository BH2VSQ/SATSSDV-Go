package fec

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"math/rand"
	"testing"
)

func TestConvStreamRoundTripSoftViterbi(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	payload := make([]byte, 4097)
	_, _ = rng.Read(payload)
	enc := NewConvStreamEncoder()
	var coded []byte
	for i := 0; i < len(payload); i += 137 {
		end := min(len(payload), i+137)
		part, err := enc.WriteBytes(payload[i:end])
		if err != nil {
			t.Fatal(err)
		}
		coded = append(coded, part...)
	}
	tail, err := enc.Close()
	if err != nil {
		t.Fatal(err)
	}
	coded = append(coded, tail...)
	llrs := make([]float32, 0, len(coded)*8)
	for _, b := range coded {
		for bit := 7; bit >= 0; bit-- {
			if b&(1<<uint(bit)) != 0 {
				llrs = append(llrs, -8)
			} else {
				llrs = append(llrs, 8)
			}
		}
	}
	dec := NewViterbiDecoder()
	var decoded []bool
	for i := 0; i < len(llrs); i += 514 {
		end := min(len(llrs), i+514)
		// Keep pair alignment when splitting chunks.
		if (end-i)%2 != 0 {
			end--
		}
		part, feedErr := dec.Feed(llrs[i:end])
		if feedErr != nil {
			t.Fatal(feedErr)
		}
		decoded = append(decoded, part...)
		i = end - 514
	}
	last, err := dec.Flush()
	if err != nil {
		t.Fatal(err)
	}
	decoded = append(decoded, last...)
	if len(decoded) < len(payload)*8+ConvMemory {
		t.Fatalf("decoded %d bits", len(decoded))
	}
	got := make([]byte, len(payload))
	for i := 0; i < len(payload)*8; i++ {
		if decoded[i] {
			got[i/8] |= 1 << uint(7-i%8)
		}
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("Viterbi decoded payload differs from source")
	}
}

func TestBlockInterleaverRoundTrip(t *testing.T) {
	input := make([]byte, InterleaveBlockBytes*3+17)
	for i := range input {
		input[i] = byte(i*37 + 5)
	}
	i := NewBlockInterleaver()
	var encoded []byte
	for pos := 0; pos < len(input); pos += 11 {
		end := min(len(input), pos+11)
		part, err := i.Write(input[pos:end])
		if err != nil {
			t.Fatal(err)
		}
		encoded = append(encoded, part...)
	}
	last, err := i.Close()
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, last...)
	if len(encoded)%InterleaveBlockBytes != 0 {
		t.Fatalf("encoded bytes not block aligned")
	}
	var decoded []byte
	for pos := 0; pos < len(encoded); pos += InterleaveBlockBytes {
		block := encoded[pos : pos+InterleaveBlockBytes]
		for row := 0; row < 8; row++ {
			for col := 0; col < 8; col++ {
				decoded = append(decoded, block[col*8+row])
			}
		}
	}
	if !bytes.Equal(decoded[:len(input)], input) {
		t.Fatal("interleaver did not round trip")
	}
}

func TestErasureParityRecoversAnyOneOrTwoPackets(t *testing.T) {
	data := make([][]byte, 8)
	for shard := range data {
		data[shard] = make([]byte, 222)
		for i := range data[shard] {
			data[shard][i] = byte(shard*29 + i*13 + 3)
		}
	}
	p0, p1, err := EncodeErasureParity(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range [][]int{{0}, {7}, {1, 6}, {3, 4}} {
		trial := make([][]byte, len(data))
		for i := range data {
			trial[i] = append([]byte(nil), data[i]...)
		}
		for _, index := range missing {
			trial[index] = nil
		}
		corrected, err := RecoverErasureShards(trial, p0, p1)
		if err != nil {
			t.Fatalf("missing %v: %v", missing, err)
		}
		if corrected != len(missing) {
			t.Fatalf("missing %v: corrected %d", missing, corrected)
		}
		for i := range data {
			if !bytes.Equal(trial[i], data[i]) {
				t.Fatalf("missing %v: shard %d mismatch", missing, i)
			}
		}
	}
}

func TestErasureParityRejectsMoreThanTwoMissingPackets(t *testing.T) {
	data := make([][]byte, 8)
	for shard := range data {
		data[shard] = make([]byte, 32)
		for i := range data[shard] {
			data[shard][i] = byte(shard*17 + i)
		}
	}
	p0, p1, err := EncodeErasureParity(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{0, 3, 7} {
		data[index] = nil
	}
	if _, err := RecoverErasureShards(data, p0, p1); err == nil {
		t.Fatal("expected recovery to reject three missing shards")
	}
}

func TestPacketParityPayloadRoundTripAndRecovery(t *testing.T) {
	const count = 8
	packets := make([][]byte, count)
	available := make(map[uint16][]byte, count)
	for i := range packets {
		raw := make([]byte, 256)
		raw[0], raw[1], raw[6] = 0x55, 0x66, 33
		raw[2], raw[3], raw[4], raw[5] = 0, 1, 2, 3
		binary.BigEndian.PutUint16(raw[7:9], uint16(100+i))
		for j := 15; j < 220; j++ {
			raw[j] = byte(i*31 + j*7)
		}
		binary.BigEndian.PutUint32(raw[220:224], crc32.ChecksumIEEE(raw[1:220]))
		parity, err := RS8Parity(raw[1:224])
		if err != nil {
			t.Fatal(err)
		}
		copy(raw[224:], parity)
		packets[i] = raw
		available[uint16(100+i)] = append([]byte(nil), raw...)
	}
	payloads, err := BuildPacketParityPayloads(packets)
	if err != nil {
		t.Fatal(err)
	}
	parsed := make([]PacketParityPayload, 2)
	for i := range payloads {
		parsed[i], err = ParsePacketParityPayload(payloads[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	delete(available, 102)
	delete(available, 106)
	recovered, err := RecoverPacketParity(parsed, available)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 2 {
		t.Fatalf("recovered %d packets", len(recovered))
	}
	for id, raw := range recovered {
		if _, err := CorrectRS8(raw[1:]); err != nil {
			t.Fatalf("packet %d RS8 invalid: %v", id, err)
		}
	}
}

func TestPacketParityCanRecoverOneLossFromSingleParityShard(t *testing.T) {
	packets := make([][]byte, 4)
	available := make(map[uint16][]byte)
	for i := range packets {
		raw := make([]byte, 256)
		raw[0], raw[1], raw[6] = 0x55, 0x66, 9
		raw[2] = 1
		binary.BigEndian.PutUint16(raw[7:9], uint16(i))
		for j := 15; j < 220; j++ {
			raw[j] = byte(i*23 + j)
		}
		binary.BigEndian.PutUint32(raw[220:224], crc32.ChecksumIEEE(raw[1:220]))
		p, _ := RS8Parity(raw[1:224])
		copy(raw[224:], p)
		packets[i] = raw
		available[uint16(i)] = append([]byte(nil), raw...)
	}
	payloads, err := BuildPacketParityPayloads(packets)
	if err != nil {
		t.Fatal(err)
	}
	parity, err := ParsePacketParityPayload(payloads[1])
	if err != nil {
		t.Fatal(err)
	}
	delete(available, 2)
	recovered, err := RecoverPacketParity([]PacketParityPayload{parity}, available)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recovered[2], packets[2]) {
		t.Fatal("single parity shard did not recover packet")
	}
}

func TestBurstHeaderSizing(t *testing.T) {
	raw := bytes.Repeat([]byte{0x5a}, 260*17)
	header := NewBurstHeader(raw)
	encoded, err := header.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseBurstHeader(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != header {
		t.Fatalf("header mismatch %+v %+v", parsed, header)
	}
	blocks, err := ExpectedInterleavedBytes(parsed.RawBytes)
	if err != nil {
		t.Fatal(err)
	}
	if blocks%InterleaveBlockBytes != 0 || int64(blocks)*8 < parsed.CodedBits() {
		t.Fatal("interleaver sizing invalid")
	}
}

func TestBurstHeaderSoftRepetitionAndCRC(t *testing.T) {
	header := NewBurstHeader([]byte("soft bootstrap header test"))
	encoded, err := header.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := RepeatBurstHeader(encoded)
	if err != nil {
		t.Fatal(err)
	}
	llrs := make([]float32, BurstHeaderWireBits)
	for bitPos := 0; bitPos < BurstHeaderWireBits; bitPos++ {
		value := wire[bitPos/8]&(1<<uint(7-bitPos%8)) != 0
		llr := float32(8)
		if value {
			llr = -8
		}
		if bitPos%3 == 2 {
			llr = -llr
		} // One bad observation out of each triplet.
		llrs[bitPos] = llr
	}
	combined, err := CombineRepeatedBurstHeader(llrs)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseBurstHeader(combined)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != header {
		t.Fatalf("header round trip got %+v want %+v", parsed, header)
	}
	combined[17] ^= 1
	if _, err := ParseBurstHeader(combined); err == nil {
		t.Fatal("header CRC corruption was accepted")
	}
}
