#!/usr/bin/env python3
"""Exercise release publication without building platform binaries."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('plugin_release', Path(__file__).with_name('plugin-release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.source = self.root / 'integrations/agent-plugins'
        self.source.mkdir(parents=True)
        (self.source / 'install.py').write_text('# installer')
        (self.source / 'package.json').write_text(json.dumps({'name': '@changed/plugin', 'files': ['install.py']}))
        self.dist = self.root / 'dist'
        self.dist.mkdir()
        self.version = '1.2.3-alpha.0'
        for target in ('linux-amd64', 'linux-arm64', 'darwin-amd64', 'darwin-arm64'):
            name = 'open-guardrail-' + self.version + '-' + target + '.tar.gz'
            (self.dist / name).write_bytes(b'native')
            (self.dist / (name + '.sha256')).write_text('sidecar')
        (self.dist / 'old-version.tgz').write_bytes(b'stale')
        self.output = self.dist / 'new-bundle'
        self.plugin_name = 'renamed-plugin-1.2.3-alpha.0.tgz'
        self.missing_plugin = False
        self.addCleanup(patch.stopall)
        patch.object(release, 'ROOT', self.root).start()
        self.run = patch.object(release.subprocess, 'run', side_effect=self.command).start()
        patch.object(release.subprocess, 'check_output', return_value='source-commit\n').start()

    def command(self, args, **kwargs):
        if args[0] == 'npm':
            destination = Path(args[args.index('--pack-destination') + 1])
            if not self.missing_plugin:
                (destination / self.plugin_name).write_bytes(b'plugin')
            return subprocess.CompletedProcess(args, 0, stdout=json.dumps([{'filename': self.plugin_name}]))
        return subprocess.CompletedProcess(args, 0)

    def test_npm_filename_and_isolated_manifest_ignore_stale_assets(self):
        release.assemble(self.version, self.output)
        value = json.loads((self.output / 'plugin-release.json').read_text())
        self.assertEqual(value['assets']['plugin']['file'], self.plugin_name)
        self.assertEqual(value['commit'], 'source-commit')
        self.assertFalse((self.output / 'old-version.tgz').exists())
        self.assertEqual((self.dist / 'old-version.tgz').read_bytes(), b'stale')
        self.assertEqual(len(list(self.output.glob('*.sha256'))), 4)
        self.assertEqual(len(list(self.output.iterdir())), 11)
        self.assertFalse(list(self.dist.glob('.plugin-release-*')))

    def test_missing_asset_has_friendly_cli_error_and_no_partial_bundle(self):
        self.missing_plugin = True
        errors = io.StringIO()
        with patch.object(sys, 'argv', ['plugin-release.py', '--version', self.version,
                                      '--output-dir', str(self.output)]), contextlib.redirect_stderr(errors):
            with self.assertRaises(SystemExit) as raised:
                release.main()
        self.assertEqual(raised.exception.code, 1)
        self.assertIn('missing release asset: ' + self.plugin_name, errors.getvalue())
        self.assertNotIn('Traceback', errors.getvalue())
        self.assertFalse(self.output.exists())
        self.assertFalse(list(self.dist.glob('.plugin-release-*')))

    def test_missing_native_asset_stops_publication(self):
        next(self.dist.glob('*linux-arm64.tar.gz')).unlink()
        with self.assertRaisesRegex(ValueError, 'missing release asset'):
            release.assemble(self.version, self.output)
        self.assertFalse(self.output.exists())

    def test_existing_bundle_is_preserved_before_any_build(self):
        self.output.mkdir()
        (self.output / 'keep').write_text('previous')
        with self.assertRaisesRegex(ValueError, 'already exists'):
            release.assemble(self.version, self.output)
        self.run.assert_not_called()
        self.assertEqual((self.output / 'keep').read_text(), 'previous')

    def test_build_failure_stops_without_publishing(self):
        self.run.side_effect = subprocess.CalledProcessError(2, ['make'])
        with self.assertRaises(subprocess.CalledProcessError):
            release.assemble(self.version, self.output)
        self.assertFalse(self.output.exists())

    def make_shared_plugin(self):
        shared = self.dist / 'shared-plugin'
        release.assemble(self.version, shared, plugin_only=True)
        return shared

    def test_plugin_only_does_not_build_or_require_native_archives(self):
        for path in self.dist.glob('*.tar.gz*'):
            path.unlink()
        shared = self.make_shared_plugin()
        manifest = json.loads((shared / 'plugin-release.json').read_text())
        self.assertEqual(set(manifest['assets']), {'plugin'})
        self.assertEqual(len(list(shared.iterdir())), 3)
        commands = [call.args[0] for call in self.run.call_args_list]
        self.assertEqual(commands[0], ['make', 'check-public-release', 'VERSION=' + self.version])
        self.assertEqual(sum(command[0] == 'npm' for command in commands), 1)

    def test_one_plugin_is_reused_by_all_four_single_platform_builds(self):
        shared = self.make_shared_plugin()
        original = {p.name: p.read_bytes() for p in shared.iterdir()}
        for target in release.TARGETS:
            output = self.dist / target
            release.assemble(self.version, output, platform=target, plugin_assets=shared)
            manifest = json.loads((output / 'plugin-release.json').read_text())
            self.assertEqual(set(manifest['assets']), {target, 'plugin'})
            self.assertEqual((output / self.plugin_name).read_bytes(), original[self.plugin_name])
            self.assertEqual(len(list(output.glob('*.tar.gz'))), 1)
        commands = [call.args[0] for call in self.run.call_args_list]
        self.assertEqual(sum(command[0] == 'npm' for command in commands), 1)
        self.assertEqual(sum(command[0] == 'node' for command in commands), 1)
        native = [command for command in commands if command[:2] == ['make', 'public-release']]
        self.assertEqual(len(native), 4)
        self.assertEqual({command[-1] for command in native}, {
            'PACKAGE_PLATFORMS=' + target.replace('-', '/') for target in release.TARGETS})
        self.assertEqual({p.name: p.read_bytes() for p in shared.iterdir()}, original)

    def test_tampered_shared_plugin_fails_before_native_build(self):
        shared = self.make_shared_plugin()
        (shared / self.plugin_name).write_bytes(b'changed')
        self.run.reset_mock()
        with self.assertRaisesRegex(ValueError, 'checksum does not match'):
            release.assemble(self.version, self.output, platform='linux-arm64', plugin_assets=shared)
        self.run.assert_not_called()
        self.assertFalse(self.output.exists())

    def test_shared_plugin_version_and_commit_are_bound_before_build(self):
        shared = self.make_shared_plugin()
        path = shared / 'plugin-release.json'
        original = json.loads(path.read_text())
        for key, value in [('version', '9.9.9'), ('commit', 'another-commit')]:
            with self.subTest(key=key):
                manifest = dict(original, **{key: value})
                path.write_text(json.dumps(manifest))
                self.run.reset_mock()
                with self.assertRaisesRegex(ValueError, 'version or source commit'):
                    release.assemble(self.version, self.output, platform='linux-arm64', plugin_assets=shared)
                self.run.assert_not_called()

    def test_shared_installer_must_match_the_same_source(self):
        shared = self.make_shared_plugin()
        (shared / 'install.py').write_text('# wrong installer')
        self.run.reset_mock()
        with self.assertRaisesRegex(ValueError, 'installer does not match'):
            release.assemble(self.version, self.output, platform='darwin-arm64', plugin_assets=shared)
        self.run.assert_not_called()

    def test_plugin_only_rejects_native_and_reuse_options(self):
        with self.assertRaisesRegex(ValueError, 'cannot be combined'):
            release.assemble(self.version, self.output, platform='linux-arm64', plugin_only=True)
        with self.assertRaisesRegex(ValueError, 'cannot be combined'):
            release.assemble(self.version, self.output, plugin_assets=self.dist, plugin_only=True)
        self.run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
