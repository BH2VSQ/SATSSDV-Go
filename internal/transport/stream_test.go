package transport

import "testing"

func TestStreamDecoderStartsNewImageWhenPacketZeroReturns(t *testing.T) {
	d := &StreamDecoder{}
	packet := ReceivedPacket{Profile: "ssdv-ax25", Callsign: "N0CALL", ImageID: 4, PacketID: 0, Raw: []byte{0x11}}
	if !d.markNew(packet) {
		t.Fatal("first packet 0 was not accepted")
	}
	for i := uint16(1); i <= 12; i++ {
		packet.PacketID = i
		if !d.markNew(packet) {
			t.Fatalf("packet %d was unexpectedly suppressed", i)
		}
	}
	packet.PacketID = 0
	packet.Raw = []byte{0x22}
	if !d.markNew(packet) {
		t.Fatal("packet 0 of the next image session was suppressed")
	}
	packet.PacketID = 1
	if !d.markNew(packet) {
		t.Fatal("packet 1 from the next image session was suppressed by old session state")
	}
}

func TestStreamDecoderStillSuppressesShortDuplicatePacketZero(t *testing.T) {
	d := &StreamDecoder{}
	packet := ReceivedPacket{Profile: "ssdv-ax25", Callsign: "N0CALL", ImageID: 5, PacketID: 0, Raw: []byte{0x33}}
	if !d.markNew(packet) {
		t.Fatal("first packet 0 was not accepted")
	}
	packet.PacketID = 1
	if !d.markNew(packet) {
		t.Fatal("packet 1 was not accepted")
	}
	packet.PacketID = 0
	if d.markNew(packet) {
		t.Fatal("short duplicate packet 0 should be suppressed")
	}
}

func TestStreamDecoderDoesNotRollOverWhenPacketZeroArrivesLate(t *testing.T) {
	d := &StreamDecoder{}
	packet := ReceivedPacket{Profile: "ssdv-ax25", Callsign: "N0CALL", ImageID: 6, Raw: []byte{0x44}}
	for i := 12; i >= 1; i-- {
		packet.PacketID = uint16(i)
		packet.EOI = i == 12
		if !d.markNew(packet) {
			t.Fatalf("out-of-order packet %d was unexpectedly suppressed", i)
		}
	}
	packet.PacketID = 0
	packet.EOI = false
	if !d.markNew(packet) {
		t.Fatal("late packet 0 was rejected")
	}
	packet.PacketID = 12
	if d.markNew(packet) {
		t.Fatal("late packet 0 incorrectly created a new session and cleared duplicate history")
	}
}
