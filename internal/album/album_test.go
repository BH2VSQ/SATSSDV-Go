package album

import (
	"testing"
	"time"
)

func TestSortNewestFirstMixesRXAndTX(t *testing.T) {
	base := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	records := []Record{
		{ID: "rx-old", Type: "RX", CreatedAt: base},
		{ID: "tx-new", Type: "TX", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "rx-mid", Type: "RX", CreatedAt: base.Add(time.Minute)},
		{ID: "tx-tie-a", Type: "TX", CreatedAt: base.Add(2 * time.Minute)},
	}
	SortNewestFirst(records)
	want := []string{"tx-tie-a", "tx-new", "rx-mid", "rx-old"}
	for i := range want {
		if records[i].ID != want[i] {
			t.Fatalf("record %d = %s, want %s", i, records[i].ID, want[i])
		}
	}
}

func TestNewestRXImagesIncludesEveryQualityAndUsesNewestFirst(t *testing.T) {
	base := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	records := []Record{
		{ID: "rx-low", Type: "RX", ImagePath: "low.png", CreatedAt: base, ImageWidth: 320, ImageHeight: 240, Quality: "low"},
		{ID: "tx-mid", Type: "TX", ImagePath: "tx.png", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "rx-high", Type: "RX", ImagePath: "high.png", CreatedAt: base.Add(3 * time.Minute), ImageWidth: 640, ImageHeight: 480, Quality: "high"},
		{ID: "rx-medium", Type: "RX", ImagePath: "medium.png", CreatedAt: base.Add(time.Minute), ImageWidth: 448, ImageHeight: 336, Quality: "medium"},
		{ID: "rx-no-image", Type: "RX", CreatedAt: base.Add(4 * time.Minute), Quality: "high"},
	}
	got := NewestRXImages(records)
	want := []string{"rx-high", "rx-medium", "rx-low"}
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("record %d = %s, want %s", i, got[i].ID, want[i])
		}
	}
}
