(() => {
  const DEFAULT_WATERFALL_SETTINGS = {
    minFrequencyHz: 0, maxFrequencyHz: 5000, minDbfs: -110, maxDbfs: -20,
    rowsPerSecond: 15, historySeconds: 20,
    backgroundColor: '#07111b', lowColor: '#153a78', midColor: '#168fae',
    highColor: '#39c6b0', peakColor: '#f7d45b'
  };
  const $ = (id) => document.getElementById(id);
  const api = () => window.go?.main?.App;
  const state = { settings: {language:'zh-CN',ptt:{mode:'vox',activeHigh:true},quality:'medium',waterfall:{...DEFAULT_WATERFALL_SETTINGS},callsign:'N0CALL',locator:'JJ00',rxProfile:'ssdv-ax25',txProfile:'ssdv-ax25'}, profiles: [], ports: [], audioDevices: {inputs:[],outputs:[]}, audioDevicesLoaded: false, locale: 'zh-CN', ptt: { active: false }, audio: { connected:false, rxActive:false, txActive:false }, countdown: null, templates: [], selectedTemplate: 0, selectedComponent: '', gallery: [], album: [], templateBackgroundAssetId: '', txFields:{myCall:'N0CALL',hisCall:'',qth:'',locator:'JJ00',report:'595',message:''}, txActive: false, txToneActive: false, txPreparing: false, imageDataCache: new Map(), imageElementCache: new Map(), imageOutlineCache: new WeakMap(), componentBounds: new Map(), previewRenderVersion: 0, previewRAF: 0, previewPendingResolve: null, latestRXData: '', latestRXLoaded:false, waterfallPreview: null, settingsTab: 'general', pendingDeleteKey:'', pendingDeleteButton:null,repeatIntervalBase:null,repeatCountdownTimer:null };
  const fallback = {
    'zh-CN': {'common.confirmDelete':'再次点击确认删除','common.confirmDeleteHint':'请再次点击删除按钮确认操作。','tx.start':'开始发射','tx.stop':'停止发射','tx.preparing':'正在准备…','tx.idle':'TX IDLE','tx.active':'TX ACTIVE','tx.done':'发射完成','tx.testTone':'1500 Hz 测试音','tx.testToneActive':'音调发射中','nav.workbench':'工作台','nav.album':'相册','nav.settings':'设置','ptt.test':'测试 PTT','ptt.stop':'停止 PTT 测试','ptt.idle':'待机','ptt.active':'PTT 测试中','ptt.remaining':'剩余 {seconds} 秒','ptt.voxHint':'VOX 由电台检测音频自动发射，不需要软件 PTT 测试。','ptt.refresh':'刷新串口','ptt.autoRelease':'最长 15 秒，后端自动释放','audio.refresh':'刷新音频设备','audio.defaultDevice':'默认','audio.ready':'音频就绪','audio.off':'音频未连接','audio.rxOn':'停止 RX','audio.rxOff':'启动 RX','audio.unavailable':'音频后端未启用，请使用 CGO + malgo 标签构建。','quality.low':'低质量 320×240','quality.medium':'中质量 448×336','quality.high':'高质量 640×480','tx.repeat':'循环发送','tx.interval':'发送间隔（秒）','rx.complete':'接收完成','rx.progress':'接收进度','rx.ssdvPending':'SSDV 图像重建完成','rx.packetCount':'已接收 {received} / {expected} 个数据包','template.addText':'＋ 文字','template.addRX':'＋ 上次 RX','template.addImage':'＋ 图像','template.noComponent':'请选择或添加组件','template.saved':'模板已保存','template.defaultText':'CQ SSTV','template.noImage':'无可用图库图像','template.content':'文字内容','template.fontSize':'字号 px','template.imageScale':'等比例缩放 %','template.dragHint':'直接在预览画布中拖动组件调整位置','template.rxPlaceholder':'暂无 RX 图像','template.outlineWidth':'描边宽度 px','template.color':'文字颜色','template.bold':'粗体','template.visible':'可见','template.outline':'描边','template.deleteComponent':'删除所选组件','template.qsoFields':'本次 QSO 信息','template.hisCall':'对方呼号','template.qth':'QTH','template.report':'RST / 报告','template.message':'消息','template.imageSource':'图片组件来源','template.outlineColor':'描边颜色','template.componentsTitle':'模板组件','template.componentProperties':'组件属性','template.saveSlot':'保存当前槽位','template.presetHint':'支持添加、复制和管理最多 32 个独立预设','template.myCall':'本台呼号','template.ax25SSID':'AX.25 SSID（本站）','template.locator':'网格定位','tx.transmission':'发射设置','gallery.open':'打开图片图库','gallery.title':'模板图片图库','gallery.subtitle':'图片保存在本机用户配置目录，模板通过资源 ID 引用','gallery.import':'导入图片','gallery.loading':'正在读取图库…','gallery.empty':'图库为空。导入图片后可设为模板底图或添加为组件。','gallery.setBackground':'设为模板底图','gallery.addLayer':'添加为图层','gallery.delete':'删除图片','gallery.background':'模板底图','gallery.noBackground':'未设置，可从图库选择底图','gallery.clearBackground':'清除底图','gallery.imported':'已导入图库：{name}','gallery.deleted':'已删除图片：{name}','gallery.inUse':'图片已从模板引用中移除','album.title':'图像相册','album.subtitle':'历史接收与发送图像，自动记录时间戳','album.all':'全部图像','album.rx':'仅接收 RX','album.tx':'仅发送 TX','album.empty':'相册暂无图像。完成接收或发射后会自动归档。','album.footnote':'点击缩略图查看大图；可导出带时间戳的 PNG 或删除记录。','album.preview':'查看大图','album.export':'导出 PNG','album.delete':'删除记录','album.deleted':'相册记录已删除'},
    en: {'common.confirmDelete':'Click again to confirm','common.confirmDeleteHint':'Click the delete action again to confirm.','tx.start':'Start TX','tx.stop':'Stop TX','tx.preparing':'Preparing…','tx.idle':'TX IDLE','tx.active':'TX ACTIVE','tx.done':'Transmission complete','tx.testTone':'1500 Hz Tone','tx.testToneActive':'Tone TX active','nav.workbench':'Workbench','nav.album':'Album','nav.settings':'Settings','ptt.test':'Test PTT','ptt.stop':'Stop PTT test','ptt.idle':'Idle','ptt.active':'PTT test active','ptt.remaining':'{seconds}s remaining','ptt.voxHint':'VOX keys the radio from audio and does not require a software PTT test.','ptt.refresh':'Refresh ports','ptt.autoRelease':'Maximum 15 seconds; backend auto-release','audio.refresh':'Refresh audio devices','audio.defaultDevice':'Default','audio.ready':'Audio ready','audio.off':'Audio off','audio.rxOn':'Stop RX','audio.rxOff':'Start RX','audio.unavailable':'Audio backend is disabled. Build with CGO and the malgo tag.','quality.low':'Low 320×240','quality.medium':'Medium 448×336','quality.high':'High 640×480','tx.repeat':'Repeat transmission','tx.interval':'Interval (seconds)','rx.complete':'RX complete','rx.progress':'RX progress','rx.ssdvPending':'SSDV image reconstructed','rx.packetCount':'Received {received} / {expected} packets','template.addText':'＋ Text','template.addRX':'＋ Last RX','template.addImage':'＋ Image','template.noComponent':'Select or add a component','template.saved':'Template saved','template.defaultText':'CQ SATELLITE','template.noImage':'No gallery image available','template.content':'Text content','template.fontSize':'Font px','template.imageScale':'Uniform scale %','template.dragHint':'Drag components directly on the preview canvas to reposition them','template.rxPlaceholder':'No RX image yet','template.outlineWidth':'Outline px','template.color':'Text color','template.bold':'Bold','template.visible':'Visible','template.outline':'Outline','template.deleteComponent':'Delete selected component','template.qsoFields':'QSO fields','template.hisCall':'His callsign','template.qth':'QTH','template.report':'RST / Report','template.message':'Message','template.imageSource':'Image component source','template.outlineColor':'Outline color','template.componentsTitle':'Template Components','template.componentProperties':'Component Properties','template.saveSlot':'Save Current Slot','template.presetHint':'Create, duplicate and manage up to 32 independent presets.','template.myCall':'My callsign','template.ax25SSID':'AX.25 SSID (local station)','template.locator':'Locator','tx.transmission':'Transmission','gallery.open':'Open Image Library','gallery.title':'Template Image Library','gallery.subtitle':'Images are stored locally and referenced by asset ID.','gallery.import':'Import Images','gallery.loading':'Loading image library…','gallery.empty':'The library is empty. Import images to set a template background or add an image component.','gallery.setBackground':'Set as Template Background','gallery.addLayer':'Add as Layer','gallery.delete':'Delete Image','gallery.background':'Template Background','gallery.noBackground':'Not set. Choose a background from the image library.','gallery.clearBackground':'Clear Background','gallery.imported':'Imported to library: {name}','gallery.deleted':'Deleted image: {name}','gallery.inUse':'Removed deleted image references from templates.','album.title':'Image Album','album.subtitle':'Timestamped RX and TX image history','album.all':'All Images','album.rx':'RX Only','album.tx':'TX Only','album.empty':'No images in the album yet. Completed RX/TX images are archived automatically.','album.footnote':'Click a thumbnail to preview. Export timestamped PNGs or delete records.','album.preview':'Preview','album.export':'Export PNG','album.delete':'Delete Record','album.deleted':'Album record deleted'},
    'ja-JP': {'common.confirmDelete':'もう一度クリックして削除','common.confirmDeleteHint':'削除操作を確定するには、もう一度クリックしてください。','tx.start':'送信開始','tx.stop':'送信停止','tx.preparing':'準備中…','tx.idle':'TX IDLE','tx.active':'TX ACTIVE','tx.done':'送信完了','tx.testTone':'1500 Hz テスト音','tx.testToneActive':'テスト音送信中','nav.workbench':'ワークベンチ','nav.album':'アルバム','nav.settings':'設定','ptt.test':'PTT テスト','ptt.stop':'PTT テスト停止','ptt.idle':'待機中','ptt.active':'PTT テスト中','ptt.remaining':'残り {seconds} 秒','ptt.voxHint':'VOX は音声を検出して送信するため、ソフトウェア PTT テストは不要です。','ptt.refresh':'ポート更新','ptt.autoRelease':'最大 15 秒、バックエンドが自動解除','audio.refresh':'オーディオデバイス更新','audio.defaultDevice':'既定','audio.ready':'オーディオ準備完了','audio.off':'オーディオ未接続','audio.rxOn':'RX 停止','audio.rxOff':'RX 開始','audio.unavailable':'オーディオバックエンドが無効です。CGO と malgo タグでビルドしてください。','quality.low':'低 320×240','quality.medium':'中 448×336','quality.high':'高 640×480','tx.repeat':'繰り返し送信','tx.interval':'送信間隔（秒）','rx.complete':'受信完了','rx.progress':'受信進捗','rx.ssdvPending':'SSDV 画像の再構築が完了しました','rx.packetCount':'受信 {received} / {expected} パケット','template.addText':'＋ テキスト','template.addRX':'＋ 最終 RX','template.addImage':'＋ 画像','template.noComponent':'コンポーネントを選択または追加','template.saved':'テンプレートを保存しました','template.defaultText':'CQ SATELLITE','template.noImage':'画像ライブラリに画像がありません','template.content':'テキスト内容','template.fontSize':'文字サイズ px','template.imageScale':'均等拡大縮小 %','template.dragHint':'プレビュー上でコンポーネントをドラッグして位置を調整','template.rxPlaceholder':'RX 画像なし','template.outlineWidth':'縁取り幅 px','template.color':'文字色','template.bold':'太字','template.visible':'表示','template.outline':'縁取り','template.deleteComponent':'選択したコンポーネントを削除','template.qsoFields':'今回の QSO 情報','template.hisCall':'相手局コールサイン','template.qth':'QTH','template.report':'RST / レポート','template.message':'メッセージ','template.imageSource':'画像コンポーネントのソース','template.outlineColor':'縁取り色','template.componentsTitle':'テンプレートコンポーネント','template.componentProperties':'コンポーネント設定','template.saveSlot':'選択スロットを保存','template.presetHint':'最大32件の独立したプリセットを作成、複製、管理できます。','template.myCall':'自局コールサイン','template.ax25SSID':'AX.25 SSID（自局）','template.locator':'グリッドロケーター','tx.transmission':'送信設定','gallery.open':'画像ライブラリを開く','gallery.title':'テンプレート画像ライブラリ','gallery.subtitle':'画像はローカルに保存され、アセット ID で参照されます。','gallery.import':'画像をインポート','gallery.loading':'画像ライブラリを読み込み中…','gallery.empty':'ライブラリは空です。画像をインポートしてテンプレート背景または画像コンポーネントとして使用できます。','gallery.setBackground':'テンプレート背景に設定','gallery.addLayer':'レイヤーとして追加','gallery.delete':'画像を削除','gallery.background':'テンプレート背景','gallery.noBackground':'未設定。画像ライブラリから背景を選択してください。','gallery.clearBackground':'背景をクリア','gallery.imported':'ライブラリに追加しました: {name}','gallery.deleted':'画像を削除しました: {name}','gallery.inUse':'テンプレートから削除画像の参照を解除しました。','album.title':'画像アルバム','album.subtitle':'RX/TX 画像履歴とタイムスタンプ','album.all':'すべて','album.rx':'RX のみ','album.tx':'TX のみ','album.empty':'アルバムに画像はありません。受信または送信完了時に自動保存されます。','album.footnote':'サムネイルをクリックしてプレビュー。タイムスタンプ付き PNG の保存や記録削除ができます。','album.preview':'プレビュー','album.export':'PNG を保存','album.delete':'記録を削除','album.deleted':'アルバム記録を削除しました。'}
  };
  const templatePresetLabels = {
    'zh-CN': {
      'template.addPreset':'＋ 新建预设','template.duplicatePreset':'复制当前','template.deletePreset':'删除当前',
      'template.presetName':'预设名称','template.savePreset':'保存预设','template.choosePreset':'选择发射模板',
      'template.newName':'模板 {number}','template.duplicateName':'{name} 副本','template.limitReached':'最多支持 32 个模板预设',
      'template.deleteConfirm':'确定删除模板“{name}”吗？','template.saved':'模板预设已保存','template.created':'已创建模板预设：{name}',
      'template.deleted':'已删除模板预设：{name}','template.addedText':'已添加文字组件','template.addedImage':'已添加图片组件','template.backgroundSet':'已设置模板底图：{name}',
      'template.backgroundCleared':'已清除模板底图','template.noSelection':'请先选择一个模板预设','template.componentLimit':'每个模板最多支持 64 个组件','template.componentDeleted':'已删除所选组件','action.failed':'操作失败：{error}'
    },
    en: {
      'template.addPreset':'＋ New Preset','template.duplicatePreset':'Duplicate','template.deletePreset':'Delete','template.presetName':'Preset name',
      'template.savePreset':'Save Preset','template.choosePreset':'Choose TX preset','template.newName':'Template {number}','template.duplicateName':'{name} Copy',
      'template.limitReached':'A maximum of 32 template presets is supported','template.deleteConfirm':'Delete template “{name}”?','template.saved':'Template preset saved',
      'template.created':'Created template preset: {name}','template.deleted':'Deleted template preset: {name}','template.addedText':'Text component added',
      'template.addedImage':'Image component added','template.backgroundSet':'Template background set: {name}','template.backgroundCleared':'Template background cleared',
      'template.noSelection':'Select a template preset first','template.componentLimit':'A template can contain up to 64 components','template.componentDeleted':'Selected component deleted','action.failed':'Operation failed: {error}'
    },
    'ja-JP': {
      'template.addPreset':'＋ 新規プリセット','template.duplicatePreset':'複製','template.deletePreset':'削除','template.presetName':'プリセット名',
      'template.savePreset':'プリセットを保存','template.choosePreset':'送信プリセットを選択','template.newName':'テンプレート {number}','template.duplicateName':'{name} のコピー',
      'template.limitReached':'テンプレートは最大 32 件まで登録できます','template.deleteConfirm':'テンプレート「{name}」を削除しますか？','template.saved':'テンプレートを保存しました',
      'template.created':'テンプレートを作成しました: {name}','template.deleted':'テンプレートを削除しました: {name}','template.addedText':'テキストを追加しました',
      'template.addedImage':'画像コンポーネントを追加しました','template.backgroundSet':'背景を設定しました: {name}','template.backgroundCleared':'背景をクリアしました',
      'template.noSelection':'先にテンプレートを選択してください','template.componentLimit':'1 つのテンプレートには最大 64 個のコンポーネントを追加できます','template.componentDeleted':'コンポーネントを削除しました','action.failed':'操作に失敗しました: {error}'
    }
  };
  Object.entries(templatePresetLabels).forEach(([locale, labels]) => Object.assign(fallback[locale], labels));
  const settingsLocaleLabels = {
    'zh-CN': {'settings.configuration':'应用程序设置','settings.saved':'设置已保存','audio.rxDevice':'RX 输入设备','audio.txDevice':'TX 输出设备','audio.default':'系统默认','audio.refresh':'刷新音频设备','audio.defaultDevice':'默认','audio.ready':'音频就绪','audio.off':'音频未连接','audio.fullDuplexHint':'共享输入和输出流，支持软件全双工。','audio.sampleRate':'音频采样率','audio.duplexDetail':'输入和输出采用独立共享音频流，TX 播放期间 RX 保持采集。','audio.deviceCount':'发现 {inputs} 个输入设备和 {outputs} 个输出设备','ptt.noPorts':'未发现可用串口','ptt.portCount':'已发现 {count} 个串口','station.title':'本站身份信息','station.callsign':'本站呼号','station.locator':'网格定位','settings.tabGeneral':'设备与电台','settings.tabDisplay':'显示设置','waterfall.title':'音频瀑布图参数','waterfall.minFrequency':'最低频率','waterfall.maxFrequency':'最高频率','waterfall.minDb':'最低电平','waterfall.maxDb':'最高电平','waterfall.rowsPerSecond':'滚动速度','waterfall.history':'历史时长','waterfall.colors':'瀑布图颜色','waterfall.background':'背景色','waterfall.lowColor':'弱信号','waterfall.midColor':'中等信号','waterfall.highColor':'强信号','waterfall.peakColor':'峰值信号','waterfall.hint':'横轴为频率，纵轴为时间；可用频率范围受当前音频采样率限制。颜色表示信号强度，修改后实时预览并在保存时应用。','waterfall.reset':'恢复默认显示设置','waterfall.rxInput':'输入 · 音频瀑布图','waterfall.txOutput':'输出 · 音频瀑布图','waterfall.rxWaiting':'等待 RX 音频','waterfall.txWaiting':'等待 TX 音频','waterfall.frequencyAxis':'横轴：频率 · 纵轴：时间','template.unselected':'未选择','waterfall.rowsUnit':'行/秒','waterfall.secondsUnit':'秒'},
    en: {'settings.configuration':'Application Settings','settings.saved':'Settings saved','audio.rxDevice':'RX input device','audio.txDevice':'TX output device','audio.default':'System default','audio.refresh':'Refresh audio devices','audio.defaultDevice':'Default','audio.ready':'Audio ready','audio.off':'Audio off','audio.fullDuplexHint':'Shared input and output streams enable software full duplex.','audio.sampleRate':'Audio sample rate','audio.duplexDetail':'RX capture remains active while TX playback uses an independent shared stream.','audio.deviceCount':'Found {inputs} input devices and {outputs} output devices','ptt.noPorts':'No available serial ports found','ptt.portCount':'Found {count} serial ports','station.title':'Station identity','station.callsign':'Station callsign','station.locator':'Maidenhead locator','settings.tabGeneral':'Devices & Radio','settings.tabDisplay':'Display','waterfall.title':'Audio waterfall parameters','waterfall.minFrequency':'Minimum frequency','waterfall.maxFrequency':'Maximum frequency','waterfall.minDb':'Minimum level','waterfall.maxDb':'Maximum level','waterfall.rowsPerSecond':'Scroll speed','waterfall.history':'History duration','waterfall.colors':'Waterfall colors','waterfall.background':'Background','waterfall.lowColor':'Weak signal','waterfall.midColor':'Medium signal','waterfall.highColor':'Strong signal','waterfall.peakColor':'Peak signal','waterfall.hint':'Frequency runs horizontally and time vertically. The active audio sample rate limits the available frequency range. Colors map signal strength; changes preview live and apply when saved.','waterfall.reset':'Restore default display settings','waterfall.rxInput':'INPUT · AUDIO WATERFALL','waterfall.txOutput':'OUTPUT · AUDIO WATERFALL','waterfall.rxWaiting':'Waiting for RX audio','waterfall.txWaiting':'Waiting for TX audio','waterfall.frequencyAxis':'Horizontal: frequency · Vertical: time','template.unselected':'Not selected','waterfall.rowsUnit':'rows/s','waterfall.secondsUnit':'s'},
    'ja-JP': {'settings.configuration':'アプリケーション設定','settings.saved':'設定を保存しました','audio.rxDevice':'RX 入力デバイス','audio.txDevice':'TX 出力デバイス','audio.default':'システム既定','audio.refresh':'オーディオデバイスを更新','audio.defaultDevice':'既定','audio.ready':'オーディオ準備完了','audio.off':'オーディオ未接続','audio.fullDuplexHint':'共有入力・出力ストリームでソフトウェア全二重に対応します。','audio.sampleRate':'オーディオサンプルレート','audio.duplexDetail':'TX 再生中も独立した共有ストリームで RX 受信を継続します。','audio.deviceCount':'入力デバイス {inputs} 台、出力デバイス {outputs} 台を検出','ptt.noPorts':'使用可能なシリアルポートがありません','ptt.portCount':'シリアルポート {count} 個を検出','station.title':'局情報','station.callsign':'自局コールサイン','station.locator':'グリッドロケーター','settings.tabGeneral':'デバイスと無線機','settings.tabDisplay':'表示設定','waterfall.title':'オーディオ・ウォーターフォール設定','waterfall.minFrequency':'最低周波数','waterfall.maxFrequency':'最高周波数','waterfall.minDb':'最小レベル','waterfall.maxDb':'最大レベル','waterfall.rowsPerSecond':'スクロール速度','waterfall.history':'履歴時間','waterfall.colors':'ウォーターフォールの色','waterfall.background':'背景色','waterfall.lowColor':'弱い信号','waterfall.midColor':'中程度の信号','waterfall.highColor':'強い信号','waterfall.peakColor':'ピーク信号','waterfall.hint':'横軸は周波数、縦軸は時間です。表示できる周波数範囲は現在のサンプルレートに制限されます。色は信号強度を表し、変更はリアルタイムでプレビューされ、保存時に適用されます。','waterfall.reset':'表示設定を初期値に戻す','waterfall.rxInput':'入力 · オーディオ・ウォーターフォール','waterfall.txOutput':'出力 · オーディオ・ウォーターフォール','waterfall.rxWaiting':'RX 音声を待機中','waterfall.txWaiting':'TX 音声を待機中','waterfall.frequencyAxis':'横軸：周波数 · 縦軸：時間','template.unselected':'未選択','waterfall.rowsUnit':'行/秒','waterfall.secondsUnit':'秒'}
  };
  Object.entries(settingsLocaleLabels).forEach(([locale, labels]) => Object.assign(fallback[locale], labels));
  let dictionary = fallback['zh-CN'];
  const tr = (key, vars = {}) => Object.entries(vars).reduce((s,[k,v])=>s.replaceAll(`{${k}}`,String(v)),dictionary[key] || key);
  const call = async (method, ...args) => { const a=api(); if(!a?.[method]) throw new Error('Wails backend is not connected'); return a[method](...args); };
  state.templates = defaultTemplatePresets();

  async function applyLocale(locale) {
    state.locale = locale;
    dictionary = fallback[locale] || fallback['zh-CN'];
    try { dictionary = { ...dictionary, ...(await call('GetLocaleDictionary', locale)) }; } catch (_) {}
    document.documentElement.lang = locale;
    document.querySelectorAll('[data-i18n]').forEach(el => { const key=el.dataset.i18n; el.textContent=tr(key); });
    document.querySelectorAll('[data-i18n-placeholder]').forEach(el => { el.placeholder=tr(el.dataset.i18nPlaceholder); });
    document.querySelectorAll('[data-i18n-title]').forEach(el => { el.title=tr(el.dataset.i18nTitle); });
    document.querySelectorAll('[data-i18n-aria]').forEach(el => { el.setAttribute('aria-label',tr(el.dataset.i18nAria)); });
    $('language-select').value = locale;
    $('template-name').placeholder=tr('template.presetName');$('template-select').setAttribute('aria-label',tr('template.choosePreset'));
    renderPorts(true);if(state.audioDevicesLoaded)renderAudioDeviceOptions(true);
    renderPTTState();
    renderAudioState(state.audio);
    updateWaterfallOutputs(effectiveWaterfallSettings());
    drawWaterfallDemo();
  }
  let toastTimer = null;
  function confirmInlineDelete(key,button,message=''){
    if(state.pendingDeleteKey===key){
      state.pendingDeleteKey='';
      if(button){button.classList.remove('confirming');button.textContent=button.dataset.defaultLabel||button.textContent;}
      state.pendingDeleteButton=null;
      return true;
    }
    if(state.pendingDeleteButton&&state.pendingDeleteButton!==button){
      const previous=state.pendingDeleteButton;
      if(previous.isConnected){previous.classList.remove('confirming');previous.textContent=previous.dataset.defaultLabel||previous.textContent;}
    }
    state.pendingDeleteKey=key;state.pendingDeleteButton=button||null;
    if(button){button.dataset.defaultLabel=button.dataset.defaultLabel||button.textContent;button.textContent=tr('common.confirmDelete');button.classList.add('confirming');}
    setMessage(message||tr('common.confirmDeleteHint'));
    return false;
  }
  function setMessage(message, error=false) {
    const text = String(message || '');
    const settingsMessage = $('settings-message');
    if (settingsMessage) { settingsMessage.textContent = text; settingsMessage.classList.toggle('error', !!error); }
    const global = $('global-state');
    if (global && text) global.textContent = text;
    const galleryFeedback = $('gallery-feedback');
    if (galleryFeedback && $('image-library-dialog')?.open) { galleryFeedback.textContent = text; galleryFeedback.classList.toggle('error', !!error); }
    const toast = $('app-toast');
    if (toast && text) {
      toast.textContent = text;
      toast.hidden = false;
      toast.classList.toggle('error', !!error);
      if (toastTimer) window.clearTimeout(toastTimer);
      toastTimer = window.setTimeout(() => { toast.hidden = true; }, error ? 9000 : 4200);
    }
  }
  function runAction(action) {
    Promise.resolve().then(action).catch(error => setMessage(tr('action.failed', {error:error?.message || String(error)}), true));
  }
  function renderAX25AddressFields(){
    const enabled=state.settings?.txProfile==='ssdv-ax25';
    const field=$('tx-ax25-ssid-field'),input=$('tx-ax25-ssid');
    if(!field||!input)return;
    field.hidden=!enabled;
    input.disabled=!enabled;
    const value=Number(state.settings?.ax25Ssid);
    input.value=String(Number.isInteger(value)&&value>=0&&value<=15?value:0);
  }
  function renderProfiles() {
    for(const pane of ['rx','tx']) {
      const parent=$(`${pane}-profile-list`); parent.replaceChildren();
      const current=state.settings?.[pane==='rx'?'rxProfile':'txProfile'];
      state.profiles.forEach(p=>{const b=document.createElement('button');b.className='profile-btn'+(p.id===current?' active':'');b.textContent=p.name;b.addEventListener('click',async()=>{state.settings[pane==='rx'?'rxProfile':'txProfile']=p.id;renderProfiles();$(`${pane}-meta`).textContent=p.name;if(pane==='tx')$('tx-mode-label').textContent=p.name;renderTXState();try{await call('SaveSettings',state.settings);}catch(e){setMessage(e.message,true);}});parent.appendChild(b);});
    }
    const tx=state.profiles.find(p=>p.id===state.settings?.txProfile);if(tx)$('tx-mode-label').textContent=tx.name;renderAX25AddressFields();clearHisCallForCQTemplate();renderTXState();
  }
  function renderPorts(preserveCurrent=false) {
    const sel=$('serial-port');
    const selected=preserveCurrent&&sel.dataset.optionsRendered==='1'?sel.value:(state.settings?.ptt?.serialPort||sel.value);
    sel.replaceChildren();
    if(!state.ports.length){const o=document.createElement('option');o.value='';o.textContent=tr('ptt.noPorts');sel.appendChild(o);}
    state.ports.forEach(p=>{const o=document.createElement('option');o.value=p;o.textContent=p;sel.appendChild(o);});
    if(state.ports.includes(selected))sel.value=selected;
    sel.dataset.optionsRendered='1';
  }
  function updatePTTFields() {
    const mode=$('ptt-mode').value;
    $('hamlib-fields').hidden=mode!=='hamlib';
    $('serial-fields').hidden=mode!=='rts'&&mode!=='dtr';
    $('vox-hint').hidden=mode!=='vox';
  }
  function renderAudioState(audio){state.audio=audio||{connected:false,rxActive:false,txActive:false};const button=$('rx-toggle');if(button){button.textContent=state.audio.rxActive?tr('audio.rxOn'):tr('audio.rxOff');button.disabled=!state.audio.connected;}$('rx-pill').textContent=state.audio.rxActive?'RX ACTIVE':'RX STOPPED';$('rx-pill').classList.toggle('active',!!state.audio.rxActive);$('rx-pill').classList.toggle('idle',!state.audio.rxActive);if(!state.audio.connected&&state.audio.error)$('global-state').textContent=state.audio.error;else $('global-state').textContent=state.audio.connected?tr('audio.ready'):tr('audio.off');}
  function renderAudioDeviceOptions(preserveCurrent=false){
    const lists=state.audioDevices||{inputs:[],outputs:[]};
    for(const [id,items,configured] of [['rx-device',lists.inputs,state.settings?.rxInputDevice],['tx-device',lists.outputs,state.settings?.txOutputDevice]]){
      const sel=$(id);if(!sel)continue;
      const selected=preserveCurrent&&sel.dataset.optionsRendered==='1'?sel.value:configured;
      sel.replaceChildren();
      const def=document.createElement('option');def.value='';def.textContent=tr('audio.default');sel.appendChild(def);
      (items||[]).forEach(device=>{const option=document.createElement('option');option.value=device.id;option.textContent=device.name+(device.isDefault?` · ${tr('audio.defaultDevice')}`:'');sel.appendChild(option);});
      if(selected)sel.value=selected;
      sel.dataset.optionsRendered='1';
    }
    const status=$('audio-device-status');if(status)status.textContent=tr('audio.deviceCount',{inputs:(lists.inputs||[]).length,outputs:(lists.outputs||[]).length});
  }
  async function refreshAudioDevices(){try{const wasLoaded=state.audioDevicesLoaded;state.audioDevices=await call('GetAudioDevices');state.audioDevicesLoaded=true;renderAudioDeviceOptions(wasLoaded);}catch(e){$('audio-device-status').textContent=tr('audio.unavailable');}}
  async function refreshAudioState(){try{renderAudioState(await call('GetAudioState'));}catch(_){renderAudioState({connected:false,rxActive:false,txActive:false});}}
  async function toggleRX(){try{if(state.audio.rxActive)await call('StopRX');else await call('StartRX');await refreshAudioState();}catch(e){$('global-state').textContent=e.message;}}
  function effectiveWaterfallSettings(){return normalizeWaterfallSettings(state.waterfallPreview||state.settings?.waterfall||DEFAULT_WATERFALL_SETTINGS);}
  function normalizeWaterfallSettings(raw={}){
    const d={...DEFAULT_WATERFALL_SETTINGS,...(raw||{})};
    const number=(v,def,min,max)=>Math.max(min,Math.min(max,Number.isFinite(Number(v))?Number(v):def));
    d.minFrequencyHz=number(d.minFrequencyHz,0,0,47000);d.maxFrequencyHz=number(d.maxFrequencyHz,5000,100,48000);
    if(d.maxFrequencyHz<=d.minFrequencyHz)d.maxFrequencyHz=Math.min(48000,d.minFrequencyHz+1000);
    d.minDbfs=number(d.minDbfs,-110,-120,-10);d.maxDbfs=number(d.maxDbfs,-20,-90,0);
    if(d.maxDbfs<=d.minDbfs+4)d.maxDbfs=Math.min(0,d.minDbfs+5);
    d.rowsPerSecond=number(d.rowsPerSecond,15,1,40);d.historySeconds=number(d.historySeconds,20,5,60);
    for(const key of ['backgroundColor','lowColor','midColor','highColor','peakColor'])if(!/^#[0-9a-f]{6}$/i.test(String(d[key]||'')))d[key]=DEFAULT_WATERFALL_SETTINGS[key];
    return d;
  }
  const waterfallViews=new Map();
  const hexRGB=hex=>{const v=String(hex||'#000000').replace('#','');return [parseInt(v.slice(0,2),16)||0,parseInt(v.slice(2,4),16)||0,parseInt(v.slice(4,6),16)||0];};
  const mixRGB=(a,b,t)=>[Math.round(a[0]+(b[0]-a[0])*t),Math.round(a[1]+(b[1]-a[1])*t),Math.round(a[2]+(b[2]-a[2])*t)];
  function waterfallColor(db,cfg){
    if(db<cfg.minDbfs)return hexRGB(cfg.backgroundColor);
    const t=Math.max(0,Math.min(1,(db-cfg.minDbfs)/(cfg.maxDbfs-cfg.minDbfs)));
    const stops=[hexRGB(cfg.lowColor),hexRGB(cfg.midColor),hexRGB(cfg.highColor),hexRGB(cfg.peakColor)];
    const scaled=t*(stops.length-1),i=Math.min(stops.length-2,Math.floor(scaled));
    return mixRGB(stops[i],stops[i+1],scaled-i);
  }
  function renderWaterfallGrid(canvas,view,cfg){
    const ctx=canvas.getContext('2d'),w=canvas.width,h=canvas.height,dpr=Math.max(1,window.devicePixelRatio||1);
    ctx.fillStyle=cfg.backgroundColor;ctx.fillRect(0,0,w,h);
    const rows=Math.min(view.count,view.buffer.height);
    if(rows>0){
      const start=(view.cursor-rows+view.buffer.height)%view.buffer.height;
      const first=Math.min(rows,view.buffer.height-start);
      const scaleY=h/view.buffer.height;
      const destinationHeight=rows*scaleY;
      const top=h-destinationHeight;
      ctx.save();ctx.imageSmoothingEnabled=false;
      ctx.drawImage(view.buffer,0,start,view.buffer.width,first,0,top,w,first*scaleY);
      if(first<rows){
        const remaining=rows-first;
        ctx.drawImage(view.buffer,0,0,view.buffer.width,remaining,0,top+first*scaleY,w,remaining*scaleY);
      }
      ctx.restore();
    }
    ctx.save();ctx.strokeStyle='rgba(190,215,230,.16)';ctx.lineWidth=Math.max(1,dpr);
    for(let i=1;i<4;i++){const y=Math.round(h*i/4)+.5;ctx.beginPath();ctx.moveTo(0,y);ctx.lineTo(w,y);ctx.stroke();}
    const minHz=view.frequencyStart??cfg.minFrequencyHz,maxHz=view.frequencyEnd??cfg.maxFrequencyHz;
    ctx.fillStyle='rgba(225,238,246,.84)';ctx.font=`${Math.max(8,8*dpr)}px Segoe UI, sans-serif`;ctx.textBaseline='top';ctx.textAlign='center';
    const firstTick=Math.ceil(minHz/500)*500;
    for(let hz=firstTick;hz<=maxHz+0.01;hz+=500){
      const x=(hz-minHz)/Math.max(1,maxHz-minHz)*w;
      ctx.strokeStyle='rgba(190,215,230,.24)';ctx.beginPath();ctx.moveTo(Math.round(x)+.5,0);ctx.lineTo(Math.round(x)+.5,h);ctx.stroke();
      ctx.fillStyle='rgba(225,238,246,.9)';ctx.fillText(String(hz),clamp(x,14*dpr,w-14*dpr),2*dpr);
    }
    ctx.restore();
    const watermark=canvas.parentElement?.querySelector('.spectrum-watermark');if(watermark)watermark.hidden=view.count>0;
  }
  function resetWaterfallView(canvas){
    const cfg=effectiveWaterfallSettings(),dpr=Math.max(1,window.devicePixelRatio||1),rect=canvas.getBoundingClientRect();
    canvas.width=Math.max(1,Math.floor(rect.width*dpr));canvas.height=Math.max(1,Math.floor(rect.height*dpr));
    const buffer=document.createElement('canvas');buffer.width=canvas.width;buffer.height=Math.max(30,Math.min(2400,Math.round(cfg.rowsPerSecond*cfg.historySeconds)));
    const bctx=buffer.getContext('2d');bctx.fillStyle=cfg.backgroundColor;bctx.fillRect(0,0,buffer.width,buffer.height);
    const view={buffer,cursor:0,count:0,lastRowAt:0,frequencyStart:cfg.minFrequencyHz,frequencyEnd:cfg.maxFrequencyHz};
    waterfallViews.set(canvas.id,view);renderWaterfallGrid(canvas,view,cfg);return view;
  }
  function drawIdleWaterfall(canvas){if(!canvas)return;resetWaterfallView(canvas);const watermark=canvas.parentElement?.querySelector('.spectrum-watermark');if(watermark)watermark.hidden=false;window.addEventListener('resize',()=>{resetWaterfallView(canvas);});}
  function drawWaterfallData(canvas,bins){
    if(!canvas||!Array.isArray(bins)||bins.length<2)return;
    let view=waterfallViews.get(canvas.id);if(!view)view=resetWaterfallView(canvas);
    const cfg=effectiveWaterfallSettings(),now=performance.now(),interval=1000/cfg.rowsPerSecond;
    if(now-view.lastRowAt<interval)return;
    view.lastRowAt=now;
    const width=view.buffer.width,data=view.buffer.getContext('2d').createImageData(width,1),pixels=data.data;
    const firstHz=Number(bins[0].frequencyHz)||0,step=(Number(bins[1].frequencyHz)-firstHz)||1,lastHz=Number(bins[bins.length-1].frequencyHz)||firstHz;
    let minHz=Math.max(firstHz,Math.min(lastHz,cfg.minFrequencyHz));
    let maxHz=Math.max(firstHz,Math.min(lastHz,cfg.maxFrequencyHz));
    if(maxHz<=minHz){maxHz=lastHz;minHz=Math.max(firstHz,lastHz-Math.max(step,1000));}
    view.frequencyStart=minHz;view.frequencyEnd=maxHz;
    for(let x=0;x<width;x++){
      const hz=minHz+(maxHz-minHz)*(x/Math.max(1,width-1));
      const index=Math.max(0,Math.min(bins.length-1,(hz-firstHz)/step)),lo=Math.floor(index),hi=Math.min(bins.length-1,lo+1),mix=index-lo;
      const db0=Number(bins[lo].magnitudeDbfs),db1=Number(bins[hi].magnitudeDbfs),db=(Number.isFinite(db0)?db0:-160)*(1-mix)+(Number.isFinite(db1)?db1:-160)*mix;
      const rgb=waterfallColor(db,cfg),off=x*4;pixels[off]=rgb[0];pixels[off+1]=rgb[1];pixels[off+2]=rgb[2];pixels[off+3]=255;
    }
    const bctx=view.buffer.getContext('2d');bctx.putImageData(data,0,view.cursor);
    view.cursor=(view.cursor+1)%view.buffer.height;view.count=Math.min(view.buffer.height,view.count+1);
    renderWaterfallGrid(canvas,view,cfg);
    const levels=bins.reduce((max,b)=>Math.max(max,Number(b.magnitudeDbfs)||-160),-160);
    if(canvas.id==='tx-spectrum'){
      const power=bins.reduce((sum,b)=>sum+Math.pow(10,(Number(b.magnitudeDbfs)||-160)/10),0)/bins.length;
      $('tx-peak').textContent=`${levels.toFixed(1)} dBFS`;
      $('tx-level').textContent=`${(10*Math.log10(Math.max(power,1e-16))).toFixed(1)} dBFS`;
    }
  }
  function resetWaterfallViews(){for(const id of ['rx-spectrum','tx-spectrum']){const canvas=$(id);if(canvas)resetWaterfallView(canvas);}}
  function updateWaterfallOutputs(cfg){
    const values={
      'waterfall-min-frequency-value':`${cfg.minFrequencyHz} Hz`,'waterfall-max-frequency-value':`${cfg.maxFrequencyHz} Hz`,
      'waterfall-min-db-value':`${cfg.minDbfs} dBFS`,'waterfall-max-db-value':`${cfg.maxDbfs} dBFS`,
      'waterfall-rows-value':`${cfg.rowsPerSecond} ${tr('waterfall.rowsUnit')}`,'waterfall-history-value':`${cfg.historySeconds} ${tr('waterfall.secondsUnit')}`
    };
    for(const [id,value] of Object.entries(values)){const el=$(id);if(el)el.textContent=value;}
  }
  function fillWaterfallForm(cfg){
    const map={'waterfall-min-frequency':'minFrequencyHz','waterfall-max-frequency':'maxFrequencyHz','waterfall-min-db':'minDbfs','waterfall-max-db':'maxDbfs','waterfall-rows':'rowsPerSecond','waterfall-history':'historySeconds','waterfall-background-color':'backgroundColor','waterfall-low-color':'lowColor','waterfall-mid-color':'midColor','waterfall-high-color':'highColor','waterfall-peak-color':'peakColor'};
    for(const [id,key] of Object.entries(map)){const el=$(id);if(el)el.value=cfg[key];}updateWaterfallOutputs(cfg);
  }
  function readWaterfallForm(){
    const w={minFrequencyHz:Number($('waterfall-min-frequency').value),maxFrequencyHz:Number($('waterfall-max-frequency').value),minDbfs:Number($('waterfall-min-db').value),maxDbfs:Number($('waterfall-max-db').value),rowsPerSecond:Number($('waterfall-rows').value),historySeconds:Number($('waterfall-history').value),backgroundColor:$('waterfall-background-color').value,lowColor:$('waterfall-low-color').value,midColor:$('waterfall-mid-color').value,highColor:$('waterfall-high-color').value,peakColor:$('waterfall-peak-color').value};
    if(w.maxFrequencyHz<=w.minFrequencyHz){w.maxFrequencyHz=Math.min(48000,w.minFrequencyHz+1000);$('waterfall-max-frequency').value=w.maxFrequencyHz;}
    if(w.maxDbfs<=w.minDbfs+4){w.maxDbfs=Math.min(0,w.minDbfs+5);$('waterfall-max-db').value=w.maxDbfs;}
    return normalizeWaterfallSettings(w);
  }
  function previewWaterfallForm(){const cfg=readWaterfallForm();state.waterfallPreview=cfg;updateWaterfallOutputs(cfg);resetWaterfallViews();drawWaterfallDemo();}
  function populateWaterfallForm(cfg){fillWaterfallForm(normalizeWaterfallSettings(cfg||DEFAULT_WATERFALL_SETTINGS));}
  function drawWaterfallDemo(){
    const canvas=$('waterfall-demo');if(!canvas)return;const cfg=state.waterfallPreview||state.settings?.waterfall||DEFAULT_WATERFALL_SETTINGS,ctx=canvas.getContext('2d');
    const fake=Array.from({length:640},(_,i)=>({frequencyHz:i*37.5,magnitudeDbfs:-108+Math.max(0,45*Math.exp(-Math.pow((i-170)/24,2))+70*Math.exp(-Math.pow((i-410)/42,2))) }));
    for(let y=0;y<canvas.height;y++){const row=ctx.createImageData(canvas.width,1);for(let x=0;x<canvas.width;x++){const idx=Math.max(0,Math.min(fake.length-1,Math.round(x/Math.max(1,canvas.width-1)*(fake.length-1))));const db=fake[idx].magnitudeDbfs+(Math.sin((x+y*3)/21)*3);const rgb=waterfallColor(db,cfg),o=x*4;row.data[o]=rgb[0];row.data[o+1]=rgb[1];row.data[o+2]=rgb[2];row.data[o+3]=255;}ctx.putImageData(row,0,y);}
  }
  function showSettingsTab(tab){state.settingsTab=tab==='display'?'display':'general';$('settings-page-general').hidden=state.settingsTab!=='general';$('settings-page-display').hidden=state.settingsTab!=='display';document.querySelectorAll('.settings-tab').forEach(b=>b.classList.toggle('active',b.dataset.settingsTab===state.settingsTab));}

  function renderPTTState() {
    const active=!!state.ptt.active;const elapsedEnds=state.ptt.endsAt?new Date(state.ptt.endsAt).getTime():0;const remaining=active&&elapsedEnds?Math.max(0,Math.ceil((elapsedEnds-Date.now())/1000)):15;
    const text=active?tr('ptt.remaining',{seconds:remaining}):tr('ptt.idle');
    for(const id of ['hamlib','serial']){const status=$(`${id}-test-status`),btn=$(`${id}-test-btn`);if(!status||!btn)continue;status.textContent=text+(state.ptt.error?` · ${state.ptt.error}`:'');status.classList.toggle('active',active);btn.textContent=active?tr('ptt.stop'):tr('ptt.test');btn.classList.toggle('running',active);btn.disabled=false;}
    if(state.countdown)clearInterval(state.countdown);if(active)state.countdown=setInterval(renderPTTState,250);
  }
  async function refreshPorts() { try{state.ports=await call('RefreshSerialPorts');renderPorts(true);setMessage(tr('ptt.portCount',{count:state.ports.length}));}catch(e){setMessage(e.message,true);} }
  async function refreshPTTState(){try{state.ptt=await call('GetPTTTestState');renderPTTState();}catch(_){} }
  async function togglePTTTest(){try{if(state.ptt.active){await call('StopPTTTest');}else{state.ptt=await call('GetPTTTestState');await call('StartPTTTest');}await refreshPTTState();}catch(e){setMessage(e.message,true);}}
  function closeSettings(){state.waterfallPreview=normalizeWaterfallSettings(state.settings?.waterfall||DEFAULT_WATERFALL_SETTINGS);populateWaterfallForm(state.waterfallPreview);resetWaterfallViews();$('settings-modal').hidden=true;}
  async function openSettings(){await refreshPTTState();$('settings-modal').hidden=false;setMessage('');const s=state.settings;$('ptt-mode').value=s.ptt.mode;$('rigctl-address').value=s.ptt.rigctlAddress||'127.0.0.1:4532';$('active-high').checked=s.ptt.activeHigh!==false;$('sample-rate').value=String(s.audioSampleRate||48000);$('callsign').value=s.callsign||'N0CALL';$('locator').value=s.locator||'JJ00';state.waterfallPreview=normalizeWaterfallSettings(s.waterfall||DEFAULT_WATERFALL_SETTINGS);populateWaterfallForm(state.waterfallPreview);showSettingsTab('general');updatePTTFields();renderPorts();await refreshAudioDevices();$('rx-device').value=s.rxInputDevice||'';$('tx-device').value=s.txOutputDevice||'';}
  async function saveSettings(){const mode=$('ptt-mode').value;const ptt={mode,rigctlAddress:$('rigctl-address').value.trim(),serialPort:$('serial-port').value,activeHigh:$('active-high').checked};const waterfall=readWaterfallForm();const s={...state.settings,language:state.locale,ptt,rxInputDevice:$('rx-device').value,txOutputDevice:$('tx-device').value,audioSampleRate:Number($('sample-rate').value),callsign:$('callsign').value.trim(),locator:$('locator').value.trim(),templateBackgroundAssetId:state.templateBackgroundAssetId,waterfall};try{await call('SaveSettings',s);state.settings=s;state.waterfallPreview=waterfall;resetWaterfallViews();$('settings-modal').hidden=true;setMessage(tr('settings.saved'))}catch(e){setMessage(e.message,true);}}
  function showPage(name){document.querySelectorAll('.page').forEach(p=>p.classList.remove('active'));$(`page-${name}`).classList.add('active');document.querySelectorAll('.nav-btn').forEach(b=>b.classList.toggle('active',b.dataset.page===name));if(name==='album')loadAlbum();}
  function renderTXState(){
    const button=$('tx-toggle');if(!button)return;
    const supported=state.profiles.some(p=>p.id===state.settings?.txProfile);
    button.disabled=state.txPreparing||(!state.txActive&&!supported);
    button.textContent=state.txPreparing?tr('tx.preparing'):(state.txActive?tr('tx.stop'):tr('tx.start'));
    button.classList.toggle('transmitting',!!state.txActive);
    button.classList.toggle('idle',!state.txActive);
    $('tx-pill').textContent=state.txActive?tr('tx.active'):tr('tx.idle');
    const tone=$('tx-tone-test');if(tone){tone.disabled=state.txPreparing||state.txActive||!supported;tone.textContent=state.txToneActive?tr('tx.testToneActive'):tr('tx.testTone');tone.classList.toggle('running',!!state.txToneActive);}
    $('tx-pill').classList.toggle('active',!!state.txActive);
    $('tx-pill').classList.toggle('idle',!state.txActive);
  }

  function syncRepeatIntervalLock(){
    const repeat=$('repeat-check'),input=$('send-interval');if(!repeat||!input)return;
    if(repeat.checked){if(state.repeatIntervalBase===null)state.repeatIntervalBase=Math.max(0,Number(input.value)||0);input.disabled=true;}
    else{input.disabled=false;state.repeatIntervalBase=null;if(state.repeatCountdownTimer){clearInterval(state.repeatCountdownTimer);state.repeatCountdownTimer=null;}}
  }
  function setRepeatCountdown(seconds){
    const input=$('send-interval');if(!input)return;
    if(!state.repeatIntervalBase)state.repeatIntervalBase=Math.max(0,Number(input.value)||0);
    input.disabled=true;input.value=String(Math.max(0,Math.ceil(Number(seconds)||0)));
  }
  function restoreRepeatInterval(){
    const input=$('send-interval');if(!input)return;
    if(state.repeatIntervalBase!==null)input.value=String(state.repeatIntervalBase);
    input.disabled=!!$('repeat-check')?.checked;
  }

  async function buildCurrentRenderedImage(){
    const canvas=document.createElement('canvas');
    await drawTemplateToCanvas(canvas);
    const template=selectedTemplate();
    return {id:`template-${state.selectedTemplate+1}`,name:template?.name||`Template ${state.selectedTemplate+1}`,pngBase64:canvas.toDataURL('image/png').split(',')[1]};
  }
  async function toggleTX(){
    if(state.txActive){await stopTX();return;}
    if(state.txPreparing)return;
    state.txPreparing=true;renderTXState();
    try{
      let ax25To='CQ';
      if(state.settings?.txProfile==='ssdv-ax25'){
        if(isCQTemplate()){
          $('tx-his-call').value='';
          $('tx-his-call').dataset.valueSource='';
          syncTXFields();
          await renderTemplatePreview();
          ax25To='CQ';
        }else{
          ax25To=String($('tx-his-call').value||'').trim().toUpperCase()||'CQ';
        }
      }
      const image=await buildCurrentRenderedImage();
      const repeat=$('repeat-check').checked;
      const interval=repeat?(state.repeatIntervalBase??(Number($('send-interval').value)||0)):(Number($('send-interval').value)||0);
      await call('StartTXRendered',image,repeat,interval,ax25To);
      state.txActive=true;
      renderTXState();
    }catch(e){setMessage(e.message,true);$('global-state').textContent=e.message;}
    finally{state.txPreparing=false;renderTXState();}
  }
  async function stopTX(){try{await call('StopTX');}catch(e){setMessage(e.message,true);}}
  async function openImageLibrary(){
    const dialog=$('image-library-dialog');
    const root=$('image-library-grid');
    const feedback=$('gallery-feedback');
    if(!dialog.open){
      if(root){root.replaceChildren();const loading=document.createElement('div');loading.className='image-library-empty';loading.textContent=tr('gallery.loading');root.appendChild(loading);}
      if(feedback){feedback.textContent='';feedback.classList.remove('error');}
      dialog.showModal();
    }
    try { await loadGallery(); setMessage(tr('gallery.open')); }
    catch(e) { setMessage(tr('action.failed',{error:e.message}),true); }
  }
  async function loadGallery(){
    state.gallery=await call('GetGallery');
    renderTemplateAssetOptions();
    await renderImageLibrary();
    await renderTemplatePreview();
    return state.gallery;
  }
  async function renderImageLibrary(){
    const root=$('image-library-grid');if(!root)return;
    root.replaceChildren();
    if(!state.gallery.length){
      const empty=document.createElement('div');empty.className='image-library-empty';empty.textContent=tr('gallery.empty');root.appendChild(empty);return;
    }
    // Render cards and action buttons immediately. Thumbnail I/O happens in
    // parallel so a slow disk or a large library never makes the dialog inert.
    for(const asset of state.gallery){
      const card=document.createElement('article');card.className='image-library-card';
      const thumb=document.createElement('div');thumb.className='image-library-thumb';
      const img=document.createElement('img');img.alt=asset.name;img.loading='lazy';img.dataset.assetId=asset.id;
      thumb.appendChild(img);
      const detail=document.createElement('div');detail.className='image-library-details';
      const title=document.createElement('b');title.textContent=asset.name;
      const size=document.createElement('small');size.textContent=asset.width&&asset.height?`${asset.width} × ${asset.height}`:'Image asset';detail.append(title,size);
      const actions=document.createElement('div');actions.className='image-library-actions';
      const bg=document.createElement('button');bg.type='button';bg.className='secondary-btn';bg.textContent=tr('gallery.setBackground');bg.onclick=()=>runAction(()=>setTemplateBackground(asset));
      const layer=document.createElement('button');layer.type='button';layer.className='secondary-btn';layer.textContent=tr('gallery.addLayer');layer.onclick=()=>runAction(()=>{const added=addTemplateComponent('image',asset.id);if(added&&$('image-library-dialog').open)$('image-library-dialog').close();});
      const del=document.createElement('button');del.type='button';del.className='text-btn danger-text';del.textContent=tr('gallery.delete');del.onclick=()=>runAction(()=>deleteGalleryAsset(asset,del));
      actions.append(bg,layer,del);card.append(thumb,detail,actions);root.appendChild(card);
      galleryData(asset.id).then(data=>{if(img.isConnected&&img.dataset.assetId===asset.id)img.src=data;}).catch(error=>{if(img.isConnected){img.alt=tr('template.noImage');img.title=error.message;}});
    }
  }
  async function importGallery(){
    try{const asset=await call('ChooseGalleryImage');if(!asset?.id)return;state.imageDataCache.delete(asset.id);await loadGallery();setMessage(tr('gallery.imported',{name:asset.name}));}
    catch(e){setMessage(tr('action.failed',{error:e.message}),true);}
  }
  async function deleteGalleryAsset(asset,button){
    if(!confirmInlineDelete(`gallery:${asset.id}`,button,`${tr('gallery.delete')} · ${asset.name}`))return;
    await call('DeleteGalleryImage',asset.id);
    state.imageDataCache.delete(asset.id);state.imageElementCache.delete(`gallery:${asset.id}`);
    if(state.templateBackgroundAssetId===asset.id){state.templateBackgroundAssetId='';state.settings.templateBackgroundAssetId='';}
    state.templates=normalizeTemplates(await call('GetTemplates'));
    state.selectedTemplate=Math.min(state.selectedTemplate,state.templates.length-1);
    state.selectedComponent=state.templates[state.selectedTemplate]?.components?.[0]?.id||'';
    $('template-name').value=state.templates[state.selectedTemplate]?.name||'';
    renderTemplateSelect();renderTemplateComponents();renderTemplateProperties();await loadGallery();renderTXState();
    setMessage(tr('gallery.deleted',{name:asset.name}));
  }
  async function setTemplateBackground(asset){
    const previous=state.templateBackgroundAssetId||'';
    state.templateBackgroundAssetId=asset.id;
    const settings={...state.settings,templateBackgroundAssetId:asset.id};
    try{
      await call('SaveSettings',settings);
      state.settings=settings;
      if($('image-library-dialog').open)$('image-library-dialog').close();
      renderTemplateBackground();
      setMessage(tr('template.backgroundSet',{name:asset.name}));
      await renderTemplatePreview();
      return true;
    }catch(e){
      state.templateBackgroundAssetId=previous;
      renderTemplateBackground();
      await renderTemplatePreview();
      throw e;
    }
  }
  function renderTemplateBackground(){
    const asset=state.gallery.find(a=>a.id===state.templateBackgroundAssetId);
    $('template-clear-background').disabled=!asset;
    $('template-clear-background').title=asset?.name||tr('gallery.noBackground');
  }
  async function clearTemplateBackground(){
    const previous=state.templateBackgroundAssetId||'';
    if(!previous)return;
    state.templateBackgroundAssetId='';
    const settings={...state.settings,templateBackgroundAssetId:''};
    try{
      await call('SaveSettings',settings);state.settings=settings;
      renderTemplateBackground();await renderTemplatePreview();setMessage(tr('template.backgroundCleared'));
    }catch(e){state.templateBackgroundAssetId=previous;renderTemplateBackground();throw e;}
  }
  async function loadAlbum(){try{state.album=await call('GetAlbum');renderAlbum();}catch(e){console.error(e);}}
  async function renderAlbum(){
    const root=$('album-list');if(!root)return;root.replaceChildren();
    const filter=$('album-filter')?.value||'all';
    const records=state.album.filter(x=>filter==='all'||x.type.toLowerCase()===filter).slice().sort((a,b)=>new Date(b.createdAt||0)-new Date(a.createdAt||0));
    if(!records.length){const empty=document.createElement('div');empty.className='album-empty';empty.textContent=tr('album.empty');root.appendChild(empty);return;}
    for(const record of records){
      const card=document.createElement('article');card.className='album-card';
      const thumb=document.createElement('button');thumb.className='album-thumb';thumb.type='button';
      const img=document.createElement('img');img.alt=`${record.type} ${record.callsign||''}`;
      let imageData='';try{imageData=await call('GetAlbumImageData',record.id);}catch(_){}
      if(imageData){img.src=imageData;thumb.appendChild(img);thumb.onclick=()=>previewAlbum(record,img.src);}
      else{const placeholder=document.createElement('span');placeholder.className='album-thumb-placeholder';placeholder.textContent=tr('album.noPreview');thumb.appendChild(placeholder);thumb.disabled=true;}
      const info=document.createElement('div');info.className='album-info';
      const chip=document.createElement('span');chip.className=`album-type-chip ${record.type.toLowerCase()==='tx'?'tx':'rx'}`;chip.textContent=record.type==='TX'?'TX · SENT':'RX · RECEIVED';
      const title=document.createElement('b');title.textContent=`${record.profile||'---'}`;
      const time=document.createElement('time');time.textContent=record.createdAt?new Date(record.createdAt).toLocaleString(state.locale):'';
      const meta=document.createElement('small');meta.textContent=`${record.callsign||'---'} · ID ${record.imageId??'--'} · ${record.imageWidth||'--'}×${record.imageHeight||'--'} · ${record.packetsReceived||0}/${record.packetsExpected||0} · ${record.status||''}`;
      info.append(chip,title,time,meta);
      const actions=document.createElement('div');actions.className='album-card-actions';
      const exportButton=document.createElement('button');exportButton.className='secondary-btn';exportButton.textContent=tr('album.export');exportButton.disabled=!img.src.startsWith('data:')&&!record.packetPath;
      exportButton.onclick=async()=>{try{const count=await call('ExportAlbumRecord',record.id);if(count>0)setMessage(tr('album.exported',{count}));}catch(e){setMessage(tr('action.failed',{error:e.message}),true);}};
      const remove=document.createElement('button');remove.className='text-btn danger-text';remove.textContent=tr('album.delete');remove.onclick=async()=>{if(!confirmInlineDelete(`album:${record.id}`,remove,`${tr('album.delete')} · ${record.callsign||record.id}`))return;try{await call('DeleteAlbumRecord',record.id);await loadAlbum();}catch(e){setMessage(e.message,true);}};
      actions.append(exportButton,remove);card.append(thumb,info,actions);root.appendChild(card);
    }
  }
  async function importTemplatePack(){
    const count=await call('ImportTemplatePack');if(!count)return;
    state.imageDataCache.clear();state.imageElementCache.clear();
    state.gallery=await call('GetGallery');state.settings=await call('GetSettings');state.templateBackgroundAssetId=state.settings.templateBackgroundAssetId||'';
    await loadTemplates();renderTemplateAssetOptions();renderImageLibrary();await renderTemplatePreview();
    setMessage(tr('template.packImported',{count}));
  }
  async function exportTemplatePack(){
    const path=await call('ExportTemplatePack');if(!path)return;
    setMessage(tr('template.packExported'));
  }
  async function exportAlbumToDirectory(){
    const count=await call('ExportAlbumToDirectory');if(count>0)setMessage(tr('album.exported',{count}));
  }
  function previewAlbum(record,dataURL){if(!dataURL||!dataURL.startsWith('data:'))return;$('album-preview-image').src=dataURL;$('album-preview-caption').textContent=`${record.type} · ${record.profile} · ${record.callsign||'---'} · ${record.createdAt?new Date(record.createdAt).toLocaleString(state.locale):''}`;$('album-preview-dialog').showModal();}
  function selectedTemplate(){return state.templates[state.selectedTemplate]||null;}
  function selectedComponent(){const template=selectedTemplate();return template?.components?.find(c=>c.id===state.selectedComponent)||null;}
  function componentLabel(c){if(c.type==='text')return `T · ${(c.text||'Text').slice(0,24)}`;if(c.type==='last-rx-image')return 'IMG · Last RX image';return `IMG · ${state.gallery.find(a=>a.id===c.assetId)?.name||'Template image'}`;}
  function defaultTemplatePresets(){
    const text=(id,value,y,fontSize,color='#ffffff')=>({id,type:'text',text:value,x:5,y,width:90,height:16,fontSize,bold:true,color,visible:true,outline:true,outlineWidth:2,outlineColor:'#101820'});
    return [
      {id:'1',name:'CQ',components:[text('cq-kicker','SATELLITE QSO · CALLING',8,15,'#58d4e8'),text('cq-title','CQ CQ CQ',31,46),text('cq-call','{MYCALL}',58,30),text('cq-locator','{LOCATOR}  ·  {DATE}',84,14,'#c7e8ef')]},
      {id:'2',name:'RST',components:[text('rst-kicker','SIGNAL REPORT',12,16,'#58d4e8'),text('rst-call','{HISCALL}',36,28),text('rst-value','RST {REPORT}',61,40),text('rst-footer','FROM {MYCALL}  ·  {LOCATOR}',86,13,'#c7e8ef')]},
      {id:'3',name:'73',components:[text('73-kicker','THANK YOU FOR THE CONTACT',10,15,'#58d4e8'),text('73-main','73',33,78),text('73-call','{MYCALL}',72,29),text('73-locator','{LOCATOR}  ·  {TIME}',88,13,'#c7e8ef')]},
      {id:'4',name:'Blank',components:[]}
    ];
  }
  function normalizeTemplates(items){
    if(!Array.isArray(items)||items.length===0)return defaultTemplatePresets();
    const seen=new Set();
    return items.slice(0,32).map((raw,index)=>{
      const tpl={...(raw||{})};
      let id=String(tpl.id||`template-${index+1}`).trim()||`template-${index+1}`;
      const baseID=id;let suffix=1;while(seen.has(id))id=`${baseID}-${++suffix}`;seen.add(id);tpl.id=id;
      tpl.name=Array.from(String(tpl.name||tr('template.newName',{number:index+1}))).slice(0,48).join('');
      const incoming=Array.isArray(tpl.components)?tpl.components:(Array.isArray(tpl.objects)?tpl.objects:[]);
      const componentIDs=new Set();
      tpl.components=incoming.slice(0,64).flatMap((rawComponent,componentIndex)=>{
        const c={...(rawComponent||{})};
        if(!['text','image','last-rx-image'].includes(c.type)){
          if(c.text!==undefined||c.content!==undefined)c.type='text';
          else return [];
        }
        c.assetId=String(c.assetId||c.imageId||'');
        // An image layer without a resource cannot be rendered or saved by the backend.
        if(c.type==='image'&&!c.assetId)return [];
        let componentID=String(c.id||`${tpl.id}-component-${componentIndex+1}`).trim();
        if(!componentID)componentID=`${tpl.id}-component-${componentIndex+1}`;
        const baseComponentID=componentID;let componentSuffix=1;
        while(componentIDs.has(componentID))componentID=`${baseComponentID}-${++componentSuffix}`;
        componentIDs.add(componentID);c.id=componentID;
        c.text=String(c.text??c.content??'');
        const number=(value,fallback,min,max)=>Math.max(min,Math.min(max,Number.isFinite(Number(value))?Number(value):fallback));
        const legacyWidth=number(c.width,c.type==='text'?90:35,0.1,100);
        const legacyHeight=number(c.height,c.type==='text'?14:30,0.1,100);
        c.width=legacyWidth;c.height=legacyHeight;
        c.scale=number(c.scale,Math.min(100,Math.max(5,Math.min(legacyWidth,legacyHeight*4/3))),5,100);
        // Width/height are retained in old persisted records for compatibility,
        // but image rendering now uses one uniform scale and text is content-sized.
        c.x=number(c.x,5,-1000,1000);
        c.y=number(c.y,5,-1000,1000);
        c.fontSize=number(c.fontSize,28,6,160);
        c.outlineWidth=number(c.outlineWidth,2,0,20);
        c.color=/^#[0-9a-f]{6}$/i.test(c.color||'')?c.color:'#ffffff';
        c.outlineColor=/^#[0-9a-f]{6}$/i.test(c.outlineColor||'')?c.outlineColor:'#000000';
        c.visible=c.visible!==false;c.outline=c.outline!==false;c.bold=!!c.bold;
        return [c];
      });
      // Legacy template backgrounds are migrated to one shared app setting.
      delete tpl.backgroundAssetId;
      tpl.updatedAt=String(tpl.updatedAt||'');
      return tpl;
    });
  }
  function renderTemplateSelect(){
    const select=$('template-select');if(!select)return;
    select.replaceChildren();
    state.templates.forEach((tpl,index)=>{const option=document.createElement('option');option.value=String(index);option.textContent=`${index+1}. ${tpl.name||'Template'}`;select.appendChild(option);});
    if(state.templates.length){state.selectedTemplate=Math.max(0,Math.min(state.selectedTemplate,state.templates.length-1));select.value=String(state.selectedTemplate);}
    $('template-delete').disabled=state.templates.length<=1;
    $('template-duplicate').disabled=state.templates.length>=32;
  }
  function isCQTemplate(template=selectedTemplate()){
    const name=String(template?.name||'').trim().replace(/^\d+[\s.、_-]*/, '').toUpperCase();
    return name==='CQ';
  }
  function clearHisCallForCQTemplate(){
    if(state.settings?.txProfile!=='ssdv-ax25'||!isCQTemplate())return;
    $('tx-his-call').value='';
    $('tx-his-call').dataset.valueSource='';
    syncTXFields();
    renderTemplatePreview();
  }
  function selectTemplate(index){
    const next=Number(index);if(!Number.isInteger(next)||!state.templates[next])return;
    state.selectedTemplate=next;const tpl=selectedTemplate();state.selectedComponent=tpl.components?.[0]?.id||'';
    $('template-name').value=tpl.name||'';renderTemplateSelect();renderTemplateComponents();renderTemplateProperties();renderTemplateBackground();renderTemplatePreview();
    clearHisCallForCQTemplate();
  }
  function fillAssetSelect(select,selected=''){
    if(!select)return;select.replaceChildren();const blank=document.createElement('option');blank.value='';blank.textContent=state.gallery.length?tr('template.unselected'):tr('template.noImage');select.appendChild(blank);
    state.gallery.forEach(asset=>{const option=document.createElement('option');option.value=asset.id;option.textContent=asset.name;select.appendChild(option);});
    if(state.gallery.some(a=>a.id===selected))select.value=selected;
  }
  function renderTemplateAssetOptions(){fillAssetSelect($('component-asset'),selectedComponent()?.assetId||'');}
  function renderTemplateComponents(){
    const root=$('template-components');if(!root)return;root.replaceChildren();const template=selectedTemplate();
    if(!template?.components?.length){const empty=document.createElement('div');empty.className='component-empty';empty.textContent=tr('template.noComponent');root.appendChild(empty);return;}
    template.components.forEach(c=>{const button=document.createElement('button');button.type='button';button.className='component-row'+(c.id===state.selectedComponent?' selected':'');button.textContent=componentLabel(c);button.onclick=()=>selectTemplateComponent(c.id);root.appendChild(button);});
  }
  function renderTemplateProperties(){
    const c=selectedComponent();
    const ids=['component-text','component-image-scale','component-font-size','component-outline-width','component-color','component-outline-color','component-asset','component-bold','component-visible','component-outline'];
    ids.forEach(id=>{const el=$(id);if(el)el.disabled=!c;});
    const isText=c?.type==='text',isImage=c?.type==='image',isImageComponent=isImage||c?.type==='last-rx-image';
    $('component-text-field').hidden=!isText;
    $('component-source-field').hidden=!isImage;
    $('component-image-scale-field').hidden=!isImageComponent;
    $('component-font-size-field').hidden=!isText;
    $('component-color-field').hidden=!isText;
    $('component-bold-field').hidden=!isText;
    if(!c){$('component-text').value='';$('component-asset').value='';return;}
    $('component-text').value=c.text||'';
    $('component-image-scale').value=c.scale??35;
    $('component-font-size').value=c.fontSize||28;$('component-outline-width').value=c.outlineWidth??2;
    $('component-color').value=/^#[0-9a-f]{6}$/i.test(c.color||'')?c.color:'#ffffff';$('component-outline-color').value=/^#[0-9a-f]{6}$/i.test(c.outlineColor||'')?c.outlineColor:'#000000';
    $('component-asset').value=c.assetId||'';$('component-asset').disabled=!isImage;$('component-bold').checked=!!c.bold;$('component-visible').checked=c.visible!==false;$('component-outline').checked=c.outline!==false;
    $('component-text').disabled=!isText;$('component-font-size').disabled=!isText;$('component-color').disabled=!isText;
    $('component-image-scale').disabled=!isImageComponent;
    $('component-outline-color').disabled=false;$('component-outline-width').disabled=false;
    updateComponentRangeOutputs(c);
  }
  function updateComponentRangeOutputs(c=selectedComponent()){
    if(!c)return;
    const values={'component-image-scale-value':`${Number(c.scale??35).toFixed(0)}%`,'component-font-size-value':`${Number(c.fontSize||28).toFixed(0)} px`,'component-outline-width-value':`${Number(c.outlineWidth||0).toFixed(1)} px`};
    for(const [id,value] of Object.entries(values)){const el=$(id);if(el)el.textContent=value;}
  }
  function selectTemplateComponent(id){state.selectedComponent=id;renderTemplateComponents();renderTemplateProperties();renderTemplatePreview();}
  function addTemplateComponent(type,assetID=''){
    const template=selectedTemplate();if(!template){setMessage(tr('template.noSelection'),true);return false;}
    if(type==='image'&&!assetID){setMessage(tr('template.noImage'),true);return false;}
    const component={id:`cmp-${Date.now()}-${Math.floor(Math.random()*100000)}`,type,x:5,y:5,scale:35,fontSize:28,color:'#ffffff',visible:true,outline:true,outlineWidth:2,outlineColor:'#000000'};
    if(type==='text')component.text=tr('template.defaultText');if(type==='image')component.assetId=assetID;
    template.components=template.components||[];if(template.components.length>=64){setMessage(tr('template.componentLimit'),true);return false;}
    template.components.push(component);selectTemplateComponent(component.id);
    setMessage(type==='text'?tr('template.addedText'):tr('template.addedImage'));
    return true;
  }
  function updateTemplateComponent(){
    const c=selectedComponent();if(!c)return;const n=id=>Number($(id).value);
    c.outlineWidth=n('component-outline-width');c.outlineColor=$('component-outline-color').value;c.visible=$('component-visible').checked;c.outline=$('component-outline').checked;
    if(c.type==='text'){c.text=$('component-text').value;c.fontSize=n('component-font-size');c.color=$('component-color').value;c.bold=$('component-bold').checked;}
    else {c.scale=n('component-image-scale');if(c.type==='image')c.assetId=$('component-asset').value;}
    updateComponentRangeOutputs(c);renderTemplateComponents();renderTemplatePreview();
  }
  async function saveTemplates(){
    const template=selectedTemplate();if(!template){setMessage(tr('template.noSelection'),true);return false;}
    template.name=$('template-name').value.trim()||tr('template.newName',{number:state.selectedTemplate+1});template.updatedAt=new Date().toISOString();
    try{await call('SaveTemplates',state.templates.map(({backgroundAssetId,...template})=>template));renderTemplateSelect();setMessage(tr('template.saved'));return true;}
    catch(e){setMessage(tr('action.failed',{error:e.message}),true);return false;}
  }
  async function createTemplatePreset(duplicate=false){
    if(state.templates.length>=32){setMessage(tr('template.limitReached'),true);return false;}
    const previousIndex=state.selectedTemplate;
    const previousComponent=state.selectedComponent;
    const source=selectedTemplate();let tpl;
    if(duplicate&&source){
      tpl=JSON.parse(JSON.stringify(source));
      tpl.id=`template-${Date.now()}-${Math.floor(Math.random()*10000)}`;
      tpl.name=Array.from(tr('template.duplicateName',{name:source.name||tr('template.title')})).slice(0,48).join('');
      tpl.components=(tpl.components||[]).map((c,index)=>({...c,id:`${tpl.id}-${index+1}-${Math.floor(Math.random()*10000)}`}));
    } else {
      tpl={id:`template-${Date.now()}-${Math.floor(Math.random()*10000)}`,name:tr('template.newName',{number:state.templates.length+1}),components:[],updatedAt:new Date().toISOString()};
    }
    state.templates.push(tpl);state.selectedTemplate=state.templates.length-1;state.selectedComponent=tpl.components?.[0]?.id||'';
    $('template-name').value=tpl.name;renderTemplateSelect();renderTemplateComponents();renderTemplateProperties();renderTemplateBackground();await renderTemplatePreview();
    if(!await saveTemplates()){
      state.templates.pop();state.selectedTemplate=Math.min(previousIndex,Math.max(0,state.templates.length-1));state.selectedComponent=previousComponent;
      selectTemplate(state.selectedTemplate);return false;
    }
    setMessage(tr('template.created',{name:tpl.name}));return true;
  }
  async function deleteTemplatePreset(){
    if(state.templates.length<=1)return false;
    const previousTemplates=state.templates.slice();const previousIndex=state.selectedTemplate;const previousComponent=state.selectedComponent;
    const template=selectedTemplate();if(!template)return false;
    if(!confirmInlineDelete(`template:${template.id}`,$('template-delete'),tr('template.deleteConfirm',{name:template.name})))return false;
    state.templates.splice(state.selectedTemplate,1);state.selectedTemplate=Math.min(state.selectedTemplate,state.templates.length-1);
    state.selectedComponent=state.templates[state.selectedTemplate]?.components?.[0]?.id||'';
    $('template-name').value=state.templates[state.selectedTemplate]?.name||'';
    renderTemplateSelect();renderTemplateComponents();renderTemplateProperties();renderTemplateBackground();await renderTemplatePreview();
    try{await call('SaveTemplates',state.templates.map(({backgroundAssetId,...template})=>template));setMessage(tr('template.deleted',{name:template.name}));return true;}
    catch(e){state.templates=previousTemplates;state.selectedTemplate=previousIndex;state.selectedComponent=previousComponent;selectTemplate(previousIndex);setMessage(tr('action.failed',{error:e.message}),true);return false;}
  }
  async function loadTemplates(){
    try{state.templates=normalizeTemplates(await call('GetTemplates'));}
    catch(e){state.templates=defaultTemplatePresets();setMessage(tr('action.failed',{error:e.message}),true);}
    state.selectedTemplate=0;state.selectedComponent=state.templates[0]?.components?.[0]?.id||'';
    $('template-name').value=state.templates[0]?.name||'';renderTemplateSelect();renderTemplateComponents();renderTemplateProperties();renderTemplateBackground();
  }
  function resolveTemplateText(text){const now=new Date();const vars={MYCALL:state.txFields.myCall||state.settings?.callsign||'N0CALL',CALLSIGN:state.txFields.myCall||state.settings?.callsign||'N0CALL',LOCATOR:state.txFields.locator||state.settings?.locator||'JJ00',REPORT:state.txFields.report||'595',HISCALL:state.txFields.hisCall||'TOCALL',QTH:state.txFields.qth||'',MESSAGE:state.txFields.message||'',DATE:now.toISOString().slice(0,10),TIME:now.toISOString().slice(11,19)+' UTC'};return String(text||'').replace(/\{([A-Z]+)\}/g,(_,key)=>vars[key]??`{${key}}`);}
  function imageFromDataURL(data,key=''){
    const cacheKey=key||data;
    if(state.imageElementCache.has(cacheKey))return state.imageElementCache.get(cacheKey);
    const promise=new Promise((resolve,reject)=>{const image=new Image();image.onload=()=>resolve(image);image.onerror=()=>{state.imageElementCache.delete(cacheKey);reject(new Error('Unable to load template image'));};image.src=data;});
    state.imageElementCache.set(cacheKey,promise);return promise;
  }
  async function galleryData(id){if(!id)return '';if(state.imageDataCache.has(id))return state.imageDataCache.get(id);const data=await call('GetGalleryImageData',id);state.imageDataCache.set(id,data);return data;}
  async function galleryImage(id){const data=await galleryData(id);return data?imageFromDataURL(data,`gallery:${id}`):null;}
  async function latestRXData(){if(state.latestRXLoaded)return state.latestRXData;const next=await call('GetLatestRXImageData');if(next!==state.latestRXData)state.imageElementCache.delete('latest-rx');state.latestRXData=next;state.latestRXLoaded=true;return state.latestRXData;}
  function drawImageCover(ctx,img,w,h){const scale=Math.max(w/img.width,h/img.height),dw=img.width*scale,dh=img.height*scale;ctx.drawImage(img,(w-dw)/2,(h-dh)/2,dw,dh);}
  function drawImageContain(ctx,img,x,y,w,h){const scale=Math.min(w/img.width,h/img.height),dw=img.width*scale,dh=img.height*scale,rect={x:x+(w-dw)/2,y:y+(h-dh)/2,width:dw,height:dh};ctx.drawImage(img,rect.x,rect.y,dw,dh);return rect;}
  function imageRectForScale(img,canvas,scalePercent){
    const fit=Math.min(canvas.width/img.width,canvas.height/img.height),factor=fit*Math.max(0.01,Math.min(1,Number(scalePercent??35)/100));
    return {width:Math.max(1,img.width*factor),height:Math.max(1,img.height*factor)};
  }
  function clamp(value,min,max){return Math.max(min,Math.min(max,value));}
  function componentPosition(component,canvas){
    const x=canvas.width*(Number(component.x)||0)/100;
    const y=canvas.height*(Number(component.y)||0)/100;
    return {x,y};
  }
  function getImageAlphaBounds(img){
    let perImage=state.imageOutlineCache.get(img);
    if(!perImage){perImage=new Map();state.imageOutlineCache.set(img,perImage);}
    if(perImage.has('__alphaBounds'))return perImage.get('__alphaBounds');
    const width=Math.max(1,img.naturalWidth||img.width),height=Math.max(1,img.naturalHeight||img.height);
    const sampleScale=Math.min(1,1024/Math.max(width,height));
    const sampleWidth=Math.max(1,Math.round(width*sampleScale)),sampleHeight=Math.max(1,Math.round(height*sampleScale));
    let bounds={x:0,y:0,width:1,height:1};
    try{
      const probe=document.createElement('canvas');probe.width=sampleWidth;probe.height=sampleHeight;
      const pctx=probe.getContext('2d',{willReadFrequently:true});pctx.drawImage(img,0,0,sampleWidth,sampleHeight);
      const pixels=pctx.getImageData(0,0,sampleWidth,sampleHeight).data;
      let left=sampleWidth,top=sampleHeight,right=-1,bottom=-1;
      for(let y=0;y<sampleHeight;y++)for(let x=0;x<sampleWidth;x++)if(pixels[(y*sampleWidth+x)*4+3]>8){
        if(x<left)left=x;if(x>right)right=x;if(y<top)top=y;if(y>bottom)bottom=y;
      }
      bounds=right>=left&&bottom>=top?{x:left/sampleWidth,y:top/sampleHeight,width:(right-left+1)/sampleWidth,height:(bottom-top+1)/sampleHeight}:null;
    }catch(error){console.warn('Unable to inspect image alpha contour; using full image bounds.',error);}
    perImage.set('__alphaBounds',bounds);return bounds;
  }
  function dilateAlphaMask(sourceAlpha,sourceWidth,sourceHeight,radius){
    const r=Math.max(0,Math.floor(Number(radius)||0)),padding=r+1;
    const width=sourceWidth+padding*2,height=sourceHeight+padding*2;
    const alpha=new Uint8ClampedArray(width*height);
    if(!sourceWidth||!sourceHeight)return {alpha,width,height,padding};
    const deque=new Int32Array(sourceWidth);
    for(let dy=-r;dy<=r;dy++){
      const dx=Math.floor(Math.sqrt(Math.max(0,r*r-dy*dy)));
      for(let sy=0;sy<sourceHeight;sy++){
        const targetY=padding+sy+dy;if(targetY<0||targetY>=height)continue;
        const sourceOffset=sy*sourceWidth,targetOffset=targetY*width;let head=0,tail=0,next=0;
        for(let targetX=0;targetX<width;targetX++){
          const right=Math.min(sourceWidth-1,targetX-padding+dx);
          while(next<=right){const value=sourceAlpha[sourceOffset+next];while(tail>head&&sourceAlpha[sourceOffset+deque[tail-1]]<=value)tail--;deque[tail++]=next++;}
          const left=targetX-padding-dx;while(head<tail&&deque[head]<left)head++;
          if(head<tail){const value=sourceAlpha[sourceOffset+deque[head]],index=targetOffset+targetX;if(value>alpha[index])alpha[index]=value;}
        }
      }
    }
    return {alpha,width,height,padding};
  }
  function drawAlphaContourOutline(ctx,img,rect,color,width,cacheKey=''){
    const radius=Math.max(0,Math.min(20,Math.ceil(width)));if(radius<=0)return;
    let perImage=state.imageOutlineCache.get(img);if(!perImage){perImage=new Map();state.imageOutlineCache.set(img,perImage);}
    const key=`${cacheKey}:${Math.round(rect.width)}x${Math.round(rect.height)}:${radius}:${color}`;let mask=perImage.get(key);
    if(!mask){
      const dw=Math.max(1,Math.ceil(rect.width)),dh=Math.max(1,Math.ceil(rect.height)),base=document.createElement('canvas');base.width=dw;base.height=dh;
      const baseContext=base.getContext('2d',{willReadFrequently:true});baseContext.drawImage(img,0,0,dw,dh);
      const pixels=baseContext.getImageData(0,0,dw,dh).data,sourceAlpha=new Uint8Array(dw*dh);for(let i=0;i<sourceAlpha.length;i++)sourceAlpha[i]=pixels[i*4+3];
      const expanded=dilateAlphaMask(sourceAlpha,dw,dh,radius);mask=document.createElement('canvas');mask.width=expanded.width;mask.height=expanded.height;
      const mctx=mask.getContext('2d'),maskPixels=mctx.createImageData(expanded.width,expanded.height);for(let i=0;i<expanded.alpha.length;i++)maskPixels.data[i*4+3]=expanded.alpha[i];
      mctx.putImageData(maskPixels,0,0);mctx.globalCompositeOperation='source-in';mctx.fillStyle=color||'#000000';mctx.fillRect(0,0,mask.width,mask.height);mctx.globalCompositeOperation='source-over';
      if(perImage.size>24){const alphaBounds=perImage.get('__alphaBounds');perImage.clear();if(alphaBounds!==undefined)perImage.set('__alphaBounds',alphaBounds);}perImage.set(key,mask);
    }
    const pad=Math.ceil(width)+1;ctx.drawImage(mask,rect.x-pad,rect.y-pad);ctx.drawImage(img,rect.x,rect.y,rect.width,rect.height);
  }
  function wrapTemplateText(ctx,text,maxWidth){
    const lines=[];for(const paragraph of String(text??'').split('\n')){let line='';for(const character of Array.from(paragraph)){const candidate=line+character;if(line&&ctx.measureText(candidate).width>maxWidth){lines.push(line);line=character;}else line=candidate;}lines.push(line);}return lines.length?lines:[''];
  }
  function measureTemplateText(ctx,component,canvas){
    const resolutionScale=canvas.height/480;
    const fontSize=Math.max(6,(Number(component.fontSize)||24)*resolutionScale);
    const outlinePad=component.outline!==false?Math.max(0,Number(component.outlineWidth)||0)*resolutionScale/2:0;
    const lines=String(resolveTemplateText(component.text)||'').split('\n');
    ctx.save();ctx.font=`${component.bold?'700 ':'400 '}${fontSize}px "Segoe UI", "Yu Gothic UI", "Microsoft YaHei", sans-serif`;
    const widths=lines.map(line=>ctx.measureText(line).width);
    const textWidth=Math.max(1,...widths);
    const lineHeight=Math.max(fontSize*1.22,fontSize+outlinePad*2+1);
    const x=canvas.width*(Number(component.x)||0)/100;
    const y=canvas.height*(Number(component.y)||0)/100;
    const width=textWidth+outlinePad*2;
    const height=Math.max(lineHeight,lines.length*lineHeight)+outlinePad*2;
    ctx.restore();
    return {fontSize,lines,widths,width,height,x,y,outlinePad,edgePad:outlinePad,resolutionScale,truncated:false,lineHeight};
  }
  function drawLastRXPlaceholder(ctx,component,x,y,canvas){
    const aspect=4/3,rect=imageRectForScale({width:aspect,height:1},canvas,component.scale??35);
    const position=componentPosition(component,canvas),frame={x:position.x,y:position.y,width:rect.width,height:rect.height};
    ctx.save();ctx.fillStyle='rgba(18,30,40,0.88)';ctx.fillRect(frame.x,frame.y,frame.width,frame.height);
    ctx.strokeStyle=component.outlineColor||'#9aaab5';ctx.lineWidth=Math.max(1,Number(component.outlineWidth||1)*canvas.height/480);ctx.strokeRect(frame.x+ctx.lineWidth/2,frame.y+ctx.lineWidth/2,Math.max(0,frame.width-ctx.lineWidth),Math.max(0,frame.height-ctx.lineWidth));
    ctx.fillStyle='#d8e0e5';ctx.textAlign='center';ctx.textBaseline='middle';ctx.font=`600 ${Math.max(9,Math.min(18,frame.width/10))}px "Segoe UI", sans-serif`;
    ctx.fillText('LAST RX',frame.x+frame.width/2,frame.y+frame.height/2-8);ctx.font=`${Math.max(7,Math.min(12,frame.width/14))}px "Segoe UI", sans-serif`;ctx.fillText(tr('template.rxPlaceholder'),frame.x+frame.width/2,frame.y+frame.height/2+9);ctx.restore();
    return frame;
  }
  async function drawTemplateToCanvas(canvas,boundsTarget=null){
    const dims={low:[320,240],medium:[448,336],high:[640,480]}[state.settings?.quality||'medium']||[448,336];
    if(canvas.width!==dims[0])canvas.width=dims[0];if(canvas.height!==dims[1])canvas.height=dims[1];
    const ctx=canvas.getContext('2d');ctx.fillStyle='#17232d';ctx.fillRect(0,0,canvas.width,canvas.height);
    const componentBounds=boundsTarget||new Map();componentBounds.clear();const template=selectedTemplate();
    const backgroundID=state.templateBackgroundAssetId;try{let backgroundImage=null;if(backgroundID)backgroundImage=await galleryImage(backgroundID);if(backgroundImage)drawImageCover(ctx,backgroundImage,canvas.width,canvas.height);}catch(e){console.warn(e);}
    for(const c of template?.components||[]){
      if(c.visible===false)continue;let selectionRect=null;
      if(c.type==='image'||c.type==='last-rx-image'){
        try{
          let img=null;if(c.type==='last-rx-image'){const data=await latestRXData();if(data)img=await imageFromDataURL(data,'latest-rx');}else img=await galleryImage(c.assetId);
          if(img){
            const size=imageRectForScale(img,canvas,c.scale??35),position=componentPosition(c,canvas),rect={x:position.x,y:position.y,width:size.width,height:size.height};
            const alpha=getImageAlphaBounds(img);if(alpha)selectionRect={x:rect.x+rect.width*alpha.x,y:rect.y+rect.height*alpha.y,width:rect.width*alpha.width,height:rect.height*alpha.height};
            if(alpha&&c.outline!==false&&Number(c.outlineWidth)>0)drawAlphaContourOutline(ctx,img,rect,c.outlineColor||'#000000',Math.max(1,Number(c.outlineWidth)*canvas.height/480),c.assetId||'last-rx');
            else ctx.drawImage(img,rect.x,rect.y,rect.width,rect.height);
          }else if(c.type==='last-rx-image'){selectionRect=drawLastRXPlaceholder(ctx,c,0,0,canvas);}
        }catch(e){console.warn(e);if(c.type==='last-rx-image')selectionRect=drawLastRXPlaceholder(ctx,c,0,0,canvas);}
      }else if(c.type==='text'){
        const layout=measureTemplateText(ctx,c,canvas);selectionRect={x:layout.x-layout.outlinePad,y:layout.y-layout.outlinePad,width:layout.width,height:layout.height};
        ctx.save();ctx.font=`${c.bold?'700 ':'400 '}${layout.fontSize}px "Segoe UI", "Yu Gothic UI", "Microsoft YaHei", sans-serif`;ctx.textBaseline='top';ctx.textAlign='left';ctx.lineJoin='round';
        layout.lines.forEach((line,index)=>{const ty=layout.y+index*layout.lineHeight;if(c.outline!==false&&Number(c.outlineWidth)>0){ctx.strokeStyle=c.outlineColor||'#000000';ctx.lineWidth=Math.max(1,Number(c.outlineWidth)*layout.resolutionScale);ctx.strokeText(line,layout.x,ty);}ctx.fillStyle=c.color||'#ffffff';ctx.fillText(line,layout.x,ty);});ctx.restore();
      }
      if(selectionRect&&selectionRect.width>0&&selectionRect.height>0)componentBounds.set(c.id,selectionRect);
    }
    return componentBounds;
  }
  function renderTemplatePreview(){
    const visibleCanvas=$('template-canvas');if(!visibleCanvas)return Promise.resolve(false);
    const version=++state.previewRenderVersion;
    if(state.previewRAF)cancelAnimationFrame(state.previewRAF);
    if(state.previewPendingResolve){state.previewPendingResolve(false);state.previewPendingResolve=null;}
    return new Promise(resolve=>{
      state.previewPendingResolve=resolve;
      state.previewRAF=requestAnimationFrame(async()=>{
        state.previewRAF=0;
        const finish=value=>{if(state.previewPendingResolve===resolve)state.previewPendingResolve=null;resolve(value);};
        const staging=document.createElement('canvas'),componentBounds=new Map();
        try{
          await drawTemplateToCanvas(staging,componentBounds);
          if(version!==state.previewRenderVersion){finish(false);return;}
          state.componentBounds=componentBounds;
          if(visibleCanvas.width!==staging.width)visibleCanvas.width=staging.width;
          if(visibleCanvas.height!==staging.height)visibleCanvas.height=staging.height;
          const ctx=visibleCanvas.getContext('2d');
          ctx.clearRect(0,0,visibleCanvas.width,visibleCanvas.height);
          ctx.drawImage(staging,0,0);
          renderTemplateBackground();finish(true);
        }catch(e){console.warn(e);finish(false);}
      });
    });
  }
  function bindTemplateCanvas(){
    const canvas=$('template-canvas');if(!canvas)return;let drag=null;
    canvas.addEventListener('pointerdown',event=>{
      const rect=canvas.getBoundingClientRect(),px=(event.clientX-rect.left)*canvas.width/rect.width,py=(event.clientY-rect.top)*canvas.height/rect.height,components=selectedTemplate()?.components||[];
      for(let i=components.length-1;i>=0;i--){
        const c=components[i],bounds=state.componentBounds.get(c.id);
        if(bounds&&px>=bounds.x&&px<=bounds.x+bounds.width&&py>=bounds.y&&py<=bounds.y+bounds.height){
          selectTemplateComponent(c.id);
          const anchorX=canvas.width*(Number(c.x)||0)/100,anchorY=canvas.height*(Number(c.y)||0)/100;
          drag={id:c.id,dx:px-anchorX,dy:py-anchorY};canvas.setPointerCapture(event.pointerId);break;
        }
      }
      // A layer moved completely off-canvas is no longer hit-testable in the
      // visible composite. Keep it recoverable: select it in the layer list,
      // then drag from the empty canvas to reposition its center.
      if(!drag&&state.selectedComponent){
        const selected=components.find(c=>c.id===state.selectedComponent),bounds=selected&&state.componentBounds.get(selected.id);
        const fullyOutside=bounds&&(bounds.x+bounds.width<=0||bounds.y+bounds.height<=0||bounds.x>=canvas.width||bounds.y>=canvas.height);
        if(selected&&fullyOutside){
          drag={id:selected.id,dx:bounds.width/2,dy:bounds.height/2};canvas.setPointerCapture(event.pointerId);
        }
      }
    });
    canvas.addEventListener('pointermove',event=>{
      if(!drag)return;
      const rect=canvas.getBoundingClientRect(),px=(event.clientX-rect.left)*canvas.width/rect.width,py=(event.clientY-rect.top)*canvas.height/rect.height,c=selectedTemplate()?.components?.find(x=>x.id===drag.id);if(!c)return;
      c.x=(px-drag.dx)/canvas.width*100;c.y=(py-drag.dy)/canvas.height*100;renderTemplatePreview();
    });
    const stop=()=>{drag=null;};canvas.addEventListener('pointerup',stop);canvas.addEventListener('pointercancel',stop);
  }
  function escapeHTML(s){return String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));}
  

  function syncTXFields(){
    state.txFields={myCall:$('tx-my-call').value.trim()||state.settings?.callsign||'N0CALL',hisCall:$('tx-his-call').value.trim(),qth:$('tx-qth').value,locator:$('tx-locator').value.trim()||state.settings?.locator||'JJ00',report:$('tx-report').value.trim()||'595',message:$('tx-message').value};
  }
  function applyDecodedCallsign(value){
    const callsign=String(value||'').trim().toUpperCase();
    if(!callsign)return false;
    if(state.settings?.txProfile==='ssdv-ax25'&&isCQTemplate())return false;
    const field=$('tx-his-call');
    if(field.dataset.valueSource==='manual')return false;
    field.value=callsign;
    field.dataset.valueSource='rx';
    syncTXFields();
    renderTemplatePreview();
    return true;
  }
  function bindUI(){
    // Paint local defaults immediately so the preset dropdown is populated
    // before any device, audio or gallery enumeration can block startup.
    $('template-name').value=selectedTemplate()?.name||'';
    renderTemplateSelect();renderTemplateComponents();renderTemplateProperties();renderTemplateBackground();
    renderTemplatePreview();
    $('language-select').addEventListener('change',async e=>{await applyLocale(e.target.value);state.settings.language=e.target.value;try{await call('SaveSettings',state.settings);}catch(err){console.error(err);}});
    document.querySelectorAll('.nav-btn').forEach(b=>b.addEventListener('click',()=>showPage(b.dataset.page)));
    $('rx-toggle').onclick=toggleRX;$('audio-refresh').onclick=refreshAudioDevices;$('settings-open').onclick=openSettings;$('settings-close').onclick=closeSettings;$('settings-cancel').onclick=closeSettings;$('settings-save').onclick=saveSettings;document.querySelectorAll('.settings-tab').forEach(b=>b.addEventListener('click',()=>showSettingsTab(b.dataset.settingsTab)));['waterfall-min-frequency','waterfall-max-frequency','waterfall-min-db','waterfall-max-db','waterfall-rows','waterfall-history','waterfall-background-color','waterfall-low-color','waterfall-mid-color','waterfall-high-color','waterfall-peak-color'].forEach(id=>$(id).addEventListener('input',previewWaterfallForm));$('waterfall-reset').onclick=()=>{populateWaterfallForm(DEFAULT_WATERFALL_SETTINGS);previewWaterfallForm();};$('ptt-mode').onchange=updatePTTFields;$('serial-refresh').onclick=refreshPorts;$('hamlib-test-btn').onclick=togglePTTTest;$('serial-test-btn').onclick=togglePTTTest;
$('template-add-text').onclick=()=>runAction(()=>addTemplateComponent('text'));$('template-add-rx').onclick=()=>runAction(()=>addTemplateComponent('last-rx-image'));$('template-add-image').onclick=()=>runAction(openImageLibrary);$('gallery-close').onclick=()=>$('image-library-dialog').close();$('gallery-import').onclick=()=>runAction(importGallery);$('template-clear-background').onclick=()=>runAction(clearTemplateBackground);$('template-save').onclick=()=>runAction(saveTemplates);$('template-import-pack').onclick=()=>runAction(importTemplatePack);$('template-export-pack').onclick=()=>runAction(exportTemplatePack);$('template-add').onclick=()=>runAction(()=>createTemplatePreset(false));$('template-duplicate').onclick=()=>runAction(()=>createTemplatePreset(true));$('template-delete').onclick=()=>runAction(deleteTemplatePreset);$('template-select').onchange=e=>selectTemplate(e.target.value);$('template-name').oninput=()=>{const tpl=selectedTemplate();if(tpl){tpl.name=$('template-name').value;renderTemplateSelect();$('template-select').value=String(state.selectedTemplate);}};$('component-delete').onclick=()=>{const tpl=selectedTemplate();if(!tpl)return;tpl.components=(tpl.components||[]).filter(c=>c.id!==state.selectedComponent);state.selectedComponent=tpl.components[0]?.id||'';renderTemplateComponents();renderTemplateProperties();renderTemplatePreview();setMessage(tr('template.componentDeleted'));};['component-text','component-image-scale','component-font-size','component-outline-width','component-color','component-outline-color','component-asset','component-bold','component-visible','component-outline'].forEach(id=>{const el=$(id);el.addEventListener('input',updateTemplateComponent);el.addEventListener('change',updateTemplateComponent);});$('component-asset').onchange=updateTemplateComponent;['tx-my-call','tx-his-call','tx-qth','tx-locator','tx-report','tx-message'].forEach(id=>$(id).addEventListener('input',()=>{if(id==='tx-his-call')$('tx-his-call').dataset.valueSource=$('tx-his-call').value.trim()?'manual':'';syncTXFields();renderTemplatePreview();}));bindTemplateCanvas();$('tx-toggle').onclick=toggleTX;$('tx-tone-test').onclick=()=>runAction(()=>call('Start1500HzTestTone'));$('repeat-check').addEventListener('change',syncRepeatIntervalLock);$('send-interval').addEventListener('input',()=>{if(!$('repeat-check').checked)state.repeatIntervalBase=null;});syncRepeatIntervalLock();$('tx-ax25-ssid').addEventListener('change',async()=>{const value=Number($('tx-ax25-ssid').value);if(!Number.isInteger(value)||value<0||value>15){$('tx-ax25-ssid').value=String(state.settings?.ax25Ssid??0);setMessage('AX.25 SSID must be 0–15',true);return;}state.settings.ax25Ssid=value;try{await call('SaveSettings',state.settings);}catch(e){setMessage(e.message,true);}});$('quality-select').onchange=e=>{state.settings.quality=e.target.value;call('SaveSettings',state.settings).catch(console.error);renderTemplatePreview();};$('album-filter').onchange=renderAlbum;$('album-export-all').onclick=()=>runAction(exportAlbumToDirectory);$('album-preview-close').onclick=()=>$('album-preview-dialog').close();
    ['rx-spectrum','tx-spectrum'].forEach(id=>drawIdleWaterfall($(id)));drawWaterfallDemo();
  }

  async function init(){try{
      // Load the small template index first; device enumeration can be slow on some hosts.
      await loadTemplates();
      state.settings=await call('GetSettings');
      state.settings.ax25Ssid=Number.isInteger(Number(state.settings.ax25Ssid))?Number(state.settings.ax25Ssid):0;
      state.settings.waterfall=normalizeWaterfallSettings(state.settings.waterfall||DEFAULT_WATERFALL_SETTINGS);state.waterfallPreview=state.settings.waterfall;populateWaterfallForm(state.waterfallPreview);resetWaterfallViews();
      state.profiles=await call('GetProfiles');
      state.ports=await call('GetSerialPorts');
      state.gallery=await call('GetGallery');
      state.templateBackgroundAssetId=state.settings.templateBackgroundAssetId||'';
      renderTemplateAssetOptions();state.txFields.myCall=state.settings.callsign||'N0CALL';state.txFields.locator=state.settings.locator||'JJ00';
      $('tx-my-call').value=state.txFields.myCall;$('tx-locator').value=state.txFields.locator;
      await applyLocale(state.settings.language||'zh-CN');$('quality-select').value=state.settings.quality||'medium';
      renderProfiles();renderPorts();renderTXState();renderImageLibrary();renderTemplatePreview();
    }catch(e){
      console.warn(e);state.settings={language:'zh-CN',ptt:{mode:'vox',activeHigh:true},rxProfile:'ssdv-ax25',txProfile:'ssdv-ax25',ax25Ssid:0,quality:'medium',callsign:'N0CALL',locator:'JJ00',templateBackgroundAssetId:'',waterfall:{...DEFAULT_WATERFALL_SETTINGS}};state.waterfallPreview=state.settings.waterfall;populateWaterfallForm(state.waterfallPreview);resetWaterfallViews();state.templateBackgroundAssetId='';
      state.profiles=[{id:'ssdv-ax25',name:'SSDV AX.25'},{id:'ssdv-ccsds-1200',name:'SSDV CCSDS 1200'},{id:'ssdv-ccsds-2400',name:'SSDV CCSDS 2400'},{id:'pcsi-ccsds-1200',name:'PCSI CCSDS 1200'},{id:'pcsi-ccsds-2400',name:'PCSI CCSDS 2400'}];
      renderProfiles();renderTXState();await loadTemplates();renderImageLibrary();renderTemplatePreview();await applyLocale('zh-CN');
    }
    if(window.runtime?.EventsOn){window.runtime.EventsOn('ptt:test-state',s=>{state.ptt=s;renderPTTState();});window.runtime.EventsOn('ptt:error',m=>setMessage(m,true));window.runtime.EventsOn('audio:error',m=>{$('global-state').textContent=m;});window.runtime.EventsOn('rx:spectrum',bins=>drawWaterfallData($('rx-spectrum'),bins));window.runtime.EventsOn('tx:spectrum',bins=>drawWaterfallData($('tx-spectrum'),bins));window.runtime.EventsOn('rx:level',v=>{$('rx-level').textContent=`${Number(v.rmsDbfs).toFixed(1)} dBFS`;$('rx-peak').textContent=Number(v.peak).toFixed(3);});window.runtime.EventsOn('tx:progress',v=>{$('tx-progress').style.width=`${Math.max(0,Math.min(1,Number(v)))*100}%`;});window.runtime.EventsOn('rx:image',u=>{if(u.error){$('global-state').textContent=u.error;return;}if(u.started){const view=document.querySelector('.image-view');view.replaceChildren();state.latestRXData='';state.latestRXLoaded=false;}$('rx-callsign').textContent=u.callsign||'---';if(u.callsign)applyDecodedCallsign(u.callsign);if(u.hasAX25SSID){$('rx-ssid').textContent=String(u.ax25SourceSSID??0);$('rx-ssid-wrap').hidden=false;}else if(u.profile!=='ssdv-ax25')$('rx-ssid-wrap').hidden=true;$('rx-image-id').textContent=u.imageId??'--';$('rx-packets').textContent=u.packetsExpected?`${u.packetsReceived||0} / ${u.packetsExpected}`:`${u.packetsReceived||0} / --`;$('rx-mode').textContent=u.profile||'---';if(u.pngBase64){state.latestRXData='';state.latestRXLoaded=false;}if(u.pngBase64){const view=document.querySelector('.rx-pane > .image-view');view.replaceChildren();const stage=document.createElement('div');stage.className='rx-image-stage';const img=document.createElement('img');img.className='rx-preview-image';img.src=`data:image/png;base64,${u.pngBase64}`;img.alt=`${u.codec||'Digital'} received image preview`;img.draggable=false;stage.appendChild(img);view.appendChild(stage);}if((u.pngBase64||u.packetPath)&&document.querySelector('#page-album').classList.contains('active'))loadAlbum();if(u.complete){$('global-state').textContent=u.codec==='SSDV'?tr('rx.ssdvPending'):tr('rx.complete');state.latestRXData='';state.latestRXLoaded=false;renderTemplatePreview();}else if(u.packetsExpected){$('global-state').textContent=tr('rx.packetCount',{received:u.packetsReceived||0,expected:u.packetsExpected});}});window.runtime.EventsOn('tx:tone-state',v=>{state.txToneActive=!!v.active;renderTXState();});window.runtime.EventsOn('tx:state',v=>{state.txActive=!!v.active;renderTXState();if(!state.txActive)$('tx-progress').style.width='0%';});window.runtime.EventsOn('tx:status',v=>{if(v.phase==='completed'){$('global-state').textContent=tr('tx.done');restoreRepeatInterval();}else if(v.phase==='countdown'){setRepeatCountdown(Number(v.error)||0);$('global-state').textContent=`下一次发射：${Math.max(0,Number(v.error)||0)} s`;}else if(v.phase==='sending'){restoreRepeatInterval();}});window.runtime.EventsOn('tx:error',m=>{setMessage(m,true);$('global-state').textContent=m;});}
    await refreshPTTState();await refreshAudioDevices();await refreshAudioState();
  }

  document.addEventListener('DOMContentLoaded',()=>{bindUI();init();});
})();
