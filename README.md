# SATSSDV-Go

SATSSDV-Go is a Go/Wails desktop workbench for amateur-satellite SSDV and PCSI image links. It provides separate RX and TX profile selection, shared-mode full-duplex audio, RX/TX audio waterfall displays with configurable display settings, PTT control, image templates, a local gallery, an album, and a repeat-send scheduler.

## Transport profiles

- SSDV AX.25
- SSDV CCSDS 1200
- SSDV CCSDS 2400
- SSDV QPSK 1600
- SSDV 8PSK 1600
- SSDV 16QAM 1600
- PCSI CCSDS 1200
- PCSI CCSDS 2400

The three M-ary entries are experimental SATSSDV-Go-specific profiles. They carry CCSDS ASM/PN-randomized SSDV data units, but use a project-specific 96-symbol acquisition preamble and Gray-labelled QPSK/8PSK/16QAM mapping. They require a 48 kHz audio path and a matching SATSSDV-Go M-ary receiver; they are not claimed to interoperate with Hamstation, ARCSSD, GNU Radio default blocks, or another CCSDS modem. The listed 1600 values are symbol rates (baud); their raw symbol bit rates are respectively 3200/4800/6400 bit/s before framing overhead.

RX and TX profiles are selected independently. RX also offers an Auto Detect selection that probes the supported packet profiles, locks to a mode only after a validated image packet, and returns to acquisition after five seconds without a valid packet. Auto Detect requires a 48 kHz audio path for the 1600-symbol-rate M-ary candidates. SSDV and PCSI MMT both have Go packet transmit paths and image receive reconstruction paths. SSDV accepts baseline sequential 8-bit 4:2:0 images, transcodes DCT coefficients into SSDV quantization tables, emits 256-byte FEC/No-FEC packets with CRC32/RS8, and reconstructs complete packet sets into JPEG/PNG. TX writes the standard MCU offset/index restart metadata into each SSDV packet. RX attempts a block-level update for every newly accepted, CRC/RS-validated packet and paints complete 16×16 MCU regions at their image coordinates on a fixed full-resolution canvas; missing regions remain visibly damaged instead of stopping the whole preview at the first packet gap. The RX album keeps partial PNG previews and periodically snapshots received packets. PCSI MMT supports packet generation, RS correction, sparse-measurement reconstruction and progressive PNG previews; partial PCSI previews are archived while reception is in progress.

## Current features

- AX.25 UI/HDLC framing, FCS, bit stuffing, NRZI and 1200-baud AFSK TX/RX foundation. AX.25 TX exposes a source SSID (0–15) setting; the CQ template uses destination `CQ` and clears HisCall, while other templates use HisCall as the destination when supplied.
- CCSDS ASM framing, PN randomization, 1200-baud modulation and the ARCSSD-style 2400-bit/s pulse-shaped TX/RX path. Continuous receive framing is tested across arbitrary audio callback chunk boundaries for the five legacy AX.25/CCSDS profiles. The three experimental M-ary profiles have separate streaming symbol-acquisition and full packet-loopback tests.
- SSDV JPEG coefficient transcoding, 256-byte FEC/No-FEC packet generation, Base40, CRC32 and RS8 validation/correction. TX uses standard MCU offset/index restart markers with byte-aligned packet-local DC predictors; RX supports out-of-order packets, duplicate suppression, per-packet 16×16 block updates, local damaged-block marking, and complete contiguous packet-set reconstruction. Packets that fail RS/CRC validation are rejected before image reconstruction. The encoder applies a quality-aware intermediate JPEG prefilter before the fixed SSDV quantizer to reduce unnecessary entropy payload.
- PCSI MMT packetization and RS8 error correction, with a Go DCT/IDCT and iterative sparse reconstruction path. The OWL-QN orthant-projection and reference coefficient-layout bugs that caused green/noisy output have been corrected; in-progress previews use a 48-iteration budget and complete images use a bounded 96-iteration budget, both with early convergence detection.
- Independent RX capture and TX playback through optional Malgo shared-mode audio streams.
- RX and TX scrolling audio waterfalls, adjustable frequency/dB range, row rate, history length and per-level color palette, input/output level reporting, stateful resampling/demodulation, and a bounded packet-processing worker. The RX packet queue applies cancellable backpressure rather than silently discarding accepted packets when temporarily full. PCSI image reconstruction runs on a separate single-worker queue; newer snapshots are coalesced while optimization is active.
- PTT through VOX, Hamlib `rigctld`, serial RTS or DTR. RTS/DTR ports are automatically enumerated and can be refreshed manually.
- RTS, DTR and Hamlib PTT test buttons with a 15-second backend-enforced timeout and manual early release.
- Low, medium and high image presets: 320x240, 448x336 and 640x480.
- SATSSTV-Go-style TX template editor with a dropdown preset selector, four initial CQ/RST/73/blank presets, and support for up to 32 independently saved presets. Presets can be created, duplicated, renamed and deleted. The editor supports draggable text/image components, one shared workbench background, QSO fields and a built-in image-library dialog. The library supports import, preview, set-as-template-background, add-as-layer and delete.
- The TX canvas contains no preview/quality overlay text. The Start/Stop action is a single toggle; separate RX/TX standby labels are not shown. The template dropdown and preset-management controls remain above the editor. The preview uses a fixed 4:3 CSS viewport, independently of the selected image encoding resolution. The underlying canvas is still rendered at 320×240, 448×336 or 640×480 for transmission and is uniformly scaled with its original aspect ratio inside the fixed preview viewport. A decoded remote callsign automatically populates HisCall; manual entry takes precedence, and clearing the field re-enables automatic fill. The top navigation contains only Workbench and Album (「工作台」 and 「相册」 in Simplified Chinese). Album is a dedicated page and remains hidden until its top navigation tab is selected; it never occupies space beneath the RX/TX workbench. The image library is opened from the TX template editor instead of occupying a separate top-level tab.
- The four default template options are rendered immediately at DOM readiness, before backend or device enumeration, and template data is fetched before potentially slow audio/serial discovery. Template, component and gallery controls are bound at DOM readiness. The image-library modal opens immediately; asset cards and their Set Background/Add Layer/Delete actions are rendered before thumbnail reads finish. The component editor owns an independent vertical scroll area while image quality, repeat and interval controls remain grouped in the TX title bar, preventing visual overlap. Successful background/layer actions close the library so the canvas result is immediately visible; errors are shown inside the modal and in the workbench status toast. Legacy template data is normalized, invalid image components without a gallery asset are discarded, and component geometry is clamped to valid canvas bounds.
- RX/TX album cards include explicit RX/TX markers, RX/TX filtering, large-image preview, timestamped PNG and raw SSDV packet export. Partial previews and packet-only records are retained automatically. Export uses the operating system's native folder picker rather than a browser download flow. RX records use receive-completion time; TX records use transmission-start time; the combined album is sorted newest-first.
- Template presets can be imported/exported as portable `.satssdv.json` packs. Packs embed all referenced gallery images and the shared workbench background, remap asset IDs on import and append presets without replacing existing ones. Fresh installs receive layered CQ/RST/73/blank starter layouts from built-in defaults.
- Single-template transmission with one combined Start/Stop button in the TX title bar. The UI exposes no multi-image TX queue. Repeat mode retransmits only the currently selected rendered template after the configured interval. The backend scheduler supports all eight configured transport profiles, including the three experimental M-ary SSDV profiles.
- Simplified Chinese, English and Japanese UI dictionaries.
- SSDV packet sanity checks include image geometry, MCU-count, MCU-index and payload-offset bounds, in addition to RS8 correction and CRC32 validation.

## Automatic RX mode detection and CPU acquisition optimization

The RX profile selector includes Auto Detect. It shares the 1200-bit/s FSK front end between AX.25 and CCSDS 1200, shares the 2400-bit/s GMSK front end between SSDV and PCSI, and probes the three 1600-symbol-rate M-ary profiles. A profile is selected only after a valid AX.25/CCSDS frame and SSDV/PCSI packet passes link and image validation; duplicate valid packets refresh the activity timer. After five seconds with no valid packet, Auto Detect returns to acquisition. M-ary candidates require a 48 kHz audio path.

To reduce the CPU cost of continuous acquisition, FSK carrier/timing search uses a coarse frequency/phase grid followed by a local fine search and retries after half an acquisition window of new samples rather than every callback. M-ary acquisition uses a low-cost coarse score with sample-level timing refinement only for promising candidates. Auto Detect skips digitally silent (< -120 dBFS peak) blocks while unlocked. RX/TX FFT and Wails spectrum events are capped at 15 Hz; the ordered demodulation path still receives every captured audio block.

## RX viewport and AX.25 / CCSDS 1200 regression

The RX preview uses height-first aspect-preserving fit in a fixed viewport; low, medium and high resolution images are fully visible without cropping. The optional RX/TX constellation display has been removed; the waterfall uses the full panel width.

The FSK acquisition path preserves samples that precede its trailing 64-symbol timing/frequency acquisition window. Production image-to-waveform-to-image packet tests cover AX.25 and CCSDS 1200 with whole-waveform, 480-, 997- and 1920-sample callback boundaries. See [docs/INTEROPERABILITY_ROUND14.md](docs/INTEROPERABILITY_ROUND14.md).

## AX.25 / CCSDS 1200 FSK acquisition

The shared 1200-bit/s FSK receiver confirms timing and tone-offset acquisition across consecutive windows before locking, preventing idle audio noise from capturing the symbol clock before a packet arrives. Regression tests cover idle-noise pre-roll, native 19.2 kHz and 48 kHz capture, and multiple callback sizes. See [docs/FSK_NOISE_ACQUISITION.md](docs/FSK_NOISE_ACQUISITION.md).

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
GitHub Actions runs the tests on Windows, Linux and macOS and publishes build artifacts for Windows x64, Linux x64 (`tar.gz`, `.deb`, AppImage), and macOS Apple Silicon/Intel. Platform packages are built on native runners; macOS artifacts are unsigned and may require the user to approve the app on first launch.

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

The SSDV codec currently supports baseline sequential 8-bit 4:2:0 JPEG. New TX packets carry standard MCU offset/index restart metadata, so the live preview can render later packet regions after a gap; a bit-exact final JPEG still requires a complete packet sequence. Legacy packets without MCU metadata fall back to contiguous-prefix preview. Progressive/grayscale JPEG, input restart intervals and non-4:2:0 sampling are rejected. Packet-for-packet comparison against the upstream SSDV CLI/Hamstation and a real RF capture are still required. The PCSI reconstruction solver is an independent Go OWL-QN/L-BFGS-style L1 implementation. Its orthonormal 2-D DCT has a numeric reference-vector test against OpenCV, and the MMT packet path has synthetic full-image and receive-worker tests. Pixel-for-pixel comparison against outputs from the supplied `pcsitximage_mmt.py` and `pcsidecoder_mmt.py` is still required. The CCSDS 2400 path has synthetic waveform and audio-IF offset tests; real recorded captures, RF Doppler through a physical radio path and on-air interoperability remain unverified.

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


## RX/TX refinements (2026-11-04)

- AX.25 local SSID is configured in the TX title-bar transmission controls, immediately before image quality, and only enabled for SSDV AX.25 TX. RX accepts any SSID and displays the decoded source SSID beside CALL.
- SSDV processing emits an RX update for every accepted packet. MCU offset/index markers let the decoder render 16×16 blocks even when earlier packet IDs are missing; missing regions are shown as local damage. PNG/raw-packet disk writes remain batched for audio-path performance.
- TX album timestamps are captured at the start of audio transmission. RX timestamps are finalized when the image receive session completes. Duplicate packet-only/preview rows from one session are merged while retaining both media paths.
- A 1500 Hz audio test tone is available beside Start/Stop TX and uses the existing TX cancellation/PTT path with a backend-enforced 60-second timeout.
- PCSI MMT transmission precomputes Y/Cb/Cr planes and increases chroma compression by quality preset (low 80, medium 60, high 40). This trades some chroma detail for approximately 4.5–6.9% fewer packets in the 640×480 reference configuration versus the legacy compression value 20.

The SSDV preview uses the standard MCU offset/index fields to locate complete blocks and show missing regions. It does not yet perform upstream `ssdv`'s full damaged-MCU placeholder synthesis for bit-exact continuation after every arbitrary loss pattern. The measured synthetic audio-IF offset tolerance is documented in [docs/DOPPLER_TOLERANCE.md](docs/DOPPLER_TOLERANCE.md); it is not a substitute for recorded linear-satellite RF/audio replay. Real RF/Hamstation comparison and Wails desktop build verification are still required.


## SSDV and Doppler refinements (2026-11-04)

- The TX packetizer now emits standard SSDV MCU offset/index restart metadata. The first complete MCU in each packet is byte-aligned and its Y/Cb/Cr DC predictors restart relative to zero; the packet stream remains continuous and does not incur an additional packet-count penalty in the 320×240 regression fixture.
- RX decodes the complete MCU strip between neighbouring packet restart points as soon as both packets are available. Newly received tail packets are decoded independently up to the last complete MCU in their payload. The UI receives one update per accepted packet; loss is localized to 16×16 regions. Full JPEG reconstruction still requires all packets.
- For the default SSDV quality level 4, the encoder uses an intermediate JPEG quality-80 prefilter before applying the canonical SSDV quantizer. On the deterministic 320×240 fixture, this reduced the packet count from 53 to 50 (5.7%) at a sampled reconstruction PSNR of 21.98 dB. A separate test confirms the MCU restart metadata itself does not increase the packet count: 10,825-byte continuous stream versus 10,851-byte restart stream, both 53 FEC packets.
- On the Linux amd64 benchmark host (Intel Xeon Platinum 8573C), full SSDV CCSDS-1200 TX encoding and waveform generation measured about 18.3/41.3/43.4 ms and allocated 8.4/15.7/18.7 MB for low/medium/high. RX audio demodulation, packet validation and SSDV preview decode measured about 0.53/1.00/1.10 s and allocated 3.1/5.8/7.4 MB. These are one-image Go microbenchmarks, not RF airtime measurements.
- PCSI MMT full CCSDS-1200 TX packetization plus waveform generation measured about 68.7/138.5/282.5 ms and allocated 26.3/51.8/107.2 MB for low/medium/high. The optimized high-quality path reduces Go allocation count from roughly 619k to 3.8k allocations per image and removes a full RGBA copy plus duplicate CCSDS data/guard buffers. Most remaining allocation bytes (about 97 MB for high quality) are the required materialized `[]float32` audio waveform. PCSI RX RS correction/measurement ingestion alone measured about 74/115/232 ms and allocated 1.1/2.1/4.3 MB. One complete OWL-QN iteration (not a full reconstruction) measured about 0.79/1.68/3.99 s and allocated 34.9/68.5/139.9 MB. PCSI's iterative DCT reconstruction remains the main high-quality RX CPU/memory bottleneck; these one-iteration measurements must not be read as full-image completion latency.
- Synthetic passband offset sweep covers -1500 to +1500 Hz in 100 Hz increments for the five legacy AX.25/CCSDS profiles. All five decode all tested offsets from -500 to +500 Hz, and all pass both +100→-100 Hz and -100→+100 Hz one-burst linear audio-IF ramps. The three experimental M-ary profiles (QPSK, 8PSK and 16QAM at 1600 baud) pass clean waveform loopbacks; residual ±80 Hz offset acquisition with idle pre-roll is verified for all three constellation orders. A conservative symmetric range of ±600 Hz was observed for SSDV AX.25, SSDV CCSDS 2400 and PCSI CCSDS 2400; the two 1200-bit/s CCSDS profiles are conservatively ±500 Hz due to a -600 Hz failure. Wider isolated successes are non-monotonic and are not considered a guaranteed range. See [docs/DOPPLER_TOLERANCE.md](docs/DOPPLER_TOLERANCE.md).
- The 1500 Hz test tone is stopped by a backend 60-second context deadline. The album is sorted newest-first; RX timestamps are receive-completion times and TX timestamps are transmission-start times.
- RX/TX pane-header standby status pills (the right-side “RX 待机” / “TX 待机” areas) have been removed; the RX toggle, TX start/stop control and global audio state remain.


## RX preview and spectrum display refinement (2026-11-04)

- RX image previews use a flexible fixed pane viewport with `object-fit: contain`; all output resolutions stay fully visible and the RX waterfall/footer baseline aligns with TX.
- Last RX template components choose the newest available RX album image across low, medium and high resolutions. Album selection is timestamp-ordered rather than quality-filtered.
- RX capture queue overflow no longer drains the whole queued audio history. It preserves already queued samples and inserts one explicit decoder discontinuity before later audio when there is room for both the marker and the next sample block.
- Waterfall x-axis labels use exact integer-Hz ticks every 500 Hz (for example, 0, 500, 1000, 1500, 2000, 2500), without shifted kHz abbreviations.


## RX preview full-image fit refinement (2026-11-04)

- RX preview images are now mounted inside an absolutely inset `.rx-image-stage` whose dimensions are exactly the visible image viewport.
- The `<img>` retains its intrinsic dimensions (`width:auto; height:auto`) and is constrained by both `max-width:100%` and `max-height:100%`; `object-fit:contain` and centered flex alignment preserve the complete image without cropping.
- Removed the runtime inline `height:100%` / `width:auto` sizing that overrode the previous CSS fit rules. The empty RX placeholder remains centered through the parent grid layout.
- Frontend regression checks assert the bounded stage, intrinsic image sizing, both-axis limits, and absence of the old inline 100%-height assignment.
