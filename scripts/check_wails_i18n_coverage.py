#!/usr/bin/env python3
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
