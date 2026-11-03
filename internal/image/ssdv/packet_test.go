package ssdv

import (
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/fec"
)

func validSSDVPacket(t *testing.T) []byte {
	t.Helper()
	packet := make([]byte, PacketSize)
	packet[0], packet[1], packet[6] = SyncWord, TypeFEC, 9
	binary.BigEndian.PutUint16(packet[7:9], 123)
	packet[9], packet[10], packet[11] = 20, 15, 0 // 320x240, 2x2 MCU mode, Q4.
	packet[12] = 0xff                             // No MCU boundary starts inside this packet.
	binary.BigEndian.PutUint16(packet[13:15], 0xffff)
	for i := 15; i < 220; i++ {
		packet[i] = byte(i*17 + 3)
	}
	binary.BigEndian.PutUint32(packet[220:224], crc32.ChecksumIEEE(packet[1:220]))
	parity, err := fec.RS8Parity(packet[1:224])
	if err != nil {
		t.Fatal(err)
	}
	copy(packet[224:], parity)
	return packet
}

func TestParsePacketCorrectsRS8ErrorsBeforeCRC(t *testing.T) {
	original := validSSDVPacket(t)
	corrupt := append([]byte(nil), original...)
	for i := 0; i < 16; i++ {
		pos := 1 + (i*13)%255
		corrupt[pos] ^= byte(i*9 + 1)
	}
	parsed, err := ParsePacket(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Callsign != "" && parsed.Callsign != "------" {
		// The test packet leaves the callsign field zeroed, which decodes to
		// padding symbols. Metadata below is the important assertion.
		t.Fatalf("unexpected callsign %q", parsed.Callsign)
	}
	if parsed.ImageID != 9 || parsed.PacketID != 123 || !parsed.FEC || parsed.Width != 320 || parsed.Height != 240 {
		t.Fatalf("unexpected packet info: %+v", parsed)
	}
	if string(parsed.Raw[:]) != string(original) {
		t.Fatal("SSDV packet bytes were not restored by RS8 correction")
	}
}

func TestParsePacketExtractsImageTerminationAndMCUFields(t *testing.T) {
	packet := validSSDVPacket(t)
	packet[11] = byte((5^4)<<3) | 0x04 | 0x02 // quality 5, EOI, 2x1 subsampling
	packet[12] = 17
	binary.BigEndian.PutUint16(packet[13:15], 0x0123)
	binary.BigEndian.PutUint32(packet[220:224], crc32.ChecksumIEEE(packet[1:220]))
	parity, err := fec.RS8Parity(packet[1:224])
	if err != nil {
		t.Fatal(err)
	}
	copy(packet[224:], parity)
	parsed, err := ParsePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Width != 320 || parsed.Height != 240 || parsed.Quality != 5 || !parsed.EOI || parsed.Subsampling != 2 || parsed.MCUOffset != 17 || parsed.MCUIndex != 0x0123 {
		t.Fatalf("SSDV metadata mismatch: %+v", parsed)
	}
}

func TestParsePacketRejectsInvalidMCUReference(t *testing.T) {
	packet := validSSDVPacket(t)
	packet[12] = 205 // FEC payload is exactly 205 bytes; valid offsets are 0..204.
	parity, err := fec.RS8Parity(packet[1:224])
	if err != nil {
		t.Fatal(err)
	}
	copy(packet[224:], parity)
	if _, err := ParsePacket(packet); err == nil {
		t.Fatal("invalid MCU payload offset was accepted")
	}

	packet = validSSDVPacket(t)
	binary.BigEndian.PutUint16(packet[13:15], 301) // Test geometry has 300 MCU units.
	binary.BigEndian.PutUint32(packet[220:224], crc32.ChecksumIEEE(packet[1:220]))
	parity, err = fec.RS8Parity(packet[1:224])
	if err != nil {
		t.Fatal(err)
	}
	copy(packet[224:], parity)
	if _, err := ParsePacket(packet); err == nil {
		t.Fatal("out-of-range MCU index was accepted")
	}
}

func TestParsePacketAcceptsNoFECLayout(t *testing.T) {
	packet := make([]byte, PacketSize)
	packet[0], packet[1], packet[6] = SyncWord, TypeNoFEC, 11
	packet[7], packet[8] = 0, 4
	packet[9], packet[10] = 20, 15
	packet[11], packet[12] = 4<<3, 0xff
	binary.BigEndian.PutUint16(packet[13:15], 0xffff)
	for i := 15; i < 252; i++ {
		packet[i] = byte(i*13 + 9)
	}
	binary.BigEndian.PutUint32(packet[252:256], crc32.ChecksumIEEE(packet[1:252]))
	parsed, err := ParsePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.FEC || parsed.PacketID != 4 || parsed.ImageID != 11 || parsed.MCUIndex != 0xffff {
		t.Fatalf("unexpected no-FEC packet info: %+v", parsed)
	}
}
