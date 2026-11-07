package i18n

import "strings"

type Locale string

const (
	Chinese  Locale = "zh-CN"
	English  Locale = "en"
	Japanese Locale = "ja-JP"
)

var supported = []Locale{Chinese, English, Japanese}
var messages = map[Locale]map[string]string{
	Chinese:  {"app.title": "SATSSDV-Go", "nav.workbench": "工作台", "nav.album": "相册", "nav.settings": "设置", "rx.title": "接收 RX", "tx.title": "发送 TX", "rx.wait": "等待接收音频", "tx.ready": "发射就绪", "mode": "通信模式", "quality": "图片质量", "quality.low": "低质量 320×240", "quality.medium": "中质量 448×336", "quality.high": "高质量 640×480", "ptt.title": "PTT 发射控制", "ptt.mode": "控制方式", "ptt.vox": "VOX 电台自动触发", "ptt.hamlib": "Hamlib Net Rigctl", "ptt.rts": "串口 RTS", "ptt.dtr": "串口 DTR", "ptt.port": "串口", "ptt.refresh": "刷新串口", "ptt.address": "rigctld 地址", "ptt.activeHigh": "高电平有效", "ptt.activeLow": "低电平有效", "ptt.test": "测试 PTT", "ptt.stop": "停止 PTT 测试", "ptt.idle": "待机", "ptt.active": "PTT 测试中", "ptt.remaining": "剩余 {seconds} 秒", "ptt.autoRelease": "最长 15 秒，后端自动释放", "ptt.voxHint": "VOX 由电台检测音频自动发射，不需要软件 PTT 测试。", "ptt.error": "PTT 操作失败", "audio.title": "音频设备", "rx.spectrum": "RX 频谱", "tx.spectrum": "TX 频谱", "tx.repeat": "循环发送", "tx.interval": "发送间隔（秒）", "template.title": "发射模板", "gallery.title": "图片图库", "album.title": "历史相册", "save": "保存", "cancel": "取消", "template.addText": "＋ 文字", "template.addRX": "＋ 上次 RX", "template.addImage": "＋ 图像", "template.content": "文字内容", "template.x": "水平位置 %", "template.y": "垂直位置 %", "template.width": "宽度 %", "template.height": "高度 %", "template.fontSize": "字号 px", "template.outlineWidth": "描边宽度 px", "template.color": "文字颜色", "template.bold": "粗体", "template.visible": "可见", "template.outline": "描边", "template.deleteComponent": "删除所选组件", "template.qsoFields": "本次 QSO 信息", "template.hisCall": "对方呼号", "template.qth": "QTH", "template.report": "RST / 报告", "template.message": "消息", "template.imageSource": "图片组件来源", "template.outlineColor": "描边颜色"},
	English:  {"app.title": "SATSSDV-Go", "nav.workbench": "Workbench", "nav.album": "Album", "nav.settings": "Settings", "rx.title": "Receiver RX", "tx.title": "Transmitter TX", "rx.wait": "Waiting for receive audio", "tx.ready": "Ready to transmit", "mode": "Transport profile", "quality": "Image quality", "quality.low": "Low 320×240", "quality.medium": "Medium 448×336", "quality.high": "High 640×480", "ptt.title": "PTT control", "ptt.mode": "PTT method", "ptt.vox": "VOX radio keying", "ptt.hamlib": "Hamlib Net Rigctl", "ptt.rts": "Serial RTS", "ptt.dtr": "Serial DTR", "ptt.port": "Serial port", "ptt.refresh": "Refresh ports", "ptt.address": "rigctld address", "ptt.activeHigh": "Active high", "ptt.activeLow": "Active low", "ptt.test": "Test PTT", "ptt.stop": "Stop PTT test", "ptt.idle": "Idle", "ptt.active": "PTT test active", "ptt.remaining": "{seconds}s remaining", "ptt.autoRelease": "Maximum 15 seconds; backend auto-release", "ptt.voxHint": "VOX keys the radio from audio and does not require a software PTT test.", "ptt.error": "PTT operation failed", "audio.title": "Audio devices", "rx.spectrum": "RX Spectrum", "tx.spectrum": "TX Spectrum", "tx.repeat": "Repeat transmission", "tx.interval": "Interval (seconds)", "template.title": "TX Templates", "gallery.title": "Image Library", "album.title": "History Album", "save": "Save", "cancel": "Cancel", "template.addText": "＋ Text", "template.addRX": "＋ Last RX", "template.addImage": "＋ Image", "template.content": "Text content", "template.x": "Horizontal %", "template.y": "Vertical %", "template.width": "Width %", "template.height": "Height %", "template.fontSize": "Font px", "template.outlineWidth": "Outline px", "template.color": "Text color", "template.bold": "Bold", "template.visible": "Visible", "template.outline": "Outline", "template.deleteComponent": "Delete selected component", "template.qsoFields": "QSO fields", "template.hisCall": "His callsign", "template.qth": "QTH", "template.report": "RST / Report", "template.message": "Message", "template.imageSource": "Image component source", "template.outlineColor": "Outline color"},
	Japanese: {"app.title": "SATSSDV-Go", "nav.workbench": "ワークベンチ", "nav.album": "アルバム", "nav.settings": "設定", "rx.title": "受信 RX", "tx.title": "送信 TX", "rx.wait": "受信音声を待機中", "tx.ready": "送信準備完了", "mode": "通信モード", "quality": "画像品質", "quality.low": "低 320×240", "quality.medium": "中 448×336", "quality.high": "高 640×480", "ptt.title": "PTT 制御", "ptt.mode": "制御方式", "ptt.vox": "VOX 無線機側で制御", "ptt.hamlib": "Hamlib Net Rigctl", "ptt.rts": "シリアル RTS", "ptt.dtr": "シリアル DTR", "ptt.port": "シリアルポート", "ptt.refresh": "ポート更新", "ptt.address": "rigctld アドレス", "ptt.activeHigh": "High 有効", "ptt.activeLow": "Low 有効", "ptt.test": "PTT テスト", "ptt.stop": "PTT テスト停止", "ptt.idle": "待機中", "ptt.active": "PTT テスト中", "ptt.remaining": "残り {seconds} 秒", "ptt.autoRelease": "最大 15 秒、バックエンドが自動解除", "ptt.voxHint": "VOX は音声を検出して送信するため、ソフトウェア PTT テストは不要です。", "ptt.error": "PTT 操作エラー", "audio.title": "オーディオデバイス", "rx.spectrum": "RX スペクトラム", "tx.spectrum": "TX スペクトラム", "tx.repeat": "繰り返し送信", "tx.interval": "送信間隔（秒）", "template.title": "送信テンプレート", "gallery.title": "画像ライブラリ", "album.title": "送受信履歴", "save": "保存", "cancel": "キャンセル", "template.addText": "＋ テキスト", "template.addRX": "＋ 最終 RX", "template.addImage": "＋ 画像", "template.content": "テキスト内容", "template.x": "水平位置 %", "template.y": "垂直位置 %", "template.width": "幅 %", "template.height": "高さ %", "template.fontSize": "文字サイズ px", "template.outlineWidth": "縁取り幅 px", "template.color": "文字色", "template.bold": "太字", "template.visible": "表示", "template.outline": "縁取り", "template.deleteComponent": "選択したコンポーネントを削除", "template.qsoFields": "今回の QSO 情報", "template.hisCall": "相手局コールサイン", "template.qth": "QTH", "template.report": "RST / レポート", "template.message": "メッセージ", "template.imageSource": "画像コンポーネントのソース", "template.outlineColor": "縁取り色"},
}

func Supported() []Locale { return append([]Locale(nil), supported...) }
func Normalize(v string) Locale {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "en", "en-us", "en-gb":
		return English
	case "ja", "ja-jp":
		return Japanese
	default:
		return Chinese
	}
}
func T(locale Locale, key string) string {
	if d, ok := messages[locale]; ok {
		if v, ok := d[key]; ok {
			return v
		}
	}
	if v, ok := messages[English][key]; ok {
		return v
	}
	return key
}
func Dictionary(locale Locale) map[string]string {
	d := messages[Normalize(string(locale))]
	out := make(map[string]string, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}

func init() {
	messages[Chinese]["tx.start"] = "开始发射"
	messages[Chinese]["tx.stop"] = "停止发射"
	messages[Chinese]["tx.preparing"] = "正在准备…"
	messages[Chinese]["tx.idle"] = "TX IDLE"
	messages[Chinese]["tx.active"] = "TX ACTIVE"
	messages[Chinese]["tx.done"] = "发射完成"
	messages[English]["tx.start"] = "Start TX"
	messages[English]["tx.stop"] = "Stop TX"
	messages[English]["tx.preparing"] = "Preparing…"
	messages[English]["tx.idle"] = "TX IDLE"
	messages[English]["tx.active"] = "TX ACTIVE"
	messages[English]["tx.done"] = "Transmission complete"
	messages[Japanese]["tx.start"] = "送信開始"
	messages[Japanese]["tx.stop"] = "送信停止"
	messages[Japanese]["tx.preparing"] = "準備中…"
	messages[Japanese]["tx.idle"] = "TX IDLE"
	messages[Japanese]["tx.active"] = "TX ACTIVE"
	messages[Japanese]["tx.done"] = "送信完了"
	messages[Chinese]["template.componentsTitle"] = "模板组件"
	messages[Chinese]["template.componentProperties"] = "组件属性"
	messages[Chinese]["template.saveSlot"] = "保存当前槽位"
	messages[Chinese]["template.presetHint"] = ""
	messages[Chinese]["template.myCall"] = "本台呼号"
	messages[Chinese]["template.locator"] = "网格定位"
	messages[Chinese]["tx.transmission"] = "发射设置"
	messages[Chinese]["gallery.open"] = "打开图片图库"
	messages[Chinese]["gallery.subtitle"] = "图片保存在本机用户配置目录，模板通过资源 ID 引用"
	messages[Chinese]["gallery.import"] = "导入图片"
	messages[Chinese]["gallery.loading"] = "正在读取图库…"
	messages[Chinese]["gallery.empty"] = "图库为空。导入图片后可设为模板底图或添加为组件图层。"
	messages[Chinese]["gallery.setBackground"] = "设为模板底图"
	messages[Chinese]["gallery.addLayer"] = "添加为图层"
	messages[Chinese]["gallery.delete"] = "删除图片"
	messages[Chinese]["gallery.background"] = "模板底图"
	messages[Chinese]["gallery.noBackground"] = "未设置，可从图库选择底图"
	messages[Chinese]["gallery.clearBackground"] = "清除底图"
	messages[Chinese]["album.subtitle"] = "历史接收与发送图像，自动记录时间戳"
	messages[Chinese]["album.all"] = "全部图像"
	messages[Chinese]["album.rx"] = "仅接收 RX"
	messages[Chinese]["album.tx"] = "仅发送 TX"
	messages[Chinese]["album.empty"] = "相册暂无图像。接收过程中也会自动保存可用预览和原始包。"
	messages[Chinese]["album.noPreview"] = "尚无可解码预览，原始接收包已保存。"
	messages[Chinese]["album.footnote"] = "点击缩略图查看大图；可导出带时间戳的 PNG 或删除记录。"
	messages[Chinese]["album.export"] = "导出文件"
	messages[Chinese]["album.exportAll"] = "导出相册到文件夹"
	messages[Chinese]["album.exported"] = "已导出 {count} 个文件"
	messages[Chinese]["album.delete"] = "删除记录"

	messages[English]["template.componentsTitle"] = "Template Components"
	messages[English]["template.componentProperties"] = "Component Properties"
	messages[English]["template.saveSlot"] = "Save Current Slot"
	messages[English]["template.presetHint"] = ""
	messages[English]["template.myCall"] = "My Callsign"
	messages[English]["template.locator"] = "Locator"
	messages[English]["tx.transmission"] = "Transmission Settings"
	messages[English]["gallery.open"] = "Open Image Library"
	messages[English]["gallery.subtitle"] = "Images are stored locally and referenced by asset ID."
	messages[English]["gallery.import"] = "Import Images"
	messages[English]["gallery.loading"] = "Loading image library…"
	messages[English]["gallery.empty"] = "The library is empty. Import images to set a template background or add an image layer."
	messages[English]["gallery.setBackground"] = "Set as Template Background"
	messages[English]["gallery.addLayer"] = "Add as Layer"
	messages[English]["gallery.delete"] = "Delete Image"
	messages[English]["gallery.background"] = "Template Background"
	messages[English]["gallery.noBackground"] = "Not set. Choose a background from the image library."
	messages[English]["gallery.clearBackground"] = "Clear Background"
	messages[English]["album.subtitle"] = "Timestamped RX and TX image history"
	messages[English]["album.all"] = "All Images"
	messages[English]["album.rx"] = "RX Only"
	messages[English]["album.tx"] = "TX Only"
	messages[English]["album.empty"] = "No images yet. Available previews and raw packets are also saved during RX."
	messages[English]["album.noPreview"] = "No decoded preview yet; received packets are saved."
	messages[English]["album.footnote"] = "Click a thumbnail to preview. Export timestamped PNGs or delete records."
	messages[English]["album.export"] = "Export Files"
	messages[English]["album.exportAll"] = "Export Album to Folder"
	messages[English]["album.exported"] = "Exported {count} files"
	messages[English]["album.delete"] = "Delete Record"

	messages[Japanese]["template.componentsTitle"] = "テンプレートコンポーネント"
	messages[Japanese]["template.componentProperties"] = "コンポーネント設定"
	messages[Japanese]["template.saveSlot"] = "選択スロットを保存"
	messages[Japanese]["template.presetHint"] = ""
	messages[Japanese]["template.myCall"] = "自局コールサイン"
	messages[Japanese]["template.locator"] = "グリッドロケーター"
	messages[Japanese]["tx.transmission"] = "送信設定"
	messages[Japanese]["gallery.open"] = "画像ライブラリを開く"
	messages[Japanese]["gallery.subtitle"] = "画像はローカルに保存され、アセット ID で参照されます。"
	messages[Japanese]["gallery.import"] = "画像をインポート"
	messages[Japanese]["gallery.loading"] = "画像ライブラリを読み込み中…"
	messages[Japanese]["gallery.empty"] = "ライブラリは空です。画像をインポートしてテンプレート背景または画像レイヤーとして使用できます。"
	messages[Japanese]["gallery.setBackground"] = "テンプレート背景に設定"
	messages[Japanese]["gallery.addLayer"] = "レイヤーとして追加"
	messages[Japanese]["gallery.delete"] = "画像を削除"
	messages[Japanese]["gallery.background"] = "テンプレート背景"
	messages[Japanese]["gallery.noBackground"] = "未設定。画像ライブラリから背景を選択してください。"
	messages[Japanese]["gallery.clearBackground"] = "背景をクリア"
	messages[Japanese]["album.subtitle"] = "RX/TX 画像履歴とタイムスタンプ"
	messages[Japanese]["album.all"] = "すべての画像"
	messages[Japanese]["album.rx"] = "RX のみ"
	messages[Japanese]["album.tx"] = "TX のみ"
	messages[Japanese]["album.empty"] = "画像はありません。受信中のプレビューと受信パケットも自動保存されます。"
	messages[Japanese]["album.noPreview"] = "デコード済みプレビューはありません。受信パケットは保存済みです。"
	messages[Japanese]["album.footnote"] = "サムネイルをクリックしてプレビュー。タイムスタンプ付き PNG の保存や記録削除ができます。"
	messages[Japanese]["album.export"] = "ファイルを保存"
	messages[Japanese]["album.exportAll"] = "アルバムをフォルダーにエクスポート"
	messages[Japanese]["album.exported"] = "{count} 個のファイルをエクスポートしました"
	messages[Japanese]["album.delete"] = "記録を削除"

	// Dynamic TX template presets and visible operation feedback.
	for locale, values := range map[Locale]map[string]string{
		Chinese: {
			"template.addPreset": "＋ 新建预设", "template.duplicatePreset": "复制当前", "template.deletePreset": "删除当前",
			"template.presetName": "预设名称", "template.savePreset": "保存预设", "template.choosePreset": "选择发射模板",
			"template.newName": "模板 {number}", "template.duplicateName": "{name} 副本", "template.limitReached": "最多支持 32 个模板预设",
			"template.deleteConfirm": "确定删除模板“{name}”吗？", "template.saved": "模板预设已保存", "template.created": "已创建模板预设：{name}",
			"template.deleted": "已删除模板预设：{name}", "template.addedText": "已添加文字组件", "template.addedImage": "已添加图片组件",
			"template.backgroundSet": "已设置模板底图：{name}", "template.backgroundCleared": "已清除模板底图", "template.noSelection": "请先选择一个模板预设",
			"template.componentLimit": "每个模板最多支持 64 个组件", "template.componentDeleted": "已删除所选组件", "template.packImport": "导入模板包", "template.packExport": "导出模板包", "template.packImported": "已导入 {count} 个模板", "template.packExported": "模板包已导出", "action.failed": "操作失败：{error}",
		},
		English: {
			"template.addPreset": "＋ New Preset", "template.duplicatePreset": "Duplicate", "template.deletePreset": "Delete",
			"template.presetName": "Preset name", "template.savePreset": "Save Preset", "template.choosePreset": "Choose TX preset",
			"template.newName": "Template {number}", "template.duplicateName": "{name} Copy", "template.limitReached": "A maximum of 32 template presets is supported",
			"template.deleteConfirm": "Delete template ‘{name}’?", "template.saved": "Template preset saved", "template.created": "Created template preset: {name}",
			"template.deleted": "Deleted template preset: {name}", "template.addedText": "Text component added", "template.addedImage": "Image component added",
			"template.backgroundSet": "Template background set: {name}", "template.backgroundCleared": "Template background cleared", "template.noSelection": "Select a template preset first",
			"template.componentLimit": "A template can contain up to 64 components", "template.componentDeleted": "Selected component deleted", "template.packImport": "Import Pack", "template.packExport": "Export Pack", "template.packImported": "Imported {count} templates", "template.packExported": "Template pack exported", "action.failed": "Operation failed: {error}",
		},
		Japanese: {
			"template.addPreset": "＋ 新規プリセット", "template.duplicatePreset": "複製", "template.deletePreset": "削除",
			"template.presetName": "プリセット名", "template.savePreset": "プリセットを保存", "template.choosePreset": "送信プリセットを選択",
			"template.newName": "テンプレート {number}", "template.duplicateName": "{name} のコピー", "template.limitReached": "テンプレートは最大 32 件まで登録できます",
			"template.deleteConfirm": "テンプレート「{name}」を削除しますか？", "template.saved": "テンプレートを保存しました", "template.created": "テンプレートを作成しました: {name}",
			"template.deleted": "テンプレートを削除しました: {name}", "template.addedText": "テキストを追加しました", "template.addedImage": "画像コンポーネントを追加しました",
			"template.backgroundSet": "背景を設定しました: {name}", "template.backgroundCleared": "背景をクリアしました", "template.noSelection": "先にテンプレートを選択してください",
			"template.componentLimit": "1 つのテンプレートには最大 64 個のコンポーネントを追加できます", "template.componentDeleted": "コンポーネントを削除しました", "template.packImport": "テンプレートをインポート", "template.packExport": "テンプレートをエクスポート", "template.packImported": "{count} 個のテンプレートをインポートしました", "template.packExported": "テンプレートをエクスポートしました", "action.failed": "操作に失敗しました: {error}",
		},
	} {
		for key, value := range values {
			messages[locale][key] = value
		}
	}
	// Settings dialog labels and runtime device-status messages.
	for locale, values := range map[Locale]map[string]string{
		Chinese: {
			"settings.configuration": "应用程序设置", "settings.saved": "设置已保存",
			"audio.rxDevice": "RX 输入设备", "audio.txDevice": "TX 输出设备", "audio.default": "系统默认",
			"audio.refresh": "刷新音频设备", "audio.defaultDevice": "默认", "audio.ready": "音频就绪", "audio.off": "音频未连接", "audio.fullDuplexHint": "共享输入和输出流，支持软件全双工。",
			"audio.sampleRate": "音频采样率", "audio.duplexDetail": "输入和输出采用独立共享音频流，TX 播放期间 RX 保持采集。",
			"audio.deviceCount": "发现 {inputs} 个输入设备和 {outputs} 个输出设备",
			"ptt.noPorts":       "未发现可用串口", "ptt.portCount": "已发现 {count} 个串口",
			"station.title": "本站身份信息", "station.callsign": "本站呼号", "station.locator": "网格定位",
		},
		English: {
			"settings.configuration": "Application Settings", "settings.saved": "Settings saved",
			"audio.rxDevice": "RX input device", "audio.txDevice": "TX output device", "audio.default": "System default",
			"audio.refresh": "Refresh audio devices", "audio.defaultDevice": "Default", "audio.ready": "Audio ready", "audio.off": "Audio off", "audio.fullDuplexHint": "Shared input and output streams enable software full duplex.",
			"audio.sampleRate": "Audio sample rate", "audio.duplexDetail": "RX capture remains active while TX playback uses an independent shared stream.",
			"audio.deviceCount": "Found {inputs} input devices and {outputs} output devices",
			"ptt.noPorts":       "No available serial ports found", "ptt.portCount": "Found {count} serial ports",
			"station.title": "Station identity", "station.callsign": "Station callsign", "station.locator": "Maidenhead locator",
		},
		Japanese: {
			"settings.configuration": "アプリケーション設定", "settings.saved": "設定を保存しました",
			"audio.rxDevice": "RX 入力デバイス", "audio.txDevice": "TX 出力デバイス", "audio.default": "システム既定",
			"audio.refresh": "オーディオデバイスを更新", "audio.defaultDevice": "既定", "audio.ready": "オーディオ準備完了", "audio.off": "オーディオ未接続", "audio.fullDuplexHint": "共有入力・出力ストリームでソフトウェア全二重に対応します。",
			"audio.sampleRate": "オーディオサンプルレート", "audio.duplexDetail": "TX 再生中も独立した共有ストリームで RX 受信を継続します。",
			"audio.deviceCount": "入力デバイス {inputs} 台、出力デバイス {outputs} 台を検出",
			"ptt.noPorts":       "使用可能なシリアルポートがありません", "ptt.portCount": "シリアルポート {count} 個を検出",
			"station.title": "局情報", "station.callsign": "自局コールサイン", "station.locator": "グリッドロケーター",
		},
	} {
		for key, value := range values {
			messages[locale][key] = value
		}
	}
}

func init() {
	messages[Chinese]["settings.tabGeneral"] = "设备与电台"
	messages[Chinese]["settings.tabDisplay"] = "显示设置"
	messages[Chinese]["waterfall.title"] = "音频瀑布图参数"
	messages[Chinese]["waterfall.minFrequency"] = "最低频率"
	messages[Chinese]["waterfall.maxFrequency"] = "最高频率"
	messages[Chinese]["waterfall.minDb"] = "最低电平"
	messages[Chinese]["waterfall.maxDb"] = "最高电平"
	messages[Chinese]["waterfall.rowsPerSecond"] = "滚动速度"
	messages[Chinese]["waterfall.history"] = "历史时长"
	messages[Chinese]["waterfall.colors"] = "瀑布图颜色"
	messages[Chinese]["waterfall.background"] = "背景色"
	messages[Chinese]["waterfall.lowColor"] = "弱信号"
	messages[Chinese]["waterfall.midColor"] = "中等信号"
	messages[Chinese]["waterfall.highColor"] = "强信号"
	messages[Chinese]["waterfall.peakColor"] = "峰值信号"
	messages[Chinese]["waterfall.hint"] = "横轴为频率，纵轴为时间；可用频率范围受当前音频采样率限制。颜色表示信号强度，修改后实时预览并在保存时应用。"
	messages[Chinese]["waterfall.reset"] = "恢复默认显示设置"
	messages[Chinese]["waterfall.rxInput"] = "输入 · 音频瀑布图"
	messages[Chinese]["waterfall.txOutput"] = "输出 · 音频瀑布图"
	messages[Chinese]["waterfall.rxWaiting"] = "等待 RX 音频"
	messages[Chinese]["waterfall.txWaiting"] = "等待 TX 音频"
	messages[Chinese]["waterfall.frequencyAxis"] = "横轴：频率 · 纵轴：时间"
	messages[Chinese]["waterfall.rowsUnit"] = "行/秒"
	messages[Chinese]["waterfall.secondsUnit"] = "秒"
	messages[Chinese]["template.unselected"] = "未选择"

	messages[English]["settings.tabGeneral"] = "Devices & Radio"
	messages[English]["settings.tabDisplay"] = "Display"
	messages[English]["waterfall.title"] = "Audio waterfall parameters"
	messages[English]["waterfall.minFrequency"] = "Minimum frequency"
	messages[English]["waterfall.maxFrequency"] = "Maximum frequency"
	messages[English]["waterfall.minDb"] = "Minimum level"
	messages[English]["waterfall.maxDb"] = "Maximum level"
	messages[English]["waterfall.rowsPerSecond"] = "Scroll speed"
	messages[English]["waterfall.history"] = "History duration"
	messages[English]["waterfall.colors"] = "Waterfall colors"
	messages[English]["waterfall.background"] = "Background"
	messages[English]["waterfall.lowColor"] = "Weak signal"
	messages[English]["waterfall.midColor"] = "Medium signal"
	messages[English]["waterfall.highColor"] = "Strong signal"
	messages[English]["waterfall.peakColor"] = "Peak signal"
	messages[English]["waterfall.hint"] = "Frequency runs horizontally and time vertically. The active audio sample rate limits the available frequency range. Colors map signal strength; changes preview live and apply when saved."
	messages[English]["waterfall.reset"] = "Restore default display settings"
	messages[English]["waterfall.rxInput"] = "INPUT · AUDIO WATERFALL"
	messages[English]["waterfall.txOutput"] = "OUTPUT · AUDIO WATERFALL"
	messages[English]["waterfall.rxWaiting"] = "Waiting for RX audio"
	messages[English]["waterfall.txWaiting"] = "Waiting for TX audio"
	messages[English]["waterfall.frequencyAxis"] = "Horizontal: frequency · Vertical: time"
	messages[English]["waterfall.rowsUnit"] = "rows/s"
	messages[English]["waterfall.secondsUnit"] = "s"
	messages[English]["template.unselected"] = "Not selected"

	messages[Japanese]["settings.tabGeneral"] = "デバイスと無線機"
	messages[Japanese]["settings.tabDisplay"] = "表示設定"
	messages[Japanese]["waterfall.title"] = "オーディオ・ウォーターフォール設定"
	messages[Japanese]["waterfall.minFrequency"] = "最低周波数"
	messages[Japanese]["waterfall.maxFrequency"] = "最高周波数"
	messages[Japanese]["waterfall.minDb"] = "最小レベル"
	messages[Japanese]["waterfall.maxDb"] = "最大レベル"
	messages[Japanese]["waterfall.rowsPerSecond"] = "スクロール速度"
	messages[Japanese]["waterfall.history"] = "履歴時間"
	messages[Japanese]["waterfall.colors"] = "ウォーターフォールの色"
	messages[Japanese]["waterfall.background"] = "背景色"
	messages[Japanese]["waterfall.lowColor"] = "弱い信号"
	messages[Japanese]["waterfall.midColor"] = "中程度の信号"
	messages[Japanese]["waterfall.highColor"] = "強い信号"
	messages[Japanese]["waterfall.peakColor"] = "ピーク信号"
	messages[Japanese]["waterfall.hint"] = "横軸は周波数、縦軸は時間です。表示できる周波数範囲は現在のサンプルレートに制限されます。色は信号強度を表し、変更はリアルタイムでプレビューされ、保存時に適用されます。"
	messages[Japanese]["waterfall.reset"] = "表示設定を初期値に戻す"
	messages[Japanese]["waterfall.rxInput"] = "入力 · オーディオ・ウォーターフォール"
	messages[Japanese]["waterfall.txOutput"] = "出力 · オーディオ・ウォーターフォール"
	messages[Japanese]["waterfall.rxWaiting"] = "RX 音声を待機中"
	messages[Japanese]["waterfall.txWaiting"] = "TX 音声を待機中"
	messages[Japanese]["waterfall.frequencyAxis"] = "横軸：周波数 · 縦軸：時間"
	messages[Japanese]["waterfall.rowsUnit"] = "行/秒"
	messages[Japanese]["waterfall.secondsUnit"] = "秒"
	messages[Japanese]["template.unselected"] = "未選択"
}
