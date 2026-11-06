#!/usr/bin/env python3
"""Collect pinned upstream legal files, or verify the committed notice bundle."""

import argparse
import csv
import hashlib
from pathlib import Path
import shutil
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parents[1]
FIELDS = ("module", "version", "scopes", "source", "path", "sha256")
PREFIXES = ("LICENSE", "LICENCE", "NOTICE", "COPYING", "COPYRIGHT", "AUTHORS", "PATENTS")


def escaped_module(module):
    return "".join("!" + char.lower() if char.isupper() else char for char in module)


def modules(root):
    result = {}
    with (root / "third_party/go-modules.tsv").open(encoding="utf-8") as source:
        for row in csv.DictReader(source, delimiter="|"):
            key = row["module"], row["version"]
            if not all(key) or any(".." in Path(value).parts or Path(value).is_absolute() for value in key):
                raise ValueError("invalid module or version in dependency inventory")
            result.setdefault(key, set()).add(row["scope"])
    version = next(line.split()[1] for line in (root / "go.mod").read_text().splitlines() if line.startswith("go "))
    result[("go", version)] = {"toolchain"}
    return result


def source_url(module, version):
    if module == "go":
        return f"https://go.dev/dl/go{version}.src.tar.gz"
    return f"https://proxy.golang.org/{escaped_module(module)}/@v/{version}.zip"


def legal_name(name):
    return name.upper().startswith(PREFIXES)


def collect(root, module_cache, go_root):
    third_party = root / "third_party"
    destination = third_party / "licenses"
    if destination.is_symlink():
        raise ValueError("notice directory must not be a symlink")
    rows = []
    with tempfile.TemporaryDirectory(dir=third_party, prefix=".notices-") as temporary:
        staged = Path(temporary) / "licenses"
        for (module, version), scopes in sorted(modules(root).items()):
            origin = go_root if module == "go" else module_cache / f"{escaped_module(module)}@{version}"
            if module == "go" and origin.joinpath("VERSION").read_text().splitlines()[0] != "go" + version:
                raise ValueError("Go source version differs from go.mod")
            sources = sorted(path for path in origin.iterdir()
                             if legal_name(path.name) and (path.is_file() or path.is_symlink()))
            if not any(path.name.upper().startswith(("LICENSE", "LICENCE", "COPYING")) for path in sources):
                raise ValueError(f"upstream license file missing: {module}@{version}")
            for path in sources:
                if path.is_symlink() or not path.is_file():
                    raise ValueError(f"upstream legal file must be regular: {module}@{version}/{path.name}")
                relative = Path(f"{module}@{version}") / path.name
                target = staged / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                data = path.read_bytes()
                if not data:
                    raise ValueError(f"upstream legal file is empty: {module}@{version}/{path.name}")
                target.write_bytes(data)
                rows.append({"module": module, "version": version, "scopes": ",".join(sorted(scopes)),
                             "source": source_url(module, version),
                             "path": (Path("third_party/licenses") / relative).as_posix(),
                             "sha256": hashlib.sha256(data).hexdigest()})
        # Replace only this script's generated legal-file directory after every
        # pinned source has been found and read successfully.
        if destination.exists():
            shutil.rmtree(destination)
        shutil.move(str(staged), destination)
        with (third_party / "notices.tsv").open("w", encoding="utf-8", newline="") as output:
            writer = csv.DictWriter(output, fieldnames=FIELDS, delimiter="|", lineterminator="\n")
            writer.writeheader()
            writer.writerows(rows)


def check(root):
    expected = modules(root)
    seen_modules, seen_paths = set(), set()
    licensed_modules = set()
    with (root / "third_party/notices.tsv").open(encoding="utf-8") as source:
        reader = csv.DictReader(source, delimiter="|")
        if tuple(reader.fieldnames or ()) != FIELDS:
            raise ValueError("invalid notice inventory header")
        for row in reader:
            key = row["module"], row["version"]
            if key not in expected or row["scopes"] != ",".join(sorted(expected[key])):
                raise ValueError("notice inventory differs from dependency versions or scopes")
            relative = Path(row["path"])
            prefix = Path("third_party/licenses") / f"{key[0]}@{key[1]}"
            if relative.parent != prefix or not legal_name(relative.name) or row["source"] != source_url(*key):
                raise ValueError("invalid legal-file path or source location")
            path = root / relative
            if row["path"] in seen_paths or path.is_symlink() or not path.is_file() or path.resolve() != path.absolute():
                raise ValueError(f"duplicate, missing or non-regular legal file: {row['path']}")
            data = path.read_bytes()
            if not data or hashlib.sha256(data).hexdigest() != row["sha256"]:
                raise ValueError(f"legal-file checksum differs: {row['path']}")
            seen_paths.add(row["path"])
            seen_modules.add(key)
            if relative.name.upper().startswith(("LICENSE", "LICENCE", "COPYING")):
                licensed_modules.add(key)
    if licensed_modules != set(expected) or seen_modules != set(expected):
        raise ValueError("notice bundle does not cover every pinned module and the Go toolchain")
    actual = {path.relative_to(root).as_posix() for path in (root / "third_party/licenses").rglob("*") if path.is_file() or path.is_symlink()}
    if actual != seen_paths:
        raise ValueError("notice directory contains files outside the inventory")
    print(f"Dependency notices verified: {len(expected)} source versions, {len(seen_paths)} original legal files")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--update", action="store_true", help="replace the generated bundle from pinned local sources")
    parser.add_argument("--module-cache", type=Path)
    parser.add_argument("--go-root", type=Path)
    args = parser.parse_args()
    try:
        if args.update:
            cache = args.module_cache or Path(subprocess.check_output(["go", "env", "GOMODCACHE"], text=True).strip())
            go_root = args.go_root or Path(subprocess.check_output(["go", "env", "GOROOT"], text=True).strip())
            collect(ROOT, cache, go_root)
        check(ROOT)
    except (OSError, ValueError, KeyError, StopIteration, subprocess.CalledProcessError) as error:
        parser.exit(1, f"dependency notices: {error}\n")


if __name__ == "__main__":
    main()
