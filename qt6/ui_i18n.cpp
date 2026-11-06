#include "ui_i18n.h"

#include <QAbstractButton>
#include <QAction>
#include <QComboBox>
#include <QGroupBox>
#include <QHeaderView>
#include <QLabel>
#include <QLineEdit>
#include <QMainWindow>
#include <QTabBar>
#include <QTabWidget>
#include <QTableWidget>
#include <QToolButton>
#include <QVariant>
#include <QVector>

#include <QHash>

namespace {
QString g_locale = QStringLiteral("zh-CN");
constexpr int kSourceRole = Qt::UserRole + 0x531;

struct Translation { const char *zh; const char *en; const char *ja; };
const Translation kTranslations[] = {
    {" 副本", " Copy", " のコピー"},
    {"工作台", "Workbench", "ワークベンチ"}, {"相册", "Album", "アルバム"},
    {"设备设置", "Device settings", "デバイス設定"}, {"等待图像", "Waiting for image", "画像を待機中"},
    {"后端启动失败", "Backend startup failed", "バックエンドの起動に失敗しました"},
    {"Go 后端不可用", "Go backend unavailable", "Go バックエンドを利用できません"},
    {"Go 后端启动失败：%1", "Go backend failed to start: %1", "Go バックエンドの起動に失敗しました: %1"},
    {"音频：正在连接", "Audio: connecting", "オーディオ: 接続中"},
    {"接收 RX", "Receive RX", "受信 RX"}, {"发送 TX", "Transmit TX", "送信 TX"},
    {"RX 待机 · 等待接收音频", "RX idle · waiting for receive audio", "RX 待機 · 受信音声を待機中"},
    {"启动 RX", "Start RX", "RX 開始"}, {"停止 RX", "Stop RX", "RX 停止"},
    {"RX 频谱 / 瀑布图", "RX Spectrum / Waterfall", "RX スペクトラム / ウォーターフォール"},
    {"TX 频谱 / 瀑布图", "TX Spectrum / Waterfall", "TX スペクトラム / ウォーターフォール"},
    {"输入 · 音频瀑布图", "Input · Audio waterfall", "入力 · オーディオウォーターフォール"},
    {"输出 · 音频瀑布图", "Output · Audio waterfall", "出力 · オーディオウォーターフォール"},
    {"等待 RX 音频", "Waiting for RX audio", "RX 音声を待機中"}, {"等待 TX 音频", "Waiting for TX audio", "TX 音声を待機中"},
    {"横轴：频率 · 纵轴：时间", "X: frequency · Y: time", "横軸: 周波数 · 縦軸: 時間"},
    {"图片质量", "Image quality", "画像品質"}, {"低质量 320×240", "Low 320×240", "低 320×240"},
    {"中质量 448×336", "Medium 448×336", "中 448×336"}, {"高质量 640×480", "High 640×480", "高 640×480"},
    {"循环发送", "Repeat transmission", "繰り返し送信"}, {"开始发射", "Start TX", "送信開始"},
    {"停止发射", "Stop TX", "送信停止"}, {"正在准备…", "Preparing…", "準備中…"},
    {"1500 Hz 测试音", "1500 Hz test tone", "1500 Hz テストトーン"},
    {"停止测试音", "Stop test tone", "テストトーン停止"},
    {"已启动 1500 Hz 测试音", "1500 Hz test tone started", "1500 Hz テストトーンを開始しました"},
    {"发射设置", "Transmission settings", "送信設定"}, {"AX.25 SSID", "AX.25 SSID", "AX.25 SSID"},
    {"预设名称", "Preset name", "プリセット名"}, {"保存预设", "Save preset", "プリセットを保存"},
    {"发射模板", "TX template", "送信テンプレート"}, {"＋ 新建预设", "＋ New preset", "＋ 新規プリセット"},
    {"复制当前", "Duplicate", "複製"}, {"删除当前", "Delete", "削除"},
    {"背景图", "Background", "背景画像"}, {"导入模板包", "Import template pack", "テンプレートパックをインポート"},
    {"导出模板包", "Export template pack", "テンプレートパックをエクスポート"},
    {"预设名称不能为空", "Preset name cannot be empty", "プリセット名を入力してください"},
    {"模板预设已保存", "Template preset saved", "テンプレートプリセットを保存しました"},
    {"最多支持 32 个模板预设", "Up to 32 template presets are supported", "テンプレートプリセットは最大 32 件です"},
    {"新建预设", "New preset", "新規プリセット"}, {"已创建空白模板预设", "Blank template preset created", "空のテンプレートプリセットを作成しました"},
    {"当前模板已复制", "Template duplicated", "テンプレートを複製しました"},
    {"至少需要保留一个模板预设", "At least one template preset must remain", "テンプレートプリセットを 1 件以上残してください"},
    {"删除模板预设", "Delete template preset", "テンプレートプリセットを削除"},
    {"确定删除当前模板预设吗？", "Delete the current template preset?", "現在のテンプレートプリセットを削除しますか?"},
    {"模板预设已删除", "Template preset deleted", "テンプレートプリセットを削除しました"},
    {"本次 QSO 信息", "QSO information", "今回の QSO 情報"}, {"本台呼号", "My callsign", "自局コールサイン"},
    {"对方呼号", "Other station callsign", "相手局コールサイン"}, {"QTH", "QTH", "QTH"},
    {"网格定位", "Maidenhead locator", "グリッドロケーター"}, {"RST / 报告", "RST / Report", "RST / レポート"},
    {"消息", "Message", "メッセージ"}, {"模板组件 / 属性", "Template components / Properties", "テンプレートコンポーネント / プロパティ"},
    {"模板组件", "Template components", "テンプレートコンポーネント"}, {"＋ 文字", "＋ Text", "＋ テキスト"},
    {"＋ 图库图像", "＋ Library image", "＋ ライブラリ画像"}, {"＋ 上次接收图像", "＋ Last RX image", "＋ 最後の RX 画像"},
    {"清除底图", "Clear background", "背景をクリア"}, {"文字内容 / {MYCALL} / {LOCATOR}", "Text / {MYCALL} / {LOCATOR}", "テキスト / {MYCALL} / {LOCATOR}"},
    {"文字内容", "Text content", "テキスト内容"}, {"图库图像", "Library image", "ライブラリ画像"},
    {"图像缩放", "Image scale", "画像倍率"}, {"文字字号", "Font size", "文字サイズ"},
    {"描边宽度", "Outline width", "縁取り幅"}, {"文字颜色", "Text color", "文字色"},
    {"描边颜色", "Outline color", "縁取り色"}, {"粗体", "Bold", "太字"},
    {"显示", "Visible", "表示"}, {"描边", "Outline", "縁取り"},
    {"删除组件", "Delete component", "コンポーネントを削除"},
    {"可直接拖动画布中的组件移动位置，拖动右下角控制点调整字号或图像比例。", "Drag components on the canvas; drag the lower-right handle to resize text or images.", "キャンバス上でコンポーネントをドラッグして移動し、右下のハンドルで文字サイズや画像倍率を調整します。"},
    {"协议编解码仍在开发验证中，当前版本不应直接用于空中发射。", "Protocol codecs are still under development and validation. Do not use this build for on-air transmission.", "プロトコルコーデックは開発・検証中です。このビルドを実際の送信に使用しないでください。"},
    {"全部图像", "All images", "すべての画像"}, {"仅接收 RX", "RX only", "RX のみ"}, {"仅发送 TX", "TX only", "TX のみ"},
    {"刷新", "Refresh", "更新"}, {"导入图库图片", "Import library image", "ライブラリ画像をインポート"},
    {"导出相册到文件夹", "Export album to folder", "アルバムをフォルダーにエクスポート"},
    {"点击缩略图预览；每张图像可单独导出或删除记录。", "Click a thumbnail to preview. Export or delete each image individually.", "サムネイルをクリックしてプレビューし、画像ごとにエクスポートまたは記録を削除できます。"},
    {"Go 后端通过本机回环 RPC 连接；RX / TX 频谱分别显示。", "Go backend connected through loopback RPC; RX and TX spectra are separate.", "Go バックエンドはローカル RPC で接続され、RX / TX スペクトラムは個別に表示されます。"},
    {"模式列表不可用", "Profile list unavailable", "モード一覧を利用できません"},
    {"读取模式列表失败：", "Failed to load profiles: ", "モード一覧の読み込みに失敗しました: "},
    {"初始化失败：%1", "Initialization failed: %1", "初期化に失敗しました: %1"},
    {"读取设置失败：", "Failed to load settings: ", "設定の読み込みに失敗しました: "},
    {"设置读取失败，使用默认值：%1", "Could not load settings; using defaults: %1", "設定を読み込めません。既定値を使用します: %1"},
    {"刷新接收图像预览失败：", "Failed to refresh RX image preview: ", "RX 画像プレビューの更新に失敗しました: "},
    {"音频后端异常", "Audio backend error", "オーディオバックエンドエラー"},
    {"已连接", "Connected", "接続済み"}, {"未初始化", "Not initialized", "未初期化"},
    {"运行", "Running", "実行中"}, {"待机", "Idle", "待機中"},
    {"选择一个组件后即可修改属性。", "Select a component to edit its properties.", "コンポーネントを選択するとプロパティを編集できます。"},
    {"文字组件：支持变量替换、字号、颜色、粗体和描边。拖动画布可移动位置。", "Text: variables, size, color, bold and outline are supported. Drag on the canvas to move.", "テキスト: 変数、サイズ、色、太字、縁取りに対応。キャンバスをドラッグして移動できます。"},
    {"图像组件：可调整等比例缩放、可见性和描边。拖动画布可移动位置。", "Image: adjust proportional scale, visibility and outline. Drag on the canvas to move.", "画像: 比率を維持した倍率、表示、縁取りを調整できます。キャンバスをドラッグして移動します。"},
    {"请先选择发射模板", "Select a TX template first", "先に送信テンプレートを選択してください"},
    {"单个模板最多支持 64 个组件", "A template supports up to 64 components", "テンプレートには最大 64 個のコンポーネントを追加できます"},
    {"新文本", "New text", "新しいテキスト"}, {"图像图库", "Image library", "画像ライブラリ"},
    {"选择图像资源", "Select image asset", "画像アセットを選択"}, {"导入图片…", "Import image…", "画像をインポート…"},
    {"选择‘设为底图’设置共享背景；选择‘添加图层’将图片放入当前 TX 模板。", "Choose ‘Set background’ for the shared background or ‘Add layer’ to add it to the current TX template.", "「背景に設定」で共有背景にし、「レイヤー追加」で現在の TX テンプレートに画像を追加します。"},
    {"加载预览…", "Loading preview…", "プレビューを読み込み中…"}, {"预览不可用", "Preview unavailable", "プレビューを利用できません"},
    {"设为底图", "Set as background", "背景に設定"}, {"添加图层", "Add layer", "レイヤーを追加"},
    {"模板共享底图已更新", "Shared template background updated", "共有テンプレート背景を更新しました"},
    {"图库暂无图片。请使用右上方‘导入图片’添加图像资源。", "The library is empty. Use ‘Import images’ to add assets.", "ライブラリは空です。右上の「画像をインポート」から追加してください。"},
    {"正在读取图像…", "Loading image…", "画像を読み込み中…"}, {"图像预览不可用", "Image preview unavailable", "画像プレビューを利用できません"},
    {"无法解码相册图像", "Unable to decode album image", "アルバム画像をデコードできません"},
    {"图像预览", "Image preview", "画像プレビュー"}, {"导出文件", "Export file", "ファイルをエクスポート"},
    {"删除记录", "Delete record", "記録を削除"}, {"导出图像", "Export image", "画像をエクスポート"},
    {"相册图像已导出", "Album image exported", "アルバム画像をエクスポートしました"},
    {"删除图像记录", "Delete image record", "画像記録を削除"}, {"确定删除所选相册记录吗？", "Delete the selected album record?", "選択したアルバム記録を削除しますか?"},
    {"相册暂无图像。", "No images in the album yet.", "アルバムに画像はありません。"},
    {"完成", "Complete", "完了"}, {"接收中", "Receiving", "受信中"},
    {"接收已启动", "RX started", "RX を開始しました"}, {"接收已停止", "RX stopped", "RX を停止しました"},
    {"RX 已启动", "RX started", "RX を開始しました"}, {"RX 已停止", "RX stopped", "RX を停止しました"},
    {"下一次循环发射：%1 秒", "Next repeat transmission in %1 s", "次の繰り返し送信まで %1 秒"},
    {"无法生成发送图像", "Unable to render transmit image", "送信画像を生成できません"},
    {"无法将发送模板编码为 PNG", "Unable to encode TX template as PNG", "TX テンプレートを PNG にエンコードできません"},
    {"已提交图像发射任务", "Image transmission job submitted", "画像送信ジョブを送信しました"},
    {"保存设置失败：", "Failed to save settings: ", "設定の保存に失敗しました: "},
    {"设备与电台设置", "Device and radio settings", "デバイスと無線機の設定"},
    {"系统默认", "System default", "システム既定"}, {"音频设备列表不可用：", "Audio device list unavailable: ", "オーディオデバイス一覧を利用できません: "},
    {"串口 RTS", "Serial RTS", "シリアル RTS"}, {"串口 DTR", "Serial DTR", "シリアル DTR"},
    {"串口列表不可用：", "Serial port list unavailable: ", "シリアルポート一覧を利用できません: "},
    {"刷新串口", "Refresh serial ports", "シリアルポートを更新"}, {"PTT 高电平有效", "PTT active high", "PTT High 有効"},
    {"本站网格", "Station locator", "自局グリッドロケーター"}, {"界面语言", "Interface language", "表示言語"},
    {"音频采样率", "Audio sample rate", "オーディオサンプルレート"}, {"RX 音频输入", "RX audio input", "RX オーディオ入力"},
    {"TX 音频输出", "TX audio output", "TX オーディオ出力"}, {"PTT 控制方式", "PTT method", "PTT 制御方式"},
    {"rigctld 地址", "rigctld address", "rigctld アドレス"}, {"串口设备", "Serial device", "シリアルデバイス"},
    {"PTT 发射前延时", "PTT lead time", "PTT 送信前遅延"}, {"PTT 发射后延时", "PTT tail time", "PTT 送信後遅延"},
    {"最低频率", "Minimum frequency", "最低周波数"}, {"最高频率", "Maximum frequency", "最高周波数"},
    {"最低电平", "Minimum level", "最低レベル"}, {"最高电平", "Maximum level", "最高レベル"},
    {"滚动速度", "Scroll rate", "スクロール速度"}, {"历史时长", "History duration", "履歴時間"},
    {"瀑布图颜色", "Waterfall colors", "ウォーターフォールの色"}, {"背景色", "Background", "背景色"},
    {"弱信号", "Weak signal", "弱信号"}, {"中等信号", "Medium signal", "中信号"},
    {"强信号", "Strong signal", "強信号"}, {"峰值信号", "Peak signal", "ピーク信号"},
    {"横轴为频率，纵轴为时间；颜色代表信号强度。修改参数后点击保存应用到 RX 与 TX 两个瀑布图。", "Frequency is on X, time on Y, and color indicates signal strength. Save to apply to RX and TX waterfalls.", "横軸は周波数、縦軸は時間、色は信号強度です。保存すると RX / TX の両方に適用されます。"},
    {"恢复默认显示设置", "Restore display defaults", "表示設定を既定値に戻す"},
    {"设备与电台", "Devices and radio", "デバイスと無線機"}, {"显示设置", "Display settings", "表示設定"},
    {"保存配置并测试 PTT", "Save settings and test PTT", "設定を保存して PTT をテスト"},
    {"停止 PTT 测试", "Stop PTT test", "PTT テストを停止"},
    {"PTT 测试已启动", "PTT test started", "PTT テストを開始しました"},
    {"PTT 测试已停止", "PTT test stopped", "PTT テストを停止しました"},
    {"设备设置已保存", "Device settings saved", "デバイス設定を保存しました"},
    {"导入图库图片", "Import library image", "ライブラリ画像をインポート"},
    {"导出完成", "Export complete", "エクスポート完了"}, {"已导出 %1 条记录", "Exported %1 records", "%1 件の記録をエクスポートしました"},
    {"模板包已导出：", "Template pack exported: ", "テンプレートパックをエクスポートしました: "},
    {"导入完成", "Import complete", "インポート完了"}, {"已导入 %1 个模板", "Imported %1 templates", "%1 個のテンプレートをインポートしました"},
    {"模板可视化编辑器", "Template visual editor", "テンプレートビジュアルエディター"},
    {"模板名称", "Template name", "テンプレート名"}, {"＋ 文字组件", "＋ Text component", "＋ テキストコンポーネント"},
    {"上移图层", "Move layer up", "レイヤーを上へ"}, {"下移图层", "Move layer down", "レイヤーを下へ"},
    {"5% 网格吸附", "5% grid snapping", "5% グリッドスナップ"}, {"删除选中组件", "Delete selected component", "選択したコンポーネントを削除"},
    {"文字组件属性", "Text component properties", "テキストコンポーネントのプロパティ"},
    {"字号", "Font size", "文字サイズ"}, {"颜色", "Color", "色"},
    {"画布操作：拖动组件移动；拖动右下角控制点调整文字字号或图像比例；选中后按 Delete 删除。", "Canvas: drag to move; drag the lower-right handle to resize; press Delete to remove the selected component.", "キャンバス: ドラッグで移動、右下ハンドルでサイズ変更、Delete キーで削除します。"},
    {"图库为空", "Image library is empty", "画像ライブラリは空です"},
    {"请先在图像图库页面导入图片，再添加图像组件。", "Import an image in the library before adding an image component.", "画像コンポーネントを追加する前に、画像ライブラリへ画像をインポートしてください。"},
    {"添加图像组件", "Add image component", "画像コンポーネントを追加"}, {"选择图库图像", "Choose library image", "ライブラリ画像を選択"},
    {"模板位置必须为 0–100%，字号必须为 6–160 px。", "Template position must be 0–100%; font size must be 6–160 px.", "テンプレート位置は 0–100%、文字サイズは 6–160 px にしてください。"},
    {"模板名称不能为空。", "Template name cannot be empty.", "テンプレート名を空にできません。"},
    {"模板布局已保存", "Template layout saved", "テンプレートレイアウトを保存しました"}, {"错误：", "Error: ", "エラー: "},
    {"VOX 由电台检测音频自动发射，不需要软件 PTT 测试。", "VOX is keyed by the radio from detected audio; a software PTT test is not needed.", "VOX は無線機側が音声を検出して送信するため、ソフトウェア PTT テストは不要です。"},
    {"日本語", "日本語", "日本語"}, {"简体中文", "简体中文", "简体中文"}, {"English", "English", "English"},
    {"新文本", "New text", "新しいテキスト"}, {"本次 QSO 信息", "QSO information", "今回の QSO 情報"},
    {"RX 已启动", "RX started", "RX を開始しました"}, {"RX 已停止", "RX stopped", "RX を停止しました"},
    {"自动识别", "Auto detect", "自動検出"}, {"图像预览", "Image preview", "画像プレビュー"},
    {"RECEIVE\n接收 RX", "RECEIVE\nReceive RX", "RECEIVE\n受信 RX"},
    {"TRANSMIT\n发送 TX", "TRANSMIT\nTransmit TX", "TRANSMIT\n送信 TX"},
    {"修改已更新到当前预设，点击‘保存预设’后写入配置。", "Changes are applied to the current preset. Select ‘Save Preset’ to write them to settings.", "変更は現在のプリセットに適用されています。「プリセットを保存」を押して設定に書き込んでください。"},
    {"RX · 上次接收图像", "RX · Last received image", "RX · 最後に受信した画像"},
    {"%1 · %2 个组件 · 拖动右侧预览画布中的图层可调整位置", "%1 · %2 components · Drag layers on the preview canvas to reposition them", "%1 · %2 コンポーネント · プレビューキャンバスでドラッグして位置を調整"},
    {"文字组件", "Text component", "テキストコンポーネント"},
    {"RX / TX HISTORY   历史相册", "RX / TX HISTORY   Image album", "RX / TX HISTORY   画像アルバム"},
    {"质量：%1", "Quality: %1", "品質: %1"},
    {"读取相册失败：", "Failed to load album: ", "アルバムの読み込みに失敗しました: "},
    {"自动识别 · %1", "Auto detect · %1", "自動検出 · %1"},
    {"%1 bins · 峰值 %2 dBFS @ %3 Hz", "%1 bins · Peak %2 dBFS @ %3 Hz", "%1 ビン · ピーク %2 dBFS @ %3 Hz"},
    {"音频：%1 · %2 Hz · RX %3 / TX %4", "Audio: %1 · %2 Hz · RX %3 / TX %4", "オーディオ: %1 · %2 Hz · RX %3 / TX %4"},
    {"本站呼号", "Station callsign", "自局コールサイン"},
    {"语言设置已保存；部分界面文本将在下次版本完善。", "Language saved; remaining UI strings will be localized in a later update.", "言語設定を保存しました。未翻訳の UI は今後の更新で対応します。"},
    {"副本", " Copy", " のコピー"}, {" 秒", " s", " 秒"}, {" 行/秒", " rows/s", " 行/秒"},
    {"640 × 480 · 拖动以移动 · 右下角调整大小", "640 × 480 · Drag to move · Resize with the lower-right handle", "640 × 480 · ドラッグで移動 · 右下のハンドルでサイズ変更"},
    {"LEVEL  -∞ dBFS       PEAK  ---       横轴：频率 · 纵轴：时间", "LEVEL  -∞ dBFS       PEAK  ---       X: frequency · Y: time", "LEVEL  -∞ dBFS       PEAK  ---       横軸: 周波数 · 縦軸: 時間"},
    {"已导入图库：", "Imported to image library: ", "画像ライブラリにインポートしました: "},
    {"拖动组件移动；拖动右下角控制点调整字号或图像比例；选中后按 Delete 删除", "Drag components to move; drag the lower-right handle to resize text or images; select and press Delete to remove.", "コンポーネントをドラッグして移動し、右下のハンドルでサイズを変更します。選択して Delete キーで削除します。"},
    {"读取相册失败：%1", "Failed to load album: %1", "アルバムの読み込みに失敗しました: %1"},
    {"语言偏好已保存：%1", "Language preference saved: %1", "言語設定を保存しました: %1"},
    {"无法加载语言词典，使用内置翻译。", "Unable to load locale dictionary; using built-in translations.", "言語辞書を読み込めません。組み込み翻訳を使用します。"},
};

QString sourceText(const QString &value) {
    for (const auto &entry : kTranslations) {
        if (value == QString::fromUtf8(entry.zh) || value == QString::fromUtf8(entry.en) || value == QString::fromUtf8(entry.ja))
            return QString::fromUtf8(entry.zh);
    }
    return value;
}
QString translate(const QString &source) {
    for (const auto &entry : kTranslations) {
        if (source == QString::fromUtf8(entry.zh)) {
            if (g_locale == QStringLiteral("en")) return QString::fromUtf8(entry.en);
            if (g_locale.startsWith(QStringLiteral("ja"))) return QString::fromUtf8(entry.ja);
            return QString::fromUtf8(entry.zh);
        }
    }
    return source;
}
QString translatedProperty(QObject *object, const char *propertyName, const QString &current) {
    QString source = object->property(propertyName).toString();
    if (source.isEmpty()) {
        source = sourceText(current);
        object->setProperty(propertyName, source);
    }
    return translate(source);
}
}

QString uiText(const QString &source) { return translate(sourceText(source)); }
void setUiLocale(const QString &locale) { g_locale = locale.startsWith(QStringLiteral("ja")) ? QStringLiteral("ja-JP") : locale == QStringLiteral("en") ? QStringLiteral("en") : QStringLiteral("zh-CN"); }

void translateWidgetTree(QWidget *root) {
    if (!root) return;
    root->setWindowTitle(translatedProperty(root, "satssdvI18nTitle", root->windowTitle()));
    const auto widgets = root->findChildren<QWidget *>();
    for (QWidget *widget : widgets) {
        if (auto *label = qobject_cast<QLabel *>(widget)) {
            label->setText(translatedProperty(label, "satssdvI18nText", label->text()));
        } else if (auto *button = qobject_cast<QAbstractButton *>(widget)) {
            button->setText(translatedProperty(button, "satssdvI18nText", button->text()));
        } else if (auto *group = qobject_cast<QGroupBox *>(widget)) {
            group->setTitle(translatedProperty(group, "satssdvI18nText", group->title()));
        }
        const QString toolTip = widget->toolTip();
        if (!toolTip.isEmpty()) widget->setToolTip(translatedProperty(widget, "satssdvI18nToolTip", toolTip));
        if (auto *line = qobject_cast<QLineEdit *>(widget)) {
            const QString placeholder = line->placeholderText();
            if (!placeholder.isEmpty()) line->setPlaceholderText(translatedProperty(line, "satssdvI18nPlaceholder", placeholder));
        }
        if (auto *combo = qobject_cast<QComboBox *>(widget)) {
            for (int i = 0; i < combo->count(); ++i) {
                QString source = combo->itemData(i, kSourceRole).toString();
                if (source.isEmpty()) {
                    source = sourceText(combo->itemText(i));
                    combo->setItemData(i, source, kSourceRole);
                }
                combo->setItemText(i, translate(source));
            }
        }
        if (auto *tabs = qobject_cast<QTabWidget *>(widget)) {
            for (int i = 0; i < tabs->count(); ++i) {
                QString source = tabs->tabBar()->tabData(i).toString();
                if (source.isEmpty()) {
                    source = sourceText(tabs->tabText(i));
                    tabs->tabBar()->setTabData(i, source);
                }
                tabs->setTabText(i, translate(source));
            }
        }
        if (auto *table = qobject_cast<QTableWidget *>(widget)) {
            if (table->horizontalHeaderItem(0)) {
                for (int col = 0; col < table->columnCount(); ++col) {
                    auto *item = table->horizontalHeaderItem(col);
                    if (!item) continue;
                    QString source = item->data(kSourceRole).toString();
                    if (source.isEmpty()) { source = sourceText(item->text()); item->setData(kSourceRole, source); }
                    item->setText(translate(source));
                }
            }
        }
    }
    for (QAction *action : root->findChildren<QAction *>()) {
        action->setText(translatedProperty(action, "satssdvI18nText", action->text()));
        if (!action->toolTip().isEmpty()) action->setToolTip(translatedProperty(action, "satssdvI18nToolTip", action->toolTip()));
    }
}
