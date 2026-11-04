package main

import (
	"testing"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/album"
	rxengine "github.com/BH2VSQ/SATSSDV-Go/internal/rx"
)

func TestCollapseRXSessionRecordsMergesPreviewAndPacketOnlyRows(t *testing.T) {
	start := time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC)
	finish := start.Add(2 * time.Minute)
	index := album.Index{Records: []album.Record{
		{ID: "packet", Type: "RX", Profile: "ssdv-ax25", Callsign: "N0CALL", ImageID: 9, SessionID: "s1", PacketPath: "/album/packets/a.ssdv", PacketsReceived: 20, PacketsExpected: 30, CreatedAt: start, Status: "receiving"},
		{ID: "preview", Type: "RX", Profile: "ssdv-ax25", Callsign: "N0CALL", ImageID: 9, SessionID: "s1", ImagePath: "/album/images/a.png", ImageWidth: 320, ImageHeight: 240, PacketsReceived: 18, PacketsExpected: 30, CreatedAt: finish, Status: "complete"},
		{ID: "other-session", Type: "RX", Profile: "ssdv-ax25", Callsign: "N0CALL", ImageID: 9, SessionID: "s2", ImagePath: "/album/images/b.png", CreatedAt: start},
	}}
	update := rxengine.Update{Profile: "ssdv-ax25", Callsign: "N0CALL", ImageID: 9, SessionID: "s1", Complete: true}
	removed := collapseRXSessionRecords(&index, update)
	if len(index.Records) != 2 {
		t.Fatalf("got %d rows, want 2 (one merged session plus one other session)", len(index.Records))
	}
	var merged *album.Record
	for i := range index.Records {
		if index.Records[i].SessionID == "s1" {
			merged = &index.Records[i]
		}
	}
	if merged == nil {
		t.Fatal("merged receive session disappeared")
	}
	if merged.ImagePath != "/album/images/a.png" || merged.PacketPath != "/album/packets/a.ssdv" {
		t.Fatalf("merged media paths were not preserved: %+v", merged)
	}
	if merged.PacketsReceived != 20 || merged.Status != "complete" || !merged.CreatedAt.Equal(finish) {
		t.Fatalf("merged completion metadata incorrect: %+v", merged)
	}
	if len(removed) != 0 {
		t.Fatalf("unexpected duplicate media files scheduled for removal: %v", removed)
	}
}

func TestCollapseRXSessionRecordsKeepsDifferentSessionsSeparate(t *testing.T) {
	index := album.Index{Records: []album.Record{
		{ID: "one", Type: "RX", Profile: "ssdv-ccsds-1200", Callsign: "N0CALL", ImageID: 2, SessionID: "one"},
		{ID: "two", Type: "RX", Profile: "ssdv-ccsds-1200", Callsign: "N0CALL", ImageID: 2, SessionID: "two"},
	}}
	collapseRXSessionRecords(&index, rxengine.Update{Profile: "ssdv-ccsds-1200", Callsign: "N0CALL", ImageID: 2, SessionID: "one"})
	if len(index.Records) != 2 {
		t.Fatalf("records from distinct receive sessions were merged: %+v", index.Records)
	}
}
