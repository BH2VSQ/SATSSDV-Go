#!/usr/bin/env python3
"""Update the Qt/Wails static parity checker for the TX repeat state machine.

Run from the repository root:
    py scripts/repair_qt_wails_parity_checker.py

This only updates scripts/check_qt_wails_parity.py. It does not alter runtime
C++ or Go code. The checker previously required the superseded
m_repeatInterval->setEnabled(!enabled) implementation.
"""
from __future__ import annotations

import shutil
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CHECKER = ROOT / "scripts" / "check_qt_wails_parity.py"
BACKUP_DIR = ROOT / ".wails-parity-backup" / "qt-wails-parity-checker-v2"
BACKUP = BACKUP_DIR / "check_qt_wails_parity.py.before-repeat-state-update"

OLD = '''# Repeat interval locks during repeat mode and supports the backend countdown phase.
assert "m_repeatInterval->setEnabled(!enabled)" in CPP
assert 'phase == QStringLiteral("countdown")' in CPP
assert "tx.testTone" in CPP'''

NEW = '''# Repeat controls follow the native repeat state machine. The interval field is
# display-only while repeat is enabled; the checkbox can only cancel an active
# inter-cycle countdown, never an image burst already in progress.
assert "const bool canToggleRepeat = !m_txPreparing && (!m_txActive || m_txCountdownActive);" in CPP
assert "m_repeatInterval->setEnabled(!repeatChecked && !m_txCountdownActive)" in CPP
assert "m_txCountdownActive = true;" in CPP
assert "cancelRepeatCountdown" in CPP and 'QStringLiteral("StopTX")' in CPP
assert "m_repeatIntervalSeconds" in CPP
assert 'phase == QStringLiteral("countdown")' in CPP
assert 'phase == QStringLiteral("cancelled")' in CPP
assert "tx.testTone" in CPP'''


def main() -> int:
    if not CHECKER.is_file():
        print(f"FAIL: checker not found: {CHECKER}", file=sys.stderr)
        return 2

    try:
        original = CHECKER.read_text(encoding="utf-8")
    except OSError as exc:
        print(f"FAIL: cannot read checker: {exc}", file=sys.stderr)
        return 2

    if NEW in original:
        print("PASS: Qt/Wails parity checker already matches the TX repeat state machine.")
        return 0

    count = original.count(OLD)
    if count != 1:
        print(
            "FAIL: expected exactly one obsolete repeat-check block in "
            f"{CHECKER.relative_to(ROOT)}, found {count}. No file was changed.",
            file=sys.stderr,
        )
        return 1

    updated = original.replace(OLD, NEW, 1)
    if "m_repeatInterval->setEnabled(!enabled)" in updated:
        print("FAIL: obsolete assertion remains after replacement. No file was changed.", file=sys.stderr)
        return 1

    try:
        BACKUP_DIR.mkdir(parents=True, exist_ok=True)
        if BACKUP.exists():
            print(f"FAIL: backup already exists: {BACKUP.relative_to(ROOT)}", file=sys.stderr)
            return 1
        shutil.copy2(CHECKER, BACKUP)
        CHECKER.write_text(updated, encoding="utf-8", newline="\n")
    except OSError as exc:
        try:
            if BACKUP.exists() and CHECKER.exists():
                CHECKER.write_text(original, encoding="utf-8", newline="\n")
        except OSError:
            pass
        print(f"FAIL: could not update checker; rollback attempted: {exc}", file=sys.stderr)
        return 1

    print("PASS: Qt/Wails parity checker updated for the new repeat state machine.")
    print(f"Backup: {BACKUP.relative_to(ROOT)}")
    print("Next: py scripts/check_qt_wails_parity.py")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
