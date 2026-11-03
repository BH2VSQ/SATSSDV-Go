package rx

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

func TestManagerBuildsPCSIImageAndDeduplicatesPackets(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 7), B: uint8((x + y) * 3), A: 255})
		}
	}
	enc, err := pcsi.NewEncoder(img, pcsi.Config{Callsign: "N0CALL", ImageID: 23, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan Update, 8)
	manager := NewManager(func(update Update) { updates <- update })
	defer manager.Close()
	// Deliver packets in reverse order and repeat one packet. PCSI sampling
	// positions are packet-indexed, so reconstruction must not depend on RX
	// arrival order and duplicates must not inflate the packet count.
	for i := enc.PacketCount() - 1; i >= 0; i-- {
		packet, err := enc.EncodePacket(i)
		if err != nil {
			t.Fatal(err)
		}
		item := transport.ReceivedPacket{Profile: "pcsi-ccsds-1200", Codec: "PCSI-MMT", Callsign: "N0CALL", ImageID: 23, PacketID: uint16(i), Raw: packet}
		if !manager.Submit(item) {
			t.Fatal("manager rejected packet")
		}
		if i == enc.PacketCount()-1 && !manager.Submit(item) {
			t.Fatal("manager rejected duplicate packet submit")
		}
	}
	deadline := time.After(4 * time.Second)
	for {
		select {
		case update := <-updates:
			if update.Error != "" {
				t.Fatal(update.Error)
			}
			if update.Complete {
				if update.PNGBase64 == "" || update.PacketsReceived != enc.PacketCount() {
					t.Fatalf("incomplete final update: %+v", update)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for reconstructed PCSI image")
		}
	}
}

func TestManagerSubmitRejectsAfterClose(t *testing.T) {
	m := NewManager(nil)
	m.Close()
	if m.Submit(transport.ReceivedPacket{}) {
		t.Fatal("submit succeeded after manager close")
	}
}

func TestSSDVManagerReconstructsJPEGFromOutOfOrderPackets(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	rawPackets, err := ssdv.EncodeImage(source, ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 7, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan Update, 128)
	manager := NewManager(func(update Update) { updates <- update })
	defer manager.Close()
	submit := func(index int) {
		t.Helper()
		parsed, parseErr := ssdv.ParsePacket(rawPackets[index])
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		wirePacket := append([]byte(nil), rawPackets[index]...)
		if index == 1 {
			// Exercise the RX manager's own validation boundary. The transport
			// normally corrects FEC first, but the manager must never archive a
			// corrupted codeword if another caller submits it directly.
			wirePacket[37] ^= 0x5a
		}
		item := transport.ReceivedPacket{
			Profile: "ssdv-ccsds-1200", Codec: "SSDV", Callsign: parsed.Callsign, ImageID: parsed.ImageID,
			PacketID: parsed.PacketID, ImageWidth: parsed.Width, ImageHeight: parsed.Height,
			Quality: parsed.Quality, EOI: parsed.EOI, Subsampling: parsed.Subsampling,
			MCUOffset: parsed.MCUOffset, MCUIndex: parsed.MCUIndex, MCUCount: parsed.MCUCount,
			FEC: parsed.FEC, Raw: wirePacket,
		}
		if !manager.Submit(item) {
			t.Fatalf("submit packet %d failed", index)
		}
	}
	// EOI arrives first, then packets arrive out of order with one deliberate
	// gap. Duplicate packet 0 must not inflate the received count.
	submit(len(rawPackets) - 1)
	for i := len(rawPackets) - 2; i >= 1; i-- {
		if i == 2 {
			continue
		}
		submit(i)
	}
	submit(0)
	submit(0)
	deadline := time.After(3 * time.Second)
	seenGap := false
	for !seenGap {
		select {
		case update := <-updates:
			if update.Error != "" {
				t.Fatal(update.Error)
			}
			if update.PacketsExpected == len(rawPackets) && update.PacketsReceived == len(rawPackets)-1 && !update.Complete {
				seenGap = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for SSDV missing-packet progress")
		}
	}
	submit(2)
	for {
		select {
		case update := <-updates:
			if update.Error != "" {
				t.Fatal(update.Error)
			}
			if update.Complete {
				if update.PNGBase64 == "" || update.PacketsReceived != len(rawPackets) || update.PacketsExpected != len(rawPackets) {
					t.Fatalf("SSDV image was not reconstructed: %+v", update)
				}
				pngData, err := base64.StdEncoding.DecodeString(update.PNGBase64)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := png.Decode(bytes.NewReader(pngData))
				if err != nil {
					t.Fatalf("decode received SSDV PNG: %v", err)
				}
				if decoded.Bounds().Dx() != 320 || decoded.Bounds().Dy() != 240 {
					t.Fatalf("unexpected image dimensions: %v", decoded.Bounds())
				}
				if update.PacketDataBase64 == "" {
					t.Fatal("expected corrected packet stream for archival")
				}
				archived, err := base64.StdEncoding.DecodeString(update.PacketDataBase64)
				if err != nil {
					t.Fatal(err)
				}
				want := bytes.Join(rawPackets, nil)
				if !bytes.Equal(archived, want) {
					t.Fatal("SSDV packet archive did not contain RS-corrected packet bytes in packet order")
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for reconstructed SSDV image")
		}
	}
}

func TestManagerKeepsIngestingPCSIWhileReconstructionRuns(t *testing.T) {
	const width, height = 160, 160
	src := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 3), B: uint8(x + y), A: 255})
		}
	}
	enc, err := pcsi.NewEncoder(src, pcsi.Config{Callsign: "N0CALL", ImageID: 31, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan Update, 128)
	manager := NewManager(func(update Update) { updates <- update })
	defer manager.Close()
	for i := 0; i < enc.PacketCount(); i++ {
		packet, err := enc.EncodePacket(i)
		if err != nil {
			t.Fatal(err)
		}
		item := transport.ReceivedPacket{Profile: "pcsi-ccsds-2400", Codec: "PCSI-MMT", Callsign: "N0CALL", ImageID: 31, PacketID: uint16(i), Raw: packet}
		queueDeadline := time.Now().Add(5 * time.Second)
		for !manager.Submit(item) {
			if time.Now().After(queueDeadline) {
				t.Fatal("RX packet queue remained full while reconstruction was running")
			}
			time.Sleep(time.Millisecond)
		}
	}
	deadline := time.After(20 * time.Second)
	for {
		select {
		case update := <-updates:
			if update.Error != "" {
				t.Fatal(update.Error)
			}
			if update.Complete {
				if update.PNGBase64 == "" || update.PacketsReceived != enc.PacketCount() || update.PacketsExpected != enc.PacketCount() {
					t.Fatalf("unexpected completed image update: %+v", update)
				}
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for final PCSI image, packets=%d", enc.PacketCount())
		}
	}
}
