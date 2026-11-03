package album

import "time"

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
	PacketsReceived int       `json:"packetsReceived,omitempty"`
	PacketsExpected int       `json:"packetsExpected,omitempty"`
	Quality         string    `json:"quality,omitempty"`
	Status          string    `json:"status"`
}
type Index struct {
	Records []Record `json:"records"`
}
