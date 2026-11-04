# GitHub Actions: Qt 6 Native Build Matrix

This workflow builds the Qt 6 desktop client and the Go backend on native runners.

## Windows: Go executable path

`actions/setup-go` adds the Go installation to the Windows runner PATH. The Qt build itself runs inside an MSYS2 MINGW64 shell. MSYS2 does not reliably inherit PATH modifications made by previous Windows actions, so `find_program(go)` can fail even though `go version` works in PowerShell.

The workflow now resolves `go.exe` in PowerShell, writes its absolute path to `GITHUB_ENV`, converts it to an MSYS2 path for a preflight check, and passes the original Windows path to CMake as `-DGO_EXECUTABLE=...`. CMake accepts an explicit Go path and reports it during configuration.

The Windows job also checks that `windeployqt` has copied the Qt runtime DLLs before uploading the build artifact.

## Linux

The Ubuntu 24.04 job installs Qt 6 Widgets/Network, CMake/Ninja and ALSA development headers, configures with the explicit `go` path, builds the Qt app and Go audio backend, verifies their dynamic dependencies, then uploads a tarball.

## macOS: two native architectures

The workflow uses separate native GitHub-hosted runners rather than cross-compiling Go/cgo:

| Artifact | Runner | Go target | CMake architecture |
|---|---|---|---|
| Apple Silicon (M-series) | `macos-26` | `darwin/arm64` | `arm64` |
| Intel Mac | `macos-26-intel` | `darwin/amd64` | `x86_64` |

Each job checks `uname -m` and `go env GOARCH`, passes `Qt6_DIR` explicitly to CMake, verifies both the Qt executable and Go backend with `lipo -verify_arch`, runs `macdeployqt`, and uploads a separate `.app` ZIP.

## Local reproduction

Linux/macOS:

```sh
cmake -S . -B build-qt6 -DCMAKE_BUILD_TYPE=Release \
  -DSATSSDV_WITH_MALGO=ON -DGO_EXECUTABLE="$(command -v go)"
cmake --build build-qt6 --parallel 2 --verbose
```

Windows PowerShell can locate the toolchain with:

```powershell
(Get-Command go.exe).Source
```

When invoking MinGW CMake from MSYS2, pass that absolute path with forward slashes:

```sh
cmake -S . -B build-qt6 -G Ninja \
  -DSATSSDV_WITH_MALGO=ON \
  -DGO_EXECUTABLE='D:/hostedtoolcache/windows/go/1.26.0/x64/bin/go.exe'
```

Replace the example path with the value resolved by `setup-go`; do not hard-code the example runner path.
