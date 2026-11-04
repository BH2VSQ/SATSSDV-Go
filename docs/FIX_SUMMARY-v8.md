# SATSSDV-Go 第八轮修复说明

## 本轮目标

1. RX 预览按图像高度优先适配，完整显示图像，不裁切底部或其他区域。
2. 删除 RX/TX 星座图设计及其后端数据采集路径。
3. 检查 AX.25 与 SSDV CCSDS 1200 的发射、接收编解码，并修复部分包接收问题。

## 1. RX 预览缩放

- RX `<img>` 使用 `height: 100%; width: auto`，先按预览区域高度等比例缩放。
- 保留 `max-width: 100%`、`max-height: 100%` 与 `object-fit: contain`，当预览宽度不足时再等比例缩小。
- 预览容器设置 `min-height: 0`、`overflow: hidden`，避免 flex 内容溢出后遮挡其他 RX 元素。
- 低、中、高三种分辨率共用同一预览区域，CSS 与动态创建的图片元素都使用相同的高度优先规则。

## 2. 删除星座图

- 删除 RX/TX 星座图 Canvas 和右侧星座图区域。
- 删除前端星座图绘制、历史点缓存及 RX/TX 星座图事件订阅。
- 删除 TX 监视路径中仅为星座图而重复解调 500 ms 音频的逻辑。
- 删除 FSK/GMSK 解调器中的星座采样缓存、transport 星座图封装和无用测试。
- RX/TX 瀑布图恢复占满频谱面板宽度，原有频谱幅度数据路径保持不变。

## 3. AX.25 / CCSDS 1200 部分收包根因与修复

在 `internal/dsp/fsk_stream.go` 的 `FSKStreamDemodulator.Feed` 中发现确定性数据丢失：

- 解调器需要最后 64 个符号来估计初始符号定时相位和公共频偏。
- 原逻辑用这段尾部窗口完成搜索后，从 `start + phase` 开始切片 `pending`。
- 这会将尾部 64 符号之前的全部音频样本丢弃。如果音频回调一次交付较大的首块，前面的 HDLC/CCSDS 前导和有效图像帧会在帧同步之前被丢掉。
- 在修复前的生产链路诊断中，同一 17 包图像一次性输入完整波形，AX.25 与 CCSDS 1200 都只解出 `0/17`；使用较小音频块时则可能成功。这与“大回调块时仅收到部分图像包”的症状一致。

修复后仍使用尾部 64 符号估算定时和频偏，但将相位换算为相对 `pending` 开头的全局采样偏移：

```go
globalPhase := (start + phase) % d.sps
d.pending = append([]float32(nil), d.pending[globalPhase:]...)
```

这样保留了前面全部对齐的符号，不再丢掉已经包含在大回调块中的有效帧。

没有放宽 AX.25 FCS、HDLC bit-stuffing、CCSDS ASM、SSDV RS8 或 SSDV CRC 校验。

## 4. 生产链路端到端回归

新增 `internal/transport/codec_roundtrip_test.go`。测试使用生产 SSDV 图像编码器生成 320×240 测试图像，再经过生产 Transport 编码器、调制器、接收解调器、帧解析器和 SSDV 图像包重组器。

- 图像编码为 17 个 SSDV FEC 包。
- 分别测试 SSDV AX.25 与 SSDV CCSDS 1200。
- 独立整段录音解码 `DecodeAudioAtRate`：两种模式均恢复 17/17 包。
- 实时 `StreamDecoder` 测试以下音频回调块大小：
  - 整段波形一次输入
  - 480 samples
  - 997 samples（不是 16 samples/symbol 的整数倍）
  - 1920 samples
- 两种模式在以上所有分块方式均恢复 17/17 包，包序号、呼号、图像 ID 正确。
- 将接收包重新组装为 JPEG 并实际调用 JPEG 解码，确认结果为 320×240 且图像 ID 正确。

## 5. 验证结果

- `go test -count=1 ./internal/...`：通过。
- `go vet ./internal/...`：通过。
- `go test -race ./internal/dsp ./internal/modem/ax25 ./internal/modem/ccsds`：通过。
- AX.25 / CCSDS 1200 端到端回归的定向 race 测试：通过。
- `node --check frontend/app.js`：通过。
- `node --check frontend/ui_structure.test.cjs`：通过。
- `node frontend/ui_structure.test.cjs`：通过，包含 RX 高度优先缩放与星座图完全移除检查。

当前运行环境安装 Go 1.23.2，测试时临时将本地 `go.mod` go 指令改为 1.23.0，完成后立即恢复。交付源码中的 `go.mod` 仍为 `go 1.26.0`。

## 6. 验证边界

这些测试证明 SATSSDV-Go 自身的图像编码、AX.25/CCSDS 1200 调制、音频解调、帧处理和图像包重组在合成波形上互相兼容，并覆盖较大的首个音频块。真实电台/SDR 录音仍需用于验证音频设备时钟误差、载波偏移、噪声、削波、多径及实际电台的音频滤波。

本机未完成 Wails 桌面标签构建；本轮修改未推送到 GitHub。
