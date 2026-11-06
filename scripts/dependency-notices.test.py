"""Regression cases for preserving upstream terms and rejecting incomplete bundles."""

import importlib.util
from pathlib import Path
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("dependency_notices", Path(__file__).with_name("dependency-notices.py"))
notices = importlib.util.module_from_spec(spec)
spec.loader.exec_module(notices)


class NoticeBundleTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.cache, self.go_root = self.root / "cache", self.root / "go"
        (self.root / "third_party").mkdir()
        (self.root / "go.mod").write_text("module example.com/project\n\ngo 1.27.3\n")
        (self.root / "third_party/go-modules.tsv").write_text(
            "module|version|scope|license|status\n"
            "example.com/dependency|v1.0.0|runtime|MIT|allowed\n"
            "example.com/dependency|v1.0.0|tool|MIT|allowed\n")
        self.upstream = self.cache / "example.com/dependency@v1.0.0"
        self.upstream.mkdir(parents=True)
        (self.upstream / "LICENSE").write_bytes(b"original copyright\noriginal terms\n")
        (self.upstream / "NOTICE").write_bytes(b"original attribution\n")
        (self.upstream / "LICENSE-3RD-PARTY.md").write_bytes(b"embedded component terms\n")
        (self.upstream / "copyright").mkdir()  # A similarly named package is not a legal file.
        self.go_root.mkdir()
        (self.go_root / "VERSION").write_text("go1.27.3\n")
        (self.go_root / "LICENSE").write_bytes(b"Go license\n")

    def collect(self):
        notices.collect(self.root, self.cache, self.go_root)

    def test_original_notices_and_supplements_survive_byte_for_byte(self):
        self.collect()
        notices.check(self.root)
        for name in ("LICENSE", "NOTICE", "LICENSE-3RD-PARTY.md"):
            bundled = self.root / "third_party/licenses/example.com/dependency@v1.0.0" / name
            self.assertEqual(bundled.read_bytes(), (self.upstream / name).read_bytes())
        self.assertIn("runtime,tool", (self.root / "third_party/notices.tsv").read_text())

    def test_changed_terms_stale_versions_and_unindexed_files_are_rejected(self):
        self.collect()
        bundled = self.root / "third_party/licenses/example.com/dependency@v1.0.0/LICENSE"
        bundled.write_bytes(b"changed terms\n")
        with self.assertRaisesRegex(ValueError, "checksum"):
            notices.check(self.root)
        self.collect()
        extra = bundled.with_name("local-notes.txt")
        extra.write_text("not an upstream legal file")
        with self.assertRaisesRegex(ValueError, "outside the inventory"):
            notices.check(self.root)
        extra.unlink()
        inventory = self.root / "third_party/go-modules.tsv"
        inventory.write_text(inventory.read_text().replace("v1.0.0", "v1.0.1"))
        with self.assertRaisesRegex(ValueError, "versions or scopes"):
            notices.check(self.root)

    def test_missing_source_preserves_bundle_and_symlink_is_rejected(self):
        self.collect()
        index = self.root / "third_party/notices.tsv"
        original = index.read_bytes()
        (self.upstream / "LICENSE").unlink()
        (self.upstream / "LICENSE-3RD-PARTY.md").unlink()
        with self.assertRaisesRegex(ValueError, "license file missing"):
            self.collect()
        self.assertEqual(index.read_bytes(), original)
        bundled = self.root / "third_party/licenses/example.com/dependency@v1.0.0/LICENSE"
        bundled.unlink()
        bundled.symlink_to(self.go_root / "LICENSE")
        with self.assertRaisesRegex(ValueError, "non-regular"):
            notices.check(self.root)


if __name__ == "__main__":
    unittest.main()
