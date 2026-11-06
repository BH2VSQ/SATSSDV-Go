#!/usr/bin/env python3
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
