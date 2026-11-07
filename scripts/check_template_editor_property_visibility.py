#!/usr/bin/env python3
"""Static regression checks for type-specific TX template properties."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CPP = (ROOT / "qt6" / "mainwindow.cpp").read_text(encoding="utf-8")
HEADER = (ROOT / "qt6" / "mainwindow.h").read_text(encoding="utf-8")
CANVAS = (ROOT / "qt6" / "template_canvas.cpp").read_text(encoding="utf-8")

checks = {
    "property rows have separately hideable labels": all(name in HEADER for name in (
        "m_componentTextLabel", "m_componentAssetLabel", "m_componentScaleLabel",
        "m_componentFontLabel", "m_componentOutlineLabel"
    )),
    "text content appears only for text components": "m_componentTextEdit->setVisible(isText)" in CPP and "m_componentTextLabel->setVisible(isText)" in CPP,
    "gallery asset selector appears only for image components": "m_componentAssetCombo->setVisible(isImage)" in CPP and "m_componentAssetLabel->setVisible(isImage)" in CPP,
    "image scale appears only for image layers": "m_componentScaleSlider->setVisible(isImageLayer)" in CPP and "m_componentScaleLabel->setVisible(isImageLayer)" in CPP,
    "font size appears only for text components": "m_componentFontSlider->setVisible(isText)" in CPP and "m_componentFontLabel->setVisible(isText)" in CPP,
    "text color and bold appear only for text components": "m_componentColorButton->setVisible(isText)" in CPP and "m_componentBoldCheck->setVisible(isText)" in CPP,
    "GIF outline width, color and toggle are hidden": "m_componentOutlineSlider->setVisible(showOutline)" in CPP and "m_componentOutlineLabel->setVisible(showOutline)" in CPP and "m_componentOutlineColorButton->setVisible(showOutline)" in CPP and "m_componentOutlineCheck->setVisible(showOutline)" in CPP,
    "GIF outline properties are removed when applying edits": "if (isGIFImage)" in CPP and 'component.remove(QStringLiteral("outlineWidth"))' in CPP and 'component.remove(QStringLiteral("outlineColor"))' in CPP,
    "GIF type is detected from the gallery asset": "isGifGalleryAsset(m_gallery" in CPP,
    "template canvas skips software outline for GIF": '!component.value(QStringLiteral("isGIF")).toBool(false)' in CANVAS,
    "static TX template renderer skips outlines for GIF data": "if (!gifImage && c.value(QStringLiteral(\"outline\"))" in CPP,
    "dynamic GIF TX renderer does not draw outlines": "if (!layer.gif && component.value(QStringLiteral(\"outline\"))" in CPP,
    "component-specific explanatory hint is removed": "m_componentEditorHint" not in CPP + HEADER and "GIF 图像组件：支持增量动画发射" not in CPP,
    "template editor has no verbose component-type descriptions": all(term not in CPP for term in ("文字组件：", "文本组件：", "图像组件：", "GIF动画组件：")),
    "template editor action labels avoid component wording": 'QStringLiteral("＋ 文字")' in CPP and 'QStringLiteral("＋ 图库图像")' in CPP and 'QStringLiteral("删除")' in CPP,
    "template editor heading is concise": 'QStringLiteral("模板编辑")' in CPP,

}

failed = [name for name, ok in checks.items() if not ok]
for name, ok in checks.items():
    print(f"{'PASS' if ok else 'FAIL'}: {name}")
if failed:
    raise SystemExit("Template editor property checks failed: " + "; ".join(failed))
print(f"PASS: {len(checks)} template editor property checks")
