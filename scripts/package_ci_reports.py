#!/usr/bin/env python3
"""Create one downloadable archive containing every CI analysis report."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import zipfile
from datetime import datetime, timezone
from pathlib import Path


def copy_tree(source: Path, destination: Path) -> list[Path]:
    copied: list[Path] = []
    if not source.is_dir():
        return copied
    for item in sorted(source.rglob("*")):
        if not item.is_file():
            continue
        relative = item.relative_to(source)
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(item.read_bytes())
        copied.append(target)
    return copied


def write_status(root: Path) -> tuple[dict[str, str], Path]:
    status = {
        "aislop": os.getenv("CI_AISLOP_RESULT", "unknown"),
        "qex_itu_and_race": os.getenv("CI_QEX_RESULT", "unknown"),
        "linux_build": os.getenv("CI_LINUX_RESULT", "unknown"),
        "windows_build": os.getenv("CI_WINDOWS_RESULT", "unknown"),
        "macos_build_matrix": os.getenv("CI_MACOS_RESULT", "unknown"),
    }
    path = root / "CI-Job-Status.json"
    path.write_text(json.dumps(status, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    return status, path


def write_readme(root: Path, status: dict[str, str], copied: list[Path]) -> Path:
    lines = [
        "# SATSSDV-Go CI Technical Report Package",
        "",
        f"- Generated: `{datetime.now(timezone.utc).isoformat(timespec='seconds')}`",
        f"- Commit: `{os.getenv('GITHUB_SHA', 'local/unknown')}`",
        f"- Workflow run: `{os.getenv('GITHUB_RUN_ID', 'local')}`",
        "",
        "## Job outcomes",
        "",
        "| Job | Result |",
        "|---|---|",
    ]
    labels = {
        "aislop": "Aislop 0.20.0 quality gate",
        "qex_itu_and_race": "QEX / ITU all-profile sensitivity + race regression",
        "linux_build": "Linux Go + Qt + packages",
        "windows_build": "Windows Go + Qt + installer/portable",
        "macos_build_matrix": "macOS ARM64 + AMD64 Go + Qt builds",
    }
    for key, value in status.items():
        lines.append(f"| {labels[key]} | `{value}` |")
    lines.extend([
        "",
        "## Contents",
        "",
        "- `Aislop/`: raw scanner JSON, stderr/exit state and generated Markdown analysis.",
        "- `QEX-ITU/`: all-profile channel CSV, per-run log, Markdown/JSON summaries, PNG curves and race-detector evidence.",
        "- `CI-Job-Status.json`: workflow job results for this exact run.",
        "",
        "## Interpretation",
        "",
        "A missing report file means the producing step did not create it; check the corresponding job result and log. A failed job is preserved in this package rather than being omitted. QEX/ITU simulations are software channel-model tests through the production stream encoder/decoder; they are not calibrated RF sensitivity measurements.",
        "",
        "## File checksums",
        "",
    ])
    for item in sorted(copied):
        digest = hashlib.sha256(item.read_bytes()).hexdigest()
        lines.append(f"- `{item.relative_to(root).as_posix()}` — `{digest}`")
    path = root / "README.md"
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return path


def build_package(input_root: Path, output: Path) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    staging = output.parent / f".{output.stem}-staging"
    if staging.exists():
        for item in sorted(staging.rglob("*"), reverse=True):
            if item.is_file() or item.is_symlink():
                item.unlink()
            elif item.is_dir():
                item.rmdir()
        staging.rmdir()
    staging.mkdir(parents=True)

    copied = []
    copied.extend(copy_tree(input_root / "aislop", staging / "Aislop"))
    copied.extend(copy_tree(input_root / "qex", staging / "QEX-ITU"))
    status, status_path = write_status(staging)
    copied.append(status_path)
    readme = write_readme(staging, status, copied)
    copied.append(readme)

    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for item in sorted(staging.rglob("*")):
            if item.is_file():
                archive.write(item, item.relative_to(staging).as_posix())

    if not zipfile.is_zipfile(output):
        raise RuntimeError(f"report package is not a valid ZIP archive: {output}")
    with zipfile.ZipFile(output) as archive:
        bad = archive.testzip()
        if bad:
            raise RuntimeError(f"report package contains a corrupt member: {bad}")
    print(f"created unified CI report package: {output}")
    print(f"included {len(copied)} report and manifest files")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input-root", type=Path, default=Path("report-input"))
    parser.add_argument("--output", type=Path, default=Path("SATSSDV-Go-CI-Reports.zip"))
    args = parser.parse_args()
    build_package(args.input_root, args.output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
