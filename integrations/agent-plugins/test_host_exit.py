"""Guard against treating a SIGTERM handler's zero status as natural exit."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("verify-host-exit.py")
spec = importlib.util.spec_from_file_location("verify_host_exit", SCRIPT)
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


class HostExitTests(unittest.TestCase):
    def test_natural_exit_and_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            for code in (0, 7):
                with self.subTest(code=code):
                    result = verifier.verify([sys.executable, "-c", f"raise SystemExit({code})"], Path(directory) / str(code), 5)
                    self.assertEqual(result["normalHostExit"], code == 0)
                    self.assertEqual(result["returnCode"], code)
                    self.assertFalse(result["supervisorTerminated"])

    def test_zero_status_after_sigterm_fails_cli_acceptance(self):
        program = "import signal,time,sys; signal.signal(signal.SIGTERM, lambda *_: sys.exit(0)); print('ready', flush=True); time.sleep(60)"
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "trial"
            process = subprocess.run([sys.executable, str(SCRIPT), "--timeout", "1", "--output-dir", str(output), "--", sys.executable, "-c", program], capture_output=True, text=True, timeout=10)
            self.assertEqual(process.returncode, 1)
            result = json.loads((output / "exit.json").read_text())
            self.assertEqual(result["returnCode"], 0)
            self.assertTrue(result["supervisorTerminated"])
            self.assertFalse(result["normalHostExit"])
            self.assertIn("ready", (output / "stdout.jsonl").read_text())
