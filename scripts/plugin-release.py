#!/usr/bin/env python3
"""Assemble a matching native/plugin bundle in a new, isolated output directory."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent


def assemble(version, output):
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?', version):
        raise ValueError('use a fixed semantic version, for example 0.1.0-alpha.0')
    if output.exists():
        raise ValueError('output directory already exists; select a new --output-dir to preserve prior assets')
    output.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(['make', 'public-release', 'VERSION=' + version], cwd=ROOT, check=True)
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    source = ROOT / 'integrations/agent-plugins'
    subprocess.run(['node', 'pack.mjs'], cwd=source, check=True)
    with tempfile.TemporaryDirectory(prefix='.plugin-release-', dir=output.parent) as temporary:
        package = Path(temporary) / 'package'
        package.mkdir()
        bundle = Path(temporary) / 'bundle'
        bundle.mkdir()
        metadata = json.loads((source / 'package.json').read_text())
        for name in metadata['files']:
            path = source / name
            if path.is_dir():
                shutil.copytree(path, package / name)
            else:
                shutil.copy2(path, package / name)
        metadata['version'] = version
        (package / 'package.json').write_text(json.dumps(metadata, indent=2) + '\n')
        result = subprocess.run(['npm', 'pack', '--offline', '--ignore-scripts', '--json',
                                 '--pack-destination', str(bundle)],
                                cwd=package, check=True, text=True, stdout=subprocess.PIPE)
        packed = json.loads(result.stdout)
        if not isinstance(packed, list) or len(packed) != 1:
            raise ValueError('npm pack must report exactly one archive')
        plugin_name = packed[0]['filename']
        if not isinstance(plugin_name, str) or Path(plugin_name).name != plugin_name or not plugin_name.endswith('.tgz'):
            raise ValueError('npm pack reported an invalid archive filename')
        assets = {}
        for target in ('linux-amd64', 'linux-arm64', 'darwin-amd64', 'darwin-arm64'):
            name = 'open-guardrail-' + version + '-' + target + '.tar.gz'
            if not (ROOT / 'dist' / name).is_file():
                raise ValueError('missing release asset: ' + name)
            shutil.copy2(ROOT / 'dist' / name, bundle / name)
            shutil.copy2(ROOT / 'dist' / (name + '.sha256'), bundle / (name + '.sha256'))
            assets[target] = name
        assets['plugin'] = plugin_name
        manifest = {'version': version, 'commit': commit, 'assets': {}}
        for target, name in assets.items():
            path = bundle / name
            if not path.is_file():
                raise ValueError('missing release asset: ' + name)
            digest = hashlib.sha256(path.read_bytes()).hexdigest()
            manifest['assets'][target] = {'file': name, 'sha256': digest}
        shutil.copy2(source / 'install.py', bundle / 'install.py')
        (bundle / 'plugin-release.json').write_text(json.dumps(manifest, indent=2) + '\n')
        bundle.rename(output)
    print('Bundle ready in ' + str(output) + '. Run its install.py to verify and install.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output-dir', type=Path, required=True, help='new directory containing only this bundle')
    args = parser.parse_args()
    try:
        assemble(args.version, args.output_dir.resolve())
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        parser.exit(1, 'Plugin release failed: ' + str(error) + '\n')


if __name__ == '__main__':
    main()
