package main

import (
	"regexp"
	"strings"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/i18n"
	"github.com/BH2VSQ/SATSSDV-Go/internal/radio/ptt"
)

type Settings struct {
	Language                  string            `json:"language"`
	RXProfile                 string            `json:"rxProfile"`
	TXProfile                 string            `json:"txProfile"`
	Quality                   string            `json:"quality"`
	Callsign                  string            `json:"callsign"`
	AX25SSID                  int               `json:"ax25Ssid"`
	Locator                   string            `json:"locator"`
	AudioSampleRate           int               `json:"audioSampleRate"`
	RXInputDevice             string            `json:"rxInputDevice"`
	TXOutputDevice            string            `json:"txOutputDevice"`
	TemplateBackgroundAssetID string            `json:"templateBackgroundAssetId,omitempty"`
	Waterfall                 WaterfallSettings `json:"waterfall"`
	PTT                       ptt.Config        `json:"ptt"`
	PTTLeadMS                 int               `json:"pttLeadMs"`
	PTTTailMS                 int               `json:"pttTailMs"`
}

type WaterfallSettings struct {
	MinFrequencyHz  int    `json:"minFrequencyHz"`
	MaxFrequencyHz  int    `json:"maxFrequencyHz"`
	MinDBFS         int    `json:"minDbfs"`
	MaxDBFS         int    `json:"maxDbfs"`
	RowsPerSecond   int    `json:"rowsPerSecond"`
	HistorySeconds  int    `json:"historySeconds"`
	BackgroundColor string `json:"backgroundColor"`
	LowColor        string `json:"lowColor"`
	MidColor        string `json:"midColor"`
	HighColor       string `json:"highColor"`
	PeakColor       string `json:"peakColor"`
}

func DefaultWaterfallSettings() WaterfallSettings {
	return WaterfallSettings{
		MinFrequencyHz: 0, MaxFrequencyHz: 5000,
		MinDBFS: -110, MaxDBFS: -20,
		RowsPerSecond: 15, HistorySeconds: 20,
		BackgroundColor: "#07111b", LowColor: "#153a78",
		MidColor: "#168fae", HighColor: "#39c6b0", PeakColor: "#f7d45b",
	}
}

var waterfallColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func normalizeWaterfallSettings(s WaterfallSettings) WaterfallSettings {
	d := DefaultWaterfallSettings()
	if s == (WaterfallSettings{}) {
		return d
	}
	if s.MinFrequencyHz >= 0 && s.MinFrequencyHz <= 48000 {
		d.MinFrequencyHz = s.MinFrequencyHz
	}
	if s.MaxFrequencyHz >= 100 && s.MaxFrequencyHz <= 48000 {
		d.MaxFrequencyHz = s.MaxFrequencyHz
	}
	if d.MinFrequencyHz >= 47900 {
		d.MinFrequencyHz = 47000
	}
	if d.MaxFrequencyHz <= d.MinFrequencyHz {
		d.MaxFrequencyHz = min(48000, d.MinFrequencyHz+1000)
	}
	if s.MinDBFS >= -120 && s.MinDBFS <= -10 {
		d.MinDBFS = s.MinDBFS
	}
	if s.MaxDBFS >= -90 && s.MaxDBFS <= 0 {
		d.MaxDBFS = s.MaxDBFS
	}
	if d.MaxDBFS <= d.MinDBFS+4 {
		d.MaxDBFS = min(0, d.MinDBFS+5)
	}
	if s.RowsPerSecond >= 1 && s.RowsPerSecond <= 40 {
		d.RowsPerSecond = s.RowsPerSecond
	}
	if s.HistorySeconds >= 5 && s.HistorySeconds <= 60 {
		d.HistorySeconds = s.HistorySeconds
	}
	if waterfallColorPattern.MatchString(s.BackgroundColor) {
		d.BackgroundColor = s.BackgroundColor
	}
	if waterfallColorPattern.MatchString(s.LowColor) {
		d.LowColor = s.LowColor
	}
	if waterfallColorPattern.MatchString(s.MidColor) {
		d.MidColor = s.MidColor
	}
	if waterfallColorPattern.MatchString(s.HighColor) {
		d.HighColor = s.HighColor
	}
	if waterfallColorPattern.MatchString(s.PeakColor) {
		d.PeakColor = s.PeakColor
	}
	return d
}

func normalizeSettings(s Settings) Settings {
	d := DefaultSettings()
	if s.Language != "" {
		d.Language = string(i18n.Normalize(s.Language))
	}
	if s.RXProfile == domain.AutoReceiveProfile {
		d.RXProfile = domain.AutoReceiveProfile
	} else if s.RXProfile != "" {
		if _, e := domain.ParseProfile(s.RXProfile); e == nil {
			d.RXProfile = s.RXProfile
		}
	}
	if s.TXProfile != "" {
		if _, e := domain.ParseProfile(s.TXProfile); e == nil {
			d.TXProfile = s.TXProfile
		}
	}
	if s.Quality == "low" || s.Quality == "medium" || s.Quality == "high" {
		d.Quality = s.Quality
	}
	if s.Callsign != "" {
		d.Callsign = s.Callsign
	}
	if s.AX25SSID >= 0 && s.AX25SSID <= 15 {
		d.AX25SSID = s.AX25SSID
	}
	if s.Locator != "" {
		d.Locator = s.Locator
	}
	if s.AudioSampleRate >= 8000 && s.AudioSampleRate <= 192000 {
		d.AudioSampleRate = s.AudioSampleRate
	}
	d.RXInputDevice = s.RXInputDevice
	d.TXOutputDevice = s.TXOutputDevice
	d.TemplateBackgroundAssetID = strings.TrimSpace(s.TemplateBackgroundAssetID)
	d.Waterfall = normalizeWaterfallSettings(s.Waterfall)
	if len(d.TemplateBackgroundAssetID) > 128 {
		d.TemplateBackgroundAssetID = ""
	}
	if s.PTT.Mode != "" {
		d.PTT = s.PTT
	}
	if s.PTTLeadMS >= 0 && s.PTTLeadMS <= 10000 {
		d.PTTLeadMS = s.PTTLeadMS
	}
	if s.PTTTailMS >= 0 && s.PTTTailMS <= 10000 {
		d.PTTTailMS = s.PTTTailMS
	}
	return d
}

func DefaultSettings() Settings {
	return Settings{Language: string(i18n.Chinese), RXProfile: string(domain.SSDVAX25), TXProfile: string(domain.SSDVAX25), Quality: "medium", Callsign: "N0CALL", Locator: "JJ00", AudioSampleRate: 48000, Waterfall: DefaultWaterfallSettings(), PTT: ptt.Config{Mode: ptt.ModeVOX, ActiveHigh: true}, PTTLeadMS: 300, PTTTailMS: 250}
}
