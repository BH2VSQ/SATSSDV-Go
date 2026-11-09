#!/usr/bin/env python3
"""Static checks for the cross-platform package and CI output contract."""
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
CMAKE = (ROOT / "CMakeLists.txt").read_text(encoding="utf-8")
ISS = ROOT / "packaging/windows/SATSSDV-Go.iss"
README = ROOT / "packaging/windows/PORTABLE-README.txt"
errors: list[str] = []

for artifact in (
    "SATSSDV-Go-linux-x86_64.tar.gz",
    "SATSSDV-Go-linux-x86_64.deb",
    "SATSSDV-Go-linux-x86_64.AppImage",
    "SATSSDV-Go-windows-x86_64-portable.zip",
    "SATSSDV-Go-windows-x86_64-Setup.exe",
):
    if artifact not in WORKFLOW:
        errors.append(f"CI workflow does not publish {artifact}")

if "CPACK_GENERATOR \"DEB\"" not in CMAKE:
    errors.append("CMake must retain Debian package generation support")
if not ISS.is_file():
    errors.append("Windows Inno Setup installer definition is missing")
else:
    iss = ISS.read_text(encoding="utf-8")
    for required in ("AppId=", "ArchitecturesAllowed=x64", "[Files]", "[Icons]", "{#SourceDir}"):
        if required not in iss:
            errors.append(f"Windows installer definition is missing {required}")
if not README.is_file():
    errors.append("Windows portable package README is missing")
if "aislop@0.20.0" not in WORKFLOW or "aislop ci" not in WORKFLOW:
    errors.append("CI workflow must run the pinned Aislop 0.20.0 quality gate")
if "SATSSDV-Go-report-input-aislop" not in WORKFLOW or "generate_aislop_report.py" not in WORKFLOW:
    errors.append("CI workflow must preserve raw Aislop JSON and generate its Markdown report")
if "qex-itu-channel-regression" not in WORKFLOW or "TestSatelliteChannelSensitivityCharacterization" not in WORKFLOW:
    errors.append("CI workflow must run the QEX/ITU production-path channel sweep")
if "SATSSDV-Go-report-input-qex" not in WORKFLOW or "generate_qex_report.py" not in WORKFLOW:
    errors.append("CI workflow must preserve the all-profile QEX/ITU Markdown, CSV, JSON and PNG report inputs")
if "SATSSDV-Go-Complete-CI-Reports" not in WORKFLOW or "package_ci_reports.py" not in WORKFLOW:
    errors.append("CI workflow must publish one unified ZIP containing Aislop, QEX/ITU and race reports")
if "race-regression:" in WORKFLOW:
    errors.append("The standalone race-regression job must be replaced by the QEX/ITU regression job")
if "SATSSDV-Go-report-input-qex" not in WORKFLOW or "go test -race ./internal/rx" not in WORKFLOW:
    errors.append("QEX/ITU regression must retain race detector evidence in its unified report input")
if "matrix.profile" in WORKFLOW or "SATSSDV_CHANNEL_PROFILE" in WORKFLOW:
    errors.append("QEX/ITU profile tests must run in one job, not a per-profile matrix")
if "SATSSDV_CHANNEL_CSV: ${{ github.workspace }}/reports/channel/channel-all-profiles.csv" not in WORKFLOW:
    errors.append("QEX/ITU tests must aggregate every profile into one measurement CSV")
if "go test -count=1 -timeout 100m -v ./internal/transport" not in WORKFLOW:
    errors.append("the unified all-profile QEX/ITU sweep must use its explicit 100-minute test timeout")
if "timeout-minutes: 120" not in WORKFLOW:
    errors.append("the unified QEX/ITU job must allow enough time for the single all-profile sweep and race tests")
for platform_job in ("qt-linux", "qt-windows", "qt-macos"):
    marker = f"  {platform_job}:\n"
    if marker not in WORKFLOW:
        errors.append(f"CI workflow is missing platform job {platform_job}")
        continue
    block_start = WORKFLOW.index(marker)
    next_match = re.search(r"\n  [a-z0-9_-]+:\n", WORKFLOW[block_start + len(marker):])
    block_end = len(WORKFLOW) if next_match is None else block_start + len(marker) + next_match.start()
    block = WORKFLOW[block_start:block_end]
    if "go test ./..." not in block or "go build -tags" not in block or "cmake --build" not in block:
        errors.append(f"{platform_job} must run Go tests, backend build and native Qt build in one job")
if "failOnMissingTools: true" not in (ROOT / ".aislop/config.yml").read_text(encoding="utf-8"):
    errors.append("aislop must fail when required analysis tools are missing")
if "*.go text eol=lf" not in (ROOT / ".gitattributes").read_text(encoding="utf-8"):
    errors.append("Go source files must use LF line endings for deterministic gofmt checks")

if errors:
    for error in errors:
        print(f"FAIL: {error}", file=sys.stderr)
    raise SystemExit(1)

print("PASS: Linux tar.gz/DEB/AppImage, Windows installer/portable, and aislop CI contracts")
