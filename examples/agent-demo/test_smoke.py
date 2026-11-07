"""Regression checks for local demo credential handling and lifecycle."""

import contextlib
import importlib.util
import io
from pathlib import Path
import unittest
from unittest.mock import Mock, patch


spec = importlib.util.spec_from_file_location("agent_demo_smoke", Path(__file__).with_name("smoke.py"))
demo = importlib.util.module_from_spec(spec)
spec.loader.exec_module(demo)


class DemoLifecycleTests(unittest.TestCase):
    def test_unsupported_python_stops_before_building_or_starting_a_service(self):
        error = io.StringIO()
        with patch.object(demo.sys, "version_info", (3, 8)), patch.object(
            demo.subprocess, "run"
        ) as run, patch.object(demo.subprocess, "Popen") as start, contextlib.redirect_stderr(error):
            with self.assertRaises(SystemExit) as stopped:
                demo.main([])
        self.assertEqual(stopped.exception.code, 2)
        self.assertIn("Python 3.9+", error.getvalue())
        self.assertIn("manual walkthrough", error.getvalue())
        run.assert_not_called()
        start.assert_not_called()

    def test_inspection_password_uses_hidden_prompts_and_is_not_printed(self):
        args = demo.options(["--keep-running"])
        password = "disposable-demo-test-only"
        output = io.StringIO()
        with patch.object(demo.sys.stdin, "isatty", return_value=True), patch.object(
            demo.getpass, "getpass", side_effect=[password, password]
        ) as prompt, contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
            self.assertEqual(demo.administrator_password(args), password)
        self.assertEqual(prompt.call_count, 2)
        self.assertNotIn(password, output.getvalue())

    def test_password_from_stdin_cannot_silently_be_ignored_by_automatic_smoke(self):
        error = io.StringIO()
        with contextlib.redirect_stderr(error), self.assertRaises(SystemExit):
            demo.options(["--password-stdin"])
        self.assertIn("--password-stdin requires --keep-running", error.getvalue())

    def test_mismatched_password_stops_before_starting_a_service(self):
        with patch.object(demo.sys.stdin, "isatty", return_value=True), patch.object(
            demo.getpass, "getpass", side_effect=["first-test-password", "other-test-password"]
        ), patch.object(demo.subprocess, "Popen") as start:
            with self.assertRaisesRegex(ValueError, "passwords do not match"):
                demo.main(["--keep-running"])
        start.assert_not_called()

    def test_noninteractive_inspection_requires_explicit_stdin_mode(self):
        with patch.object(demo.sys.stdin, "isatty", return_value=False):
            with self.assertRaisesRegex(ValueError, "--password-stdin"):
                demo.administrator_password(demo.options(["--keep-running"]))
        with patch.object(demo.sys, "stdin", io.StringIO("stdin-demo-test-only\n")):
            self.assertEqual(
                demo.administrator_password(demo.options(["--keep-running", "--password-stdin"])),
                "stdin-demo-test-only",
            )

    def test_readiness_failure_reports_exit_code_and_next_step(self):
        service = Mock()
        service.poll.return_value = -9
        service.returncode = -9
        with self.assertRaisesRegex(RuntimeError, r"exit -9.*terminal"):
            demo.wait_for_service(service, "http://127.0.0.1:1")

    def test_inspection_waits_until_interrupt_without_printing_a_password(self):
        service = Mock()
        service.poll.return_value = None
        output = io.StringIO()
        with patch.object(demo.time, "sleep", side_effect=KeyboardInterrupt), contextlib.redirect_stdout(output):
            demo.inspect_in_browser(service, "http://127.0.0.1:8080")
        self.assertIn("sign in as demo", output.getvalue())
        self.assertIn("Ctrl-C", output.getvalue())
        self.assertIn("remove its temporary data", output.getvalue())
        service.terminate.assert_not_called()

    def test_lost_service_does_not_report_successful_inspection(self):
        service = Mock()
        service.poll.return_value = 1
        service.returncode = 1
        with contextlib.redirect_stdout(io.StringIO()), self.assertRaisesRegex(RuntimeError, "exit 1"):
            demo.inspect_in_browser(service, "http://127.0.0.1:8080")


if __name__ == "__main__":
    unittest.main()
