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
    {"RX 解码诊断 · 等待音频", "RX decoder diagnostics · waiting for audio", "RX デコーダ診断 · 音声待機中"},
    {"RX 解码器发生异常，已重置并继续监听：%1", "RX decoder error; reset and continue listening: %1", "RX デコーダーエラー。リセットして受信を継続します: %1"},
    {"启动 RX", "Start RX", "RX 開始"}, {"停止 RX", "Stop RX", "RX 停止"},
    {"RX 瀑布图", "RX Waterfall", "RX ウォーターフォール"},
    {"TX 瀑布图", "TX Waterfall", "TX ウォーターフォール"},
    {"等待 RX 音频", "Waiting for RX audio", "RX 音声を待機中"}, {"等待 TX 音频", "Waiting for TX audio", "TX 音声を待機中"},
    {"横轴：频率 · 纵轴：时间", "X: frequency · Y: time", "横軸: 周波数 · 縦軸: 時間"},
    {"图片质量", "Image quality", "画像品質"}, {"低质量 320×240", "Low 320×240", "低 320×240"},
    {"中质量 448×336", "Medium 448×336", "中 448×336"}, {"高质量 640×480", "High 640×480", "高 640×480"},
    {"循环发送", "Repeat transmission", "繰り返し送信"}, {"开始发射", "Start TX", "送信開始"},
    {"预计 %1 · 已发射 %2 · 剩余 %3 · 数据帧 %4 / %5", "Est. %1 · Sent %2 · Remaining %3 · Data frames %4 / %5", "予定 %1 · 送信済み %2 · 残り %3 · データフレーム %4 / %5"},
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
    {"消息", "Message", "メッセージ"},
    {"＋ 文字", "＋ Text", "＋ テキスト"},
    {"＋ 图库图像", "＋ Library image", "＋ ライブラリ画像"}, {"＋ 上次接收图像", "＋ Last RX image", "＋ 最後の RX 画像"},
    {"清除底图", "Clear background", "背景をクリア"}, {"文字内容 / {MYCALL} / {LOCATOR}", "Text / {MYCALL} / {LOCATOR}", "テキスト / {MYCALL} / {LOCATOR}"},
    {"文字内容", "Text content", "テキスト内容"}, {"图库图像", "Library image", "ライブラリ画像"},
    {"图像缩放", "Image scale", "画像倍率"}, {"文字字号", "Font size", "文字サイズ"},
    {"描边宽度", "Outline width", "縁取り幅"}, {"文字颜色", "Text color", "文字色"},
    {"描边颜色", "Outline color", "縁取り色"}, {"粗体", "Bold", "太字"},
    {"显示", "Visible", "表示"}, {"描边", "Outline", "縁取り"},
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
    {"GIF 预览播放失败：%1", "Unable to play GIF preview: %1", "GIF プレビューを再生できません: %1"},
    {"音频后端异常", "Audio backend error", "オーディオバックエンドエラー"},
    {"已连接", "Connected", "接続済み"}, {"未初始化", "Not initialized", "未初期化"},
    {"运行", "Running", "実行中"}, {"待机", "Idle", "待機中"},
    {"%1 · %2 项", "%1 · %2 items", "%1 · %2 項目"}, {"属性", "Properties", "プロパティ"}, {"文字", "Text", "テキスト"},
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
    {"GIF 动图预览", "Animated GIF preview", "アニメーション GIF プレビュー"},
    {"GIF 第 %1 帧 · %2", "GIF frame %1 · %2", "GIF 第 %1 フレーム · %2"},
    {"帧完成", "Frame complete", "フレーム完了"}, {"解码中", "Decoding", "デコード中"},
    {"GIF 已解码，但相册保存失败：%1", "GIF decoded, but saving to the album failed: %1", "GIF はデコードされましたが、アルバムへの保存に失敗しました: %1"},
    {"GIF 动图接收完成并已保存到相册（%1 帧）", "Animated GIF received and saved to the album (%1 frames)", "アニメーション GIF を受信し、アルバムに保存しました（%1 フレーム）"},
    {"GIF 接收中断，已保存可解码的部分动画（%1 帧）", "GIF reception stopped; decodable partial animation saved (%1 frames)", "GIF 受信が中断されました。デコード可能な部分アニメーションを保存しました（%1 フレーム）"},
    {"加载接收 GIF 动图失败：", "Failed to load received GIF animation: ", "受信した GIF アニメーションの読み込みに失敗しました: "},
    {"GIF 接收结束事件已到达，但后端尚未提供有效动画数据", "GIF reception completed, but the backend has not provided valid animation data", "GIF 受信完了イベントを受信しましたが、バックエンドから有効なアニメーションデータがまだ提供されていません"},
    {"GIF 部分接收内容已自动保存到相册", "Partial GIF reception was automatically saved to the album", "GIF の部分受信データをアルバムに自動保存しました"},
    {"帧不完整，继续重同步", "Frame incomplete; resynchronizing", "フレームが不完全です。再同期しています"},
    {"等待关键帧，增量帧仍继续接收", "Waiting for a keyframe; delta frames are still being received", "キーフレーム待機中。差分フレームの受信は継続しています"},
    {"图像预览", "Image preview", "画像プレビュー"}, {"导出文件", "Export file", "ファイルをエクスポート"},
    {"删除记录", "Delete record", "記録を削除"}, {"导出图像", "Export image", "画像をエクスポート"},
    {"相册图像已导出", "Album image exported", "アルバム画像をエクスポートしました"},
    {"删除图像记录", "Delete image record", "画像記録を削除"}, {"确定删除所选相册记录吗？", "Delete the selected album record?", "選択したアルバム記録を削除しますか?"},
    {"相册暂无图像。", "No images in the album yet.", "アルバムに画像はありません。"},
    {"完成", "Complete", "完了"}, {"接收中", "Receiving", "受信中"},
    {"接收已启动", "RX started", "RX を開始しました"}, {"接收已停止", "RX stopped", "RX を停止しました"},
    {"RX 已启动", "RX started", "RX を開始しました"}, {"RX 已停止", "RX stopped", "RX を停止しました"},
    {"循环发射倒计时：%1 秒", "Repeat TX countdown: %1 s", "繰り返し送信まで %1 秒"},
    {"无法生成发送图像", "Unable to render transmit image", "送信画像を生成できません"},
    {"无法准备发送模板", "Unable to prepare the TX template", "送信テンプレートを準備できません"},
    {"无法将发送模板编码为 PNG", "Unable to encode TX template as PNG", "TX テンプレートを PNG にエンコードできません"},
    {"当前每个模板最多只能发射一个可见 GIF 动图", "Only one visible GIF animation can be transmitted per template", "1 つのテンプレートで送信できる GIF アニメーションは 1 つまでです"},
    {"无法提取模板 GIF 原始数据或显示区域", "Unable to extract the template GIF data or display bounds", "テンプレート GIF データまたは表示領域を取得できません"},
    {"无法编码 GIF 模板的静态图层", "Unable to encode the static layers of the GIF template", "GIF テンプレートの静的レイヤーをエンコードできません"},
    {"无法解码模板中的 GIF 图像，请重新导入 GIF 文件", "Unable to decode the template GIF image; please import the GIF file again", "テンプレート内の GIF 画像をデコードできません。GIF ファイルを再インポートしてください"},
    {"已提交图像发射任务", "Image transmission job submitted", "画像送信ジョブを送信しました"},
    {"保存设置失败：", "Failed to save settings: ", "設定の保存に失敗しました: "},
    {"设备与电台设置", "Device and radio settings", "デバイスと無線機の設定"},
    {"系统默认", "System default", "システム既定"}, {"音频设备列表不可用：", "Audio device list unavailable: ", "オーディオデバイス一覧を利用できません: "},
    {"串口 RTS", "Serial RTS", "シリアル RTS"}, {"串口 DTR", "Serial DTR", "シリアル DTR"},
    {"串口列表不可用：", "Serial port list unavailable: ", "シリアルポート一覧を利用できません: "},
    {"刷新串口", "Refresh serial ports", "シリアルポートを更新"}, {"PTT 高电平有效", "PTT active high", "PTT High 有効"},
    {"本站网格", "Station locator", "自局グリッドロケーター"}, {"界面语言", "Interface language", "表示言語"},
    {"音频采样率", "Audio sample rate", "オーディオサンプルレート"},
    {"M-ary FEC 策略", "M-ary FEC policy", "M-ary FEC 方針"},
    {"速度优先（均衡 FEC）", "Balanced / throughput priority", "バランス / スループット優先"},
    {"弱信号（高冗余 FEC）", "Robust / weak-signal priority", "高耐性 / 弱信号優先"},
    {"均衡：QPSK 5/6、8PSK 3/4、16QAM 2/3，并按调制方式添加包级纠删冗余；高冗余：全部采用 1/2 卷积码和 8+2 包级恢复。高冗余会增加发射时间。", "Balanced: QPSK 5/6, 8PSK 3/4 and 16QAM 2/3 with modulation-specific packet-erasure parity; Robust: rate-1/2 convolutional coding plus 8+2 packet recovery for all modes. Robust increases airtime.", "バランス: QPSK 5/6、8PSK 3/4、16QAM 2/3 と変調方式別パケット消失訂正。高耐性: 全モードでレート 1/2 畳み込み符号と 8+2 パケット復元。高耐性は送信時間が長くなります。"},
    {"均衡：QPSK 5/6、8PSK 3/4、16QAM 2/3；高冗余：全部采用 1/2 卷积码和 8+2 包级恢复。高冗余会增加发射时间。", "Balanced: QPSK 5/6, 8PSK 3/4, 16QAM 2/3. Robust: rate-1/2 convolutional FEC and 8+2 packet recovery for all modes; it increases airtime.", "バランス: QPSK 5/6、8PSK 3/4、16QAM 2/3。高耐性: 全モードで畳み込み符号 1/2 と 8+2 パケット復元を使用し、送信時間が長くなります。"},
    {"RX 音频输入", "RX audio input", "RX オーディオ入力"},
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
    {"M-ary 后解调 EVM-SNR 估算值 · 从已同步星座符号测量 · 参考带宽为符号判决孔径，不可直接与全带宽 PCM SNR、RF SNR 或 Eb/N0 比较", "M-ary post-demodulation EVM-SNR · measured from synchronized constellation symbols · its reference bandwidth is the symbol decision aperture; do not compare directly with full-band PCM SNR, RF SNR or Eb/N0", "M-ary 復調後 EVM-SNR · 同期したコンステレーションシンボルから測定 · 基準帯域はシンボル判定窓であり、全帯域 PCM SNR、RF SNR、Eb/N0 と直接比較できません"},
    {"M-ary EVM-SNR 等待载波/定时同步及至少 48 个有效符号", "M-ary EVM-SNR is waiting for carrier/timing acquisition and at least 48 synchronized payload symbols", "M-ary EVM-SNR はキャリア/タイミング同期と 48 個以上の有効シンボルを待機中です"},
    {"当前模式暂不提供可靠的解调域 SNR 估算，因此不显示频谱推算值。", "No validated demodulator-domain SNR estimate is available for this mode; spectrum-only estimates are intentionally hidden.", "このモードでは信頼できる復調器ドメインの SNR 推定が未実装のため、スペクトルのみの推定値は表示しません。"},
    {"AFSK1200 音频 SNR 估算 · 双音频谱特征 + 解调同步 + 最近 1.2 秒内 CRC 校验通过的数据包 · 未经射频校准 · 噪声底 %1 dBFS/bin", "AFSK1200 audio SNR estimate · two separated tone groups + decoder sync + CRC-valid packet in last 1.2 s · not RF-calibrated · noise %1 dBFS/bin", "AFSK1200 音声 SNR 推定 · 分離した 2 音調 + 復調同期 + 過去 1.2 秒以内の CRC 正常パケット · RF 校正なし · ノイズ底 %1 dBFS/bin"},
    {"AFSK1200 音频 SNR 正在等待空闲噪声基线、双音频谱特征、解调同步和有效 CRC 数据包", "AFSK1200 audio SNR is waiting for an idle-noise baseline, two-tone spectrum evidence, modem synchronization and a fresh CRC-valid packet", "AFSK1200 音声 SNR はアイドル時ノイズ基準、2 音調スペクトル、復調同期、CRC 正常パケットを待機中です"},
    {"CCSDS 2400 GMSK 音频带内 SNR 估算 · 空闲噪声基线 + AFC 同步 + 最近 1.2 秒内有效数据包 · 未经射频校准 · 噪声底 %1 dBFS/bin", "CCSDS 2400 GMSK in-band audio SNR estimate · idle-noise baseline + AFC acquisition + valid packet in the last 1.2 s · not RF-calibrated · noise %1 dBFS/bin", "CCSDS 2400 GMSK 帯域内音声 SNR 推定 · アイドル時ノイズ基準 + AFC 同期 + 過去 1.2 秒以内の有効パケット · RF 校正なし · ノイズ底 %1 dBFS/bin"},
    {"CCSDS 2400 GMSK 音频 SNR 正在等待空闲噪声基线、AFC 同步和有效数据包", "CCSDS 2400 GMSK audio SNR is waiting for an idle-noise baseline, AFC acquisition and a valid packet", "CCSDS 2400 GMSK 音声 SNR はアイドル時ノイズ基準、AFC 同期、有効パケットを待機中です"},
    {"Auto RX 正在等待已同步的解调候选；M-ary 候选锁定后会显示后解调 EVM-SNR", "Auto RX is waiting for a synchronized decoder candidate; M-ary EVM-SNR is shown when a candidate locks", "Auto RX は同期した復調候補を待機中です。M-ary 候補がロックすると EVM-SNR を表示します"},
    {"设备设置已保存", "Device settings saved", "デバイス設定を保存しました"},
    {"导入图库图片", "Import library image", "ライブラリ画像をインポート"},
    {"导出完成", "Export complete", "エクスポート完了"}, {"已导出 %1 条记录", "Exported %1 records", "%1 件の記録をエクスポートしました"},
    {"模板包已导出：", "Template pack exported: ", "テンプレートパックをエクスポートしました: "},
    {"导入完成", "Import complete", "インポート完了"}, {"已导入 %1 个模板", "Imported %1 templates", "%1 個のテンプレートをインポートしました"},
    {"模板可视化编辑器", "Template visual editor", "テンプレートビジュアルエディター"},
    {"模板编辑", "Template editor", "テンプレート編集"}, {"删除", "Delete", "削除"},
    {"模板名称", "Template name", "テンプレート名"}, {"＋ 文字", "＋ Text", "＋ テキスト"},
    {"＋ 图库图像", "＋ Library image", "＋ ライブラリ画像"}, {"＋ 上次接收图像", "＋ Last RX image", "＋ 最後の RX 画像"},
    {"上移图层", "Move layer up", "レイヤーを上へ"}, {"下移图层", "Move layer down", "レイヤーを下へ"},
    {"5% 网格吸附", "5% grid snapping", "5% グリッドスナップ"}, {"删除选中项", "Delete selected item", "選択項目を削除"},
    {"字号", "Font size", "文字サイズ"}, {"颜色", "Color", "色"},
    {"图库为空", "Image library is empty", "画像ライブラリは空です"},
    {"请先导入图库图像。", "Import an image into the library first.", "先にライブラリへ画像をインポートしてください。"},
    {"选择图像", "Choose image", "画像を選択"}, {"选择图库图像", "Choose library image", "ライブラリ画像を選択"},
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
