package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	rxengine "github.com/BH2VSQ/SATSSDV-Go/internal/rx"
)

func TestRPCInvokeGetProfilesAndSettings(t *testing.T) {
	app := NewApp()
	profiles, err := invokeApp(app, rpcRequest{Method: "GetProfiles"})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := profiles.([]domain.Profile)
	if !ok || len(items) < 8 {
		t.Fatalf("unexpected profiles result: %T %#v", profiles, profiles)
	}
	settings, err := invokeApp(app, rpcRequest{Method: "GetSettings"})
	if err != nil {
		t.Fatal(err)
	}
	if settings.(Settings).AudioSampleRate != 48000 {
		t.Fatalf("unexpected default settings: %+v", settings)
	}
}

func TestRPCInvokeTypedArguments(t *testing.T) {
	app := NewApp()
	arg, _ := json.Marshal("en")
	result, err := invokeApp(app, rpcRequest{Method: "GetLocaleDictionary", Args: []json.RawMessage{arg}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.(map[string]string); !ok {
		t.Fatalf("unexpected locale dictionary: %T", result)
	}
}

func TestRPCRejectsUnknownAndWrongArgumentCount(t *testing.T) {
	app := NewApp()
	if _, err := invokeApp(app, rpcRequest{Method: "NoSuchMethod"}); err == nil {
		t.Fatal("expected unknown method error")
	}
	if _, err := invokeApp(app, rpcRequest{Method: "GetLocaleDictionary"}); err == nil {
		t.Fatal("expected argument count error")
	}
}

func TestEventBrokerRetainsBoundedHistory(t *testing.T) {
	broker := NewEventBroker()
	for i := 0; i < 520; i++ {
		broker.Publish("test", i)
	}
	events, next, reset := broker.Since(0)
	if !reset || len(events) != 512 || next != 520 || events[0].Sequence != 9 {
		t.Fatalf("unexpected event retention: reset=%t count=%d next=%d first=%d", reset, len(events), next, events[0].Sequence)
	}
	newer, next, reset := broker.Since(518)
	if reset || next != 520 || len(newer) != 2 || newer[0].Sequence != 519 {
		t.Fatalf("unexpected incremental event read: reset=%t count=%d next=%d", reset, len(newer), next)
	}
}

func TestRPCRejectsNonUIAppMethods(t *testing.T) {
	app := NewApp()
	if _, err := invokeApp(app, rpcRequest{Method: "TransmitPCM"}); err == nil {
		t.Fatal("TransmitPCM must not be exposed through the native UI RPC")
	}
	if _, err := invokeApp(app, rpcRequest{Method: "ExportAlbumToDirectory"}); err == nil {
		t.Fatal("interactive file-dialog wrappers must not be exposed over RPC")
	}
}

func TestRXImageEventsDoNotRetainBinaryPayloads(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.handleRXImageUpdate(rxengine.Update{
		Profile:          "ssdv-qpsk-1600",
		PNGBase64:        base64.StdEncoding.EncodeToString(encoded.Bytes()),
		PacketDataBase64: base64.StdEncoding.EncodeToString([]byte("packet-payload")),
	})
	events, _, _ := app.events.Since(0)
	if len(events) != 1 {
		t.Fatalf("expected one image event, got %d", len(events))
	}
	update, ok := events[0].Payload.(rxengine.Update)
	if !ok {
		t.Fatalf("unexpected event payload type %T", events[0].Payload)
	}
	if !update.HasPreview {
		t.Fatal("image preview flag was not retained")
	}
	if update.PNGBase64 != "" || update.PacketDataBase64 != "" {
		t.Fatal("binary image or packet bytes must not be stored in the event history")
	}
	latest, err := app.GetLatestRXImageData()
	if err != nil {
		t.Fatal(err)
	}
	if latest == "" {
		t.Fatal("latest preview cache was not accessible through GetLatestRXImageData")
	}
}
