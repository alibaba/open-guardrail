"""Offline installer regression tests, including untrusted archive boundaries."""
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import platform
import tarfile
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('plugin_install', Path(__file__).with_name('install.py'))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class InstallTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.assets = self.root / 'assets'
        self.assets.mkdir()
        self.prefix = self.root / 'install with spaces'
        self.version = '0.1.0-alpha.0'
        architecture = {'x86_64': 'amd64', 'amd64': 'amd64', 'arm64': 'arm64', 'aarch64': 'arm64'}[platform.machine().lower()]
        self.platform = platform.system().lower() + '-' + architecture
        self.archive('native.tar.gz', [('open-guardrail', b'binary', 0o755), ('guardrailctl', b'binary', 0o755)])
        self.archive('plugin.tgz', [('package/package.json', json.dumps({'version': self.version}).encode(), 0o644)])
        self.manifest()

    def archive(self, name, files):
        with tarfile.open(self.assets / name, 'w:gz') as bundle:
            for path, data, mode in files:
                member = tarfile.TarInfo(path)
                member.size, member.mode = len(data), mode
                bundle.addfile(member, io.BytesIO(data))

    def manifest(self):
        value = {'version': self.version, 'assets': {}}
        for key, name in [(self.platform, 'native.tar.gz'), ('plugin', 'plugin.tgz')]:
            value['assets'][key] = {'file': name, 'sha256': hashlib.sha256((self.assets / name).read_bytes()).hexdigest()}
        (self.assets / 'plugin-release.json').write_text(json.dumps(value))

    def install(self):
        return installer.install(self.assets, self.prefix, self.version)

    def test_install_and_existing_version_preserved(self):
        target = self.install()
        self.assertIn(str(target / 'native'), (target / 'env.sh').read_text())
        with self.assertRaises(FileExistsError):
            self.install()
        self.assertEqual((target / 'native/guardrailctl').read_bytes(), b'binary')

    def test_corrupt_archive_stops_before_destination_creation(self):
        (self.assets / 'plugin.tgz').write_bytes(b'corrupt')
        with self.assertRaisesRegex(ValueError, 'checksum'):
            self.install()
        self.assertFalse(self.prefix.exists())

    def test_version_mismatch_rolls_back_owned_directory(self):
        self.archive('plugin.tgz', [('package/package.json', b'{"version":"9.0.0"}', 0o644)])
        self.manifest()
        with self.assertRaisesRegex(ValueError, 'plugin version'):
            self.install()
        self.assertFalse((self.prefix / self.version).exists())

    def test_path_traversal_rejected_and_no_partial_install(self):
        self.archive('plugin.tgz', [('package/../../escape', b'bad', 0o644)])
        self.manifest()
        with self.assertRaisesRegex(ValueError, 'unsafe'):
            self.install()
        self.assertFalse((self.prefix / self.version).exists())
        self.assertFalse((self.prefix / 'escape').exists())

    def test_symlink_rejected(self):
        with tarfile.open(self.assets / 'plugin.tgz', 'w:gz') as bundle:
            member = tarfile.TarInfo('package/link')
            member.type, member.linkname = tarfile.SYMTYPE, '/tmp'
            bundle.addfile(member)
        self.manifest()
        with self.assertRaisesRegex(ValueError, 'unsafe'):
            self.install()

    def test_manifest_cannot_escape_assets_directory(self):
        value = json.loads((self.assets / 'plugin-release.json').read_text())
        value['assets']['plugin']['file'] = '../plugin.tgz'
        (self.assets / 'plugin-release.json').write_text(json.dumps(value))
        with self.assertRaisesRegex(ValueError, 'filename'):
            self.install()

    def test_requested_version_must_match_manifest(self):
        with self.assertRaisesRegex(ValueError, 'requested version'):
            installer.install(self.assets, self.prefix, '1.0.0')


if __name__ == '__main__':
    unittest.main()
