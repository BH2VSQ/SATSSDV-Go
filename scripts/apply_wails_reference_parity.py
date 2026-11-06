#!/usr/bin/env python3
"""Apply the Wails reference UI behavior to the SATSSDV-Go Qt6 client.

Run from the repository root:
    python scripts/apply_wails_reference_parity.py

The script is intentionally strict and transactional: it validates all anchors
before writing any file, creates backups, and refuses to apply to an unexpected
Qt source revision instead of silently producing a partial patch.
"""
from __future__ import annotations

import re
import shutil
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FILES = {
    "cpp": ROOT / "qt6/mainwindow.cpp",
    "header": ROOT / "qt6/mainwindow.h",
    "canvas": ROOT / "qt6/template_canvas.cpp",
    "cmake": ROOT / "CMakeLists.txt",
}


def fail(message: str) -> None:
    raise SystemExit(f"ERROR: {message}")


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        fail(f"{label}: expected exactly one anchor, found {count}; no files were changed")
    return text.replace(old, new, 1)


def require(text: str, needle: str, label: str) -> None:
    if needle not in text:
        fail(f"{label}: source anchor not found; no files were changed")


for key, path in FILES.items():
    if not path.is_file():
        fail(f"missing required source file: {path.relative_to(ROOT)}")

cpp = FILES["cpp"].read_text(encoding="utf-8")
header = FILES["header"].read_text(encoding="utf-8")
canvas = FILES["canvas"].read_text(encoding="utf-8")
cmake = FILES["cmake"].read_text(encoding="utf-8")

# Reject a second application. This avoids wrapping uiText(uiText(...)) and
# keeps repeat runs safe.
if '#include "ui_i18n.h"' in cpp or '#include "alpha_outline.h"' in canvas:
    fail("the Wails parity layer already appears to be installed")

# ------------------------------- C++ sources -------------------------------

header = replace_once(
    header,
    "    void updateTXToggle();\n    void toggleRX();\n    void toggleTX();\n    void applyLanguage(const QString &locale);",
    "    void updateTXToggle();\n    void updateTXProfileControls();\n    void clearHisCallForCQTemplate();\n    void toggleRX();\n    void toggleTX();\n    void applyLanguage(const QString &locale);",
    "mainwindow declarations: profile/recipient state helpers",
)
header = replace_once(
    header,
    "    bool m_rxActive = false;\n    bool m_txActive = false;\n    bool m_txToneActive = false;",
    "    bool m_rxActive = false;\n    bool m_txActive = false;\n    bool m_txToneActive = false;\n    bool m_txPreparing = false;\n    bool m_audioConnected = false;\n    bool m_hisCallManuallyEdited = false;",
    "mainwindow state: TX preparation/audio/recipient source flags",
)
header = replace_once(
    header,
    "    QLineEdit *m_message = nullptr;\n    QLineEdit *m_ax25To = nullptr;\n    QSpinBox *m_ax25SSID = nullptr;",
    "    QLineEdit *m_message = nullptr;\n    QWidget *m_ax25SSIDField = nullptr;\n    QSpinBox *m_ax25SSID = nullptr;",
    "mainwindow fields: remove AX.25 To editor and add conditional SSID container",
)

# New reusable helpers.
cpp = replace_once(
    cpp,
    '#include "mainwindow.h"\n#include "template_canvas.h"',
    '#include "mainwindow.h"\n#include "template_canvas.h"\n#include "alpha_outline.h"\n#include "ui_i18n.h"',
    "mainwindow include block",
)
canvas = replace_once(
    canvas,
    '#include "template_canvas.h"',
    '#include "template_canvas.h"\n#include "alpha_outline.h"\n#include "ui_i18n.h"',
    "template canvas include block",
)

# Replace every hard-coded CJK QStringLiteral in the two UI translation units.
# API keys and protocol IDs are ASCII and therefore left untouched.
def wrap_cjk_literals(source: str) -> str:
    pattern = re.compile(r'QStringLiteral\("((?:[^"\\]|\\.)*)"\)')
    def replace(match: re.Match[str]) -> str:
        value = match.group(1)
        if not re.search(r"[\u3400-\u9fff\u3040-\u30ff\uac00-\ud7af]", value):
            return match.group(0)
        return f'uiText(QStringLiteral("{value}"))'
    return pattern.sub(replace, source)

cpp = wrap_cjk_literals(cpp)
canvas = wrap_cjk_literals(canvas)

# RX preview must clear stale images on a new receive session.
cpp = replace_once(
    cpp,
    "    void setSourceImage(const QImage &image) {\n        m_source = image;\n        updatePixmap();\n    }",
    "    void setSourceImage(const QImage &image) {\n        m_source = image;\n        updatePixmap();\n    }\n\n    void clearSourceImage() {\n        m_source = QImage();\n        setPixmap(QPixmap());\n        setText(uiText(QStringLiteral(\"等待图像\")));\n    }",
    "ImagePreviewLabel: explicit clear API",
)
cpp = replace_once(
    cpp,
    "    void updatePixmap() {\n        if (m_source.isNull() || contentsRect().isEmpty()) return;\n        setPixmap(QPixmap::fromImage(m_source.scaled(contentsRect().size(), Qt::KeepAspectRatio, Qt::SmoothTransformation)));\n    }",
    "    void updatePixmap() {\n        if (m_source.isNull()) {\n            setPixmap(QPixmap());\n            setText(uiText(QStringLiteral(\"等待图像\")));\n            return;\n        }\n        if (contentsRect().isEmpty()) return;\n        setText(QString());\n        setPixmap(QPixmap::fromImage(m_source.scaled(contentsRect().size(), Qt::KeepAspectRatio, Qt::SmoothTransformation)));\n    }",
    "ImagePreviewLabel: clear empty state and remove placeholder after image load",
)

# Keep the QSO recipient empty. It is not the AX.25 destination field.
cpp = replace_once(
    cpp,
    '    addQsoField(0, 1, uiText(QStringLiteral("对方呼号")), &m_hisCall, QStringLiteral("CQ"));',
    '    addQsoField(0, 1, uiText(QStringLiteral("对方呼号")), &m_hisCall);\n    m_hisCall->setPlaceholderText(uiText(QStringLiteral("N0TEST")));\n    connect(m_hisCall, &QLineEdit::textEdited, this, [this] { m_hisCallManuallyEdited = true; });',
    "QSO recipient starts empty and tracks manual edits",
)

# Remove both AX.25 controls from the QSO information grid. AX.25 SSID is a
# transmit-profile setting and is recreated in the transmission toolbar below.
old_ax25_qso = '''    auto *ax25Cell = new QWidget(qsoBox);
    auto *ax25Layout = new QVBoxLayout(ax25Cell);
    ax25Layout->setContentsMargins(0, 0, 0, 0);
    ax25Layout->setSpacing(2);
    ax25Layout->addWidget(new QLabel(QStringLiteral("AX.25 To"), ax25Cell));
    m_ax25To = new QLineEdit(QStringLiteral("CQ"), ax25Cell);
    ax25Layout->addWidget(m_ax25To);
    qsoGrid->addWidget(ax25Cell, 2, 0);
    auto *ssidCell = new QWidget(qsoBox);
    auto *ssidLayout = new QVBoxLayout(ssidCell);
    ssidLayout->setContentsMargins(0, 0, 0, 0);
    ssidLayout->setSpacing(2);
    ssidLayout->addWidget(new QLabel(QStringLiteral("Source SSID"), ssidCell));
    m_ax25SSID = new QSpinBox(ssidCell);
    m_ax25SSID->setRange(0, 15);
    m_ax25SSID->setValue(0);
    ssidLayout->addWidget(m_ax25SSID);
    qsoGrid->addWidget(ssidCell, 2, 1);
'''
cpp = replace_once(cpp, old_ax25_qso, "", "remove AX.25 To/SSID from QSO grid")

# Add the SSID field into the TX transmission-controls row, next to quality.
cpp = replace_once(
    cpp,
    '    txHeader->addStretch();\n    txHeader->addWidget(new QLabel(uiText(QStringLiteral("图片质量")), txPane));',
    '''    txHeader->addStretch();
    m_ax25SSIDField = new QWidget(txPane);
    auto *ax25SSIDLayout = new QHBoxLayout(m_ax25SSIDField);
    ax25SSIDLayout->setContentsMargins(0, 0, 0, 0);
    ax25SSIDLayout->setSpacing(4);
    ax25SSIDLayout->addWidget(new QLabel(uiText(QStringLiteral("AX.25 SSID")), m_ax25SSIDField));
    m_ax25SSID = new QSpinBox(m_ax25SSIDField);
    m_ax25SSID->setRange(0, 15);
    m_ax25SSID->setValue(0);
    m_ax25SSID->setFixedWidth(64);
    ax25SSIDLayout->addWidget(m_ax25SSID);
    txHeader->addWidget(m_ax25SSIDField);
    txHeader->addWidget(new QLabel(uiText(QStringLiteral("图片质量")), txPane));''',
    "insert AX.25 SSID under TX transmission settings",
)

# The TX profile row updates the conditional SSID field and CQ recipient rule.
cpp = replace_once(
    cpp,
    '            m_settings.insert(settingKey, id);\n            saveSettings(m_settings);',
    '            m_settings.insert(settingKey, id);\n            if (settingKey == QStringLiteral("txProfile")) updateTXProfileControls();\n            saveSettings(m_settings);',
    "profile selector callback",
)

# Existing image layers were historically created with outline=false in Qt.
# Migrate them once to the Wails default contour outline while preserving later
# user changes (the migration marker prevents a recurring override).
cpp = replace_once(
    cpp,
    '    if(!error.isEmpty()){appendLog(error);return;} m_templates=result.toArray();',
    """    if (!error.isEmpty()) { appendLog(error); return; }
    m_templates = result.toArray();
    if (!m_settings.value(QStringLiteral("imageOutlineMigratedV1")).toBool()) {
        bool changed = false;
        for (int i = 0; i < m_templates.size(); ++i) {
            QJsonObject item = m_templates.at(i).toObject();
            QJsonArray components = item.value(QStringLiteral("components")).toArray();
            for (int j = 0; j < components.size(); ++j) {
                QJsonObject component = components.at(j).toObject();
                const QString type = component.value(QStringLiteral("type")).toString();
                if (type != QStringLiteral("image") && type != QStringLiteral("last-rx-image")) continue;
                if (!component.value(QStringLiteral("outline")).toBool(false)) {
                    component.insert(QStringLiteral("outline"), true);
                    changed = true;
                }
                if (!component.contains(QStringLiteral("outlineWidth"))) {
                    component.insert(QStringLiteral("outlineWidth"), 2.0);
                    changed = true;
                }
                if (!component.contains(QStringLiteral("outlineColor"))) {
                    component.insert(QStringLiteral("outlineColor"), QStringLiteral("#000000"));
                    changed = true;
                }
                components.replace(j, component);
            }
            item.insert(QStringLiteral("components"), components);
            m_templates.replace(i, item);
        }
        bool migrationSaved = true;
        if (changed) {
            QString migrationError;
            m_backend->callSync(QStringLiteral("SaveTemplates"), QJsonArray{m_templates}, &migrationError, 15000);
            if (!migrationError.isEmpty()) {
                appendLog(QStringLiteral("Image outline migration failed: ") + migrationError);
                migrationSaved = false;
            }
        }
        if (migrationSaved) {
            m_settings.insert(QStringLiteral("imageOutlineMigratedV1"), true);
            QString settingsError;
            m_backend->callSync(QStringLiteral("SaveSettings"), QJsonArray{m_settings}, &settingsError, 15000);
            if (!settingsError.isEmpty()) appendLog(QStringLiteral("Unable to save image-outline migration state: ") + settingsError);
        }
    }
""",
    "migrate existing image layers to Wails contour outline default",
)

# Template changes must clear the recipient for CQ presets, just like Wails.
cpp = replace_once(
    cpp,
    '        const QJsonObject selected = selectedTemplate();\n        if (m_templateName) m_templateName->setText(selected.value(QStringLiteral("name")).toString());\n        refreshTemplateComponentList();',
    '        const QJsonObject selected = selectedTemplate();\n        if (m_templateName) m_templateName->setText(selected.value(QStringLiteral("name")).toString());\n        clearHisCallForCQTemplate();\n        refreshTemplateComponentList();',
    "template selection: CQ recipient reset",
)

# Persist SSID changes after loading the stored value (not during hydration).
cpp = replace_once(
    cpp,
    '    m_ax25SSID->setValue(settings.value(QStringLiteral("ax25Ssid")).toInt(0));\n    const QString rx = valueOr(settings, QStringLiteral("rxProfile"), QStringLiteral("auto"));',
    '    m_ax25SSID->setValue(settings.value(QStringLiteral("ax25Ssid")).toInt(0));\n    connect(m_ax25SSID, QOverload<int>::of(&QSpinBox::valueChanged), this, [this](int value) {\n        m_settings.insert(QStringLiteral("ax25Ssid"), value);\n        saveSettings(m_settings);\n    });\n    const QString rx = valueOr(settings, QStringLiteral("rxProfile"), QStringLiteral("auto"));',
    "AX.25 SSID setting persistence",
)

# Initial state: select the correct profile-dependent SSID visibility.
cpp = replace_once(
    cpp,
    '    const QString quality = valueOr(settings, QStringLiteral("quality"), QStringLiteral("medium"));',
    '    updateTXProfileControls();\n    clearHisCallForCQTemplate();\n    const QString quality = valueOr(settings, QStringLiteral("quality"), QStringLiteral("medium"));\n    updateRXToggle();\n    updateTXToggle();',
    "initial load: SSID visibility and CQ recipient state",
)

# Status state gates RX the same way as the Wails reference.
cpp = replace_once(
    cpp,
    '        if (!ok) {\n            m_audioStatus->setText(uiText(QStringLiteral("音频后端异常")));',
    '        if (!ok) {\n            m_audioConnected = false;\n            updateRXToggle();\n            m_audioStatus->setText(uiText(QStringLiteral("音频后端异常")));',
    "audio status error state",
)
cpp = replace_once(
    cpp,
    '        const bool connected = state.value(QStringLiteral("connected")).toBool();\n        m_rxActive = state.value(QStringLiteral("rxActive")).toBool();',
    '        const bool connected = state.value(QStringLiteral("connected")).toBool();\n        m_audioConnected = connected;\n        m_rxActive = state.value(QStringLiteral("rxActive")).toBool();',
    "audio connected state cache",
)

# RX state label only exposes SSID when the current event actually carries one.
cpp = replace_once(
    cpp,
    '        const QString ssid = hasSSID ? QString::number(payload.value(QStringLiteral("ax25SourceSSID")).toInt()) : QStringLiteral("--");',
    '        const QString ssid = hasSSID ? QStringLiteral("   SSID  %1").arg(payload.value(QStringLiteral("ax25SourceSSID")).toInt()) : QString();',
    "RX SSID optional display value",
)
cpp = replace_once(
    cpp,
    '        m_rxMeta->setText(QStringLiteral("CALL  %1   SSID  %2   IMAGE ID  %3   PACKETS  %4 / %5   MODE  %6")\n            .arg(callsign, ssid).arg(imageId).arg(received).arg(expected).arg(shownProfile));',
    '        m_rxMeta->setText(QStringLiteral("CALL  %1%2   IMAGE ID  %3   PACKETS  %4 / %5   MODE  %6")\n            .arg(callsign, ssid).arg(imageId).arg(received).arg(expected).arg(shownProfile));',
    "RX metadata omits SSID when unavailable",
)

# RX auto-fills the QSO recipient from a decoded callsign unless the operator
# edited it manually or the selected AX.25 template is the CQ preset.
cpp = replace_once(
    cpp,
    '        const QString callsign = payload.value(QStringLiteral("callsign")).toString();\n        const int received',
    '        const QString callsign = payload.value(QStringLiteral("callsign")).toString();\n        if (!callsign.trimmed().isEmpty() && !m_hisCallManuallyEdited) {\n            const bool cqTemplate = selectedTemplate().value(QStringLiteral("name")).toString().trimmed().compare(QStringLiteral("CQ"), Qt::CaseInsensitive) == 0;\n            if (!(m_txProfile && m_txProfile->currentData().toString() == QStringLiteral("ssdv-ax25") && cqTemplate)) {\n                m_hisCall->setText(callsign.trimmed().toUpper());\n                refreshTemplateCanvas();\n            }\n        }\n        const int received',
    "RX decoded callsign auto-fill",
)

# A fresh RX session clears the old preview before the backend starts receiving.
cpp = replace_once(
    cpp,
    'void MainWindow::startRX() {\n    m_settings.insert(QStringLiteral("rxProfile"), m_rxProfile->currentData().toString());',
    'void MainWindow::startRX() {\n    if (!m_audioConnected) { showError(uiText(QStringLiteral("音频后端异常"))); return; }\n    if (m_rxPreview) m_rxPreview->clearSourceImage();\n    if (m_rxMeta) m_rxMeta->setText(QStringLiteral("CALL  ---   IMAGE ID  --   PACKETS  0 / 0   MODE  ---"));\n    m_settings.insert(QStringLiteral("rxProfile"), m_rxProfile->currentData().toString());',
    "RX start clears the prior preview immediately",
)

# Update RX toggle gate and TX preparing state.
cpp = replace_once(
    cpp,
    '    m_rxToggle->setProperty("role", m_rxActive ? "danger" : "primary");',
    '    m_rxToggle->setEnabled(m_backend && m_backend->isRunning() && (m_rxActive || m_audioConnected));\n    m_rxToggle->setProperty("role", m_rxActive ? "danger" : "primary");',
    "RX toggle availability gate",
)
cpp = replace_once(
    cpp,
    '    m_txToggle->setText(m_txActive\n        ? m_localeDictionary.value(QStringLiteral("tx.stop")).toString(uiText(QStringLiteral("停止发射")))\n        : m_localeDictionary.value(QStringLiteral("tx.start")).toString(uiText(QStringLiteral("开始发射"))));',
    '    m_txToggle->setText(m_txPreparing\n        ? m_localeDictionary.value(QStringLiteral("tx.preparing")).toString(uiText(QStringLiteral("正在准备…")))\n        : (m_txActive\n            ? m_localeDictionary.value(QStringLiteral("tx.stop")).toString(uiText(QStringLiteral("停止发射")))\n            : m_localeDictionary.value(QStringLiteral("tx.start")).toString(uiText(QStringLiteral("开始发射")))));',
    "TX toggle preparing label",
)
cpp = replace_once(
    cpp,
    '    m_txToggle->setEnabled(m_backend && m_backend->isRunning() && !m_txToneActive);',
    '    m_txToggle->setEnabled(m_backend && m_backend->isRunning() && (m_txActive || (m_audioConnected && !m_txToneActive && !m_txPreparing)));',
    "TX toggle lock during preparation",
)
cpp = replace_once(
    cpp,
    '        m_txToneToggle->setEnabled(m_txToneActive || (!m_txActive && m_backend && m_backend->isRunning()));',
    '        m_txToneToggle->setEnabled(m_txToneActive || (!m_txActive && m_audioConnected && m_backend && m_backend->isRunning()));',
    "test tone requires active audio connection",
)

# Install the profile state helpers before the current RX toggle updater.
cpp = replace_once(
    cpp,
    'void MainWindow::updateRXToggle() {',
    '''void MainWindow::updateTXProfileControls() {
    const bool ax25 = m_txProfile && m_txProfile->currentData().toString() == QStringLiteral("ssdv-ax25");
    if (m_ax25SSIDField) m_ax25SSIDField->setVisible(ax25);
    if (m_ax25SSID) m_ax25SSID->setEnabled(ax25);
    clearHisCallForCQTemplate();
}

void MainWindow::clearHisCallForCQTemplate() {
    if (!m_hisCall || !m_txProfile || m_txProfile->currentData().toString() != QStringLiteral("ssdv-ax25")) return;
    QString name = selectedTemplate().value(QStringLiteral("name")).toString().trimmed();
    name.remove(QRegularExpression(QStringLiteral("^\\\\d+[\\\\s._-]*")));
    if (name.compare(QStringLiteral("CQ"), Qt::CaseInsensitive) != 0) return;
    if (!m_hisCall->text().isEmpty()) m_hisCall->clear();
    m_hisCallManuallyEdited = false;
}

void MainWindow::updateRXToggle() {''',
    "profile-dependent AX.25 field and CQ recipient helpers",
)

# startTX implements the Wails internal destination and preparing lock.
start_marker = 'void MainWindow::startTX() {'
start_pos = cpp.find(start_marker)
if start_pos < 0:
    fail("startTX function not found; no files were changed")
end_marker = '\nvoid MainWindow::saveSettings(const QJsonObject &settings) {'
end_pos = cpp.find(end_marker, start_pos)
if end_pos < 0:
    fail("startTX function end not found; no files were changed")
new_start_tx = '''void MainWindow::startTX() {
    if (m_txPreparing || m_txActive || !m_backend || !m_backend->isRunning()) return;
    if (!m_audioConnected) { showError(uiText(QStringLiteral("音频后端异常"))); return; }
    m_txPreparing = true;
    updateTXToggle();

    m_settings.insert(QStringLiteral("callsign"), m_callsign->text().trimmed());
    m_settings.insert(QStringLiteral("locator"), m_locator->text().trimmed());
    m_settings.insert(QStringLiteral("txProfile"), m_txProfile->currentData().toString());
    m_settings.insert(QStringLiteral("quality"), selectedQuality());
    if (m_ax25SSID) m_settings.insert(QStringLiteral("ax25Ssid"), m_ax25SSID->value());

    QString ax25To = QStringLiteral("CQ");
    if (m_txProfile->currentData().toString() == QStringLiteral("ssdv-ax25")) {
        clearHisCallForCQTemplate();
        const QString templateName = selectedTemplate().value(QStringLiteral("name")).toString().trimmed();
        const bool cqTemplate = templateName.compare(QStringLiteral("CQ"), Qt::CaseInsensitive) == 0;
        const QString hisCall = m_hisCall ? m_hisCall->text().trimmed().toUpper() : QString();
        if (!cqTemplate && !hisCall.isEmpty()) ax25To = hisCall;
    }

    QString saveError;
    m_backend->callSync(QStringLiteral("SaveSettings"), QJsonArray{m_settings}, &saveError, 15000);
    if (!saveError.isEmpty()) {
        m_txPreparing = false;
        updateTXToggle();
        showError(saveError);
        return;
    }

    QString renderError;
    QImage image = renderTemplate(&renderError);
    if (image.isNull()) {
        m_txPreparing = false;
        updateTXToggle();
        showError(renderError.isEmpty() ? uiText(QStringLiteral("无法生成发送图像")) : renderError);
        return;
    }
    QByteArray png;
    QBuffer buffer(&png);
    if (!buffer.open(QIODevice::WriteOnly) || !image.save(&buffer, "PNG")) {
        m_txPreparing = false;
        updateTXToggle();
        showError(uiText(QStringLiteral("无法将发送模板编码为 PNG")));
        return;
    }
    m_txPreview->setSourceImage(image);

    QJsonObject rendered;
    rendered.insert(QStringLiteral("id"), QStringLiteral("qt-%1").arg(QDateTime::currentMSecsSinceEpoch()));
    rendered.insert(QStringLiteral("name"), QStringLiteral("Qt Native Template"));
    rendered.insert(QStringLiteral("pngBase64"), QString::fromLatin1(png.toBase64()));
    const QJsonArray args{rendered, m_repeat->isChecked(), m_repeatInterval->value(), ax25To};
    m_backend->call(QStringLiteral("StartTXRendered"), args, [this](bool ok, const QJsonValue &, const QString &error) {
        m_txPreparing = false;
        if (ok) {
            m_txActive = true;
            updateTXToggle();
            m_txProgress->setValue(0);
            appendLog(uiText(QStringLiteral("已提交图像发射任务")));
        } else {
            updateTXToggle();
            showError(error);
        }
    });
}
'''
cpp = cpp[:start_pos] + new_start_tx + cpp[end_pos:]

# Apply the active locale to every already-created widget (and use uiText() on
# all later dynamic text, since all CJK literals were wrapped above).
cpp = replace_once(
    cpp,
    '    if (error.isEmpty()) m_localeDictionary = dictionary;\n    if (!error.isEmpty()) {',
    '    m_localeDictionary = error.isEmpty() ? dictionary : QJsonObject{};\n    setUiLocale(locale);\n    translateWidgetTree(this);\n    if (!error.isEmpty()) {',
    "applyLanguage: set locale and recursively translate current widget tree",
)
cpp = replace_once(
    cpp,
    '    if (!error.isEmpty()) {\n        statusBar()->showMessage(uiText(QStringLiteral("语言设置已保存；部分界面文本将在下次版本完善。")), 5000);\n        return;\n    }',
    '    if (!error.isEmpty()) statusBar()->showMessage(uiText(QStringLiteral("无法加载语言词典，使用内置翻译。")), 5000);',
    "applyLanguage: continue with built-in locale when backend dictionary is unavailable",
)
# Locale initialization also runs when language was loaded from settings.
cpp = replace_once(
    cpp,
    '        m_localeDictionary = m_backend->callSync(QStringLiteral("GetLocaleDictionary"), QJsonArray{locale}, &localeError, 5000).toObject();\n    }\n    if (m_languageSelect)',
    '        m_localeDictionary = m_backend->callSync(QStringLiteral("GetLocaleDictionary"), QJsonArray{locale}, &localeError, 5000).toObject();\n    }\n    setUiLocale(locale);\n    if (m_languageSelect)',
    "loadInitialState: set locale before rendering widgets",
)
cpp = replace_once(
    cpp,
    '    m_callsign->setText(valueOr(settings, QStringLiteral("callsign"), QStringLiteral("N0CALL")));',
    '    translateWidgetTree(this);\n    m_callsign->setText(valueOr(settings, QStringLiteral("callsign"), QStringLiteral("N0CALL")));',
    "loadInitialState: retranslate widgets after locale loaded",
)

# Do not offer software PTT testing while the operator uses VOX.
cpp = replace_once(
    cpp,
    """    pttActions->addStretch();
    generalLayout->addLayout(pttActions);
    generalLayout->addStretch();""",
    """    pttActions->addStretch();
    generalLayout->addLayout(pttActions);
    auto updatePTTActionVisibility = [pttMode, pttStart, pttStop]() {
        const bool softwarePTT = pttMode->currentIndex() != 0;
        pttStart->setVisible(softwarePTT);
        pttStop->setVisible(softwarePTT);
    };
    connect(pttMode, QOverload<int>::of(&QComboBox::currentIndexChanged), &dialog,
            [updatePTTActionVisibility](int) { updatePTTActionVisibility(); });
    updatePTTActionVisibility();
    generalLayout->addStretch();""",
    "hide software PTT test buttons in VOX mode",
)

# PTT fields are visible only for their applicable control mode.
cpp = replace_once(
    cpp,
    '    form->addRow(uiText(QStringLiteral("PTT 发射后延时")), tail);\n    generalLayout->addLayout(form);',
    '''    form->addRow(uiText(QStringLiteral("PTT 发射后延时")), tail);
    auto *voxHint = new QLabel(uiText(QStringLiteral("VOX 由电台检测音频自动发射，不需要软件 PTT 测试。")), generalPage);
    voxHint->setWordWrap(true);
    form->addRow(voxHint);
    auto updatePTTFieldVisibility = [=]() {
        const int selectedMode = pttMode->currentIndex();
        const bool useHamlib = selectedMode == 1;
        const bool useSerial = selectedMode == 2 || selectedMode == 3;
        rigctl->setVisible(useHamlib);
        if (QWidget *label = form->labelForField(rigctl)) label->setVisible(useHamlib);
        serialRow->setVisible(useSerial);
        if (QWidget *label = form->labelForField(serialRow)) label->setVisible(useSerial);
        const bool softwarePTT = selectedMode != 0;
        activeHigh->setVisible(selectedMode == 2 || selectedMode == 3);
        lead->setVisible(softwarePTT);
        tail->setVisible(softwarePTT);
        if (QWidget *label = form->labelForField(lead)) label->setVisible(softwarePTT);
        if (QWidget *label = form->labelForField(tail)) label->setVisible(softwarePTT);
        voxHint->setVisible(selectedMode == 0);
    };
    connect(pttMode, QOverload<int>::of(&QComboBox::currentIndexChanged), &dialog,
            [updatePTTFieldVisibility](int) { updatePTTFieldVisibility(); });
    updatePTTFieldVisibility();
    generalLayout->addLayout(form);''',
    "PTT settings conditional visibility",
)

# Profile state also updates immediately after startup even before a user click.
cpp = replace_once(
    cpp,
    '    addProfileButton(m_txProfileButtons, m_txProfile, QStringLiteral("txProfile"), id, label, m_txProfile->currentData().toString() == id);',
    '    addProfileButton(m_txProfileButtons, m_txProfile, QStringLiteral("txProfile"), id, label, m_txProfile->currentData().toString() == id);',
    "TX profile button registration validation",
)

# Ensure all settings-dialog and album/gallery child widgets are translated at
# creation time as well as when the language selector changes.
cpp = replace_once(
    cpp,
    '    dialog.setWindowTitle(uiText(QStringLiteral("设备与电台设置")));\n    dialog.resize(620, 760);',
    '    dialog.setWindowTitle(uiText(QStringLiteral("设备与电台设置")));\n    dialog.resize(620, 760);',
    "settings dialog title remains localized",
)

# Image defaults should match Wails: image layers are outlined unless disabled.
cpp = cpp.replace('component.insert(QStringLiteral("outline"), false);', 'component.insert(QStringLiteral("outline"), true);')
cpp = cpp.replace('component.insert(QStringLiteral("outlineWidth"), 2.0);', 'component.insert(QStringLiteral("outlineWidth"), 2.0);')

# The final rasterized TX image uses the same alpha-contour outline as the live
# editing canvas. Replace the old rectangle border with the shared helper.
old_render_border = '''            if(c.value(QStringLiteral("outline")).toBool()&&c.value(QStringLiteral("outlineWidth")).toDouble()>0){painter.setPen(QPen(QColor(valueOr(c,QStringLiteral("outlineColor"),QStringLiteral("#000000"))),c.value(QStringLiteral("outlineWidth")).toDouble()*size.height()/480.0));painter.drawRect(rect.adjusted(-1,-1,1,1));}
            painter.drawImage(rect,component);'''
new_render_border = '''            if (c.value(QStringLiteral("outline")).toBool(true) && c.value(QStringLiteral("outlineWidth")).toDouble(2.0) > 0) {
                drawAlphaContourOutline(painter, component, rect,
                    QColor(valueOr(c, QStringLiteral("outlineColor"), QStringLiteral("#000000"))),
                    c.value(QStringLiteral("outlineWidth")).toDouble(2.0) * size.height() / 480.0);
            } else {
                painter.drawImage(rect, component);
            }'''
cpp = replace_once(cpp, old_render_border, new_render_border, "TX raster renderer alpha-contour outline")

# ----------------------------- template canvas -----------------------------
canvas = replace_once(
    canvas,
    '    return qBound(0.0, component.value(QStringLiteral("x")).toDouble(), 100.0) * kCanvasWidth / 100.0;',
    '    return component.value(QStringLiteral("x")).toDouble() * kCanvasWidth / 100.0;',
    "canvas X coordinate preserves off-canvas component position",
)
canvas = replace_once(
    canvas,
    '    return qBound(0.0, component.value(QStringLiteral("y")).toDouble(), 100.0) * kCanvasHeight / 100.0;',
    '    return component.value(QStringLiteral("y")).toDouble() * kCanvasHeight / 100.0;',
    "canvas Y coordinate preserves off-canvas component position",
)
canvas = replace_once(
    canvas,
    '            const QColor outline(component.value(QStringLiteral("outlineColor")).toString(QStringLiteral("#000000")));\n            const qreal outlineWidth = qBound(0.0, component.value(QStringLiteral("outlineWidth")).toDouble(2.0), 20.0);',
    '            const QColor outline(component.value(QStringLiteral("outlineColor")).toString(QStringLiteral("#000000")));\n            const qreal outlineWidth = qBound(0.0, component.value(QStringLiteral("outlineWidth")).toDouble(2.0), 20.0);',
    "template text outline declaration validation",
)
canvas = replace_once(
    canvas,
    '                painter.drawImage(QRectF(x, y, image.width() * factor, image.height() * factor), image);',
    '''                const QRectF imageRect(x, y, image.width() * factor, image.height() * factor);
                const QColor imageOutline(component.value(QStringLiteral("outlineColor")).toString(QStringLiteral("#000000")));
                const qreal imageOutlineWidth = qBound(0.0, component.value(QStringLiteral("outlineWidth")).toDouble(2.0), 20.0);
                if (component.value(QStringLiteral("outline")).toBool(true) && imageOutlineWidth > 0) {
                    drawAlphaContourOutline(painter, image, imageRect, imageOutline, imageOutlineWidth);
                } else {
                    painter.drawImage(imageRect, image);
                }''',
    "template canvas: alpha-contour outlines for transparent image layers",
)
# The drag engine and component coordinate helpers must permit layers to move
# beyond the canvas, as the Wails canvas clips rather than clamps them.
canvas = replace_once(
    canvas,
    '        component.insert(QStringLiteral("x"), qBound(0.0, x, 100.0));\n        component.insert(QStringLiteral("y"), qBound(0.0, y, 100.0));',
    '        component.insert(QStringLiteral("x"), x);\n        component.insert(QStringLiteral("y"), y);',
    "template canvas: allow off-canvas layer positions",
)

# ------------------------------ CMake wiring -------------------------------
cmake = replace_once(
    cmake,
    '        qt6/template_canvas.cpp\n        qt6/template_canvas.h',
    '        qt6/template_canvas.cpp\n        qt6/template_canvas.h\n        qt6/alpha_outline.cpp\n        qt6/alpha_outline.h\n        qt6/ui_i18n.cpp\n        qt6/ui_i18n.h',
    "CMake Qt source list",
)

# --------------------------------- helpers ----------------------------------
UI_I18N_H = r'''#pragma once

#include <QString>
#include <QWidget>

QString uiText(const QString &source);
void setUiLocale(const QString &locale);
void translateWidgetTree(QWidget *root);
'''

UI_I18N_CPP = r'''#include "ui_i18n.h"

#include <QAbstractButton>
#include <QAction>
#include <QComboBox>
#include <QGroupBox>
#include <QHeaderView>
#include <QLabel>
#include <QLineEdit>
#include <QMainWindow>
#include <QTabBar>
#include <QTabWidget>
#include <QTableWidget>
#include <QToolButton>
#include <QVariant>
#include <QVector>

#include <QHash>

namespace {
QString g_locale = QStringLiteral("zh-CN");
constexpr int kSourceRole = Qt::UserRole + 0x531;

struct Translation { const char *zh; const char *en; const char *ja; };
const Translation kTranslations[] = {
    {"工作台", "Workbench", "ワークベンチ"}, {"相册", "Album", "アルバム"},
    {"设备设置", "Device settings", "デバイス設定"}, {"等待图像", "Waiting for image", "画像を待機中"},
    {"后端启动失败", "Backend startup failed", "バックエンドの起動に失敗しました"},
    {"Go 后端不可用", "Go backend unavailable", "Go バックエンドを利用できません"},
    {"Go 后端启动失败：%1", "Go backend failed to start: %1", "Go バックエンドの起動に失敗しました: %1"},
    {"音频：正在连接", "Audio: connecting", "オーディオ: 接続中"},
    {"接收 RX", "Receive RX", "受信 RX"}, {"发送 TX", "Transmit TX", "送信 TX"},
    {"RX 待机 · 等待接收音频", "RX idle · waiting for receive audio", "RX 待機 · 受信音声を待機中"},
    {"启动 RX", "Start RX", "RX 開始"}, {"停止 RX", "Stop RX", "RX 停止"},
    {"RX 频谱 / 瀑布图", "RX Spectrum / Waterfall", "RX スペクトラム / ウォーターフォール"},
    {"TX 频谱 / 瀑布图", "TX Spectrum / Waterfall", "TX スペクトラム / ウォーターフォール"},
    {"输入 · 音频瀑布图", "Input · Audio waterfall", "入力 · オーディオウォーターフォール"},
    {"输出 · 音频瀑布图", "Output · Audio waterfall", "出力 · オーディオウォーターフォール"},
    {"等待 RX 音频", "Waiting for RX audio", "RX 音声を待機中"}, {"等待 TX 音频", "Waiting for TX audio", "TX 音声を待機中"},
    {"横轴：频率 · 纵轴：时间", "X: frequency · Y: time", "横軸: 周波数 · 縦軸: 時間"},
    {"图片质量", "Image quality", "画像品質"}, {"低质量 320×240", "Low 320×240", "低 320×240"},
    {"中质量 448×336", "Medium 448×336", "中 448×336"}, {"高质量 640×480", "High 640×480", "高 640×480"},
    {"循环发送", "Repeat transmission", "繰り返し送信"}, {"开始发射", "Start TX", "送信開始"},
    {"停止发射", "Stop TX", "送信停止"}, {"正在准备…", "Preparing…", "準備中…"},
    {"1500 Hz 测试音", "1500 Hz test tone", "1500 Hz テストトーン"},
    {"停止测试音", "Stop test tone", "テストトーン停止"},
    {"已启动 1500 Hz 测试音", "1500 Hz test tone started", "1500 Hz テストトーンを開始しました"},
    {"发射设置", "Transmission settings", "送信設定"}, {"AX.25 SSID", "AX.25 SSID", "AX.25 SSID"},
    {"预设名称", "Preset name", "プリセット名"}, {"保存预设", "Save preset", "プリセットを保存"},
    {"发射模板", "TX template", "送信テンプレート"}, {"＋ 新建预设", "＋ New preset", "＋ 新規プリセット"},
    {"复制当前", "Duplicate", "複製"}, {"删除当前", "Delete", "削除"},
    {"背景图", "Background", "背景画像"}, {"导入模板包", "Import template pack", "テンプレートパックをインポート"},
    {"导出模板包", "Export template pack", "テンプレートパックをエクスポート"},
    {"预设名称不能为空", "Preset name cannot be empty", "プリセット名を入力してください"},
    {"模板预设已保存", "Template preset saved", "テンプレートプリセットを保存しました"},
    {"最多支持 32 个模板预设", "Up to 32 template presets are supported", "テンプレートプリセットは最大 32 件です"},
    {"新建预设", "New preset", "新規プリセット"}, {"已创建空白模板预设", "Blank template preset created", "空のテンプレートプリセットを作成しました"},
    {"当前模板已复制", "Template duplicated", "テンプレートを複製しました"},
    {"至少需要保留一个模板预设", "At least one template preset must remain", "テンプレートプリセットを 1 件以上残してください"},
    {"删除模板预设", "Delete template preset", "テンプレートプリセットを削除"},
    {"确定删除当前模板预设吗？", "Delete the current template preset?", "現在のテンプレートプリセットを削除しますか?"},
    {"模板预设已删除", "Template preset deleted", "テンプレートプリセットを削除しました"},
    {"本次 QSO 信息", "QSO information", "今回の QSO 情報"}, {"本台呼号", "My callsign", "自局コールサイン"},
    {"对方呼号", "Other station callsign", "相手局コールサイン"}, {"QTH", "QTH", "QTH"},
    {"网格定位", "Maidenhead locator", "グリッドロケーター"}, {"RST / 报告", "RST / Report", "RST / レポート"},
    {"消息", "Message", "メッセージ"}, {"模板组件 / 属性", "Template components / Properties", "テンプレートコンポーネント / プロパティ"},
    {"模板组件", "Template components", "テンプレートコンポーネント"}, {"＋ 文字", "＋ Text", "＋ テキスト"},
    {"＋ 图库图像", "＋ Library image", "＋ ライブラリ画像"}, {"＋ 上次接收图像", "＋ Last RX image", "＋ 最後の RX 画像"},
    {"清除底图", "Clear background", "背景をクリア"}, {"文字内容 / {MYCALL} / {LOCATOR}", "Text / {MYCALL} / {LOCATOR}", "テキスト / {MYCALL} / {LOCATOR}"},
    {"文字内容", "Text content", "テキスト内容"}, {"图库图像", "Library image", "ライブラリ画像"},
    {"图像缩放", "Image scale", "画像倍率"}, {"文字字号", "Font size", "文字サイズ"},
    {"描边宽度", "Outline width", "縁取り幅"}, {"文字颜色", "Text color", "文字色"},
    {"描边颜色", "Outline color", "縁取り色"}, {"粗体", "Bold", "太字"},
    {"显示", "Visible", "表示"}, {"描边", "Outline", "縁取り"},
    {"删除组件", "Delete component", "コンポーネントを削除"},
    {"可直接拖动画布中的组件移动位置，拖动右下角控制点调整字号或图像比例。", "Drag components on the canvas; drag the lower-right handle to resize text or images.", "キャンバス上でコンポーネントをドラッグして移動し、右下のハンドルで文字サイズや画像倍率を調整します。"},
    {"协议编解码仍在开发验证中，当前版本不应直接用于空中发射。", "Protocol codecs are still under development and validation. Do not use this build for on-air transmission.", "プロトコルコーデックは開発・検証中です。このビルドを実際の送信に使用しないでください。"},
    {"全部图像", "All images", "すべての画像"}, {"仅接收 RX", "RX only", "RX のみ"}, {"仅发送 TX", "TX only", "TX のみ"},
    {"刷新", "Refresh", "更新"}, {"导入图库图片", "Import library image", "ライブラリ画像をインポート"},
    {"导出相册到文件夹", "Export album to folder", "アルバムをフォルダーにエクスポート"},
    {"点击缩略图预览；每张图像可单独导出或删除记录。", "Click a thumbnail to preview. Export or delete each image individually.", "サムネイルをクリックしてプレビューし、画像ごとにエクスポートまたは記録を削除できます。"},
    {"Go 后端通过本机回环 RPC 连接；RX / TX 频谱分别显示。", "Go backend connected through loopback RPC; RX and TX spectra are separate.", "Go バックエンドはローカル RPC で接続され、RX / TX スペクトラムは個別に表示されます。"},
    {"模式列表不可用", "Profile list unavailable", "モード一覧を利用できません"},
    {"读取模式列表失败：", "Failed to load profiles: ", "モード一覧の読み込みに失敗しました: "},
    {"初始化失败：%1", "Initialization failed: %1", "初期化に失敗しました: %1"},
    {"读取设置失败：", "Failed to load settings: ", "設定の読み込みに失敗しました: "},
    {"设置读取失败，使用默认值：%1", "Could not load settings; using defaults: %1", "設定を読み込めません。既定値を使用します: %1"},
    {"刷新接收图像预览失败：", "Failed to refresh RX image preview: ", "RX 画像プレビューの更新に失敗しました: "},
    {"音频后端异常", "Audio backend error", "オーディオバックエンドエラー"},
    {"已连接", "Connected", "接続済み"}, {"未初始化", "Not initialized", "未初期化"},
    {"运行", "Running", "実行中"}, {"待机", "Idle", "待機中"},
    {"选择一个组件后即可修改属性。", "Select a component to edit its properties.", "コンポーネントを選択するとプロパティを編集できます。"},
    {"文字组件：支持变量替换、字号、颜色、粗体和描边。拖动画布可移动位置。", "Text: variables, size, color, bold and outline are supported. Drag on the canvas to move.", "テキスト: 変数、サイズ、色、太字、縁取りに対応。キャンバスをドラッグして移動できます。"},
    {"图像组件：可调整等比例缩放、可见性和描边。拖动画布可移动位置。", "Image: adjust proportional scale, visibility and outline. Drag on the canvas to move.", "画像: 比率を維持した倍率、表示、縁取りを調整できます。キャンバスをドラッグして移動します。"},
    {"请先选择发射模板", "Select a TX template first", "先に送信テンプレートを選択してください"},
    {"单个模板最多支持 64 个组件", "A template supports up to 64 components", "テンプレートには最大 64 個のコンポーネントを追加できます"},
    {"新文本", "New text", "新しいテキスト"}, {"图像图库", "Image library", "画像ライブラリ"},
    {"选择图像资源", "Select image asset", "画像アセットを選択"}, {"导入图片…", "Import image…", "画像をインポート…"},
    {"选择‘设为底图’设置共享背景；选择‘添加图层’将图片放入当前 TX 模板。", "Choose ‘Set background’ for the shared background or ‘Add layer’ to add it to the current TX template.", "「背景に設定」で共有背景にし、「レイヤー追加」で現在の TX テンプレートに画像を追加します。"},
    {"加载预览…", "Loading preview…", "プレビューを読み込み中…"}, {"预览不可用", "Preview unavailable", "プレビューを利用できません"},
    {"设为底图", "Set as background", "背景に設定"}, {"添加图层", "Add layer", "レイヤーを追加"},
    {"模板共享底图已更新", "Shared template background updated", "共有テンプレート背景を更新しました"},
    {"图库暂无图片。请使用右上方‘导入图片’添加图像资源。", "The library is empty. Use ‘Import images’ to add assets.", "ライブラリは空です。右上の「画像をインポート」から追加してください。"},
    {"正在读取图像…", "Loading image…", "画像を読み込み中…"}, {"图像预览不可用", "Image preview unavailable", "画像プレビューを利用できません"},
    {"无法解码相册图像", "Unable to decode album image", "アルバム画像をデコードできません"},
    {"图像预览", "Image preview", "画像プレビュー"}, {"导出文件", "Export file", "ファイルをエクスポート"},
    {"删除记录", "Delete record", "記録を削除"}, {"导出图像", "Export image", "画像をエクスポート"},
    {"相册图像已导出", "Album image exported", "アルバム画像をエクスポートしました"},
    {"删除图像记录", "Delete image record", "画像記録を削除"}, {"确定删除所选相册记录吗？", "Delete the selected album record?", "選択したアルバム記録を削除しますか?"},
    {"相册暂无图像。", "No images in the album yet.", "アルバムに画像はありません。"},
    {"完成", "Complete", "完了"}, {"接收中", "Receiving", "受信中"},
    {"接收已启动", "RX started", "RX を開始しました"}, {"接收已停止", "RX stopped", "RX を停止しました"},
    {"RX 已启动", "RX started", "RX を開始しました"}, {"RX 已停止", "RX stopped", "RX を停止しました"},
    {"下一次循环发射：%1 秒", "Next repeat transmission in %1 s", "次の繰り返し送信まで %1 秒"},
    {"无法生成发送图像", "Unable to render transmit image", "送信画像を生成できません"},
    {"无法将发送模板编码为 PNG", "Unable to encode TX template as PNG", "TX テンプレートを PNG にエンコードできません"},
    {"已提交图像发射任务", "Image transmission job submitted", "画像送信ジョブを送信しました"},
    {"保存设置失败：", "Failed to save settings: ", "設定の保存に失敗しました: "},
    {"设备与电台设置", "Device and radio settings", "デバイスと無線機の設定"},
    {"系统默认", "System default", "システム既定"}, {"音频设备列表不可用：", "Audio device list unavailable: ", "オーディオデバイス一覧を利用できません: "},
    {"串口 RTS", "Serial RTS", "シリアル RTS"}, {"串口 DTR", "Serial DTR", "シリアル DTR"},
    {"串口列表不可用：", "Serial port list unavailable: ", "シリアルポート一覧を利用できません: "},
    {"刷新串口", "Refresh serial ports", "シリアルポートを更新"}, {"PTT 高电平有效", "PTT active high", "PTT High 有効"},
    {"本站网格", "Station locator", "自局グリッドロケーター"}, {"界面语言", "Interface language", "表示言語"},
    {"音频采样率", "Audio sample rate", "オーディオサンプルレート"}, {"RX 音频输入", "RX audio input", "RX オーディオ入力"},
    {"TX 音频输出", "TX audio output", "TX オーディオ出力"}, {"PTT 控制方式", "PTT method", "PTT 制御方式"},
    {"rigctld 地址", "rigctld address", "rigctld アドレス"}, {"串口设备", "Serial device", "シリアルデバイス"},
    {"PTT 发射前延时", "PTT lead time", "PTT 送信前遅延"}, {"PTT 发射后延时", "PTT tail time", "PTT 送信後遅延"},
    {"最低频率", "Minimum frequency", "最低周波数"}, {"最高频率", "Maximum frequency", "最高周波数"},
    {"最低电平", "Minimum level", "最低レベル"}, {"最高电平", "Maximum level", "最高レベル"},
    {"滚动速度", "Scroll rate", "スクロール速度"}, {"历史时长", "History duration", "履歴時間"},
    {"瀑布图颜色", "Waterfall colors", "ウォーターフォールの色"}, {"背景色", "Background", "背景色"},
    {"弱信号", "Weak signal", "弱信号"}, {"中等信号", "Medium signal", "中信号"},
    {"强信号", "Strong signal", "強信号"}, {"峰值信号", "Peak signal", "ピーク信号"},
    {"横轴为频率，纵轴为时间；颜色代表信号强度。修改参数后点击保存应用到 RX 与 TX 两个瀑布图。", "Frequency is on X, time on Y, and color indicates signal strength. Save to apply to RX and TX waterfalls.", "横軸は周波数、縦軸は時間、色は信号強度です。保存すると RX / TX の両方に適用されます。"},
    {"恢复默认显示设置", "Restore display defaults", "表示設定を既定値に戻す"},
    {"设备与电台", "Devices and radio", "デバイスと無線機"}, {"显示设置", "Display settings", "表示設定"},
    {"保存配置并测试 PTT", "Save settings and test PTT", "設定を保存して PTT をテスト"},
    {"停止 PTT 测试", "Stop PTT test", "PTT テストを停止"},
    {"PTT 测试已启动", "PTT test started", "PTT テストを開始しました"},
    {"PTT 测试已停止", "PTT test stopped", "PTT テストを停止しました"},
    {"设备设置已保存", "Device settings saved", "デバイス設定を保存しました"},
    {"导入图库图片", "Import library image", "ライブラリ画像をインポート"},
    {"导出完成", "Export complete", "エクスポート完了"}, {"已导出 %1 条记录", "Exported %1 records", "%1 件の記録をエクスポートしました"},
    {"模板包已导出：", "Template pack exported: ", "テンプレートパックをエクスポートしました: "},
    {"导入完成", "Import complete", "インポート完了"}, {"已导入 %1 个模板", "Imported %1 templates", "%1 個のテンプレートをインポートしました"},
    {"模板可视化编辑器", "Template visual editor", "テンプレートビジュアルエディター"},
    {"模板名称", "Template name", "テンプレート名"}, {"＋ 文字组件", "＋ Text component", "＋ テキストコンポーネント"},
    {"上移图层", "Move layer up", "レイヤーを上へ"}, {"下移图层", "Move layer down", "レイヤーを下へ"},
    {"5% 网格吸附", "5% grid snapping", "5% グリッドスナップ"}, {"删除选中组件", "Delete selected component", "選択したコンポーネントを削除"},
    {"文字组件属性", "Text component properties", "テキストコンポーネントのプロパティ"},
    {"字号", "Font size", "文字サイズ"}, {"颜色", "Color", "色"},
    {"画布操作：拖动组件移动；拖动右下角控制点调整文字字号或图像比例；选中后按 Delete 删除。", "Canvas: drag to move; drag the lower-right handle to resize; press Delete to remove the selected component.", "キャンバス: ドラッグで移動、右下ハンドルでサイズ変更、Delete キーで削除します。"},
    {"图库为空", "Image library is empty", "画像ライブラリは空です"},
    {"请先在图像图库页面导入图片，再添加图像组件。", "Import an image in the library before adding an image component.", "画像コンポーネントを追加する前に、画像ライブラリへ画像をインポートしてください。"},
    {"添加图像组件", "Add image component", "画像コンポーネントを追加"}, {"选择图库图像", "Choose library image", "ライブラリ画像を選択"},
    {"模板位置必须为 0–100%，字号必须为 6–160 px。", "Template position must be 0–100%; font size must be 6–160 px.", "テンプレート位置は 0–100%、文字サイズは 6–160 px にしてください。"},
    {"模板名称不能为空。", "Template name cannot be empty.", "テンプレート名を空にできません。"},
    {"模板布局已保存", "Template layout saved", "テンプレートレイアウトを保存しました"}, {"错误：", "Error: ", "エラー: "},
    {"VOX 由电台检测音频自动发射，不需要软件 PTT 测试。", "VOX is keyed by the radio from detected audio; a software PTT test is not needed.", "VOX は無線機側が音声を検出して送信するため、ソフトウェア PTT テストは不要です。"},
    {"日本語", "日本語", "日本語"}, {"简体中文", "简体中文", "简体中文"}, {"English", "English", "English"},
    {"新文本", "New text", "新しいテキスト"}, {"本次 QSO 信息", "QSO information", "今回の QSO 情報"},
    {"RX 已启动", "RX started", "RX を開始しました"}, {"RX 已停止", "RX stopped", "RX を停止しました"},
    {"自动识别", "Auto detect", "自動検出"}, {"图像预览", "Image preview", "画像プレビュー"},
    {"RECEIVE\n接收 RX", "RECEIVE\nReceive RX", "RECEIVE\n受信 RX"},
    {"TRANSMIT\n发送 TX", "TRANSMIT\nTransmit TX", "TRANSMIT\n送信 TX"},
    {"修改已更新到当前预设，点击‘保存预设’后写入配置。", "Changes are applied to the current preset. Select ‘Save Preset’ to write them to settings.", "変更は現在のプリセットに適用されています。「プリセットを保存」を押して設定に書き込んでください。"},
    {"RX · 上次接收图像", "RX · Last received image", "RX · 最後に受信した画像"},
    {"%1 · %2 个组件 · 拖动右侧预览画布中的图层可调整位置", "%1 · %2 components · Drag layers on the preview canvas to reposition them", "%1 · %2 コンポーネント · プレビューキャンバスでドラッグして位置を調整"},
    {"文字组件", "Text component", "テキストコンポーネント"},
    {"RX / TX HISTORY   历史相册", "RX / TX HISTORY   Image album", "RX / TX HISTORY   画像アルバム"},
    {"质量：%1", "Quality: %1", "品質: %1"},
    {"读取相册失败：", "Failed to load album: ", "アルバムの読み込みに失敗しました: "},
    {"自动识别 · %1", "Auto detect · %1", "自動検出 · %1"},
    {"%1 bins · 峰值 %2 dBFS @ %3 Hz", "%1 bins · Peak %2 dBFS @ %3 Hz", "%1 ビン · ピーク %2 dBFS @ %3 Hz"},
    {"音频：%1 · %2 Hz · RX %3 / TX %4", "Audio: %1 · %2 Hz · RX %3 / TX %4", "オーディオ: %1 · %2 Hz · RX %3 / TX %4"},
    {"本站呼号", "Station callsign", "自局コールサイン"},
    {"语言设置已保存；部分界面文本将在下次版本完善。", "Language saved; remaining UI strings will be localized in a later update.", "言語設定を保存しました。未翻訳の UI は今後の更新で対応します。"},
    {"副本", " Copy", " のコピー"}, {" 秒", " s", " 秒"}, {" 行/秒", " rows/s", " 行/秒"},
    {"640 × 480 · 拖动以移动 · 右下角调整大小", "640 × 480 · Drag to move · Resize with the lower-right handle", "640 × 480 · ドラッグで移動 · 右下のハンドルでサイズ変更"},
    {"LEVEL  -∞ dBFS       PEAK  ---       横轴：频率 · 纵轴：时间", "LEVEL  -∞ dBFS       PEAK  ---       X: frequency · Y: time", "LEVEL  -∞ dBFS       PEAK  ---       横軸: 周波数 · 縦軸: 時間"},
    {"已导入图库：", "Imported to image library: ", "画像ライブラリにインポートしました: "},
    {"拖动组件移动；拖动右下角控制点调整字号或图像比例；选中后按 Delete 删除", "Drag components to move; drag the lower-right handle to resize text or images; select and press Delete to remove.", "コンポーネントをドラッグして移動し、右下のハンドルでサイズを変更します。選択して Delete キーで削除します。"},
    {"读取相册失败：%1", "Failed to load album: %1", "アルバムの読み込みに失敗しました: %1"},
    {"语言偏好已保存：%1", "Language preference saved: %1", "言語設定を保存しました: %1"},
    {"无法加载语言词典，使用内置翻译。", "Unable to load locale dictionary; using built-in translations.", "言語辞書を読み込めません。組み込み翻訳を使用します。"},
};

QString sourceText(const QString &value) {
    for (const auto &entry : kTranslations) {
        if (value == QString::fromUtf8(entry.zh) || value == QString::fromUtf8(entry.en) || value == QString::fromUtf8(entry.ja))
            return QString::fromUtf8(entry.zh);
    }
    return value;
}
QString translate(const QString &source) {
    for (const auto &entry : kTranslations) {
        if (source == QString::fromUtf8(entry.zh)) {
            if (g_locale == QStringLiteral("en")) return QString::fromUtf8(entry.en);
            if (g_locale.startsWith(QStringLiteral("ja"))) return QString::fromUtf8(entry.ja);
            return QString::fromUtf8(entry.zh);
        }
    }
    return source;
}
QString translatedProperty(QObject *object, const char *propertyName, const QString &current) {
    QString source = object->property(propertyName).toString();
    if (source.isEmpty()) {
        source = sourceText(current);
        object->setProperty(propertyName, source);
    }
    return translate(source);
}
}

QString uiText(const QString &source) { return translate(sourceText(source)); }
void setUiLocale(const QString &locale) { g_locale = locale.startsWith(QStringLiteral("ja")) ? QStringLiteral("ja-JP") : locale == QStringLiteral("en") ? QStringLiteral("en") : QStringLiteral("zh-CN"); }

void translateWidgetTree(QWidget *root) {
    if (!root) return;
    root->setWindowTitle(translatedProperty(root, "satssdvI18nTitle", root->windowTitle()));
    const auto widgets = root->findChildren<QWidget *>();
    for (QWidget *widget : widgets) {
        if (auto *label = qobject_cast<QLabel *>(widget)) {
            label->setText(translatedProperty(label, "satssdvI18nText", label->text()));
        } else if (auto *button = qobject_cast<QAbstractButton *>(widget)) {
            button->setText(translatedProperty(button, "satssdvI18nText", button->text()));
        } else if (auto *group = qobject_cast<QGroupBox *>(widget)) {
            group->setTitle(translatedProperty(group, "satssdvI18nText", group->title()));
        }
        const QString toolTip = widget->toolTip();
        if (!toolTip.isEmpty()) widget->setToolTip(translatedProperty(widget, "satssdvI18nToolTip", toolTip));
        if (auto *line = qobject_cast<QLineEdit *>(widget)) {
            const QString placeholder = line->placeholderText();
            if (!placeholder.isEmpty()) line->setPlaceholderText(translatedProperty(line, "satssdvI18nPlaceholder", placeholder));
        }
        if (auto *combo = qobject_cast<QComboBox *>(widget)) {
            for (int i = 0; i < combo->count(); ++i) {
                QString source = combo->itemData(i, kSourceRole).toString();
                if (source.isEmpty()) {
                    source = sourceText(combo->itemText(i));
                    combo->setItemData(i, source, kSourceRole);
                }
                combo->setItemText(i, translate(source));
            }
        }
        if (auto *tabs = qobject_cast<QTabWidget *>(widget)) {
            for (int i = 0; i < tabs->count(); ++i) {
                QString source = tabs->tabBar()->tabData(i).toString();
                if (source.isEmpty()) {
                    source = sourceText(tabs->tabText(i));
                    tabs->tabBar()->setTabData(i, source);
                }
                tabs->setTabText(i, translate(source));
            }
        }
        if (auto *table = qobject_cast<QTableWidget *>(widget)) {
            if (table->horizontalHeaderItem(0)) {
                for (int col = 0; col < table->columnCount(); ++col) {
                    auto *item = table->horizontalHeaderItem(col);
                    if (!item) continue;
                    QString source = item->data(kSourceRole).toString();
                    if (source.isEmpty()) { source = sourceText(item->text()); item->setData(kSourceRole, source); }
                    item->setText(translate(source));
                }
            }
        }
    }
    for (QAction *action : root->findChildren<QAction *>()) {
        action->setText(translatedProperty(action, "satssdvI18nText", action->text()));
        if (!action->toolTip().isEmpty()) action->setToolTip(translatedProperty(action, "satssdvI18nToolTip", action->toolTip()));
    }
}
'''

ALPHA_H = r'''#pragma once

#include <QColor>
#include <QImage>
#include <QPainter>
#include <QRectF>

void drawAlphaContourOutline(QPainter &painter, const QImage &image,
                             const QRectF &target, const QColor &color,
                             qreal outlineWidth);
'''

ALPHA_CPP = r'''#include "alpha_outline.h"

#include <QVector>
#include <QtMath>
#include <algorithm>
#include <cmath>

void drawAlphaContourOutline(QPainter &painter, const QImage &image,
                             const QRectF &target, const QColor &color,
                             qreal outlineWidth) {
    if (image.isNull() || target.isEmpty()) return;
    const int radius = qBound(0, qCeil(outlineWidth), 20);
    if (radius <= 0) { painter.drawImage(target, image); return; }

    const int sourceWidth = qMax(1, qCeil(target.width()));
    const int sourceHeight = qMax(1, qCeil(target.height()));
    const QImage scaled = image.scaled(sourceWidth, sourceHeight, Qt::IgnoreAspectRatio, Qt::SmoothTransformation)
                              .convertToFormat(QImage::Format_ARGB32);
    const int padding = radius + 1;
    const int outputWidth = sourceWidth + padding * 2;
    const int outputHeight = sourceHeight + padding * 2;
    QVector<quint8> sourceAlpha(sourceWidth * sourceHeight);
    for (int y = 0; y < sourceHeight; ++y) {
        const QRgb *row = reinterpret_cast<const QRgb *>(scaled.constScanLine(y));
        for (int x = 0; x < sourceWidth; ++x)
            sourceAlpha[y * sourceWidth + x] = static_cast<quint8>(qAlpha(row[x]));
    }

    QVector<quint8> expanded(outputWidth * outputHeight, 0);
    QVector<int> deque(sourceWidth + 1);
    // Circular dilation, using a sliding maximum for each horizontal span.
    // This matches the Wails alpha-mask algorithm without drawing a rectangular
    // border around transparent PNG padding.
    for (int dy = -radius; dy <= radius; ++dy) {
        const int dx = static_cast<int>(std::floor(std::sqrt(double(radius * radius - dy * dy))));
        for (int sy = 0; sy < sourceHeight; ++sy) {
            const int targetY = padding + sy + dy;
            if (targetY < 0 || targetY >= outputHeight) continue;
            const int sourceOffset = sy * sourceWidth;
            const int targetOffset = targetY * outputWidth;
            int head = 0, tail = 0, next = 0;
            for (int targetX = 0; targetX < outputWidth; ++targetX) {
                const int right = qMin(sourceWidth - 1, targetX - padding + dx);
                while (next <= right) {
                    const quint8 value = sourceAlpha[sourceOffset + next];
                    while (tail > head && sourceAlpha[sourceOffset + deque[tail - 1]] <= value) --tail;
                    deque[tail++] = next++;
                }
                const int left = targetX - padding - dx;
                while (head < tail && deque[head] < left) ++head;
                if (head < tail) {
                    const quint8 value = sourceAlpha[sourceOffset + deque[head]];
                    const int index = targetOffset + targetX;
                    if (value > expanded[index]) expanded[index] = value;
                }
            }
        }
    }

    QImage mask(outputWidth, outputHeight, QImage::Format_ARGB32);
    mask.fill(Qt::transparent);
    for (int y = 0; y < outputHeight; ++y) {
        QRgb *row = reinterpret_cast<QRgb *>(mask.scanLine(y));
        for (int x = 0; x < outputWidth; ++x) {
            const int alpha = expanded[y * outputWidth + x];
            if (alpha > 0) row[x] = qRgba(color.red(), color.green(), color.blue(), alpha);
        }
    }
    const qreal sx = target.width() / qMax(1, sourceWidth);
    const qreal sy = target.height() / qMax(1, sourceHeight);
    const QRectF maskRect(target.x() - padding * sx, target.y() - padding * sy,
                          outputWidth * sx, outputHeight * sy);
    painter.drawImage(maskRect, mask);
    painter.drawImage(target, image);
}
'''

# A compact static regression guard ensures this migration does not regress the
# primary items the user reported.
REGRESSION_TEST = r'''#!/usr/bin/env python3
from pathlib import Path
import re

root = Path(__file__).resolve().parents[1]
cpp = (root / "qt6/mainwindow.cpp").read_text(encoding="utf-8")
header = (root / "qt6/mainwindow.h").read_text(encoding="utf-8")
canvas = (root / "qt6/template_canvas.cpp").read_text(encoding="utf-8")

assert "m_ax25To" not in cpp + header, "AX.25 To must not be shown as a user-facing field"
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
'''

coverage = r'''#!/usr/bin/env python3
"""Verify every CJK QStringLiteral in the Qt UI has a locale catalog entry."""
from pathlib import Path
import re

root = Path(__file__).resolve().parents[1]
ui_files = [root / "qt6/mainwindow.cpp", root / "qt6/template_canvas.cpp"]
catalog = (root / "qt6/ui_i18n.cpp").read_text(encoding="utf-8")

def unescape(value: str) -> str:
    return value.replace(r"\n", "\n").replace(r'\"', '"').replace(r"\\", "\\")

source_literals = set()
pattern = re.compile(r'QStringLiteral\("((?:[^"\\]|\\.)*)"\)')
for path in ui_files:
    source = path.read_text(encoding="utf-8")
    for match in pattern.finditer(source):
        value = unescape(match.group(1))
        if re.search(r"[\u3400-\u9fff\u3040-\u30ff\uac00-\ud7af]", value):
            source_literals.add(value)
entries = set()
for match in re.finditer(r'\{\s*"((?:[^"\\]|\\.)*)",', catalog):
    entries.add(unescape(match.group(1)))
missing = sorted(source_literals - entries)
if missing:
    raise SystemExit("Missing i18n entries:\n" + "\n".join(missing))
print(f"PASS: {len(source_literals)} CJK UI strings have locale catalog entries")
'''

# CMake and sources may already have source names in comments; make sure these
# newly created files don't overwrite anything.
new_files = {
    ROOT / "qt6/ui_i18n.h": UI_I18N_H,
    ROOT / "qt6/ui_i18n.cpp": UI_I18N_CPP,
    ROOT / "qt6/alpha_outline.h": ALPHA_H,
    ROOT / "qt6/alpha_outline.cpp": ALPHA_CPP,
    ROOT / "scripts/check_wails_parity_regressions.py": REGRESSION_TEST,
    ROOT / "scripts/check_wails_i18n_coverage.py": coverage,
}
for path in new_files:
    if path.exists():
        fail(f"refusing to overwrite existing file: {path.relative_to(ROOT)}")

# Validate post-edit invariants before touching the user's working tree.
if "m_ax25To" in cpp:
    fail("an AX.25 To widget/reference remains after migration")
for needle, label in [
    ("updateTXProfileControls()", "TX profile controls"),
    ("clearHisCallForCQTemplate()", "CQ callsign reset"),
    ("m_txPreparing", "TX preparation lock"),
    ("drawAlphaContourOutline", "alpha contour outline"),
    ("translateWidgetTree(this)", "widget tree translation"),
]:
    require(cpp, needle, label)

# Write only after every exact anchor and postcondition has passed.
backup_dir = ROOT / ".wails-parity-backup"
backup_dir.mkdir(exist_ok=False)
for path in FILES.values():
    rel = path.relative_to(ROOT)
    target = backup_dir / rel
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(path, target)

try:
    FILES["cpp"].write_text(cpp, encoding="utf-8")
    FILES["header"].write_text(header, encoding="utf-8")
    FILES["canvas"].write_text(canvas, encoding="utf-8")
    FILES["cmake"].write_text(cmake, encoding="utf-8")
    for path, content in new_files.items():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
except Exception:
    for path in FILES.values():
        backup = backup_dir / path.relative_to(ROOT)
        if backup.exists(): shutil.copy2(backup, path)
    for path in new_files:
        if path.exists(): path.unlink()
    raise

print("Wails reference parity migration applied.")
print("Backups: .wails-parity-backup/")
print("Next: python scripts/check_wails_parity_regressions.py")
print("Next: python scripts/check_wails_i18n_coverage.py")
