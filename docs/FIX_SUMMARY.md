# SATSSDV-Go 修复与交付说明（2026-11-04，第二轮）

本次在用户提供的 SATSSDV-Go 源码副本上继续修复，不会自动向远程 GitHub 仓库推送。

## 1. PCSI 偏色与进度预览

- 修正 PCSI OWL-QN 正交投影：只有当前系数为零时才限制其进入选定正交象限；非零系数允许沿当前符号方向回退到零。旧逻辑会屏蔽过冲后的有效下降方向，导致重建冻结在错误的色彩系数。
- 修正 Python 参考流程 `reshape((width, height)).T` 与 Go 行优先二维 DCT 矩阵之间的列优先/行优先系数映射。
- 将 PCSI 接收过程中的预览迭代预算从 2 提高到 48，最终重建预算为 96；满足梯度收敛条件时提前结束。新增 320×240 纯红图像早期预览回归测试，检查红色通道不会被错误重建为绿色。
- 已验证 64×64 合成红图上，修正优化器后继续迭代能明显改善红色恢复。该合成用例并不等同真实图像/无线信道的逐像素对照，仍需同一 RF/WAV 包集与 Hamstation 或上游 Python 解码器比较。

## 2. SSDV 图像预览与新图像会话

- SSDV 预览改为每 8 个连续新增包形成一个更新检查点，不再因每个新增包都尝试解码而呈现逐行快速刷新的视觉效果。
- 部分 JPEG 预览绘制到最终图像尺寸的固定画布，RX 视口使用居中、等比 contain 显示；接收时预览区域不会随已解码行数不断缩放。
- SSDV 仍只能解码从 packet ID 0 开始的连续包前缀；当前发射器没有逐包 MCU 重同步元数据，缺包后的后续图像区域不能可靠恢复。缺失包及已收到包仍保存在 `.ssdv` 快照中。
- 修正图像 ID 复用时旧包去重状态阻止新图像的问题。传输层和图像管理层使用 packet 0 的有效载荷指纹、前一图像完成状态及较长空闲间隔判断会话边界。
- 特别处理乱序：packet 0 可以在较高序号包之后到达，不能仅因已看到高序号包或 EOI 标记就重置会话；只有 EOI 对应的 packet 0..EOI 序列完整时才把 SSDV 会话标为完成。
- 相同内容、缺包且没有可区分的会话间隔的连续重发仍存在协议层歧义，因为 SSDV/PCSI 线上包头没有独立的传输会话随机数。

## 3. AX.25 TX 呼号与 SSID

- 在 TX 设置中增加 0–15 的 AX.25 源 SSID 数字输入，仅选择 SSDV AX.25 TX profile 时显示并启用；CCSDS/PCSI CCSDS 模式隐藏并禁用该字段。
- AX.25 地址中的源呼号使用配置的 SSID。
- 使用 CQ 模板发射时，目的 TO 固定为 `CQ`，同时自动清空 HisCall 输入框，防止残留对方呼号污染 CQ 呼叫。
- 使用其他模板时，HisCall 非空则转换为大写并作为目的 TO；为空时回退为 `CQ`。
- 新增编码测试，验证 SSID、目的呼号和非法 SSID 拒绝逻辑；新增前端结构回归，验证 SSID 字段显示条件和 CQ/HisCall 规则。

## 4. 前一轮已有修复继续保留

- RX 包队列由静默丢包改为有界、可取消背压。
- PCSI 与 SSDV 的部分图像/包快照自动归档；TX/RX 相册项有明确方向标识。
- 相册导出使用系统原生目录选择器，不再通过浏览器下载机制。
- 模板预设可导入/导出 `.satssdv.json`，包含模板引用的图片资产和共享 TX 背景；首次启动有 CQ/RST/73/Blank 初始预设。
- GitHub Actions 配置 Windows x64、Linux x64（tar.gz、deb、AppImage）以及 macOS arm64/amd64 打包。

## 5. 本轮验证

在本机 Go 1.23.2 环境中临时降低本地测试进程读取的 `go.mod` go directive 到 1.23.0 后执行测试，随后已恢复项目原始 `go 1.26.0` 声明：

- `go test -count=1 ./...`：通过。
- `go vet ./...`：通过。
- `go test -race ./internal/transport ./internal/modem/ax25`：通过。
- `go test -race ./internal/rx -run 'Test(SSDVImageIDReuseStartsFreshRXSession|PCSIImageIDReuseStartsFreshRXSession|SSDVOutOfOrderPacketZeroKeepsExistingSession|ManagerSubmitAppliesBackpressureInsteadOfDropping)'`：通过。
- `node --check frontend/app.js`：通过。
- `node frontend/ui_structure.test.cjs`：通过。

完整的 `go test -race ./...` 在此执行环境耗时超限；已对传输、AX.25 和 RX 会话/队列关键测试执行定向 race 检查。

## 6. 尚待真实验证

- 当前环境没有可用的 Wails 原生桌面构建工具链，尚未在此处实际产出 Windows/Linux/macOS 可执行安装包；需推送分支后由 GitHub Actions 各平台 runner 执行。
- 没有用户现场 AX.25/CCSDS RF/WAV 录音，因此 AX.25 偶发只显示一行、Hamstation 识别差异、CCSDS 频偏与时钟漂移仍需真实录音回放定位。
- PCSI 的合成颜色回归通过，但尚未对用户截图所对应的真实源图像/完整包集进行与 Hamstation 的逐像素比较。48/96 次预算改善了可复现测试，不应被描述为已证明所有真实图像偏色彻底消失。
- SSDV 缺包后的 MCU 重同步/缺失区域隐藏修复尚未实现；当前只能渲染缺口之前的连续前缀。
- 本次只改写用户上传的本地源码副本，未提交或推送 `BH2VSQ/SATSSDV-Go`、`by2hit/pcsi`、`by2hit/arcssd-go` 或 `by2hit/arcss_panel_pc` 远程仓库。
