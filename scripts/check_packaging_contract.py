#!/usr/bin/env python3
"""Static checks for the cross-platform package and CI output contract."""
from pathlib import Path
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
if "scanaislop/aislop@v0.20.0" not in WORKFLOW:
    errors.append("CI workflow must run the pinned aislop quality gate")
if "failOnMissingTools: true" not in (ROOT / ".aislop/config.yml").read_text(encoding="utf-8"):
    errors.append("aislop must fail when required analysis tools are missing")
if "*.go text eol=lf" not in (ROOT / ".gitattributes").read_text(encoding="utf-8"):
    errors.append("Go source files must use LF line endings for deterministic gofmt checks")

if errors:
    for error in errors:
        print(f"FAIL: {error}", file=sys.stderr)
    raise SystemExit(1)

print("PASS: Linux tar.gz/DEB/AppImage, Windows installer/portable, and aislop CI contracts")
