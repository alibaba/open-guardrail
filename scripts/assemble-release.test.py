#!/usr/bin/env python3
"""Reject unverified, mixed or changed native release artifacts."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('assemble_release', Path(__file__).with_name('assemble-release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class AssemblyTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.output = self.root / 'final'
        self.version, self.commit = '0.1.0-alpha.0', 'a' * 40
        self.plugin = 'openguardrail-agent-plugins-preview-' + self.version + '.tgz'
        self.common = self.root / 'release-plugin'
        self.common.mkdir()
        (self.common / self.plugin).write_bytes(b'same-plugin')
        plugin = {'file': self.plugin, 'sha256': release.digest(self.common / self.plugin)}
        (self.common / 'plugin-release.json').write_text(json.dumps({
            'version': self.version, 'commit': self.commit, 'assets': {'plugin': plugin}}))
        (self.common / (self.plugin + '.spdx.json')).write_text(json.dumps({
            'spdxVersion': 'SPDX-2.3', 'packages': [
                {'name': '@openguardrail/agent-plugins-preview', 'versionInfo': self.version}]}))
        (self.common / (self.plugin + '.sbom.sigstore.json')).write_text('{"test":"not-a-signature"}')
        (self.common / 'provenance.sigstore.json').write_text('{"test":"not-a-signature"}')
        (self.common / 'install.py').write_text('# synthetic installer\n')
        self.directories = []
        for target in release.TARGETS:
            directory = self.root / ('release-' + target)
            directory.mkdir()
            self.directories.append(directory)
            name = 'open-guardrail-' + self.version + '-' + target + '.tar.gz'
            (directory / name).write_bytes(target.encode())
            archive = {'file': name, 'sha256': release.digest(directory / name)}
            (directory / (name + '.sha256')).write_text(archive['sha256'] + '  ' + name + '\n')
            (directory / 'platform-manifest.json').write_text(json.dumps({
                'version': self.version, 'commit': self.commit,
                'assets': {target: archive, 'plugin': plugin}}))
            for filename in (name,):
                package = {'name': 'github.com/alibaba/open-guardrail'}
                (directory / (filename + '.spdx.json')).write_text(json.dumps({
                    'spdxVersion': 'SPDX-2.3', 'packages': [package]}))
                (directory / (filename + '.sbom.sigstore.json')).write_text('{"test":"not-a-signature"}')
            (directory / 'provenance.sigstore.json').write_text('{"test":"not-a-signature"}')
            (directory / 'untracked-secret.txt').write_text('must-not-be-published')

    def assemble(self):
        release.assemble(self.root, self.output, self.version, self.commit)

    def change_manifest(self, change, index=0):
        path = self.directories[index] / 'platform-manifest.json'
        manifest = json.loads(path.read_text())
        change(manifest)
        path.write_text(json.dumps(manifest))

    def test_collects_native_archives_and_matching_plugin_without_extra_files(self):
        self.assemble()
        manifest = json.loads((self.output / 'plugin-release.json').read_text())
        self.assertEqual(set(manifest['assets']), set(release.TARGETS) | {'plugin'})
        for line in (self.output / 'SHA256SUMS').read_text().splitlines():
            checksum, name = line.split()
            self.assertEqual(checksum, release.digest(self.output / name))
        self.assertFalse((self.output / 'untracked-secret.txt').exists())
        self.assertEqual(len(list(self.output.glob('*.tgz'))), 1)

    def test_mixed_commits_fail_without_partial_output(self):
        self.change_manifest(lambda m: m.update(commit='b' * 40))
        with self.assertRaisesRegex(ValueError, 'mixed release'):
            self.assemble()
        self.assertFalse(self.output.exists())

    def test_different_tested_plugins_fail(self):
        self.change_manifest(lambda m: m['assets']['plugin'].update(sha256='b' * 64), index=1)
        with self.assertRaisesRegex(ValueError, 'different plugin bytes'):
            self.assemble()
        self.assertFalse(self.output.exists())

    def test_tampered_native_archive_fails(self):
        next(self.directories[0].glob('*.tar.gz')).write_bytes(b'changed')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            self.assemble()

    def test_missing_native_platform_fails(self):
        (self.directories[-1] / 'platform-manifest.json').unlink()
        with self.assertRaisesRegex(ValueError, 'missing regular'):
            self.assemble()
        self.assertFalse(self.output.exists())

    def test_missing_sbom_attestation_fails(self):
        next(self.directories[1].glob('*.tar.gz.sbom.sigstore.json')).unlink()
        with self.assertRaisesRegex(ValueError, 'missing regular'):
            self.assemble()

    def test_wrong_sidecar_fails(self):
        next(self.directories[0].glob('*.sha256')).write_text('0' * 64 + '  unrelated.tar.gz\n')
        with self.assertRaisesRegex(ValueError, 'sidecar mismatch'):
            self.assemble()

    def test_native_sbom_without_product_inventory_fails(self):
        next(self.directories[0].glob('*.tar.gz.spdx.json')).write_text(
            '{"spdxVersion":"SPDX-2.3","packages":[]}')
        with self.assertRaisesRegex(ValueError, 'does not inventory'):
            self.assemble()

    def test_plugin_sbom_with_wrong_version_fails(self):
        (self.common / (self.plugin + '.spdx.json')).write_text(json.dumps({
            'spdxVersion': 'SPDX-2.3', 'packages': [
                {'name': '@openguardrail/agent-plugins-preview', 'versionInfo': 'old'}]}))
        with self.assertRaisesRegex(ValueError, 'does not inventory'):
            self.assemble()

    def test_shared_plugin_from_another_commit_fails(self):
        path = self.common / 'plugin-release.json'
        manifest = json.loads(path.read_text())
        manifest['commit'] = 'b' * 40
        path.write_text(json.dumps(manifest))
        with self.assertRaisesRegex(ValueError, 'mixed release'):
            self.assemble()
        self.assertFalse(self.output.exists())

    def test_tampered_shared_plugin_fails(self):
        (self.common / self.plugin).write_bytes(b'changed')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            self.assemble()
        self.assertFalse(self.output.exists())

    def test_symlinked_input_is_rejected(self):
        archive = next(self.directories[0].glob('*.tar.gz'))
        saved = archive.with_suffix('.saved')
        archive.rename(saved)
        archive.symlink_to(saved)
        with self.assertRaisesRegex(ValueError, 'missing regular'):
            self.assemble()

    def test_existing_assets_are_preserved(self):
        self.output.mkdir()
        (self.output / 'keep').write_text('previous release')
        with self.assertRaisesRegex(ValueError, 'already exists'):
            self.assemble()
        self.assertEqual((self.output / 'keep').read_text(), 'previous release')


if __name__ == '__main__':
    unittest.main()
