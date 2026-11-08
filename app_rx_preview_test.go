package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	rxengine "github.com/BH2VSQ/SATSSDV-Go/internal/rx"
)

func TestRXImageEventContainsItsOwnProgressivePreviewSnapshot(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 96, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 96; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 2), G: uint8(y * 3), B: uint8(x ^ y), A: 255})
		}
	}
	var pngBuffer bytes.Buffer
	if err := png.Encode(&pngBuffer, img); err != nil {
		t.Fatal(err)
	}
	app := &App{events: NewEventBroker()}
	app.handleRXImageUpdate(rxengine.Update{
		Profile: "ssdv-qpsk-1600", Codec: "SSDV", Callsign: "N0CALL", ImageID: 7,
		PacketsReceived: 3, PNGBase64: base64.StdEncoding.EncodeToString(pngBuffer.Bytes()), HasPreview: true,
	})
	events, _, _ := app.events.Since(0)
	if len(events) != 1 {
		t.Fatalf("got %d events, want one RX image event", len(events))
	}
	update, ok := events[0].Payload.(rxengine.Update)
	if !ok {
		t.Fatalf("unexpected event payload type %T", events[0].Payload)
	}
	if update.PNGBase64 != "" {
		t.Fatal("full-resolution PNG unexpectedly retained in bounded event history")
	}
	if !strings.HasPrefix(update.PreviewDataURL, "data:image/jpeg;base64,") {
		t.Fatalf("event is missing its immutable live preview: %q", update.PreviewDataURL[:min(48, len(update.PreviewDataURL))])
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(update.PreviewDataURL, "data:image/jpeg;base64,"))
	if err != nil || len(data) == 0 {
		t.Fatalf("event preview JPEG is invalid: len=%d err=%v", len(data), err)
	}
	if latest, err := app.GetLatestRXImageData(); err != nil || !strings.HasPrefix(latest, "data:image/png;base64,") {
		t.Fatalf("full-resolution latest preview was not preserved: %q err=%v", latest[:min(48, len(latest))], err)
	}
}
