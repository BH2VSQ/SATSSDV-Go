package main

import (
	"encoding/json"
	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"testing"
)

func TestDefaultWaterfallSettings(t *testing.T) {
	w := DefaultSettings().Waterfall
	if w.MinFrequencyHz != 0 || w.MaxFrequencyHz != 5000 || w.MinDBFS != -110 || w.MaxDBFS != -20 {
		t.Fatalf("unexpected default waterfall range: %+v", w)
	}
	if w.RowsPerSecond != 15 || w.HistorySeconds != 20 {
		t.Fatalf("unexpected default waterfall timing: %+v", w)
	}
}

func TestNormalizeWaterfallSettingsClampsInvalidValues(t *testing.T) {
	w := normalizeWaterfallSettings(WaterfallSettings{
		MinFrequencyHz: -10, MaxFrequencyHz: 500, MinDBFS: -130, MaxDBFS: -140,
		RowsPerSecond: 100, HistorySeconds: 1,
		BackgroundColor: "red", LowColor: "#123456", MidColor: "#abc", HighColor: "#abcdef", PeakColor: "#ABCDEF",
	})
	if w.MinFrequencyHz != 0 || w.MaxFrequencyHz != 500 {
		t.Fatalf("frequency values not normalized: %+v", w)
	}
	if w.MinDBFS != -110 || w.MaxDBFS <= w.MinDBFS {
		t.Fatalf("dBFS range not normalized: %+v", w)
	}
	if w.RowsPerSecond != 15 || w.HistorySeconds != 20 {
		t.Fatalf("timing values not normalized: %+v", w)
	}
	if w.BackgroundColor != DefaultWaterfallSettings().BackgroundColor || w.LowColor != "#123456" || w.MidColor != DefaultWaterfallSettings().MidColor || w.HighColor != "#abcdef" || w.PeakColor != "#ABCDEF" {
		t.Fatalf("colors not normalized: %+v", w)
	}
}

func TestNormalizeSettingsMigratesMissingWaterfallSettings(t *testing.T) {
	got := normalizeSettings(Settings{Language: "zh-CN"})
	want := DefaultWaterfallSettings()
	if got.Waterfall != want {
		t.Fatalf("missing waterfall settings should use defaults: got %+v want %+v", got.Waterfall, want)
	}
}

func TestNormalizeWaterfallSettingsUsesSupportedNyquistCeiling(t *testing.T) {
	w := normalizeWaterfallSettings(WaterfallSettings{MinFrequencyHz: 47000, MaxFrequencyHz: 96000})
	if w.MinFrequencyHz != 47000 || w.MaxFrequencyHz != 48000 {
		t.Fatalf("frequency range must be clamped to 48 kHz Nyquist: %+v", w)
	}
	w = normalizeWaterfallSettings(WaterfallSettings{MinFrequencyHz: 48000, MaxFrequencyHz: 48000})
	if w.MaxFrequencyHz <= w.MinFrequencyHz || w.MaxFrequencyHz > 48000 {
		t.Fatalf("normalized frequency band must remain valid and within Nyquist: %+v", w)
	}
}

func TestWaterfallSettingsJSONRoundTrip(t *testing.T) {
	want := DefaultSettings()
	want.AX25SSID = 6
	want.Waterfall = WaterfallSettings{
		MinFrequencyHz: 1200, MaxFrequencyHz: 9600, MinDBFS: -100, MaxDBFS: -15,
		RowsPerSecond: 24, HistorySeconds: 45, BackgroundColor: "#010203",
		LowColor: "#102030", MidColor: "#405060", HighColor: "#708090", PeakColor: "#A0B0C0",
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Settings
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Waterfall != want.Waterfall {
		t.Fatalf("waterfall settings did not persist through JSON: got %+v want %+v", got.Waterfall, want.Waterfall)
	}
	if got.AX25SSID != want.AX25SSID {
		t.Fatalf("AX.25 SSID did not persist through JSON: got %d want %d", got.AX25SSID, want.AX25SSID)
	}
}

func TestNormalizeSettingsKeepsWaterfallSettings(t *testing.T) {
	want := DefaultWaterfallSettings()
	want.MinFrequencyHz = 1500
	want.MaxFrequencyHz = 10500
	want.RowsPerSecond = 27
	got := normalizeSettings(Settings{Waterfall: want})
	if got.Waterfall != want {
		t.Fatalf("normalizeSettings did not retain display preferences: got %+v want %+v", got.Waterfall, want)
	}
}

func TestNormalizeSettingsClampsAX25SSID(t *testing.T) {
	if got := normalizeSettings(Settings{AX25SSID: 7}).AX25SSID; got != 7 {
		t.Fatalf("valid AX.25 SSID not retained: got %d", got)
	}
	if got := normalizeSettings(Settings{AX25SSID: 16}).AX25SSID; got != 0 {
		t.Fatalf("out-of-range AX.25 SSID should fall back to zero, got %d", got)
	}
	if got := normalizeSettings(Settings{AX25SSID: -1}).AX25SSID; got != 0 {
		t.Fatalf("negative AX.25 SSID should fall back to zero, got %d", got)
	}
}

func TestNormalizeSettingsKeepsAutoReceiveMode(t *testing.T) {
	s := normalizeSettings(Settings{RXProfile: domain.AutoReceiveProfile, TXProfile: string(domain.SSDVAX25)})
	if s.RXProfile != domain.AutoReceiveProfile {
		t.Fatalf("RX profile=%q, want auto", s.RXProfile)
	}
}
