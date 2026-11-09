# Release packaging

GitHub Actions produces the following native desktop packages from the same Qt 6
and Go backend build:

| Platform | Artifact | Contents |
|---|---|---|
| Linux x86_64 | `SATSSDV-Go-linux-x86_64.AppImage` | AppImage with Qt runtime deployment |
| Linux x86_64 | `SATSSDV-Go-linux-x86_64.deb` | Debian package with shared-library dependencies |
| Linux x86_64 | `SATSSDV-Go-linux-x86_64.tar.gz` | Installed filesystem tree (`bin/` and desktop/icon integration) |
| Windows x86_64 | `SATSSDV-Go-windows-x86_64-Setup.exe` | Inno Setup installer, Start Menu shortcut, optional desktop shortcut and uninstaller |
| Windows x86_64 | `SATSSDV-Go-windows-x86_64-portable.zip` | Self-contained directory with Qt/MinGW runtime DLLs, plugins and Go backend |

Linux artifacts include `SATSSDV-Go-linux-x86_64.sha256` checksums. The Linux
AppImage and tarball are x86_64 builds; the tarball uses system Qt libraries,
whereas the AppImage bundles the Qt deployment selected by linuxdeploy.

The Windows installer is defined in `packaging/windows/SATSSDV-Go.iss`. The
portable package is staged from `build-qt6/bin` after `windeployqt` and the
transitive MinGW DLL deployment script have completed. Do not publish the raw
`build-qt6/bin` folder as the portable package without those deployment steps.

## Local Windows installer build

After building and deploying the Windows directory with the same MinGW/Qt
configuration as CI, run from the repository root:

```powershell
$portable = (Resolve-Path "build-qt6/bin").Path
New-Item -ItemType Directory -Force "dist/windows/portable" | Out-Null
Copy-Item "$portable/*" "dist/windows/portable" -Recurse -Force
Copy-Item "packaging/windows/PORTABLE-README.txt" "dist/windows/portable"
$portable = (Resolve-Path "dist/windows/portable").Path
Compress-Archive "$portable/*" "SATSSDV-Go-windows-x86_64-portable.zip" -Force
& "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe" "/DSourceDir=$portable" "packaging/windows/SATSSDV-Go.iss"
```

## Code-quality gate

CI pins `scanaislop/aislop@v0.20.0` and runs its deterministic `aislop ci` gate.
The project policy is in `.aislop/config.yml`; required external tools are
installed in the quality job, and `failOnMissingTools` prevents a partial scan
from being treated as a pass. Run locally with:

```bash
npx --yes aislop@0.20.0 ci
```

The C/C++ scan uses the CMake compilation database produced in CI. The Go and
Python formatting/lint engines are included in the same quality gate.
