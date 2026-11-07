package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/gifmode"
	rxengine "github.com/BH2VSQ/SATSSDV-Go/internal/rx"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

func TestReceivedGIFIsDecodedAndArchivedAsAnimatedAlbumItem(t *testing.T) {
	first := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(first, first.Bounds(), image.NewUniform(color.RGBA{R: 22, G: 72, B: 128, A: 255}), image.Point{}, draw.Src)
	second := image.NewRGBA(first.Bounds())
	draw.Draw(second, second.Bounds(), first, first.Bounds().Min, draw.Src)
	draw.Draw(second, image.Rect(64, 64, 96, 96), image.NewUniform(color.RGBA{R: 240, G: 25, B: 32, A: 255}), image.Point{}, draw.Src)

	packets, _, err := gifmode.Encode([]gifmode.Frame{
		{Image: first, DelayCS: 10},
		{Image: second, DelayCS: 18},
	}, "N0CALL", 63, 4)
	if err != nil {
		t.Fatalf("encode test GIF stream: %v", err)
	}

	updates := make(chan rxengine.Update, len(packets)*2)
	manager := rxengine.NewManager(func(update rxengine.Update) { updates <- update })
	defer manager.Close()
	app := &App{dataDir: t.TempDir(), events: NewEventBroker(), rxManager: manager}

	for index, raw := range packets {
		packet := transport.ReceivedPacket{
			Profile: "ssdv-ccsds-1200", Codec: "SSDV-GIF", Raw: raw,
		}
		if !manager.Submit(packet) {
			t.Fatalf("submit GIF packet %d to RX manager", index)
		}
	}

	var finalUpdate rxengine.Update
	deadline := time.After(10 * time.Second)
	for finalUpdate.SessionID == "" || !finalUpdate.GIFEnd {
		select {
		case update := <-updates:
			if update.Error != "" {
				t.Fatalf("RX GIF decoder reported an error: %s", update.Error)
			}
			if update.GIFEnd {
				finalUpdate = update
			}
		case <-deadline:
			t.Fatal("RX did not emit a completed GIF frame/stream update")
		}
	}
	if finalUpdate.Codec != "SSDV-GIF" || finalUpdate.GIFFrame != 2 || finalUpdate.PNGBase64 == "" {
		t.Fatalf("unexpected final GIF update: %+v", finalUpdate)
	}
	// Exercise the same completion handler used by live RX. It must replace
	// the progressive PNG preview with the completed animated GIF before
	// publishing rx:image, so the native RX pane can start animation playback.
	app.handleRXImageUpdate(finalUpdate)

	latestPreview, err := app.GetLatestRXImageData()
	if err != nil {
		t.Fatalf("get latest RX preview after GIF completion: %v", err)
	}
	if !strings.HasPrefix(latestPreview, "data:image/gif;base64,") {
		t.Fatalf("completed GIF did not become the live RX preview; got %q", latestPreview[:min(len(latestPreview), 48)])
	}

	records, err := app.GetAlbum()
	if err != nil {
		t.Fatalf("load RX album: %v", err)
	}
	if len(records) != 1 || !strings.HasSuffix(strings.ToLower(records[0].ImagePath), ".gif") {
		t.Fatalf("expected one animated GIF album record, got %+v", records)
	}
	if records[0].Quality != "SSDV GIF · 2 frames" || records[0].Status != "complete" {
		t.Fatalf("unexpected GIF album metadata: %+v", records[0])
	}
	file, err := os.Open(records[0].ImagePath)
	if err != nil {
		t.Fatalf("open archived GIF: %v", err)
	}
	decoded, err := gif.DecodeAll(file)
	_ = file.Close()
	if err != nil {
		t.Fatalf("decode archived GIF: %v", err)
	}
	if len(decoded.Image) != 2 || len(decoded.Delay) != 2 || decoded.Delay[1] != 18 {
		t.Fatalf("archived GIF lost frames/delays: frames=%d delays=%v", len(decoded.Image), decoded.Delay)
	}
	previewBytes, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(latestPreview, "data:image/gif;base64,"))
	if err != nil {
		t.Fatalf("decode latest RX preview GIF data URL: %v", err)
	}
	previewAnimation, err := gif.DecodeAll(bytes.NewReader(previewBytes))
	if err != nil || len(previewAnimation.Image) != 2 {
		t.Fatalf("latest RX preview is not an animated two-frame GIF: frames=%d err=%v", len(previewAnimation.Image), err)
	}

	dataURL, err := app.GetAlbumImageData(records[0].ID)
	if err != nil {
		t.Fatalf("get archived GIF data URL: %v", err)
	}
	if !strings.HasPrefix(dataURL, "data:image/gif;base64,") {
		t.Fatalf("album returned wrong GIF content type: %q", dataURL[:min(len(dataURL), 48)])
	}
	if _, err := os.Stat(filepath.Join(app.dataDir, "album.json")); err != nil {
		t.Fatalf("GIF album index was not persisted: %v", err)
	}
}
