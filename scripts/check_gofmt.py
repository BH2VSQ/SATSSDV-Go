#!/usr/bin/env python3
"""Fail when an active Go source file is not canonical gofmt output."""
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
EXCLUDED = {".git", ".wails-parity-backup", "reference_implementation", "vendor"}
files = sorted(
    path for path in ROOT.rglob("*.go")
    if not any(part in EXCLUDED for part in path.relative_to(ROOT).parts)
    and not any(part.startswith("build") for part in path.relative_to(ROOT).parts)
)
if not files:
    print("FAIL: no Go source files found", file=sys.stderr)
    raise SystemExit(1)
result = subprocess.run(
    ["gofmt", "-l", *(str(path) for path in files)],
    cwd=ROOT,
    check=False,
    capture_output=True,
    text=True,
)
if result.returncode != 0:
    print(result.stderr, file=sys.stderr)
    raise SystemExit(result.returncode)
unformatted = [line for line in result.stdout.splitlines() if line.strip()]
if unformatted:
    print("Go files need gofmt:", file=sys.stderr)
    print("\n".join(unformatted), file=sys.stderr)
    raise SystemExit(1)
print(f"PASS: {len(files)} Go source files are gofmt-clean")
