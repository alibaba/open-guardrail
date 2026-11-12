#!/usr/bin/env python3
"""Collect the exact archives verified by all four native release jobs."""
import argparse
import gzip
import hashlib
import io
import json
from pathlib import Path
import re
import shutil
import tarfile
import tempfile

TARGETS = ('linux-amd64', 'linux-arm64', 'darwin-amd64', 'darwin-arm64')


def plugin_bundle_name(version):
    return 'openguardrail-agent-plugins-preview-' + version + '-install.tar.gz'


def write_plugin_bundle(path, manifest, plugin, installer):
    """Keep the npm archive unchanged and include everything its installer needs."""
    entries = {
        plugin.name: plugin.read_bytes(),
        'install.py': installer.read_bytes(),
        'plugin-release.json': (json.dumps(manifest, indent=2) + '\n').encode(),
    }
    # Fixed metadata avoids local names, paths and timestamps in the public bundle.
    with path.open('xb') as output:
        with gzip.GzipFile(filename='', fileobj=output, mode='wb', mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode='w', format=tarfile.USTAR_FORMAT) as archive:
                for name, data in sorted(entries.items()):
                    member = tarfile.TarInfo(name)
                    member.size, member.mode, member.mtime = len(data), 0o644, 0
                    archive.addfile(member, io.BytesIO(data))


def regular(root, name):
    path = root / name
    if (Path(name).name != name or path.is_symlink() or not path.is_file()
            or root.is_symlink() or path.resolve() != path.absolute()):
        raise ValueError('missing regular release file: ' + name)
    return path


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def archive_files(directory, entry, expected_name, package_name, package_version=None):
    name = entry['file']
    if name != expected_name:
        raise ValueError('unexpected archive name: ' + name)
    archive = regular(directory, name)
    if digest(archive) != entry['sha256']:
        raise ValueError('archive checksum mismatch: ' + name)
    sbom = regular(directory, name + '.spdx.json')
    inventory = json.loads(sbom.read_text())
    if inventory['spdxVersion'] != 'SPDX-2.3':
        raise ValueError('release SBOM must use SPDX 2.3: ' + name)
    if not any(item.get('name') == package_name
               and (package_version is None or item.get('versionInfo') == package_version)
               for item in inventory.get('packages', [])):
        raise ValueError('SBOM does not inventory the released component: ' + name)
    # Signatures are verified by gh in the workflow, including source/workflow
    # identity. File presence or JSON parsing alone does not authenticate them.
    provenance = regular(directory, 'provenance.sigstore.json')
    sbom_bundle = regular(directory, name + '.sbom.sigstore.json')
    return {name: archive, name + '.spdx.json': sbom,
            name + '.provenance.sigstore.json': provenance,
            name + '.sbom.sigstore.json': sbom_bundle}


def assemble(source, output, version, commit):
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?', version):
        raise ValueError('use a fixed semantic version')
    if not re.fullmatch(r'[0-9a-f]{40}', commit):
        raise ValueError('use the full public source commit')
    if output.exists() or output.is_symlink():
        raise ValueError('output already exists; preserve prior release assets')
    # Canonicalize the chosen root (macOS /var and /tmp are aliases), while
    # still rejecting symlinks within a downloaded platform artifact.
    source = source.resolve(strict=True)
    common = source / 'release-plugin'
    common_manifest = json.loads(regular(common, 'plugin-release.json').read_text())
    if common_manifest['version'] != version or common_manifest['commit'] != commit:
        raise ValueError('mixed release version or commit: shared plugin')
    plugin = common_manifest['assets']['plugin']
    files, assets = {}, {}
    for target in TARGETS:
        directory = source / ('release-' + target)
        manifest = json.loads(regular(directory, 'platform-manifest.json').read_text())
        if manifest['version'] != version or manifest['commit'] != commit:
            raise ValueError('mixed release version or commit: ' + target)
        entry = manifest['assets'][target]
        name = 'open-guardrail-' + version + '-' + target + '.tar.gz'
        checked = archive_files(directory, entry, name, 'github.com/alibaba/open-guardrail')
        files[name] = checked[name]
        sidecar = regular(directory, name + '.sha256')
        if sidecar.read_text().split() != [entry['sha256'], name]:
            raise ValueError('native checksum sidecar mismatch: ' + target)
        assets[target] = entry
        current_plugin = manifest['assets']['plugin']
        if plugin != current_plugin:
            raise ValueError('native jobs tested different plugin bytes')
    plugin_name = 'openguardrail-agent-plugins-preview-' + version + '.tgz'
    checked_plugin = archive_files(common, plugin, plugin_name, '@openguardrail/agent-plugins-preview', version)
    installer = regular(common, 'install.py')
    assets['plugin'] = plugin
    # Validate everything before creating an output; never reuse stale dist files.
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.release-', dir=output.parent) as temporary:
        staged = Path(temporary) / 'assets'
        staged.mkdir()
        for name, path in files.items():
            shutil.copy2(path, staged / name)
        manifest = {'version': version, 'commit': commit, 'assets': assets}
        write_plugin_bundle(staged / plugin_bundle_name(version), manifest,
                            checked_plugin[plugin_name], installer)
        (staged / 'SHA256SUMS').write_text(''.join(
            digest(path) + '  ' + path.name + '\n' for path in sorted(staged.iterdir())))
        staged.rename(output)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--commit', required=True)
    args = parser.parse_args()
    try:
        assemble(args.input.absolute(), args.output.absolute(), args.version, args.commit)
    except (OSError, ValueError, KeyError, TypeError) as error:
        parser.exit(1, 'Release collection failed: ' + str(error) + '\n')


if __name__ == '__main__':
    main()
