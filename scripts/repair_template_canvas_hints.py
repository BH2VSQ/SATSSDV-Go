#!/usr/bin/env python3
"""Remove the template-canvas resolution label and hover tooltip safely."""
from __future__ import annotations

import shutil
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CANVAS = ROOT / "qt6" / "template_canvas.cpp"
CHECKER = ROOT / "scripts" / "check_tx_ui_behavior.py"
BACKUP_DIR = ROOT / ".wails-parity-backup" / "template-canvas-hints-v1"

TOOLTIP_LINE = (
    '    setToolTip(uiText(QStringLiteral('
    '"拖动组件移动；拖动右下角控制点调整字号或图像比例；选中后按 Delete 删除")));'
)
RESOLUTION_BLOCK = (
    '    painter.setPen(QColor(QStringLiteral("#718896")));\n'
    '    painter.drawText(QRectF(8, height() - 22, width() - 16, 16), Qt::AlignRight | Qt::AlignVCenter,\n'
    '                     uiText(QStringLiteral("640 × 480")));'
)

OLD_CHECK = (
    '    "canvas no longer displays drag/resize caption": '
    '"拖动以移动" not in text["canvas"] and "右下角调整大小" not in text["canvas"],\n'
)
NEW_CHECKS = (
    '    "canvas has no hover tooltip": "setToolTip(" not in text["canvas"],\n'
    '    "canvas has no resolution label": "640 × 480" not in text["canvas"],\n'
)


def replace_exactly_once(source: str, old: str, new: str, description: str) -> tuple[str, bool]:
    count = source.count(old)
    if count == 0 and new.strip() and new in source:
        return source, False
    if count != 1:
        raise RuntimeError(f"{description}: expected exactly one anchor, found {count}")
    return source.replace(old, new, 1), True


def atomic_write(path: Path, content: str) -> None:
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(content, encoding="utf-8", newline="")
    temporary.replace(path)


def main() -> int:
    if not CANVAS.is_file():
        print("FAIL: qt6/template_canvas.cpp not found", file=sys.stderr)
        return 2

    canvas_original = CANVAS.read_text(encoding="utf-8")
    canvas_updated = canvas_original
    changed_canvas = False

    if TOOLTIP_LINE in canvas_updated:
        canvas_updated, _ = replace_exactly_once(canvas_updated, TOOLTIP_LINE, "", "canvas hover tooltip")
        changed_canvas = True
    elif "setToolTip(" in canvas_updated:
        print("FAIL: unexpected tooltip code in template_canvas.cpp; no files changed", file=sys.stderr)
        return 2

    if RESOLUTION_BLOCK in canvas_updated:
        canvas_updated, _ = replace_exactly_once(canvas_updated, RESOLUTION_BLOCK, "", "canvas resolution label")
        changed_canvas = True
    elif "640 × 480" in canvas_updated:
        print("FAIL: resolution text exists in an unexpected form; no files changed", file=sys.stderr)
        return 2

    checker_original = None
    checker_updated = None
    changed_checker = False
    if CHECKER.is_file():
        checker_original = CHECKER.read_text(encoding="utf-8")
        checker_updated = checker_original
        if OLD_CHECK in checker_updated:
            checker_updated, _ = replace_exactly_once(checker_updated, OLD_CHECK, OLD_CHECK + NEW_CHECKS, "TX/UI checker insertion")
            changed_checker = True
        else:
            required = (
                '"canvas has no hover tooltip"',
                '"canvas has no resolution label"',
            )
            missing = [item for item in required if item not in checker_updated]
            if missing:
                print("FAIL: TX/UI checker does not contain the expected insertion point", file=sys.stderr)
                return 2
    else:
        print("WARN: scripts/check_tx_ui_behavior.py not found; canvas will still be repaired")

    if not changed_canvas and not changed_checker:
        print("PASS: template canvas already has no resolution label or hover tooltip")
        return 0

    BACKUP_DIR.mkdir(parents=True, exist_ok=True)
    backups: list[tuple[Path, Path]] = []
    if changed_canvas:
        backup = BACKUP_DIR / "template_canvas.cpp.before"
        if not backup.exists():
            shutil.copy2(CANVAS, backup)
        backups.append((CANVAS, backup))
    if changed_checker and CHECKER.is_file():
        backup = BACKUP_DIR / "check_tx_ui_behavior.py.before"
        if not backup.exists():
            shutil.copy2(CHECKER, backup)
        backups.append((CHECKER, backup))

    try:
        if changed_canvas:
            atomic_write(CANVAS, canvas_updated)
        if changed_checker and checker_updated is not None:
            atomic_write(CHECKER, checker_updated)
    except Exception:
        if changed_canvas:
            atomic_write(CANVAS, canvas_original)
        if changed_checker and checker_original is not None:
            atomic_write(CHECKER, checker_original)
        raise

    verified = CANVAS.read_text(encoding="utf-8")
    if "setToolTip(" in verified or "640 × 480" in verified:
        atomic_write(CANVAS, canvas_original)
        if changed_checker and checker_original is not None:
            atomic_write(CHECKER, checker_original)
        print("FAIL: post-write verification failed; changes rolled back", file=sys.stderr)
        return 1

    print("PASS: removed the 640 × 480 canvas label")
    print("PASS: removed the template-canvas hover tooltip")
    if changed_checker:
        print("PASS: added two TX/UI regression checks")
    print(f"Backups: {BACKUP_DIR.relative_to(ROOT)}")
    if CHECKER.is_file():
        print("Next: py scripts/check_tx_ui_behavior.py")
    print("Next: py scripts/check_wails_i18n_coverage.py")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        raise SystemExit(1)
