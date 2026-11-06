#!/usr/bin/env python3
"""Verify the native release checksum and run its isolated, source-free demo."""

import argparse
import hashlib
import os
from pathlib import Path
import platform
import subprocess
import sys
import tarfile
import tempfile


def native_platform():
    operating_system = platform.system().lower()
    architecture = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(
        platform.machine().lower()
    )
    if operating_system not in ("linux", "darwin") or architecture is None:
        raise ValueError("release smoke supports Linux/macOS on amd64/arm64")
    return operating_system + "-" + architecture


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
    except (OSError, ValueError, tarfile.TarError, subprocess.SubprocessError) as error:
        parser.exit(1, f"release smoke failed: {error}\n")


if __name__ == "__main__":
    main()
