#!/usr/bin/env python3
"""Assemble a deterministic release from tracked public assets and built binaries."""

import argparse
import csv
import gzip
import hashlib
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile


PUBLIC_PATHS = (
    "README.md", "CONTRIBUTING.md", "LICENSE*", "NOTICE*", "SECURITY.md",
    "CODE_OF_CONDUCT.md", "MAINTAINERS.md", "third_party", "docs", "examples", "integrations",
    "scenarios",
    ".agents/skills/open-guardrail-demo", "cmd/guardrailctl/README.md",
    "api/openapi/openapi.yaml", "deploy/config.example.yaml",
)
PUBLIC_REQUIRED = ("LICENSE", "NOTICE", "SECURITY.md", "CODE_OF_CONDUCT.md",
                   "third_party/go-modules.tsv", "third_party/notices.tsv", "third_party/README.md")
BINARIES = ("open-guardrail", "guardrailctl")


def tracked_assets(root, public=False):
    result = subprocess.run(
        ["git", "ls-files", "--cached", "-z", "--", *PUBLIC_PATHS],
        cwd=root, capture_output=True, check=True,
    )
    assets = {}
    for raw in result.stdout.split(b"\0"):
        if not raw:
            continue
        name = os.fsdecode(raw)
        relative = Path(name)
        if relative.is_absolute() or ".." in relative.parts:
            raise ValueError("release asset path must stay inside the checkout")
        path = root / relative
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"release asset must be a regular file: {name}")
        # A symlinked parent must not import files from outside the checkout.
        if path.resolve() != path.absolute():
            raise ValueError(f"release asset cannot use symlinked directories: {name}")
        assets[name] = path
    for required in ("README.md", "CONTRIBUTING.md", "deploy/config.example.yaml"):
        if required not in assets:
            raise ValueError(f"required release asset is not tracked: {required}")
    if public:
        for name in PUBLIC_REQUIRED:
            if name not in assets or assets[name].stat().st_size == 0:
                raise ValueError(f"public release asset must be tracked and nonempty: {name}")
        # An index alone is insufficient: every original notice it references
        # must be tracked, unchanged and included in the same archive.
        with assets["third_party/notices.tsv"].open(encoding="utf-8") as source:
            reader = csv.DictReader(source, delimiter="|")
            if not {"path", "sha256"}.issubset(reader.fieldnames or []):
                raise ValueError("public notice inventory requires path and sha256 columns")
            notices = set()
            for row in reader:
                name = row["path"]
                if name in notices or not name.startswith("third_party/licenses/"):
                    raise ValueError("public notice inventory has an invalid or duplicate path")
                if name not in assets or assets[name].stat().st_size == 0:
                    raise ValueError(f"public notice file must be tracked and nonempty: {name}")
                if hashlib.sha256(assets[name].read_bytes()).hexdigest() != row["sha256"]:
                    raise ValueError(f"public notice file checksum differs: {name}")
                notices.add(name)
            if not notices:
                raise ValueError("public notice inventory must contain original legal files")
    return assets


def assemble(root, binaries, destination, epoch, public=False):
    if not 0 <= epoch <= 0xFFFFFFFF:
        raise ValueError("SOURCE_DATE_EPOCH must be an integer from 0 to 4294967295")
    assets = tracked_assets(root, public)
    for name in BINARIES:
        path = binaries / name
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"missing regular release binary: {name}")
        assets[name] = path
    directories = {"."}
    for name in assets:
        directories.update(str(parent) for parent in Path(name).parents)
    destination.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(dir=destination.parent, prefix=".release-")
    try:
        with os.fdopen(descriptor, "wb") as output:
            with gzip.GzipFile(filename="", mode="wb", fileobj=output, mtime=epoch) as compressed:
                with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as archive:
                    for name in sorted(directories | assets.keys()):
                        item = tarfile.TarInfo("." if name == "." else "./" + name)
                        item.mtime = epoch
                        item.uid = item.gid = 0
                        item.uname = item.gname = ""
                        if name in directories:
                            item.type = tarfile.DIRTYPE
                            item.mode = 0o755
                            archive.addfile(item)
                        else:
                            item.mode = 0o755 if name in BINARIES else 0o644
                            with assets[name].open("rb") as source:
                                item.size = os.fstat(source.fileno()).st_size
                                archive.addfile(item, source)
        if public:
            with tarfile.open(temporary, "r:gz") as archive:
                names = {member.name.removeprefix("./") for member in archive if member.isfile()}
                if not set(PUBLIC_REQUIRED).issubset(names):
                    raise ValueError("public archive is missing required release assets")
        os.chmod(temporary, 0o644)
        os.replace(temporary, destination)
    finally:
        Path(temporary).unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check-public-assets", action="store_true")
    parser.add_argument("--public", action="store_true")
    parser.add_argument("--binaries", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--epoch", type=int)
    args = parser.parse_args()
    try:
        if args.check_public_assets:
            tracked_assets(Path.cwd().resolve(), public=True)
            return
        if args.binaries is None or args.output is None or args.epoch is None:
            parser.error("assembly requires --binaries, --output and --epoch")
        assemble(Path.cwd().resolve(), args.binaries, args.output, args.epoch, args.public)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"release assembly failed: {error}\n")


if __name__ == "__main__":
    main()
