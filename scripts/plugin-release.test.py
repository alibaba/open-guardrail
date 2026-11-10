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


if __name__ == '__main__':
    unittest.main()
