package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BH2VSQ/SATSSDV-Go/internal/album"
	rxengine "github.com/BH2VSQ/SATSSDV-Go/internal/rx"
)

// collapseRXSessionRecords merges any duplicate RX rows belonging to the same
// receive session. A session can first create a packet-only album row and later
// receive a decoded preview; both must remain one visible album entry.
func collapseRXSessionRecords(index *album.Index, update rxengine.Update) []string {
	if index == nil || update.SessionID == "" {
		return nil
	}
	matched := make([]album.Record, 0, 2)
	other := make([]album.Record, 0, len(index.Records))
	for _, record := range index.Records {
		if record.Type == "RX" && record.Profile == update.Profile && record.Callsign == update.Callsign &&
			record.ImageID == uint16(update.ImageID) && record.SessionID == update.SessionID {
			matched = append(matched, record)
		} else {
			other = append(other, record)
		}
	}
	if len(matched) < 2 {
		return nil
	}
	// Keep the latest row, then fill any media path it lacks from older rows.
	merged := matched[len(matched)-1]
	var duplicates []string
	for i := len(matched) - 2; i >= 0; i-- {
		r := matched[i]
		if merged.ImagePath == "" {
			merged.ImagePath = r.ImagePath
		} else if r.ImagePath != "" && r.ImagePath != merged.ImagePath {
			duplicates = append(duplicates, r.ImagePath)
		}
		if merged.PacketPath == "" {
			merged.PacketPath = r.PacketPath
		} else if r.PacketPath != "" && r.PacketPath != merged.PacketPath {
			duplicates = append(duplicates, r.PacketPath)
		}
		if r.PacketsReceived > merged.PacketsReceived {
			merged.PacketsReceived = r.PacketsReceived
		}
		if r.PacketsExpected > merged.PacketsExpected {
			merged.PacketsExpected = r.PacketsExpected
		}
		if merged.ImageWidth == 0 {
			merged.ImageWidth = r.ImageWidth
		}
		if merged.ImageHeight == 0 {
			merged.ImageHeight = r.ImageHeight
		}
		if merged.Quality == "" {
			merged.Quality = r.Quality
		}
		if merged.Status != "complete" && r.Status == "complete" {
			merged.Status = r.Status
		}
		if r.CreatedAt.After(merged.CreatedAt) {
			merged.CreatedAt = r.CreatedAt
		}
	}
	index.Records = append(other, merged)
	return duplicates
}

func removeUnreferencedAlbumFiles(index album.Index, dataDir string, candidates []string) {
	if len(candidates) == 0 {
		return
	}
	used := make(map[string]struct{}, len(index.Records)*2)
	for _, record := range index.Records {
		if record.ImagePath != "" {
			if abs, err := filepath.Abs(record.ImagePath); err == nil {
				used[abs] = struct{}{}
			}
		}
		if record.PacketPath != "" {
			if abs, err := filepath.Abs(record.PacketPath); err == nil {
				used[abs] = struct{}{}
			}
		}
	}
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, exists := used[abs]; exists {
			continue
		}
		base := filepath.Join(dataDir, "album")
		root, rootErr := filepath.Abs(base)
		if rootErr != nil {
			continue
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			_ = os.Remove(abs)
		}
	}
}
