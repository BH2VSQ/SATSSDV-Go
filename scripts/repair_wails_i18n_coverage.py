#!/usr/bin/env python3
"""Repair the Wails parity i18n coverage checker and one real whitespace alias.

Run from the SATSSDV-Go repository root:
    py scripts/repair_wails_i18n_coverage.py

The Qt translation resolver recognizes a string if it appears in ANY of the
three columns (zh/en/ja). The old coverage checker inspected only column 1,
so it falsely reported existing Japanese translations. This repair aligns
coverage checking with the resolver and adds the genuinely missing leading-
space alias " 副本" when it appears in the UI source.
"""
from __future__ import annotations

import re
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
UI_FILES = [ROOT / "qt6/mainwindow.cpp", ROOT / "qt6/template_canvas.cpp"]
CATALOG = ROOT / "qt6/ui_i18n.cpp"
CHECKER = ROOT / "scripts/check_wails_i18n_coverage.py"
BACKUP_DIR = ROOT / ".wails-parity-backup"
CATALOG_BACKUP = BACKUP_DIR / "ui_i18n.cpp.before-coverage-repair"
CHECKER_BACKUP = BACKUP_DIR / "check_wails_i18n_coverage.py.before-coverage-repair"

ROW_RE = re.compile(
    r'\{\s*"((?:[^"\\]|\\.)*)"\s*,\s*'
    r'"((?:[^"\\]|\\.)*)"\s*,\s*'
    r'"((?:[^"\\]|\\.)*)"\s*\}'
)
LITERAL_RE = re.compile(r'QStringLiteral\("((?:[^"\\]|\\.)*)"\)')


def unescape(value: str) -> str:
    return value.replace(r"\n", "\n").replace(r'\"', '"').replace(r"\\", "\\")


def get_source_literals() -> set[str]:
    found: set[str] = set()
    for path in UI_FILES:
        if not path.is_file():
            raise SystemExit(f"Required Qt source file not found: {path}")
        for match in LITERAL_RE.finditer(path.read_text(encoding="utf-8")):
            value = unescape(match.group(1))
            if re.search(r"[\u3400-\u9fff\u3040-\u30ff\uac00-\ud7af]", value):
                found.add(value)
    return found


def get_catalog_values(catalog: str) -> set[str]:
    values: set[str] = set()
    for match in ROW_RE.finditer(catalog):
        values.update(unescape(match.group(i)) for i in (1, 2, 3))
    return values


def main() -> int:
    for path in (CATALOG, CHECKER):
        if not path.is_file():
            raise SystemExit(f"Required file not found: {path}")
    if CATALOG_BACKUP.exists() or CHECKER_BACKUP.exists():
        raise SystemExit(
            "A coverage-repair backup already exists; refusing to overwrite it.\n"
            f"Catalog backup: {CATALOG_BACKUP}\nChecker backup: {CHECKER_BACKUP}"
        )

    catalog_before = CATALOG.read_text(encoding="utf-8")
    source_literals = get_source_literals()
    catalog_values = get_catalog_values(catalog_before)
    missing = sorted(source_literals - catalog_values)
    print("Unregistered CJK source literals:", repr(missing))

    # The reported ' 副本' has a leading space and is not equivalent to '副本'.
    # Preserve that whitespace in the Chinese source column; English/Japanese
    # use the existing suffix forms already present in the catalogue.
    additions: list[str] = []
    for value in missing:
        if value == " 副本":
            additions.append('    {" 副本", " Copy", " のコピー"},')
        else:
            raise SystemExit(
                "Refusing to invent translations for unrecognized source text:\n  "
                + "\n  ".join(repr(item) for item in missing if item != " 副本")
            )

    catalog_after = catalog_before
    if additions:
        anchor = "const Translation kTranslations[] = {"
        if catalog_after.count(anchor) != 1:
            raise SystemExit("Cannot find a unique translation-table insertion point.")
        catalog_after = catalog_after.replace(anchor, anchor + "\n" + "\n".join(additions), 1)

    checker_after = r'''#!/usr/bin/env python3
"""Verify every CJK Qt UI literal is recognized by the locale resolver."""
from pathlib import Path
import re

root = Path(__file__).resolve().parents[1]
ui_files = [root / "qt6/mainwindow.cpp", root / "qt6/template_canvas.cpp"]
catalog = (root / "qt6/ui_i18n.cpp").read_text(encoding="utf-8")

def unescape(value: str) -> str:
    return value.replace(r"\n", "\n").replace(r'\"', '"').replace(r"\\", "\\")

source_literals = set()
literal_pattern = re.compile(r'QStringLiteral\("((?:[^"\\]|\\.)*)"\)')
for path in ui_files:
    source = path.read_text(encoding="utf-8")
    for match in literal_pattern.finditer(source):
        value = unescape(match.group(1))
        if re.search(r"[\u3400-\u9fff\u3040-\u30ff\uac00-\ud7af]", value):
            source_literals.add(value)

# sourceText() in ui_i18n.cpp accepts zh, en, and ja values as input strings.
# Therefore all three columns are valid lookup aliases, not just column one.
row_pattern = re.compile(
    r'\{\s*"((?:[^"\\]|\\.)*)"\s*,\s*'
    r'"((?:[^"\\]|\\.)*)"\s*,\s*'
    r'"((?:[^"\\]|\\.)*)"\s*\}'
)
registered_values = set()
for match in row_pattern.finditer(catalog):
    registered_values.update(unescape(match.group(i)) for i in (1, 2, 3))

missing = sorted(source_literals - registered_values)
if missing:
    raise SystemExit("Missing i18n entries:\n" + "\n".join(repr(value) for value in missing))
print(f"PASS: {len(source_literals)} CJK UI strings have locale catalog coverage")
'''

    BACKUP_DIR.mkdir(parents=True, exist_ok=True)
    shutil.copy2(CATALOG, CATALOG_BACKUP)
    shutil.copy2(CHECKER, CHECKER_BACKUP)
    try:
        if additions:
            CATALOG.write_text(catalog_after, encoding="utf-8", newline="\n")
        CHECKER.write_text(checker_after, encoding="utf-8", newline="\n")
        result = subprocess.run([sys.executable, str(CHECKER)], cwd=ROOT)
        if result.returncode != 0:
            raise subprocess.CalledProcessError(result.returncode, result.args)
    except Exception:
        shutil.copy2(CATALOG_BACKUP, CATALOG)
        shutil.copy2(CHECKER_BACKUP, CHECKER)
        print("Repair validation failed; original files restored.", file=sys.stderr)
        return 1

    print("PASS: i18n coverage checker repaired.")
    if additions:
        print("PASS: leading-space ' 副本' alias added to the locale table.")
        print(f"Catalog backup: {CATALOG_BACKUP.relative_to(ROOT)}")
    print(f"Checker backup: {CHECKER_BACKUP.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
