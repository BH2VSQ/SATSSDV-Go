package transport

import (
	"testing"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
)

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

func TestStreamDiagnosticsRequireRecentlyValidatedPacket(t *testing.T) {
	decoder, err := NewStreamDecoder(domain.SSDVCCSDS2400, 19200)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoder.Diagnostics(); got.ValidPacketRecent {
		t.Fatalf("new decoder claims a recent valid packet: %+v", got)
	}
	// Carrier acquisition by itself is deliberately insufficient evidence.
	if decoder.gmsk2400.CarrierLocked() && decoder.Diagnostics().ValidPacketRecent {
		t.Fatal("AFC lock alone must not validate a packet")
	}
	decoder.noteValidPackets([]ReceivedPacket{{PacketID: 1}})
	if got := decoder.Diagnostics(); !got.ValidPacketRecent {
		t.Fatalf("recent integrity-checked packet was not exposed: %+v", got)
	}
	decoder.lastValidPacketAt = time.Now().Add(-4 * time.Second)
	if got := decoder.Diagnostics(); got.ValidPacketRecent {
		t.Fatalf("stale packet remained valid for SNR gating: %+v", got)
	}
	decoder.Reset()
	if got := decoder.Diagnostics(); got.ValidPacketRecent {
		t.Fatalf("reset retained recent-packet evidence: %+v", got)
	}
}
