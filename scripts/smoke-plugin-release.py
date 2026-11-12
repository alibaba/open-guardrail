#!/usr/bin/env python3
"""Install release assets outside the checkout and verify the packaged entry points."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile


TARGETS = ('linux-amd64', 'linux-arm64', 'darwin-amd64', 'darwin-arm64')


def prepare_assets(assets, version, destination, *, public=False):
    """Validate and open public assets only when explicitly requested."""
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?', version):
        raise ValueError('use a fixed semantic version')
    if not public:
        return assets
    native = {'open-guardrail-' + version + '-' + target + '.tar.gz' for target in TARGETS}
    bundle_name = 'openguardrail-agent-plugins-preview-' + version + '-install.tar.gz'
    payloads = native | {bundle_name}
    if {p.name for p in assets.iterdir()} != payloads | {'SHA256SUMS'}:
        raise ValueError('public release must contain exactly the six user assets')
    if any(p.is_symlink() or not p.is_file() for p in assets.iterdir()):
        raise ValueError('public assets must be regular files')
    checksums = {}
    for line in (assets / 'SHA256SUMS').read_text().splitlines():
        fields = line.split()
        if len(fields) != 2:
            raise ValueError('invalid or duplicate public checksum')
        checksum, name = fields
        if name in checksums or not re.fullmatch(r'[0-9a-f]{64}', checksum):
            raise ValueError('invalid or duplicate public checksum')
        checksums[name] = checksum
    if set(checksums) != payloads:
        raise ValueError('checksum manifest must cover the five release archives')
    for name, checksum in checksums.items():
        with (assets / name).open('rb') as source:
            if hashlib.file_digest(source, 'sha256').hexdigest() != checksum:
                raise ValueError('public archive checksum mismatch: ' + name)
    plugin_name = 'openguardrail-agent-plugins-preview-' + version + '.tgz'
    expected = {'install.py', 'plugin-release.json', plugin_name}
    with tarfile.open(assets / bundle_name, 'r:gz') as archive:
        members = archive.getmembers()
        if ({member.name for member in members} != expected or len(members) != len(expected)
                or any(not member.isfile() for member in members)
                or sum(member.size for member in members) > 64 * 1024 * 1024):
            raise ValueError('plugin installation bundle has unsafe or unexpected entries')
        destination.mkdir()
        for member in members:
            with archive.extractfile(member) as source, (destination / member.name).open('xb') as output:
                shutil.copyfileobj(source, output)
    for name in native:
        shutil.copy2(assets / name, destination / name)
    return destination


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--assets-dir', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--public', action='store_true',
                        help='require exactly the six public assets and verify their checksums before installation')
    parser.add_argument('--doctor-only', action='store_true',
                        help='verify installation and live readiness; native jobs already run the full trial')
    args = parser.parse_args()
    assets = args.assets_dir.resolve()
    node = shutil.which('node')
    if not node:
        parser.error('Node.js 22+ is required for the plugin trial')
    # The validation process must leave the published asset directory unchanged.
    sys.dont_write_bytecode = True
    with tempfile.TemporaryDirectory(prefix='guardrail-independent-install-') as temporary:
        root = Path(temporary)
        assets = prepare_assets(assets, args.version, root / 'downloads', public=args.public)
        spec = importlib.util.spec_from_file_location('installer', assets / 'install.py')
        installer = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(installer)
        installed = installer.install(assets, root / 'versions with spaces', args.version)
        empty_path = root / 'empty-path'
        empty_path.mkdir()
        environment = dict(os.environ, PATH=str(empty_path), HOME=str(root),
                           OPEN_GUARDRAIL_BINARIES=str(installed / 'native'),
                           OPEN_GUARDRAIL_PYTHON=sys.executable)
        environment.pop('PYTHONPATH', None)
        environment.pop('PYTHONHOME', None)
        commands = ('doctor.mjs',) if args.doctor_only else ('doctor.mjs', 'trial.mjs')
        for command in commands:
            options = ['--live'] if command == 'doctor.mjs' else []
            # Doctor permits startup + version query; trial permits ten serial
            # calls (420s + 9 * 90s), with cleanup and scheduling margin.
            timeout = 480 if command == 'doctor.mjs' else 1500
            completed = subprocess.run([node, str(installed / 'plugin' / command), *options],
                                       env=environment, cwd=root, text=True, timeout=timeout, stdout=subprocess.PIPE)
            print(completed.stdout.strip(), flush=True)
            completed.check_returncode()
            result = json.loads(completed.stdout)
            if command == 'trial.mjs' and result.get('passed') is not True:
                raise ValueError('installation trial did not pass')
    print('Independent installation passed with no checkout, Go, npm or host credentials on PATH.')


if __name__ == '__main__':
    main()
