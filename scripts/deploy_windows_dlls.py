#!/usr/bin/env python3
"""Copy transitive MinGW DLL imports into the Windows application directory."""
from __future__ import annotations

import argparse
import os
import re
import shutil
import subprocess
import sys
from collections import deque
from pathlib import Path

DLL_RE = re.compile(r"^\s*DLL Name:\s*(\S+)\s*$", re.IGNORECASE | re.MULTILINE)
API_SET_PREFIXES = ("api-ms-win-", "ext-ms-win-")


def dll_imports(objdump: Path, binary: Path) -> list[str]:
    result = subprocess.run(
        [str(objdump), "-p", str(binary)],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    return DLL_RE.findall(result.stdout)


def find_named_file(directory: Path, filename: str) -> Path | None:
    direct = directory / filename
    if direct.is_file():
        return direct
    wanted = filename.casefold()
    try:
        for candidate in directory.iterdir():
            if candidate.is_file() and candidate.name.casefold() == wanted:
                return candidate
    except OSError:
        return None
    return None


def index_packaged_dlls(root: Path) -> dict[str, Path]:
    return {
        path.name.casefold(): path
        for path in root.rglob("*")
        if path.is_file() and path.suffix.casefold() == ".dll"
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--package-dir", required=True, type=Path)
    parser.add_argument("--mingw-bin", required=True, type=Path)
    parser.add_argument("--objdump", required=True, type=Path)
    args = parser.parse_args()

    package_dir = args.package_dir.resolve()
    mingw_bin = args.mingw_bin.resolve()
    objdump = args.objdump.resolve()
    if not package_dir.is_dir():
        parser.error(f"package directory does not exist: {package_dir}")
    if not mingw_bin.is_dir():
        parser.error(f"MinGW bin directory does not exist: {mingw_bin}")
    if not objdump.is_file():
        parser.error(f"objdump executable does not exist: {objdump}")

    windir = Path(os.environ.get("WINDIR", r"C:\Windows"))
    system_dirs = [windir / "System32", windir / "SysWOW64"]
    packaged = index_packaged_dlls(package_dir)
    queue: deque[Path] = deque(
        path for path in package_dir.rglob("*")
        if path.is_file() and path.suffix.casefold() in (".exe", ".dll")
    )
    visited: set[str] = set()
    copied: list[str] = []
    unresolved: list[tuple[Path, str]] = []

    while queue:
        binary = queue.popleft()
        try:
            identity = str(binary.resolve()).casefold()
        except OSError:
            identity = str(binary).casefold()
        if identity in visited:
            continue
        visited.add(identity)

        for dependency in dll_imports(objdump, binary):
            key = dependency.casefold()
            packaged_path = packaged.get(key)
            if packaged_path is not None:
                queue.append(packaged_path)
                continue

            source = find_named_file(mingw_bin, dependency)
            if source is not None:
                target = package_dir / source.name
                if not target.exists():
                    shutil.copy2(source, target)
                    copied.append(source.name)
                packaged[key] = target
                queue.append(target)
                continue

            if key.startswith(API_SET_PREFIXES):
                continue
            if any(find_named_file(system_dir, dependency) for system_dir in system_dirs):
                continue
            unresolved.append((binary, dependency))

    if unresolved:
        details = "\n".join(f"  {binary.name} -> {dependency}" for binary, dependency in unresolved)
        print("Unresolved non-system DLL imports remain:\n" + details, file=sys.stderr)
        return 1

    print(f"Scanned {len(visited)} PE files; copied {len(copied)} MinGW DLL dependencies.")
    if copied:
        print("Bundled: " + ", ".join(sorted(set(copied))))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
