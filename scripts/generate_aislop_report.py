#!/usr/bin/env python3
"""Convert the Aislop CI JSON result into a durable Markdown report."""

from __future__ import annotations

import argparse
import json
import os
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def read_result(path: Path) -> tuple[dict[str, Any] | None, str]:
    if not path.is_file():
        return None, f"Aislop JSON output was not created: {path}"
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        return None, f"Unable to parse Aislop JSON: {exc}"
    if not isinstance(value, dict):
        return None, "Aislop JSON root must be an object"
    return value, ""


def format_engine_table(engines: dict[str, Any]) -> list[str]:
    rows = ["| Engine | Findings | Skipped | Time (ms) |", "|---|---:|:---:|---:|"]
    for name, data in sorted(engines.items()):
        if not isinstance(data, dict):
            continue
        rows.append(
            f"| `{name}` | {data.get('issues', 0)} | "
            f"{'yes' if data.get('skipped') else 'no'} | {data.get('elapsed', 0):.1f} |"
        )
    return rows


def group_diagnostics(diagnostics: list[dict[str, Any]]) -> tuple[Counter, dict, dict]:
    by_rule: Counter = Counter()
    by_file: dict[str, Counter] = defaultdict(Counter)
    by_assessment: Counter = Counter()
    for item in diagnostics:
        severity = str(item.get("severity", "unknown")).lower()
        rule = str(item.get("rule", "unknown"))
        engine = str(item.get("engine", "unknown"))
        by_rule[(engine, rule, severity)] += 1
        by_file[str(item.get("filePath", "unknown"))][severity] += 1
        assessment = item.get("assessment") or {}
        by_assessment[str(assessment.get("kind", "unclassified"))] += 1
    return by_rule, by_file, by_assessment


def format_diagnostic_appendix(diagnostics: list[dict[str, Any]]) -> list[str]:
    rows = ["| Severity | Rule | File | Location | Finding |", "|---|---|---|---:|---|"]
    ordered = sorted(
        diagnostics,
        key=lambda item: (
            0 if str(item.get("severity", "")).lower() == "error" else 1,
            str(item.get("filePath", "")),
            int(item.get("line", 0) or 0),
        ),
    )
    for item in ordered:
        file_path = str(item.get("filePath", "unknown"))
        line = int(item.get("line", 0) or 0)
        column = int(item.get("column", 0) or 0)
        location = f"{line}:{column}" if line else "—"
        message = str(item.get("message", "")).replace("|", "\\|").replace("\n", " ")
        rows.append(
            f"| {item.get('severity', 'unknown')} | `{item.get('rule', 'unknown')}` | "
            f"`{file_path}` | {location} | {message} |"
        )
    return rows


def build_report(result: dict[str, Any] | None, error: str, args: argparse.Namespace) -> str:
    timestamp = datetime.now(timezone.utc).isoformat(timespec="seconds")
    lines = [
        "# Aislop Code Analysis Report",
        "",
        f"- Generated: `{timestamp}`",
        f"- Commit: `{os.getenv('GITHUB_SHA', 'local/unknown')}`",
        f"- Workflow run: `{os.getenv('GITHUB_RUN_ID', 'local')}`",
        f"- Aislop step outcome: `{args.scan_outcome}` (exit code: `{args.scan_exit}`)",
        "",
    ]
    if result is None:
        lines.extend([
            "## Report status",
            "",
            "<badge unavailable>",
            error,
            "",
            "The workflow preserves this report even when the scanner failed before producing JSON.",
        ])
        return "\n".join(lines) + "\n"

    summary = result.get("summary") or {}
    coverage = result.get("coverage") or {}
    diagnostics = result.get("diagnostics") or []
    if not isinstance(diagnostics, list):
        diagnostics = []
    lines.extend([
        "## Executive summary",
        "",
        f"- **Score:** {result.get('score', 'not scoreable')} ({result.get('label', 'unknown')})",
        f"- **Files scanned:** {summary.get('files', coverage.get('supportedFiles', 'unknown'))}",
        f"- **Errors:** {summary.get('errors', 0)}",
        f"- **Warnings:** {summary.get('warnings', len(diagnostics))}",
        f"- **Auto-fixable findings:** {summary.get('fixable', 0)}",
        f"- **Supported / unsupported files:** {coverage.get('supportedFiles', 'unknown')} / {coverage.get('unsupportedFiles', 'unknown')}",
        f"- **CLI version:** `{result.get('cliVersion', result.get('version', 'unknown'))}`",
        "",
        "Aislop warnings are deterministic static-analysis findings, not automatically confirmed runtime defects. Their assessment class and confidence are retained below; remediation priority should consider the affected code path and regression risk.",
        "",
        "## Engine results",
        "",
    ])
    lines.extend(format_engine_table(result.get("engines") or {}))
    by_rule, by_file, by_assessment = group_diagnostics(diagnostics)
    lines.extend(["", "## Assessment classification", "", "| Classification | Findings |", "|---|---:|"])
    for kind, count in sorted(by_assessment.items()):
        lines.append(f"| `{kind}` | {count} |")
    lines.extend(["", "## Findings by rule", "", "| Engine | Rule | Severity | Count |", "|---|---|---|---:|"])
    for (engine, rule, severity), count in sorted(by_rule.items()):
        lines.append(f"| `{engine}` | `{rule}` | {severity} | {count} |")
    lines.extend(["", "## Files with most findings", "", "| File | Errors | Warnings |", "|---|---:|---:|"])
    ranked_files = sorted(
        by_file.items(),
        key=lambda entry: (entry[1].get("error", 0), entry[1].get("warning", 0)),
        reverse=True,
    )
    for file_path, counts in ranked_files[:30]:
        lines.append(f"| `{file_path}` | {counts.get('error', 0)} | {counts.get('warning', 0)} |")
    lines.extend(["", "## Full diagnostic appendix", ""])
    lines.extend(format_diagnostic_appendix(diagnostics))
    lines.extend([
        "",
        "## Recommended remediation order",
        "",
        "1. Review security- or correctness-class diagnostics first; do not auto-fix them without tests.",
        "2. Refactor long functions around cohesive responsibilities and typed option/session structures; preserve existing behavior with focused regression tests.",
        "3. Apply mechanical formatting fixes in a dedicated change, then rerun Aislop and the complete project test suite.",
        "4. Review AI-slop indicators such as library panics and manual memory deletion in context; the rule classification is a review signal, not proof of a defect.",
        "",
        "## Reproducibility",
        "",
        "The raw `aislop.json` emitted by the same CI run is included beside this report artifact. This Markdown is generated from that JSON; no diagnostic counts are manually authored.",
    ])
    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--json", type=Path, required=True, dest="json_path")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--scan-outcome", default="unknown")
    parser.add_argument("--scan-exit", default="unknown")
    args = parser.parse_args()
    result, error = read_result(args.json_path)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(build_report(result, error, args), encoding="utf-8")
    print(f"wrote Aislop Markdown report: {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
