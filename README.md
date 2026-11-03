# SATSSDV-Go

SATSSDV-Go is a Go/Wails desktop workbench for amateur-satellite SSDV and PCSI image links. It provides separate RX and TX profile selection, shared-mode full-duplex audio, RX/TX audio waterfall displays with configurable display settings, PTT control, image templates, a local gallery, an album, and a repeat-send scheduler.

## Transport profiles

- SSDV AX.25
- SSDV CCSDS 1200
- SSDV CCSDS 2400
- PCSI CCSDS 1200
- PCSI CCSDS 2400

RX and TX profiles are selected independently. SSDV and PCSI MMT both have Go packet transmit paths and image receive reconstruction paths. SSDV accepts baseline sequential 8-bit 4:2:0 images, transcodes DCT coefficients into SSDV quantization tables, emits 256-byte FEC/No-FEC packets with CRC32/RS8, and reconstructs complete contiguous packet sets into JPEG/PNG. The RX album retains the corrected raw SSDV packet stream alongside the reconstructed image. PCSI MMT supports packet generation, RS correction, sparse-measurement reconstruction and PNG previews.

## Current features

- AX.25 UI/HDLC framing, FCS, bit stuffing, NRZI and 1200-baud AFSK TX/RX foundation.
- CCSDS ASM framing, PN randomization, 1200-baud modulation and the ARCSSD-style 2400-bit/s pulse-shaped TX/RX path. Continuous receive framing is tested across arbitrary audio callback chunk boundaries for all five profiles.
- SSDV JPEG coefficient transcoding, 256-byte FEC/No-FEC packet generation, Base40, CRC32 and RS8 validation/correction. RX supports out-of-order packet arrival, duplicate suppression and complete contiguous packet-set reconstruction to PNG.
- PCSI MMT packetization and RS8 error correction, with a Go DCT/IDCT and iterative sparse reconstruction path.
- Independent RX capture and TX playback through optional Malgo shared-mode audio streams.
- RX and TX scrolling audio waterfalls, adjustable frequency/dB range, row rate, history length and per-level color palette, input/output level reporting, stateful resampling/demodulation, and a bounded packet-processing worker. PCSI image reconstruction runs on a separate single-worker queue; newer snapshots are coalesced while optimization is active.
- PTT through VOX, Hamlib `rigctld`, serial RTS or DTR. RTS/DTR ports are automatically enumerated and can be refreshed manually.
- RTS, DTR and Hamlib PTT test buttons with a 15-second backend-enforced timeout and manual early release.
- Low, medium and high image presets: 320x240, 448x336 and 640x480.
- SATSSTV-Go-style TX template editor with a dropdown preset selector, four initial CQ/RST/73/blank presets, and support for up to 32 independently saved presets. Presets can be created, duplicated, renamed and deleted. The editor supports draggable text/image components, one shared workbench background, QSO fields and a built-in image-library dialog. The library supports import, preview, set-as-template-background, add-as-layer and delete.
- The TX canvas contains no preview/quality overlay text. The Start/Stop action is a single toggle immediately left of TX IDLE. The template dropdown and preset-management controls remain above the editor. The preview uses a fixed 4:3 CSS viewport, independently of the selected image encoding resolution. The underlying canvas is still rendered at 320×240, 448×336 or 640×480 for transmission and is uniformly scaled with its original aspect ratio inside the fixed preview viewport. A decoded remote callsign automatically populates HisCall; manual entry takes precedence, and clearing the field re-enables automatic fill. The top navigation contains only Workbench and Album (「工作台」 and 「相册」 in Simplified Chinese). Album is a dedicated page and remains hidden until its top navigation tab is selected; it never occupies space beneath the RX/TX workbench. The image library is opened from the TX template editor instead of occupying a separate top-level tab.
- The four default template options are rendered immediately at DOM readiness, before backend or device enumeration, and template data is fetched before potentially slow audio/serial discovery. Template, component and gallery controls are bound at DOM readiness. The image-library modal opens immediately; asset cards and their Set Background/Add Layer/Delete actions are rendered before thumbnail reads finish. The component editor owns an independent vertical scroll area while image quality, repeat and interval controls remain grouped in the TX title bar, preventing visual overlap. Successful background/layer actions close the library so the canvas result is immediately visible; errors are shown inside the modal and in the workbench status toast. Legacy template data is normalized, invalid image components without a gallery asset are discarded, and component geometry is clamped to valid canvas bounds.
- RX/TX album cards include thumbnails, RX/TX filtering, large-image preview, timestamped PNG export and record deletion.
- Single-template transmission with one combined Start/Stop button in the TX title bar. The UI exposes no multi-image TX queue. Repeat mode retransmits only the currently selected rendered template after the configured interval. The backend scheduler supports all five configured profiles, including SSDV AX.25/CCSDS and PCSI CCSDS.
- Simplified Chinese, English and Japanese UI dictionaries.
- SSDV packet sanity checks include image geometry, MCU-count, MCU-index and payload-offset bounds, in addition to RS8 correction and CRC32 validation.

## Build and test

The repository pins Wails CLI/runtime v2.17.1 and requires Go 1.26.0 or newer.

```powershell
$env:GOTOOLCHAIN = "auto"
go mod tidy
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1
$env:CGO_ENABLED = "1"
wails build -clean -tags "wails malgo" -platform windows/amd64
```

The Wails desktop build also requires the native WebView toolchain for the target operating system. The `wails` build tag enables the Wails application entry point. The `malgo` tag enables the native shared-mode audio runtime. Core packages can be tested without the GUI and native audio dependencies:

```powershell
go test ./...
go test -race ./...
go vet ./...
node --check frontend/app.js
node frontend/ui_structure.test.cjs
```

## Data storage

Settings, gallery files, templates and album entries are stored under the platform's user configuration/data directory in a `SATSSDV-Go` folder. Template presets store only their own name and component references. The workbench background asset ID is stored separately in application settings and remains unchanged when switching or saving presets. Gallery deletion clears matching template references before removing the image file. RX PCSI and reconstructed SSDV images are archived as PNG. SSDV album records also retain the corrected raw `.ssdv` packet stream.

## Important limitations

The SSDV codec currently supports baseline sequential 8-bit 4:2:0 JPEG and complete contiguous packet sequences. It emits sentinel MCU metadata rather than per-packet MCU-boundary resynchronization metadata, so a missing packet prevents SSDV JPEG reconstruction; progressive/grayscale JPEG, restart intervals and non-4:2:0 sampling are rejected. Independent packet-for-packet comparison against the upstream SSDV CLI/Hamstation is still required. The PCSI reconstruction solver is an independent Go OWL-QN/L-BFGS-style L1 implementation. Its orthonormal 2-D DCT has a numeric reference-vector test against OpenCV, and the MMT packet path has synthetic full-image and receive-worker tests. Pixel-for-pixel comparison against outputs from the supplied `pcsitximage_mmt.py` and `pcsidecoder_mmt.py` is still required. The CCSDS 2400 path has synthetic waveform round-trip tests, while real recorded captures, AFC/carrier offset tolerance and on-air interoperability remain unverified.

See [docs/PROTOCOL_STATUS.md](docs/PROTOCOL_STATUS.md) for the per-profile implementation matrix, test scope and remaining work. Licensing of translated ARCSSD modem algorithms and SSDV algorithm/constants based on upstream references must be reviewed before public binary distribution.

## TX 发射设置位置调整（2026-11-03）

- 图片质量、循环发送开关和发送间隔已移动到 TX 标题栏操作区，排列在合并的开始/停止发射按钮左侧。
- TX 侧栏不再显示发射设置区，组件编辑区域获得更多纵向空间。
- 发射设置中不再提供图片图库入口。图库仍通过模板组件区域的“＋图像”按钮打开，图片导入、设为底图和添加图层流程保持不变。
- 三项设置仍使用原有控件 ID 和状态逻辑，图片质量保存方式及循环发送间隔参数不变。
- 增加 UI 结构回归测试，验证三项设置只出现一次、位于 TX 按钮左侧，并且发射设置区域不包含图库入口。


## TX editor and settings refinements (2026-11-03)

- Live component edits render into an offscreen canvas and atomically replace the visible frame only after the latest render completes. Superseded render jobs are cancelled by generation ID, and decoded image bitmaps are cached to prevent preview flicker.
- The workbench background asset is global to the TX editor. It is persisted in `settings.json`, not in `templates.json`; template switching never changes it, and the backend strips any legacy background fields when saving presets. Startup migrates one legacy per-template background to the shared setting and clears the old fields.
- The clear-background action sits in the template-component heading, immediately before Add Text. The former background-information panel beside QSO fields is removed.
- QSO fields occupy the left TX column beneath the fixed 4:3 preview. The component editor occupies the right column and stretches to the same workbench row bottom; their shared vertical boundary is aligned.
- Audio-device default markers, ready/off status and serial-port empty/count messages are localized. Changing the interface language re-renders dynamically generated audio and serial device options so labels cannot remain in the previous language.
- RTS and DTR are selected directly from the PTT method dropdown. There is no second PTT signal selector; active-high remains an electrical polarity preference.

### TX preview aspect-ratio behavior

The TX preview uses a fixed 4:3 viewport. The canvas bitmap keeps its intrinsic aspect ratio and is uniformly reduced to fit the viewport (`object-fit: contain`); it is never stretched to the CSS box. The low, medium, and high image targets remain 320×240, 448×336, and 640×480 respectively, so quality changes do not alter the preview frame size.


## Display and template component controls (2026-11-03)

- RX and TX audio monitors are scrolling waterfalls. Frequency runs horizontally and time runs vertically; each row is generated from the latest audio FFT magnitudes and stored in a bounded circular history buffer. The history buffer is scaled to the visible canvas height so the configured history duration always fills the display proportionally.
- Settings now has a dedicated Display tab for minimum/maximum frequency, dBFS range, waterfall row rate, history duration and background/weak/medium/strong/peak colors. These settings are persisted in `settings.json` and applied to both RX and TX. Frequency selection is bounded to the 48 kHz maximum Nyquist range supported by the 96 kHz audio-rate option; the renderer also clamps the visible axis to the bins available at the active sample rate.
- Template component X/Y/width/height/font size/outline width controls are sliders. Text components expose position, font size, outline and text appearance; their dimensions are computed from glyph metrics and available canvas area, with no width/height or image-source controls. Image components expose position, width, height, image source and outline controls, without text font-size or text-color fields.
- Text outlines use glyph stroking. Image outlines are generated from the decoded bitmap alpha mask, and their selection bounds use the non-transparent image contour rather than the full source canvas rectangle.
- When the gallery contains assets, the image-source selector shows a “Not selected” placeholder. The empty-library message is shown only when there are no gallery images.


### Waterfall and contour rendering refinement

- The TX/RX monitors no longer use an FFT line graph. They retain time-ordered intensity rows in a bounded rolling waterfall buffer, with frequency on the horizontal axis and time on the vertical axis.
- The Display settings page persists frequency and dBFS ranges, row rate, history duration, and five palette colors used by both monitors.
- Text component outlines follow glyph contours through canvas text stroking. Image outlines are generated from the image alpha mask, so transparent margins do not produce a rectangular frame. The editor no longer draws a rectangular selection frame around the selected component; selection is indicated in the component list, and hit bounds are kept separately for dragging.
- X/Y, image width/height, font size and outline width use range sliders. Text-only and image-only controls are hidden according to component type.

### Component rendering size-boundary fix (2026-11-03)

- Fixed large-font disappearance when the text anchor is near the lower canvas edge. The previous line-fit calculation could return zero visible lines once a complete line exceeded the remaining height. The renderer now keeps at least the first line and lets the canvas clip pixels outside its viewport, so changing font size cannot silently remove the whole text component.
- Text hit bounds are limited to the visible canvas area. Font size, line wrapping, bold and glyph outline rendering continue to use the selected output resolution.
- Image and Last RX components were checked together because they share the same image-render path. They do not use the text font-size calculation. Fully transparent images no longer create a misleading full-frame hit area.
- Image outline generation now uses circular alpha-mask dilation with a sliding-window maximum instead of repeatedly blitting the entire component bitmap for every outline sample. This bounds work for thick outlines and keeps the visible image overlaid above its outline.
- The front-end regression test exercises 160 px text at lower-canvas positions for all three output sizes and validates circular alpha dilation on a synthetic transparent image.


## Template positioning and image scale

Template component positions are adjusted by dragging directly on the TX preview. The property panel no longer exposes numeric horizontal or vertical position controls. Image and Last RX layers share one proportional scale slider, preserving each source image's aspect ratio. Text is measured, wrapped and clamped to the 4:3 preview canvas; when exceptionally long text cannot fit at the minimum supported size, the final visible line receives an ellipsis rather than being cut by the canvas edge. A missing Last RX image is represented by a framed placeholder and remains selectable and draggable. Components are composited in component-list order, allowing upper layers to partially cover lower text or images.
