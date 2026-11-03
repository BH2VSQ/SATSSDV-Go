package i18n

import "testing"

func TestThreeLocalesProvidePTTKeys(t *testing.T) {
	for _, l := range Supported() {
		d := Dictionary(l)
		for _, k := range []string{"ptt.test", "ptt.stop", "ptt.autoRelease", "ptt.voxHint"} {
			if d[k] == "" {
				t.Fatalf("locale %s missing %s", l, k)
			}
		}
	}
}
func TestNormalizeLocale(t *testing.T) {
	if Normalize("en-US") != English || Normalize("ja") != Japanese || Normalize("zh-CN") != Chinese {
		t.Fatal("locale normalization failed")
	}
}

func TestAlbumNavigationLabelIsConciseAcrossLocales(t *testing.T) {
	want := map[Locale]string{Chinese: "相册", English: "Album", Japanese: "アルバム"}
	for locale, label := range want {
		if got := T(locale, "nav.album"); got != label {
			t.Fatalf("locale %s nav.album = %q, want %q", locale, got, label)
		}
	}
}

func TestSingleImageTXLabelsAcrossLocales(t *testing.T) {
	want := map[Locale]struct{ repeat, interval string }{
		Chinese:  {repeat: "循环发送", interval: "发送间隔（秒）"},
		English:  {repeat: "Repeat transmission", interval: "Interval (seconds)"},
		Japanese: {repeat: "繰り返し送信", interval: "送信間隔（秒）"},
	}
	for locale, expected := range want {
		d := Dictionary(locale)
		if d["tx.repeat"] != expected.repeat || d["tx.interval"] != expected.interval {
			t.Fatalf("locale %s has incorrect single-image repeat labels: %#v", locale, d)
		}
		if d["gallery.addQueue"] != "" {
			t.Fatalf("locale %s still exposes the removed gallery queue action", locale)
		}
	}
}

func TestTXStateLabelsAcrossLocales(t *testing.T) {
	keys := []string{"tx.start", "tx.stop", "tx.preparing", "tx.idle", "tx.active", "tx.done"}
	for _, locale := range Supported() {
		d := Dictionary(locale)
		for _, key := range keys {
			if d[key] == "" {
				t.Fatalf("locale %s missing TX state label %s", locale, key)
			}
		}
	}
}

func TestTemplatePresetManagementLabelsAcrossLocales(t *testing.T) {
	keys := []string{"template.addPreset", "template.duplicatePreset", "template.deletePreset", "template.presetName", "template.savePreset", "template.choosePreset", "template.limitReached", "template.addedText", "template.addedImage", "template.backgroundSet", "action.failed"}
	for _, locale := range Supported() {
		d := Dictionary(locale)
		for _, key := range keys {
			if d[key] == "" {
				t.Fatalf("locale %s missing template preset label %s", locale, key)
			}
		}
	}
}

func TestSettingsDialogLabelsAreLocalizedAcrossAllLocales(t *testing.T) {
	want := map[Locale]map[string]string{
		Chinese: {
			"settings.configuration": "应用程序设置", "audio.rxDevice": "RX 输入设备", "audio.txDevice": "TX 输出设备",
			"audio.default": "系统默认", "audio.defaultDevice": "默认", "audio.ready": "音频就绪", "audio.off": "音频未连接", "audio.refresh": "刷新音频设备",
			"audio.sampleRate": "音频采样率", "station.title": "本站身份信息", "station.callsign": "本站呼号",
		},
		English: {
			"settings.configuration": "Application Settings", "audio.rxDevice": "RX input device", "audio.txDevice": "TX output device",
			"audio.default": "System default", "audio.defaultDevice": "Default", "audio.ready": "Audio ready", "audio.off": "Audio off", "audio.refresh": "Refresh audio devices",
			"audio.sampleRate": "Audio sample rate", "station.title": "Station identity", "station.callsign": "Station callsign",
		},
		Japanese: {
			"settings.configuration": "アプリケーション設定", "audio.rxDevice": "RX 入力デバイス", "audio.txDevice": "TX 出力デバイス",
			"audio.default": "システム既定", "audio.defaultDevice": "既定", "audio.ready": "オーディオ準備完了", "audio.off": "オーディオ未接続", "audio.refresh": "オーディオデバイスを更新",
			"audio.sampleRate": "オーディオサンプルレート", "station.title": "局情報", "station.callsign": "自局コールサイン",
		},
	}
	for locale, expected := range want {
		d := Dictionary(locale)
		for key, value := range expected {
			if d[key] != value {
				t.Fatalf("locale %s key %s = %q, want %q", locale, key, d[key], value)
			}
		}
	}
}

func TestWaterfallDisplaySettingsAreLocalizedAcrossLocales(t *testing.T) {
	keys := []string{
		"settings.tabDisplay", "waterfall.title", "waterfall.minFrequency", "waterfall.maxFrequency",
		"waterfall.minDb", "waterfall.maxDb", "waterfall.rowsPerSecond", "waterfall.history",
		"waterfall.colors", "waterfall.background", "waterfall.lowColor", "waterfall.midColor",
		"waterfall.highColor", "waterfall.peakColor", "waterfall.hint", "waterfall.reset",
		"waterfall.rxInput", "waterfall.txOutput", "waterfall.rxWaiting", "waterfall.txWaiting",
		"waterfall.frequencyAxis", "waterfall.rowsUnit", "waterfall.secondsUnit", "template.unselected",
	}
	for _, locale := range Supported() {
		d := Dictionary(locale)
		for _, key := range keys {
			if d[key] == "" {
				t.Fatalf("locale %s missing display setting label %s", locale, key)
			}
		}
	}
}
