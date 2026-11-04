#!/usr/bin/env python3
"""Static guardrails for the native Qt 6 desktop architecture."""
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
errors: list[str] = []

if (ROOT / "frontend").exists():
    errors.append("legacy frontend/ tree must not be present")

go_mod = (ROOT / "go.mod").read_text(encoding="utf-8")
if re.search(r"wails|webview", go_mod, re.IGNORECASE):
    errors.append("Go module must not depend on Wails or a WebView runtime")

cmake = (ROOT / "CMakeLists.txt").read_text(encoding="utf-8")
for required in ("Qt6 6.4 REQUIRED COMPONENTS Widgets Network", "qt6/main.cpp", "qt6/mainwindow.cpp", "qt6/template_canvas.cpp", "qt6/template_canvas.h", "satssdv-backend"):
    if required not in cmake:
        errors.append(f"CMake configuration is missing required native target item: {required}")
if re.search(r"Qt6.*WebEngine|QWebEngineView|QWebView", cmake, re.IGNORECASE):
    errors.append("Qt WebEngine/WebView must not be included")

main_cpp = (ROOT / "qt6/main.cpp").read_text(encoding="utf-8")
if "QApplication" not in main_cpp or "MainWindow" not in main_cpp:
    errors.append("Qt desktop entry must create QApplication and MainWindow")

server = (ROOT / "backend_server.go").read_text(encoding="utf-8")
for required in ('net.Listen("tcp", "127.0.0.1:0")', "subtle.ConstantTimeCompare", "rpcMethodAllowlist"):
    if required not in server:
        errors.append(f"Go local RPC server is missing safety control: {required}")

allowlist_block = re.search(r"var rpcMethodAllowlist = map\[string\]bool\{(.*?)\n\}", server, re.S)
if not allowlist_block:
    errors.append("RPC method allowlist could not be parsed")
else:
    allowed = set(re.findall(r'"([A-Za-z0-9_]+)"\s*:\s*true', allowlist_block.group(1)))
    ui = (ROOT / "qt6/mainwindow.cpp").read_text(encoding="utf-8")
    used = set(re.findall(r'm_backend->call(?:Sync)?\(QStringLiteral\("([A-Za-z0-9_]+)"\)', ui))
    missing = sorted(used - allowed)
    if missing:
        errors.append("Qt calls RPC methods absent from the backend allowlist: " + ", ".join(missing))


canvas_h = (ROOT / "qt6/template_canvas.h").read_text(encoding="utf-8")
canvas_cpp = (ROOT / "qt6/template_canvas.cpp").read_text(encoding="utf-8")
mainwindow = (ROOT / "qt6/mainwindow.cpp").read_text(encoding="utf-8")
if "mouseMoveEvent" not in canvas_cpp or "DragMode::Resize" not in canvas_cpp or "removeSelectedComponent" not in canvas_cpp:
    errors.append("native template canvas must support drag, resize and deletion")
if ("moveSelectedLayerUp" not in canvas_h or "moveSelectedLayerDown" not in canvas_h
        or "setSnapToGrid" not in canvas_h or "5% 网格吸附" not in mainwindow):
    errors.append("native template canvas must support layer ordering and optional grid snapping")
if "new TemplateCanvas" not in mainwindow or "＋ 图库图像" not in mainwindow or "＋ 上次接收图像" not in mainwindow:
    errors.append("native template editor is missing canvas or image-layer authoring controls")
if "refreshLatestRXPreview" not in mainwindow or "hasPreview" not in mainwindow:
    errors.append("Qt RX preview must fetch large image data through the dedicated RPC")
if "m_statusRequestPending" not in (ROOT / "qt6/mainwindow.h").read_text(encoding="utf-8"):
    errors.append("periodic status polling must be guarded against overlapping requests")
if "readyReadStandardError" not in (ROOT / "qt6/backend_client.cpp").read_text(encoding="utf-8"):
    errors.append("Qt must drain the Go child process stderr pipe")

rx_source = (ROOT / "app.go").read_text(encoding="utf-8")
if 'update.PNGBase64 = ""' not in rx_source or 'update.PacketDataBase64 = ""' not in rx_source:
    errors.append("large binary RX payloads must be omitted from retained UI events")

if errors:
    for error in errors:
        print(f"FAIL: {error}", file=sys.stderr)
    sys.exit(1)

print("PASS: Qt Widgets entry, visual template canvas, bounded RPC events, child-process lifecycle guards and allowlist coverage")
