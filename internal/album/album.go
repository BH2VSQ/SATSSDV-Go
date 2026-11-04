package album

import (
	"sort"
	"time"
)

type Record struct {
	ID              string    `json:"id"`
	Type            string    `json:"type"` // RX or TX
	ImagePath       string    `json:"imagePath,omitempty"`
	PacketPath      string    `json:"packetPath,omitempty"`
	ImageWidth      int       `json:"imageWidth,omitempty"`
	ImageHeight     int       `json:"imageHeight,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	Profile         string    `json:"profile"`
	Callsign        string    `json:"callsign,omitempty"`
	ImageID         uint16    `json:"imageId,omitempty"`
	SessionID       string    `json:"sessionId,omitempty"`
	PacketsReceived int       `json:"packetsReceived,omitempty"`
	PacketsExpected int       `json:"packetsExpected,omitempty"`
	Quality         string    `json:"quality,omitempty"`
	Status          string    `json:"status"`
}

type Index struct {
	Records []Record `json:"records"`
}

// SortNewestFirst orders RX and TX records together by their timestamp. IDs
// provide deterministic ordering when timestamps have the same precision.
func SortNewestFirst(records []Record) {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].ID > records[j].ID
		}
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
}

// NewestRXImages returns RX records with an image path in newest-first order.
// Resolution/quality is deliberately not filtered: callers must use the most
// recent received image whether it is 320x240, 448x336, or 640x480.
func NewestRXImages(records []Record) []Record {
	ordered := append([]Record(nil), records...)
	SortNewestFirst(ordered)
	out := make([]Record, 0, len(ordered))
	for _, record := range ordered {
		if record.Type == "RX" && record.ImagePath != "" {
			out = append(out, record)
		}
	}
	return out
}
