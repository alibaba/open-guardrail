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
TARGETS = ('linux-amd64', 'linux-arm64', 'darwin-amd64', 'darwin-arm64')


def shared_plugin(directory, version, commit, source):
    """Check the shared package before building any native binaries.

    The release workflow authenticates the shared files' provenance first.
    These checks bind the installed bytes to the requested source and version.
    """
    def regular(name):
        path = directory / name
        if (Path(name).name != name or path.is_symlink() or not path.is_file()
                or path.resolve() != directory.resolve() / name):
            raise ValueError('missing regular shared plugin asset: ' + name)
        return path

    manifest = json.loads(regular('plugin-release.json').read_text())
    if manifest['version'] != version or manifest['commit'] != commit:
        raise ValueError('shared plugin version or source commit does not match')
    entry = manifest['assets']['plugin']
    name = entry['file']
    if not name.endswith('.tgz'):
        raise ValueError('shared plugin must be an npm archive')
    if hashlib.sha256(regular(name).read_bytes()).hexdigest() != entry['sha256']:
        raise ValueError('shared plugin checksum does not match')
    if regular('install.py').read_bytes() != (source / 'install.py').read_bytes():
        raise ValueError('shared installer does not match the source commit')
    return name


def assemble(version, output, platform=None, plugin_assets=None, plugin_only=False):
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?', version):
        raise ValueError('use a fixed semantic version, for example 0.1.0-alpha.0')
    if output.exists() or output.is_symlink():
        raise ValueError('output directory already exists; select a new --output-dir to preserve prior assets')
    if platform is not None and platform not in TARGETS:
        raise ValueError('unsupported native platform')
    if plugin_only and (platform is not None or plugin_assets is not None):
        raise ValueError('--plugin-only cannot be combined with native/reuse options')
    targets = () if plugin_only else ((platform,) if platform is not None else TARGETS)
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    source = ROOT / 'integrations/agent-plugins'
    plugin_name = None
    if plugin_assets is not None:
        plugin_name = shared_plugin(plugin_assets, version, commit, source)
    build = ['make', 'check-public-release' if plugin_only else 'public-release', 'VERSION=' + version]
    if platform is not None:
        build.append('PACKAGE_PLATFORMS=' + platform.replace('-', '/'))
    output.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(build, cwd=ROOT, check=True)
    if plugin_assets is None:
        subprocess.run(['node', 'pack.mjs'], cwd=source, check=True)
    with tempfile.TemporaryDirectory(prefix='.plugin-release-', dir=output.parent) as temporary:
        package = Path(temporary) / 'package'
        package.mkdir()
        bundle = Path(temporary) / 'bundle'
        bundle.mkdir()
        if plugin_assets is not None:
            shutil.copy2(plugin_assets / plugin_name, bundle / plugin_name)
        else:
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
        for target in targets:
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
        installer = (plugin_assets if plugin_assets is not None else source) / 'install.py'
        shutil.copy2(installer, bundle / 'install.py')
        (bundle / 'plugin-release.json').write_text(json.dumps(manifest, indent=2) + '\n')
        bundle.rename(output)
    print('Bundle ready in ' + str(output) + '. Run its install.py to verify and install.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output-dir', type=Path, required=True, help='new directory containing only this bundle')
    parser.add_argument('--platform', choices=TARGETS, help='build only this native platform')
    parser.add_argument('--plugin-only', action='store_true', help='pack the shared plugin without native builds')
    parser.add_argument('--plugin-assets', type=Path, help='reuse a verified shared plugin directory without invoking node/npm')
    args = parser.parse_args()
    try:
        assemble(args.version, args.output_dir.absolute(), args.platform,
                 args.plugin_assets.resolve() if args.plugin_assets is not None else None, args.plugin_only)
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        parser.exit(1, 'Plugin release failed: ' + str(error) + '\n')


if __name__ == '__main__':
    main()
