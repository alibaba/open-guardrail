#!/usr/bin/env python3
"""Install a verified native/plugin pair from a downloaded release directory."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import shlex
import shutil
import tarfile


def unpack(archive, destination, prefix=''):
    with tarfile.open(archive, 'r:gz') as package:
        members = package.getmembers()
        if sum(member.size for member in members) > 1024 ** 3:
            raise ValueError('archive exceeds the installation size limit')
        for member in members:
            relative = PurePosixPath(member.name)
            if relative.is_absolute() or '..' in relative.parts or not (member.isfile() or member.isdir()):
                raise ValueError('archive contains an unsafe entry')
            if prefix:
                if not relative.parts or relative.parts[0] != prefix:
                    raise ValueError('archive has an unexpected package root')
                relative = PurePosixPath(*relative.parts[1:])
            target = destination.joinpath(*relative.parts)
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with package.extractfile(member) as source, target.open('xb') as output:
                    shutil.copyfileobj(source, output)
                target.chmod(0o755 if member.mode & 0o111 else 0o644)


def install(assets, prefix, version):
    manifest = json.loads((assets / 'plugin-release.json').read_text())
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?', version):
        raise ValueError('use a fixed semantic version')
    if manifest['version'] != version:
        raise ValueError('manifest does not match the requested version')
    architecture = {'x86_64': 'amd64', 'amd64': 'amd64', 'arm64': 'arm64', 'aarch64': 'arm64'}.get(platform.machine().lower())
    operating_system = platform.system().lower()
    if operating_system not in ('linux', 'darwin') or architecture is None:
        raise ValueError('supported platforms are Linux/macOS on amd64/arm64')
    selected = []
    for key in (operating_system + '-' + architecture, 'plugin'):
        entry = manifest['assets'][key]
        name = entry['file']
        if Path(name).name != name or name in ('', '.', '..'):
            raise ValueError('manifest asset must be a filename')
        archive = assets / name
        with archive.open('rb') as source:
            digest = hashlib.sha256()
            for chunk in iter(lambda: source.read(1024 * 1024), b''):
                digest.update(chunk)
        if digest.hexdigest() != entry['sha256']:
            raise ValueError('asset checksum mismatch for ' + name)
        selected.append(archive)
    prefix.mkdir(parents=True, exist_ok=True)
    target = prefix / version
    target.mkdir(mode=0o700)  # Exclusive creation also protects concurrent installs.
    try:
        unpack(selected[0], target / 'native')
        unpack(selected[1], target / 'plugin', 'package')
        metadata = json.loads((target / 'plugin/package.json').read_text())
        if metadata['version'] != version:
            raise ValueError('plugin version does not match the requested version')
        for name in ('open-guardrail', 'guardrailctl'):
            if not os.access(target / 'native' / name, os.X_OK):
                raise ValueError('native package is missing an executable')
        (target / 'env.sh').write_text('export OPEN_GUARDRAIL_BINARIES=' + shlex.quote(str(target / 'native')) + '\n')
        (target / 'installation.json').write_text(json.dumps(manifest, indent=2) + '\n')
    except BaseException:
        shutil.rmtree(target)
        raise
    return target


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--assets-dir', type=Path, required=True)
    parser.add_argument('--prefix', type=Path, required=True)
    parser.add_argument('--version', required=True)
    args = parser.parse_args()
    try:
        target = install(args.assets_dir.resolve(), args.prefix.expanduser().resolve(), args.version)
    except (OSError, ValueError, KeyError, tarfile.TarError):
        parser.exit(1, 'Installation failed. Check the version, checksums, archive contents and unused destination.\n')
    print('Installed ' + str(target))
    print('source ' + shlex.quote(str(target / 'env.sh')))
    print('node ' + shlex.quote(str(target / 'plugin/doctor.mjs')) + ' --live')


if __name__ == '__main__':
    main()
