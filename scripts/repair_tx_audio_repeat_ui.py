#!/usr/bin/env python3
"""Apply the SATSSDV-Go TX tone, repeat-cycle, and Qt control-style fixes.

Run from the repository root:
    py scripts/repair_tx_audio_repeat_ui.py

The script is intentionally strict and transactional. It only edits the four
known source files after every expected source anchor has been found exactly
once. Original files are copied to .wails-parity-backup/tx-ui-audio-loop-v1/.
"""
from __future__ import annotations

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
BACKUP_DIR = ROOT / ".wails-parity-backup" / "tx-ui-audio-loop-v1"


def replace_once(source: str, old: str, new: str, label: str) -> str:
    count = source.count(old)
    if count != 1:
        raise RuntimeError(f"{label}: expected exactly one anchor, found {count}")
    return source.replace(old, new, 1)


def transform_header(text: str) -> str:
    text = replace_once(
        text,
        "    void updateTXToggle();\n    void updateTXProfileControls();",
        "    void updateTXToggle();\n    void updateRepeatControls();\n    void updateTXProfileControls();",
        "mainwindow.h repeat-control declaration",
    )
    text = replace_once(
        text,
        "    bool m_hisCallManuallyEdited = false;\n    int m_repeatIntervalSeconds = 120;",
        "    bool m_hisCallManuallyEdited = false;\n"
        "    bool m_txCountdownActive = false;\n"
        "    int m_repeatIntervalSeconds = 120;",
        "mainwindow.h countdown state",
    )
    return text


def transform_mainwindow(text: str) -> str:
    # The global QWidget rule deliberately has no border. Give Qt sliders an
    # explicit borderless appearance so platform style engines cannot draw a
    # framed control around the track.
    text = replace_once(
        text,
        '        "QComboBox,QLineEdit,QSpinBox { min-height:22px; }"',
        '        "QComboBox,QLineEdit,QSpinBox { min-height:22px; }"\n'
        '        "QSlider { border:0; background:transparent; padding:0; }"\n'
        '        "QSlider::groove:horizontal { height:5px; background:#d3dbe0; border:0; border-radius:2px; }"\n'
        '        "QSlider::handle:horizontal { width:12px; background:#6fa5ba; border:0; border-radius:6px; margin:-4px 0; }"\n'
        '        "QSlider::groove:vertical { width:5px; background:#d3dbe0; border:0; border-radius:2px; }"\n'
        '        "QSlider::handle:vertical { height:12px; background:#6fa5ba; border:0; border-radius:6px; margin:0 -4px; }"',
        "mainwindow.cpp global slider style",
    )

    text = replace_once(
        text,
        '    m_componentEditorHint->setStyleSheet(QStringLiteral("color:#687780; font-size:8pt; padding:4px; background:#e9eef1; border:1px solid #cbd3d8;"));',
        '    m_componentEditorHint->setStyleSheet(QStringLiteral("color:#687780; font-size:8pt; padding:4px; background:transparent; border:0;"));',
        "mainwindow.cpp plain text hint style",
    )

    old_repeat = '''    connect(m_repeat, &QCheckBox::toggled, this, [this](bool enabled) {
        if (!m_repeatInterval) return;
        if (enabled) m_repeatIntervalSeconds = m_repeatInterval->value();
        else m_repeatInterval->setValue(m_repeatIntervalSeconds);
        m_repeatInterval->setEnabled(!enabled);
    });'''
    new_repeat = '''    connect(m_repeat, &QCheckBox::toggled, this, [this](bool enabled) {
        if (!m_repeatInterval) return;
        if (enabled) {
            m_repeatIntervalSeconds = m_repeatInterval->value();
            updateRepeatControls();
            return;
        }

        // Repeat can be toggled off only while the scheduler is in its
        // inter-cycle countdown. The current image burst is never interrupted.
        const bool cancelRepeatCountdown = m_txActive && m_txCountdownActive;
        m_txCountdownActive = false;
        m_repeatInterval->setValue(m_repeatIntervalSeconds);
        updateRepeatControls();
        if (cancelRepeatCountdown && m_backend && m_backend->isRunning()) {
            m_backend->call(QStringLiteral("StopTX"), {}, [this](bool ok, const QJsonValue &, const QString &error) {
                if (!ok) showError(error);
            });
        }
    });
    connect(m_repeatInterval, QOverload<int>::of(&QSpinBox::valueChanged), this, [this](int value) {
        if (!m_repeat || !m_repeat->isChecked()) m_repeatIntervalSeconds = value;
    });
    updateRepeatControls();'''
    text = replace_once(text, old_repeat, new_repeat, "mainwindow.cpp repeat checkbox handler")

    # The backend reservation spans the whole repeat job, including idle
    # countdown periods. Keep the native controls synchronized with that state.
    text = replace_once(
        text,
        '        m_txActive = state.value(QStringLiteral("txActive")).toBool();\n        const int rate = state.value(QStringLiteral("sampleRate")).toInt();',
        '        m_txActive = state.value(QStringLiteral("txActive")).toBool();\n'
        '        updateRepeatControls();\n'
        '        const int rate = state.value(QStringLiteral("sampleRate")).toInt();',
        "mainwindow.cpp audio status repeat synchronization",
    )

    text = replace_once(
        text,
        '        m_txToneToggle->setEnabled(m_txToneActive || (!m_txActive && m_audioConnected && m_backend && m_backend->isRunning()));',
        '        m_txToneToggle->setEnabled(m_txToneActive || (!m_txActive && !m_txPreparing && m_audioConnected && m_backend && m_backend->isRunning()));',
        "mainwindow.cpp prevent tone during TX preparation",
    )

    # Add a single helper so both event updates and the periodic status refresh
    # apply the same enabled/disabled rules.
    text = replace_once(
        text,
        '''void MainWindow::toggleRX() { if (m_rxActive) stopRX(); else startRX(); }''',
        '''void MainWindow::updateRepeatControls() {
    const bool repeatChecked = m_repeat && m_repeat->isChecked();
    const bool canToggleRepeat = !m_txPreparing && (!m_txActive || m_txCountdownActive);
    if (m_repeat) m_repeat->setEnabled(canToggleRepeat);
    if (m_repeatInterval) {
        // The spin box is a display-only countdown while repeat is enabled.
        m_repeatInterval->setEnabled(!repeatChecked && !m_txCountdownActive);
        if (!repeatChecked && !m_txCountdownActive)
            m_repeatInterval->setValue(m_repeatIntervalSeconds);
    }
}

void MainWindow::toggleRX() { if (m_rxActive) stopRX(); else startRX(); }''',
        "mainwindow.cpp repeat-control helper insertion",
    )

    text = replace_once(
        text,
        '''        m_txActive = payloadValue.toObject().value(QStringLiteral("active")).toBool();
        updateTXToggle();''',
        '''        m_txActive = payloadValue.toObject().value(QStringLiteral("active")).toBool();
        if (!m_txActive) m_txCountdownActive = false;
        updateRepeatControls();
        updateTXToggle();''',
        "mainwindow.cpp tx state event handling",
    )

    old_status = '''        if (phase == QStringLiteral("countdown") && m_repeat && m_repeat->isChecked()) {
            const int remaining = qMax(0, p.value(QStringLiteral("error")).toInt());
            m_repeatInterval->setEnabled(false);
            m_repeatInterval->setValue(remaining);
            statusBar()->showMessage(uiText(QStringLiteral("下一次循环发射：%1 秒")).arg(remaining));
        } else if (phase == QStringLiteral("sending") || phase == QStringLiteral("completed")) {
            m_repeatInterval->setValue(m_repeatIntervalSeconds);
            m_repeatInterval->setEnabled(!m_repeat->isChecked());
        }'''
    new_status = '''        if (phase == QStringLiteral("countdown") && m_repeat && m_repeat->isChecked()) {
            m_txCountdownActive = true;
            updateRepeatControls();
            const int remaining = qMax(0, p.value(QStringLiteral("error")).toInt());
            m_repeatInterval->setValue(remaining);
            statusBar()->showMessage(uiText(QStringLiteral("下一次循环发射：%1 秒")).arg(remaining));
        } else if (phase == QStringLiteral("sending") || phase == QStringLiteral("completed")) {
            m_txCountdownActive = false;
            m_repeatInterval->setValue(m_repeatIntervalSeconds);
            updateRepeatControls();
        } else if (phase == QStringLiteral("cancelled")) {
            m_txCountdownActive = false;
            m_repeatInterval->setValue(m_repeatIntervalSeconds);
            updateRepeatControls();
        }'''
    text = replace_once(text, old_status, new_status, "mainwindow.cpp repeat status state machine")

    text = replace_once(
        text,
        '    m_txPreparing = true;\n    updateTXToggle();',
        '    m_txPreparing = true;\n    updateRepeatControls();\n    updateTXToggle();',
        "mainwindow.cpp TX preparation repeat lock",
    )

    text = replace_once(
        text,
        '        m_txPreparing = false;\n        if (ok) {',
        '        m_txPreparing = false;\n        updateRepeatControls();\n        if (ok) {',
        "mainwindow.cpp finish TX preparation repeat state",
    )

    text = replace_once(
        text,
        '    const QJsonArray args{rendered, m_repeat->isChecked(), m_repeatInterval->value(), ax25To};',
        '    const int repeatInterval = m_repeat->isChecked() ? m_repeatIntervalSeconds : m_repeatInterval->value();\n'
        '    const QJsonArray args{rendered, m_repeat->isChecked(), repeatInterval, ax25To};',
        "mainwindow.cpp stable repeat interval RPC argument",
    )

    text = replace_once(
        text,
        '''        if (ok) {
            m_txActive = true;
            updateTXToggle();''',
        '''        if (ok) {
            m_txActive = true;
            updateRepeatControls();
            updateTXToggle();''',
        "mainwindow.cpp TX accepted state",
    )
    return text


def transform_canvas(text: str) -> str:
    return replace_once(
        text,
        'uiText(QStringLiteral("640 × 480 · 拖动以移动 · 右下角调整大小"))',
        'uiText(QStringLiteral("640 × 480"))',
        "template canvas remove drag/resize caption",
    )


def transform_app(text: str) -> str:
    # txReserved covers the scheduler's between-image countdown, when the
    # low-level audio duplex is correctly idle but the overall TX task is not.
    text = replace_once(
        text,
        '''func (a *App) GetAudioState() AudioState {
	a.mu.Lock()
	d := a.audioDuplex
	rate := a.settings.AudioSampleRate
	audioErr := a.audioError
	ready := a.audioRuntime != nil
	a.mu.Unlock()
	state := AudioState{Connected: ready, SampleRate: rate, Error: audioErr}
	if d != nil {
		s := d.State()
		state.RXActive, state.TXActive = s.RXActive, s.TXActive
	}
	return state
}''',
        '''func (a *App) GetAudioState() AudioState {
	a.mu.Lock()
	d := a.audioDuplex
	rate := a.settings.AudioSampleRate
	audioErr := a.audioError
	ready := a.audioRuntime != nil
	a.mu.Unlock()

	// txReserved remains true for the full TX task, including the scheduler's
	// inter-cycle countdown when Duplex.State().TXActive is temporarily false.
	a.txGate.Lock()
	txReserved := a.txReserved
	a.txGate.Unlock()

	state := AudioState{Connected: ready, SampleRate: rate, Error: audioErr}
	if d != nil {
		s := d.State()
		state.RXActive = s.RXActive
		state.TXActive = s.TXActive || txReserved
	} else {
		state.TXActive = txReserved
	}
	return state
}''',
        "app.go report reserved TX task state",
    )

    text = replace_once(
        text,
        'ctx, cancel := context.WithTimeout(parent, 60*time.Second)',
        'ctx, cancel := context.WithTimeout(parent, 75*time.Second)',
        "app.go tone deadline includes PTT/output startup margin",
    )
    text = replace_once(
        text,
        '\t\t\tgain := 0.30',
        '\t\t\t// Higher level improves VOX-trigger margin while retaining 3 dBFS headroom.\n\t\t\tgain := 0.70',
        "app.go 1500 Hz test tone output level",
    )
    return text


def main() -> int:
    paths = {
        "mainwindow.cpp": ROOT / "qt6" / "mainwindow.cpp",
        "mainwindow.h": ROOT / "qt6" / "mainwindow.h",
        "template_canvas.cpp": ROOT / "qt6" / "template_canvas.cpp",
        "app.go": ROOT / "app.go",
    }
    missing = [str(path.relative_to(ROOT)) for path in paths.values() if not path.is_file()]
    if missing:
        print("Missing required project files: " + ", ".join(missing), file=sys.stderr)
        return 2
    if BACKUP_DIR.exists():
        print(f"Refusing to overwrite existing backup directory: {BACKUP_DIR}", file=sys.stderr)
        return 2

    originals = {name: path.read_text(encoding="utf-8") for name, path in paths.items()}
    try:
        updated = {
            "mainwindow.cpp": transform_mainwindow(originals["mainwindow.cpp"]),
            "mainwindow.h": transform_header(originals["mainwindow.h"]),
            "template_canvas.cpp": transform_canvas(originals["template_canvas.cpp"]),
            "app.go": transform_app(originals["app.go"]),
        }
    except Exception as exc:
        print(f"No files changed: {exc}", file=sys.stderr)
        return 2

    # Pre-write invariants.
    cpp = updated["mainwindow.cpp"]
    header = updated["mainwindow.h"]
    app = updated["app.go"]
    canvas = updated["template_canvas.cpp"]
    invariants = [
        ("countdown state declaration", "bool m_txCountdownActive = false;" in header),
        ("repeat control is locked during TX preparation", "const bool canToggleRepeat = !m_txPreparing && (!m_txActive || m_txCountdownActive);" in cpp),
        ("repeat checkbox cancellation", "cancelRepeatCountdown" in cpp and 'QStringLiteral("StopTX")' in cpp),
        ("repeat countdown phase", "m_txCountdownActive = true;" in cpp),
        ("stable interval submission", "const int repeatInterval = m_repeat->isChecked() ? m_repeatIntervalSeconds" in cpp),
        ("borderless QSlider style", "QSlider { border:0; background:transparent; padding:0; }" in cpp),
        ("plain editor hint", 'background:transparent; border:0;' in cpp),
        ("canvas instruction removed", "拖动以移动" not in canvas and "右下角调整大小" not in canvas),
        ("reserved TX state", "txReserved := a.txReserved" in app and "s.TXActive || txReserved" in app),
        ("stronger test tone", "gain := 0.70" in app),
        ("tone deadline margin", "context.WithTimeout(parent, 75*time.Second)" in app),
    ]
    failed = [label for label, ok in invariants if not ok]
    if failed:
        print("No files changed; pre-write verification failed: " + ", ".join(failed), file=sys.stderr)
        return 2

    BACKUP_DIR.mkdir(parents=True)
    try:
        for name, path in paths.items():
            backup = BACKUP_DIR / path.relative_to(ROOT)
            backup.parent.mkdir(parents=True, exist_ok=True)
            backup.write_text(originals[name], encoding="utf-8", newline="")
        for name, path in paths.items():
            path.write_text(updated[name], encoding="utf-8", newline="")
    except Exception as exc:
        # Restore every touched file if any write fails.
        for name, path in paths.items():
            if (BACKUP_DIR / path.relative_to(ROOT)).exists():
                path.write_text(originals[name], encoding="utf-8", newline="")
        print(f"Write failed; original files restored: {exc}", file=sys.stderr)
        return 2

    print("TX tone, repeat scheduler UI, slider style, and canvas caption fixes applied.")
    print(f"Backups: {BACKUP_DIR.relative_to(ROOT)}")
    print("Next: py scripts/check_tx_ui_behavior.py")
    print("Next: py scripts/check_wails_parity_regressions.py")
    print("Next: py scripts/check_wails_i18n_coverage.py")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
