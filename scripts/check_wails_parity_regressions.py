#!/usr/bin/env python3
from pathlib import Path
import re

root = Path(__file__).resolve().parents[1]
cpp = (root / "qt6/mainwindow.cpp").read_text(encoding="utf-8")
header = (root / "qt6/mainwindow.h").read_text(encoding="utf-8")
canvas = (root / "qt6/template_canvas.cpp").read_text(encoding="utf-8")

assert "m_ax25To" not in cpp + header, "AX.25 To must not be shown as a user-facing field"
assert cpp.count('    const QString current=m_template->currentData().toString(); m_template->clear();') == 1, "refreshTemplates must declare current exactly once"
assert 'addQsoField(0, 1, uiText(QStringLiteral("对方呼号")), &m_hisCall);' in cpp
assert "m_ax25SSIDField->setVisible(ax25)" in cpp
assert "m_txPreparing" in cpp and 'QStringLiteral("tx.preparing")' in cpp
assert "drawAlphaContourOutline" in cpp and "drawAlphaContourOutline" in canvas
assert 'setUiLocale(locale)' in cpp and 'translateWidgetTree(this)' in cpp
assert 'm_audioConnected' in cpp and 'setEnabled(m_backend && m_backend->isRunning() && (m_rxActive || m_audioConnected))' in cpp
assert 'clearHisCallForCQTemplate();' in cpp
assert 'imageOutlineMigratedV1' in cpp and 'SaveTemplates' in cpp
assert 'm_rxPreview->clearSourceImage()' in cpp
assert 'if (!m_audioConnected)' in cpp
assert 'm_audioConnected && !m_txToneActive' in cpp
print("PASS: Wails parity regression checks")
