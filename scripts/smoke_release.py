#!/usr/bin/env python3
"""Verify the native release checksum and run its isolated, source-free demo."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import signal
import socket
import subprocess
import sys
import tarfile
import tempfile
import time
from urllib.parse import urlsplit
from urllib.request import build_opener, ProxyHandler


def native_platform():
    operating_system = platform.system().lower()
    architecture = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(
        platform.machine().lower()
    )
    if operating_system not in ("linux", "darwin") or architecture is None:
        raise ValueError("release smoke supports Linux/macOS on amd64/arm64")
    return operating_system + "-" + architecture


def smoke_cleanup(extracted, temporary, environment, python=None):
    """Start the documented archive entry point, probe it, then stop it cleanly."""
    opener = build_opener(ProxyHandler({}))
    log = temporary / "cleanup-startup.log"
    with log.open("w") as output:
        process = subprocess.Popen(
            [python or sys.executable, str(extracted / "examples/cleanup-demo/demo.py"),
             "--password-stdin", "--port", "0"],
            cwd=temporary, env=environment, stdin=subprocess.PIPE,
            stdout=output, stderr=subprocess.STDOUT, text=True, start_new_session=True,
        )
    ready = False
    try:
        process.stdin.write(secrets.token_urlsafe(24) + "\n")
        process.stdin.close()
        deadline = time.monotonic() + 60
        while True:
            match = re.search(r"^Demo: (http://127\.0\.0\.1:\d+/)$", log.read_text(), re.MULTILINE)
            if match:
                demo_url = match.group(1)
                break
            if process.poll() is not None:
                raise RuntimeError(f"cleanup demo exited before readiness (exit {process.returncode})")
            if time.monotonic() >= deadline:
                raise RuntimeError("cleanup demo did not become ready within 60 seconds")
            time.sleep(.05)
        with opener.open(demo_url, timeout=5) as response:
            if b'task-form' not in response.read():
                raise ValueError("cleanup demo page did not load")
        with opener.open(demo_url + "api/state", timeout=5) as response:
            state = json.load(response)
        if (state["busy"] or state["jobs"] or not state["workspaceId"]
            or {item["name"] for item in state["files"]} != {"cache.tmp", "download.tmp", "orders.csv", "config.yaml"}):
            raise ValueError("cleanup demo did not expose the expected initial state")
        console = urlsplit(state["consoleUrl"])
        if (console.scheme != "http" or console.hostname != "127.0.0.1" or not console.port
            or console.username or console.password or console.path or console.query or console.fragment):
            raise ValueError("cleanup demo returned an unexpected local console URL")
        with opener.open(state["consoleUrl"] + "/readyz", timeout=5) as response:
            if response.status != 200:
                raise ValueError("cleanup demo service is not ready")
        ready = True
    finally:
        if process.poll() is None:
            process.send_signal(signal.SIGINT)
            try:
                process.wait(timeout=50)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
                raise RuntimeError("cleanup demo did not shut down within its cleanup deadline")
        if ready and process.returncode != 0:
            raise RuntimeError(f"cleanup demo did not stop cleanly (exit {process.returncode})")
    # A successful shutdown must stop its private service as well as the demo.
    for url in (demo_url, state["consoleUrl"] + "/readyz"):
        address = urlsplit(url)
        with socket.socket() as connection:
            connection.settimeout(.5)
            if connection.connect_ex((address.hostname, address.port)) == 0:
                raise RuntimeError("cleanup demo left a local service running after shutdown")


def smoke(archive):
    checksum = Path(str(archive) + ".sha256").read_text(encoding="utf-8").split()
    if len(checksum) != 2 or checksum[1] != archive.name:
        raise ValueError("release checksum sidecar must identify this archive")
    with archive.open("rb") as source:
        digest = hashlib.file_digest(source, "sha256").hexdigest()
    if checksum[0] != digest:
        raise ValueError("release checksum does not match archive")
    with tempfile.TemporaryDirectory(prefix="open-guardrail-release-") as temporary:
        root = Path(temporary)
        extracted = root / "release"
        extracted.mkdir()
        with tarfile.open(archive, "r:gz") as package:
            package.extractall(extracted, filter="data")
        for name in ("open-guardrail", "guardrailctl", "examples/agent-demo/smoke.py",
                     "examples/cleanup-demo/demo.py", "examples/cleanup-demo/policy.yaml",
                     "examples/cleanup-demo/index.html",
                     "examples/policies/recovery.yaml", "integrations/jenkins/agent_client.py",
                     ".agents/skills/open-guardrail-demo/SKILL.md"):
            if not (extracted / name).is_file():
                raise ValueError(f"missing release asset: {name}")
        if (extracted / "go.mod").exists():
            raise ValueError("release smoke must run without the source module")
        empty_path = root / "empty-path"
        empty_path.mkdir()
        environment = {key: value for key, value in os.environ.items()
                       if key not in ("PYTHONPATH", "PYTHONHOME")}
        environment.update(PATH=str(empty_path), PYTHONDONTWRITEBYTECODE="1")
        # sys.executable and both product commands are absolute paths. Go/Node
        # are deliberately unavailable, so a source-build fallback cannot pass.
        subprocess.run([sys.executable, str(extracted / "examples/agent-demo/smoke.py")],
                       cwd=root, env=environment, check=True, timeout=180)
        smoke_cleanup(extracted, root, environment)
    print(f"release smoke passed without Go/Node: {archive.name}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
    if not args.version or any(c not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"
                               for c in args.version):
        parser.error("invalid release version")
    try:
        archive = Path("dist") / f"open-guardrail-{args.version}-{native_platform()}.tar.gz"
        smoke(archive)
    except (OSError, ValueError, RuntimeError, tarfile.TarError, subprocess.SubprocessError) as error:
        parser.exit(1, f"release smoke failed: {error}\n")


if __name__ == "__main__":
    main()
