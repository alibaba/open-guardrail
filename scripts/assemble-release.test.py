#!/usr/bin/env python3
"""Reject unverified, mixed or changed native release artifacts."""
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('assemble_release', Path(__file__).with_name('assemble-release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)
smoke_spec = importlib.util.spec_from_file_location('smoke_plugin_release', Path(__file__).with_name('smoke-plugin-release.py'))
smoke = importlib.util.module_from_spec(smoke_spec)
smoke_spec.loader.exec_module(smoke)


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
        expected = {'open-guardrail-' + self.version + '-' + target + '.tar.gz' for target in release.TARGETS}
        expected |= {release.plugin_bundle_name(self.version), 'SHA256SUMS'}
        self.assertEqual({p.name for p in self.output.iterdir()}, expected)
        with tarfile.open(self.output / release.plugin_bundle_name(self.version)) as bundle:
            self.assertEqual(set(bundle.getnames()), {'install.py', 'plugin-release.json', self.plugin})
            manifest = json.load(bundle.extractfile('plugin-release.json'))
            self.assertEqual(bundle.extractfile(self.plugin).read(), b'same-plugin')
            self.assertEqual(bundle.extractfile('install.py').read(), b'# synthetic installer\n')
        self.assertEqual(set(manifest['assets']), set(release.TARGETS) | {'plugin'})
        checksums = (self.output / 'SHA256SUMS').read_text().splitlines()
        self.assertEqual(len(checksums), 5)
        for line in checksums:
            checksum, name = line.split()
            self.assertEqual(checksum, release.digest(self.output / name))
        self.assertFalse((self.output / 'untracked-secret.txt').exists())
        self.assertFalse(list(self.output.glob('*.tgz')))

    def test_plugin_installation_bundle_is_reproducible_and_has_no_local_identity(self):
        self.assemble()
        another = self.root / 'another'
        release.assemble(self.root, another, self.version, self.commit)
        name = release.plugin_bundle_name(self.version)
        self.assertEqual((self.output / name).read_bytes(), (another / name).read_bytes())
        with tarfile.open(self.output / name) as bundle:
            for member in bundle:
                self.assertTrue(member.isfile())
                self.assertEqual((member.uid, member.gid, member.uname, member.gname, member.mtime),
                                 (0, 0, '', '', 0))

    def public_assets(self):
        return smoke.prepare_assets(self.output, self.version, self.root / 'downloads', public=True)

    def test_public_bundle_opens_without_modifying_published_files(self):
        self.assemble()
        original = {p.name: release.digest(p) for p in self.output.iterdir()}
        downloads = self.public_assets()
        self.assertEqual((downloads / self.plugin).read_bytes(), b'same-plugin')
        self.assertEqual(set(json.loads((downloads / 'plugin-release.json').read_text())['assets']),
                         set(release.TARGETS) | {'plugin'})
        self.assertEqual({p.name: release.digest(p) for p in self.output.iterdir()}, original)

    def test_public_download_corruption_is_rejected_before_loading_installer(self):
        self.assemble()
        next(self.output.glob('open-guardrail-*.tar.gz')).write_bytes(b'changed')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            self.public_assets()
        self.assertFalse((self.root / 'downloads').exists())

    def test_public_directory_cannot_include_extra_proofs_or_untracked_files(self):
        self.assemble()
        (self.output / 'unexpected.sigstore.json').write_text('{}')
        with self.assertRaisesRegex(ValueError, 'exactly the six'):
            self.public_assets()

    def test_public_directory_with_raw_manifest_cannot_bypass_validation(self):
        self.assemble()
        (self.output / 'plugin-release.json').write_bytes((self.common / 'plugin-release.json').read_bytes())
        with self.assertRaisesRegex(ValueError, 'exactly the six'):
            self.public_assets()
        self.assertFalse((self.root / 'downloads').exists())

    def test_public_checksum_manifest_rejects_malformed_lines_with_clear_error(self):
        self.assemble()
        path = self.output / 'SHA256SUMS'
        original = path.read_text()
        for malformed in ('', 'checksum-only', '0' * 64 + '  archive.tar.gz  extra'):
            with self.subTest(line=malformed):
                path.write_text(malformed + '\n' + original)
                with self.assertRaisesRegex(ValueError, '^invalid or duplicate public checksum$'):
                    self.public_assets()
                self.assertFalse((self.root / 'downloads').exists())

    def test_public_checksum_manifest_cannot_omit_an_archive(self):
        self.assemble()
        path = self.output / 'SHA256SUMS'
        path.write_text('\n'.join(path.read_text().splitlines()[1:]) + '\n')
        with self.assertRaisesRegex(ValueError, 'cover the five'):
            self.public_assets()

    def test_unsafe_install_bundle_is_rejected_even_with_matching_checksum(self):
        self.assemble()
        name = release.plugin_bundle_name(self.version)
        with tarfile.open(self.output / name, 'w:gz') as bundle:
            member = tarfile.TarInfo('../install.py')
            member.size = 1
            bundle.addfile(member, io.BytesIO(b'x'))
        path = self.output / 'SHA256SUMS'
        path.write_text(''.join(release.digest(p) + '  ' + p.name + '\n'
                                for p in sorted(self.output.iterdir()) if p.name != 'SHA256SUMS'))
        with self.assertRaisesRegex(ValueError, 'unsafe or unexpected'):
            self.public_assets()
        self.assertFalse((self.root / 'downloads').exists())

    def test_local_raw_bundle_mode_remains_available(self):
        self.assertEqual(smoke.prepare_assets(self.common, self.version, self.root / 'unused'), self.common)
        self.assertFalse((self.root / 'unused').exists())

    def test_complete_public_bundle_installs_native_and_plugin_from_downloads_only(self):
        # Real archives and the real installer, without Go compilation or a host SDK.
        def archive(path, entries):
            with tarfile.open(path, 'w:gz') as package:
                for name, data, mode in entries:
                    member = tarfile.TarInfo(name)
                    member.size, member.mode = len(data), mode
                    package.addfile(member, io.BytesIO(data))
        archive(self.common / self.plugin, [
            ('package/package.json', json.dumps({'version': self.version}).encode(), 0o644)])
        plugin = {'file': self.plugin, 'sha256': release.digest(self.common / self.plugin)}
        path = self.common / 'plugin-release.json'
        value = json.loads(path.read_text())
        value['assets']['plugin'] = plugin
        path.write_text(json.dumps(value))
        installer_source = Path(__file__).resolve().parents[1] / 'integrations/agent-plugins/install.py'
        (self.common / 'install.py').write_bytes(installer_source.read_bytes())
        for target, directory in zip(release.TARGETS, self.directories):
            name = 'open-guardrail-' + self.version + '-' + target + '.tar.gz'
            archive(directory / name, [(binary, b'#!/bin/sh\nexit 0\n', 0o755)
                                      for binary in ('open-guardrail', 'guardrailctl')])
            checksum = release.digest(directory / name)
            (directory / (name + '.sha256')).write_text(checksum + '  ' + name + '\n')
            self.change_manifest(lambda m, target=target, checksum=checksum: m['assets'].update({
                'plugin': plugin, target: {'file': name, 'sha256': checksum}}),
                index=self.directories.index(directory))
        self.assemble()
        downloads = self.public_assets()
        installer_spec = importlib.util.spec_from_file_location('downloaded_installer', downloads / 'install.py')
        installer = importlib.util.module_from_spec(installer_spec)
        installer_spec.loader.exec_module(installer)
        installed = installer.install(downloads, self.root / 'versions with spaces', self.version)
        for binary in ('open-guardrail', 'guardrailctl'):
            self.assertTrue(os.access(installed / 'native' / binary, os.X_OK))
        self.assertEqual(json.loads((installed / 'plugin/package.json').read_text())['version'], self.version)

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
