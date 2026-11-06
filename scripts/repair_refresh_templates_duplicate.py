#!/usr/bin/env python3
"""Repair the duplicate current declaration introduced in refreshTemplates().

Run from a SATSSDV-Go checkout by placing this file at scripts/ and executing:
    python scripts/repair_refresh_templates_duplicate.py

The script repairs the already-migrated Qt source, updates the migration
installer so a clean re-application will not reproduce the issue, and adds a
regression assertion. Each changed file is backed up before being written.
"""
from __future__ import annotations

from pathlib import Path
import shutil
import sys

ROOT = Path(__file__).resolve().parents[1]
BACKUP_DIR = ROOT / ".wails-parity-backup"

CPP_REL = Path("qt6/mainwindow.cpp")
INSTALLER_REL = Path("scripts/apply_wails_reference_parity.py")
CHECKER_REL = Path("scripts/check_wails_parity_regressions.py")

DECL = "    const QString current=m_template->currentData().toString(); m_template->clear();"
CHECKER_ANCHOR = 'assert "m_ax25To" not in cpp + header, "AX.25 To must not be shown as a user-facing field"'
CHECKER_GUARD = 'assert cpp.count(\'    const QString current=m_template->currentData().toString(); m_template->clear();\') == 1, "refreshTemplates must declare current exactly once"'


def fail(message: str) -> None:
    raise SystemExit(f"ERROR: {message}")


def read_text(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8")
    except FileNotFoundError:
        fail(f"required file not found: {path.relative_to(ROOT)}")
    except UnicodeDecodeError as exc:
        fail(f"file is not valid UTF-8: {path.relative_to(ROOT)} ({exc})")


def main() -> int:
    cpp_path = ROOT / CPP_REL
    cpp = read_text(cpp_path)
    newline = "\r\n" if "\r\n" in cpp else "\n"
    pair = DECL + newline + DECL
    pair_count = cpp.count(pair)
    decl_count = cpp.count(DECL)

    if pair_count == 1 and decl_count == 2:
        repaired_cpp = cpp.replace(pair, DECL, 1)
        cpp_changed = True
    elif pair_count == 0 and decl_count == 1:
        repaired_cpp = cpp
        cpp_changed = False
    else:
        fail(
            "unexpected refreshTemplates declaration layout; refusing to edit. "
            f"Found {pair_count} adjacent duplicate pair(s) and {decl_count} exact declaration line(s). "
            "Inspect qt6/mainwindow.cpp around MainWindow::refreshTemplates()."
        )

    changes: dict[Path, str] = {}
    if cpp_changed:
        changes[cpp_path] = repaired_cpp

    installer_path = ROOT / INSTALLER_REL
    if installer_path.exists():
        installer = read_text(installer_path)
        installer_count = installer.count(DECL)
        if installer_count == 1:
            # The source anchor replacement must leave the original following
            # declaration intact, not insert a second declaration itself.
            changes[installer_path] = installer.replace(DECL + '""",', '""",', 1)
            if changes[installer_path] == installer:
                fail("found the installer declaration, but its expected replacement-block ending did not match")
        elif installer_count != 0:
            fail(f"unexpected installer declaration count: {installer_count}; refusing to edit installer")

    checker_path = ROOT / CHECKER_REL
    if checker_path.exists():
        checker = read_text(checker_path)
        if CHECKER_GUARD in checker:
            pass
        elif CHECKER_ANCHOR in checker:
            changes[checker_path] = checker.replace(
                CHECKER_ANCHOR,
                CHECKER_ANCHOR + "\n" + CHECKER_GUARD,
                1,
            )
        else:
            fail("could not locate a safe insertion point in check_wails_parity_regressions.py")

    if not changes:
        print("Already repaired: refreshTemplates contains exactly one current declaration.")
        return 0

    BACKUP_DIR.mkdir(parents=True, exist_ok=True)
    backups: dict[Path, Path] = {}
    for path in changes:
        backup = BACKUP_DIR / (path.name + ".before-refreshTemplates-duplicate-fix")
        if backup.exists():
            fail(f"backup already exists; refusing to overwrite it: {backup.relative_to(ROOT)}")
        backups[path] = backup

    originals = {path: path.read_bytes() for path in changes}
    try:
        for path, backup in backups.items():
            shutil.copy2(path, backup)
        for path, content in changes.items():
            path.write_text(content, encoding="utf-8", newline="\r\n" if originals[path].count(b"\r\n") else "\n")

        verified_cpp = read_text(cpp_path)
        if verified_cpp.count(DECL) != 1 or pair in verified_cpp:
            fail("post-write verification failed for qt6/mainwindow.cpp")
        if installer_path in changes and DECL in read_text(installer_path):
            fail("post-write verification failed for migration installer")
        if checker_path.exists() and CHECKER_GUARD not in read_text(checker_path):
            fail("post-write verification failed for regression checker")
    except Exception:
        for path, data in originals.items():
            path.write_bytes(data)
        raise

    print("PASS: duplicate current declaration removed from refreshTemplates()." if cpp_changed else "PASS: Qt source already contains one declaration.")
    if installer_path in changes:
        print("PASS: migration installer corrected to avoid reintroducing the duplicate.")
    if checker_path in changes:
        print("PASS: regression checker now guards against duplicate declarations.")
    print("Backups:")
    for backup in backups.values():
        print(f"  {backup.relative_to(ROOT)}")
    print("Next:")
    print("  python scripts/check_wails_parity_regressions.py")
    print("  cmake --build build-qt6 --parallel 2 --verbose")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SystemExit:
        raise
    except Exception as exc:  # Keep the failure actionable on Windows CI.
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
