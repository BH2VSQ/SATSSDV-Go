#!/usr/bin/env python3
"""Static guardrails for parity with Wails UI commit 68f58f3."""
from __future__ import annotations
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CPP = (ROOT / "qt6/mainwindow.cpp").read_text(encoding="utf-8")
HEADER = (ROOT / "qt6/mainwindow.h").read_text(encoding="utf-8")

# Top navigation stays in the header; the QTabWidget tab strip is not visible.
for token in (
    "m_workbenchNav = new QPushButton",
    "m_albumNav = new QPushButton",
    "m_languageSelect = makeCombo",
    "tabs->tabBar()->hide()",
    "m_mainTabs->setCurrentIndex(0)",
    "m_mainTabs->setCurrentIndex(1)",
):
    assert token in CPP, f"missing Wails topbar behavior: {token}"

# Wails uses one toggle for each radio direction, plus an independent tone test.
for token in (
    "m_rxToggle = new QPushButton",
    "connect(m_rxToggle, &QPushButton::clicked, this, [this] { toggleRX(); })",
    "m_txToggle = new QPushButton",
    "connect(m_txToggle, &QPushButton::clicked, this, [this] { toggleTX(); })",
    "m_txToneToggle = new QPushButton",
    "void MainWindow::toggleRX()",
    "void MainWindow::toggleTX()",
    "tx:tone-state",
):
    assert token in CPP or token in HEADER, f"missing unified RX/TX action: {token}"
assert "auto *rxStart = new QPushButton" not in CPP
assert "auto *rxStop = new QPushButton" not in CPP
assert "auto *send = new QPushButton" not in CPP
assert "auto *stop = new QPushButton(QStringLiteral(\"停止 TX\")" not in CPP

# TX canvas and component property editor are inline in the main workbench.
for token in (
    "m_templateCanvas = new TemplateCanvas",
    "m_componentTextEdit = new QTextEdit",
    "m_componentAssetCombo = makeCombo",
    "m_componentScaleSlider = new QSlider",
    "m_componentFontSlider = new QSlider",
    "m_componentOutlineSlider = new QSlider",
    "void MainWindow::applyInlineTemplateProperties()",
    "void MainWindow::refreshTemplateCanvas()",
    "void MainWindow::selectInlineGalleryAsset()",
    "设为底图",
    "添加图层",
):
    assert token in CPP or token in HEADER, f"missing inline TX editor behavior: {token}"

# Wails receive view includes auto-profile selection, SSID metadata, and separate
# RX/TX waterfalls; album cards are sorted newest-first and retain per-item actions.
for token in (
    "QStringLiteral(\"auto\")",
    "ax25SourceSSID",
    "m_rxSpectrum",
    "m_txSpectrum",
    "std::stable_sort(orderedAlbum.begin(), orderedAlbum.end()",
    "GetAlbumImageData",
    "ExportAlbumRecordToDirectory",
    "DeleteAlbumRecord",
):
    assert token in CPP or token in HEADER, f"missing Wails behavior: {token}"

# Repeat controls follow the native repeat state machine. The interval field is
# display-only while repeat is enabled; the checkbox can only cancel an active
# inter-cycle countdown, never an image burst already in progress.
assert "const bool canToggleRepeat = !m_txPreparing && (!m_txActive || m_txCountdownActive);" in CPP
assert "m_repeatInterval->setEnabled(!repeatChecked && !m_txCountdownActive)" in CPP
assert "m_txCountdownActive = true;" in CPP
assert "cancelRepeatCountdown" in CPP and 'QStringLiteral("StopTX")' in CPP
assert "m_repeatIntervalSeconds" in CPP
assert 'phase == QStringLiteral("countdown")' in CPP
assert 'phase == QStringLiteral("cancelled")' in CPP
assert "tx.testTone" in CPP

print("Qt/Wails UI parity static checks passed.")
