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
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
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
	latestGIF, err := app.GetLatestRXGIFData()
	if err != nil {
		t.Fatalf("get dedicated latest RX GIF data: %v", err)
	}
	if !strings.HasPrefix(latestGIF, "data:image/gif;base64,") {
		t.Fatalf("dedicated RX GIF endpoint returned %q", latestGIF[:min(len(latestGIF), 48)])
	}
	if latestGIF != latestPreview {
		t.Fatal("dedicated GIF endpoint and generic latest preview disagree after GIF completion")
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

func TestInterruptedGIFIsAutomaticallyArchivedAsPartialAnimation(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < frame.Bounds().Dy(); y++ {
		for x := 0; x < frame.Bounds().Dx(); x++ {
			frame.SetRGBA(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 239), B: 80, A: 255})
		}
	}
	packets, _, err := gifmode.Encode([]gifmode.Frame{{Image: frame, DelayCS: 12}}, "N0CALL", 119, 4)
	if err != nil {
		t.Fatalf("encode partial GIF fixture: %v", err)
	}

	app := &App{dataDir: t.TempDir(), events: NewEventBroker()}
	manager := rxengine.NewManager(app.handleRXImageUpdate)
	app.rxManager = manager
	defer manager.Close()

	dataPackets := 0
	for _, raw := range packets {
		parsed, parseErr := ssdv.ParsePacket(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if parsed.GIFControl && parsed.GIFFrameEnd {
			continue // Simulate loss of the frame-close and stream-end markers.
		}
		if !parsed.GIFControl {
			dataPackets++
		}
		if !manager.Submit(transport.ReceivedPacket{Profile: "ssdv-ccsds-1200", Codec: "SSDV-GIF", Raw: raw}) {
			t.Fatal("submit partial GIF packet")
		}
	}
	if dataPackets < 2 {
		t.Fatalf("fixture unexpectedly has only %d data packets", dataPackets)
	}

	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		records, albumErr := app.GetAlbum()
		if albumErr != nil {
			t.Fatalf("read album while waiting for partial GIF checkpoint: %v", albumErr)
		}
		if len(records) > 0 {
			record := records[0]
			if record.Status != "partial" {
				t.Fatalf("interrupted GIF album item status=%q, want partial", record.Status)
			}
			if !strings.HasSuffix(strings.ToLower(record.ImagePath), ".gif") {
				t.Fatalf("partial RX animation was not saved as GIF: %s", record.ImagePath)
			}
			file, openErr := os.Open(record.ImagePath)
			if openErr != nil {
				t.Fatalf("open partial GIF album item: %v", openErr)
			}
			decoded, decodeErr := gif.DecodeAll(file)
			_ = file.Close()
			if decodeErr != nil || len(decoded.Image) == 0 {
				t.Fatalf("partial album GIF is not playable: frames=%d err=%v", len(decoded.Image), decodeErr)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("interrupted GIF was not automatically checkpointed into the album")
}
