# SATSSDV-Go 第六轮修改说明（v6）

本轮基于上一轮 `SATSSDV-Go-fixed-v5.zip` 修改。

## 修改内容

### 1. RX 图片完整缩放及底部对齐
- RX 预览使用 RX pane 中的弹性剩余空间。
- 图片强制 `object-fit: contain`、居中显示，保持原始比例，不裁切。
- 320×240、448×336、640×480 三种尺寸共用相同预览区域。
- RX 的元数据、瀑布图和底部状态栏固定在 pane 底部，与 TX 瀑布图/星座图区域对齐。

### 2. Last RX 图层识别所有质量档位
- 修复 `GetLatestRXImageData` 遍历方向错误。
- `GetAlbum` 返回最新记录在前，旧代码却反向遍历，因此 Last RX 实际拿到最旧 RX 图片。
- 新增 `album.NewestRXImages`，按时间倒序查找最新 RX 图像，不按 Quality 或分辨率过滤；遇到缺失或不可读文件继续查找下一张。
- 新增低/中/高质量混合 TX/RX 记录回归测试。

### 3. SSDV 接收丢包缓解
- RX 音频工作队列从 256 增加到 512 个采集块。
- 修复队列满时清空整个队列的策略。
- 现在保留已排队音频，不再一次性丢弃数秒历史数据；记录一个待处理 discontinuity，在有两个空位时依次插入解码器重置标记和下一音频块。
- 如果 DSP 长期慢于音频采集，仍可能发生输入块丢失；本改动主要避免旧实现的“大批量清空”造成极端图像缺包。没有真实 RF 录音，不能将其描述为已经测得的空口丢包改善。

### 4. RX/TX 方形星座显示
- `dsp.Spectrum` 输出增加归一化 FFT 复数实部、虚部 `real`、`imag`。
- RX/TX 收到频谱事件后，从复数 FFT bin 生成 I/Q 平面点云。
- 星座画布固定方形，绘制幅度参考圆环和 I/Q 轴，不再被宽瀑布区域拉伸。
- 图内标注 `FFT I/Q`：它是按用户要求直接从音频 FFT 频谱生成的频域复数投影，并非解调器符号判决点。若将来要获得传统通信星座图，还需从 AX.25/CCSDS 解调器导出每个符号的 matched-filter I/Q。

### 5. 瀑布图频率标记
- 频率网格以绝对 500 Hz 为间隔生成。
- 标签使用 `0、500、1000、1500、2000、2500` 这样的整数 Hz，不再显示 `1.1k` 一类小数 kHz 标记。

## 测试

- `go test ./...`：通过。
- `go vet ./...`：通过。
- `go test -race ./internal/dsp ./internal/album`：通过。
- `node --check frontend/app.js`：通过。
- `node --check frontend/ui_structure.test.cjs`：通过。
- `node frontend/ui_structure.test.cjs`：通过；新增功能性 canvas 检查验证 I/Q 点绘制为 132×132 正方形，瀑布图频率标签精确覆盖 0–2000 Hz 的 500 Hz 分段。

## 环境限制

- 本机只有 Go 1.23.2，测试时临时将 `go.mod` 的 `go 1.26.0` 声明降至 1.23.0，测试完成后立即恢复；交付包中仍为 `go 1.26.0`。
- Wails v2.17.1 未缓存，当前环境无法访问 Go module proxy，因此无法完成 `-tags wails` 的桌面入口编译。
- 没有真实 SDR/RF 录音，本轮未完成真实卫星过境丢包改善测量。
