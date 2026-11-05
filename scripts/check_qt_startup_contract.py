#!/usr/bin/env python3
"""Static guardrails for the native Qt single-window startup contract."""
from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MAIN_CPP = (ROOT / "qt6/main.cpp").read_text(encoding="utf-8")
WINDOW_CPP = (ROOT / "qt6/mainwindow.cpp").read_text(encoding="utf-8")
WINDOW_H = (ROOT / "qt6/mainwindow.h").read_text(encoding="utf-8")
CMAKE = (ROOT / "CMakeLists.txt").read_text(encoding="utf-8")


def method_body(source: str, signature: str) -> str:
    start = source.find(signature)
    if start < 0:
        raise AssertionError(f"missing method: {signature}")
    brace = source.find("{", start)
    if brace < 0:
        raise AssertionError(f"missing method body: {signature}")
    depth = 0
    in_string = False
    escaped = False
    for index in range(brace, len(source)):
        char = source[index]
        if in_string:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                in_string = False
            continue
        if char == '"':
            in_string = True
        elif char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return source[brace + 1 : index]
    raise AssertionError(f"unterminated method body: {signature}")


# The launcher never constructs a critical dialog. It always creates/shows the
# main window, even when the helper process cannot start.
assert "QMessageBox" not in MAIN_CPP
assert "const bool backendStarted = backend.start" in MAIN_CPP
assert "MainWindow window(&backend, backendStarted ? QString() : error)" in MAIN_CPP
assert "window.show()" in MAIN_CPP

# Startup failure and RPC errors are status/log messages, not extra windows.
show_error = method_body(WINDOW_CPP, "void MainWindow::showError")
assert "QMessageBox" not in show_error
load_initial = method_body(WINDOW_CPP, "void MainWindow::loadInitialState")
assert "showError(" not in load_initial

# RX/TX have independent waterfall widgets, and the album uses image cards.
assert "SpectrumWaterfallWidget *m_rxSpectrum" in WINDOW_H
assert "SpectrumWaterfallWidget *m_txSpectrum" in WINDOW_H
assert "m_rxSpectrum->pushBins" not in WINDOW_CPP  # routed through target pointer
assert "name.startsWith(QStringLiteral(\"rx\")) ? m_rxSpectrum : m_txSpectrum" in WINDOW_CPP
assert "QGridLayout *m_albumCardsLayout" in WINDOW_H
assert "GetAlbumImageData" in WINDOW_CPP
assert "新建预设" in WINDOW_CPP
assert "复制当前" in WINDOW_CPP
assert "删除当前" in WINDOW_CPP
assert "保存预设" in WINDOW_CPP

# Both Windows executables must be built with GUI subsystem flags.
assert "add_executable(SATSSDV-Go WIN32" in CMAKE
assert '"-ldflags=-H=windowsgui"' in CMAKE

print("Qt UI startup contract checks passed.")
