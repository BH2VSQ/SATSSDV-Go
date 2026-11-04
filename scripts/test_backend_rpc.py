#!/usr/bin/env python3
"""Build and exercise the native Go backend's loopback RPC lifecycle."""
from __future__ import annotations

import json
import os
import queue
import threading
from pathlib import Path
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
LOCAL_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def request(address: str, path: str, method: str = "GET", body=None, token: str | None = None):
    headers: dict[str, str] = {}
    if token:
        headers["X-SATSSDV-Token"] = token
    data = None
    if body is not None:
        data = json.dumps(body, separators=(",", ":")).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(address + path, data=data, headers=headers, method=method)
    try:
        with LOCAL_OPENER.open(req, timeout=10) as response:
            return response.status, json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        return exc.code, json.loads(exc.read().decode("utf-8"))


def main() -> int:
    with tempfile.TemporaryDirectory(prefix="satssdv-rpc-test-") as temporary:
        executable = Path(temporary) / ("satssdv-backend.exe" if os.name == "nt" else "satssdv-backend")
        env = os.environ.copy()
        env["CGO_ENABLED"] = "0"
        subprocess.run(
            ["go", "build", "-trimpath", "-tags", "", "-o", str(executable), "."],
            cwd=ROOT,
            env=env,
            check=True,
        )

        process = subprocess.Popen(
            [str(executable)], cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding="utf-8", bufsize=1,
        )
        try:
            assert process.stdout is not None
            handshake_queue: queue.Queue[str] = queue.Queue(maxsize=1)
            reader = threading.Thread(
                target=lambda: handshake_queue.put(process.stdout.readline()), daemon=True
            )
            reader.start()
            try:
                handshake_line = handshake_queue.get(timeout=15)
            except queue.Empty as exc:
                raise TimeoutError("timed out waiting for the backend handshake") from exc
            if not handshake_line:
                stderr = process.stderr.read() if process.stderr else ""
                raise RuntimeError(f"backend exited before handshake: {stderr}")
            handshake = json.loads(handshake_line)
            address = handshake["address"]
            token = handshake["token"]
            assert address.startswith("http://127.0.0.1:"), address
            assert len(token) == 64

            status, health = request(address, "/health")
            assert status == 200 and health.get("ok") is True

            status, _ = request(address, "/rpc", "POST", {"method": "GetProfiles", "args": []})
            assert status == 401, f"unauthenticated RPC returned HTTP {status}"

            status, result = request(
                address, "/rpc", "POST", {"method": "GetProfiles", "args": []}, token
            )
            assert status == 200 and result.get("ok") is True, result
            profile_ids = {profile.get("id") for profile in result.get("data", [])}
            expected = {"ssdv-qpsk-1600", "ssdv-8psk-1600", "ssdv-16qam-1600"}
            assert expected.issubset(profile_ids), sorted(profile_ids)

            status, result = request(
                address, "/rpc", "POST", {"method": "TransmitPCM", "args": []}, token
            )
            assert status == 200 and result.get("ok") is False
            assert "not exposed" in result.get("error", "")

            status, result = request(address, "/shutdown", "POST", {}, token)
            assert status == 200 and result.get("ok") is True
            exit_code = process.wait(timeout=10)
            assert exit_code == 0, f"backend exited with {exit_code}"
            print("PASS: backend handshake, loopback binding, health, token auth, M-ary profiles, RPC allowlist and shutdown")
            return 0
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=3)


if __name__ == "__main__":
    raise SystemExit(main())
