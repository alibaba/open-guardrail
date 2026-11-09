"""Real service/filesystem acceptance; requires OPEN_GUARDRAIL_BINARIES."""
import importlib.util
import os
from pathlib import Path
import time
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("cleanup_worker", Path(__file__).with_name("worker.py"))
worker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(worker)


@unittest.skipUnless(os.environ.get("OPEN_GUARDRAIL_BINARIES"), "set native binaries for live plugin checks")
class CleanupTests(unittest.TestCase):
    def setUp(self):
        self.app = worker.Cleanup(os.environ["OPEN_GUARDRAIL_BINARIES"])
        self.addCleanup(self.app.close)

    def checks(self, verdict="PASS"):
        return [{"checkId": name, "verdict": verdict, "summary": "Assessment from the frozen observation"}
                for name in ("temporary-only", "batch-limit")]

    def prepare(self, targets=None, request="request"):
        return self.app.prepare(request, targets or ["cache.tmp", "download.tmp"])

    def names(self):
        return {f["name"] for f in self.app.tool.inventory()}

    def test_denial_then_new_run_narrowing_has_actual_effect(self):
        denied = self.prepare(list(worker.demo.SEEDS), "broad")
        final = self.app.submit(denied["runId"], self.checks("BLOCKED"))
        self.assertEqual(final["run"]["decision"], "DENY")
        with self.assertRaises(ValueError):
            self.app.execute(denied["runId"], denied["targets"])
        self.assertEqual(self.names(), set(worker.demo.SEEDS))
        allowed = self.prepare(request="narrow")
        final = self.app.submit(allowed["runId"], self.checks())
        self.assertEqual(final["run"]["decision"], "ALLOW")
        effect = self.app.execute(allowed["runId"], allowed["targets"])
        self.assertEqual(set(effect["removed"]), set(worker.demo.TEMPORARY))
        self.assertEqual(self.names(), {"orders.csv", "config.yaml"})
        self.assertNotEqual(denied["runId"], allowed["runId"])
        self.assertEqual(len(self.app.status()["runs"]), 2)
        history = self.app.status(denied["runId"])["runs"][0]
        self.assertNotIn("acceptedSubmission", self.app.status()["runs"][0])
        self.assertLess(len(worker.demo.identity(self.app.status())), 8000)
        self.assertEqual(history["acceptedSubmission"]["snapshot"]["verdict"], "BLOCKED")
        self.assertEqual(history["acceptedSubmission"]["runId"], denied["runId"])

    def test_forged_pass_is_not_authority_to_delete_business_files(self):
        prepared = self.prepare(list(worker.demo.SEEDS))
        final = self.app.submit(prepared["runId"], self.checks())
        self.assertEqual(final["run"]["decision"], "ALLOW")
        with self.assertRaises(ValueError):
            self.app.execute(prepared["runId"], prepared["targets"])
        self.assertEqual(self.names(), set(worker.demo.SEEDS))

    def test_unknown_does_not_execute(self):
        prepared = self.prepare()
        final = self.app.submit(prepared["runId"], self.checks("UNKNOWN"))
        self.assertEqual(final["run"]["decision"], "DENY")
        with self.assertRaises(ValueError):
            self.app.execute(prepared["runId"], prepared["targets"])
        self.assertEqual(self.names(), set(worker.demo.SEEDS))

    def test_retry_preserves_run_and_submission_identity(self):
        prepared = self.prepare()
        self.assertEqual(self.prepare()["runId"], prepared["runId"])
        first = self.app.submit(prepared["runId"], self.checks())
        self.assertFalse((self.app.records[prepared["runId"]]["directory"] / "credential.json").exists())
        second = self.app.submit(prepared["runId"], list(reversed(self.checks())))
        self.assertEqual(first["run"]["finalSubmissionId"], second["run"]["finalSubmissionId"])
        with self.assertRaises(ValueError):
            self.prepare(["orders.csv"])
        with self.assertRaises(ValueError):
            self.app.submit(prepared["runId"], self.checks("BLOCKED"))

    def test_uncertain_creation_retry_preserves_directory_and_operation(self):
        original = self.app.gateway.cli
        for lost_response in (False, True):
            request = "create-lost" if lost_response else "create-failed"
            created = []

            def fail_create(*args, **kwargs):
                if args[:2] == ("run", "create"):
                    if lost_response:
                        created.append(original(*args, **kwargs))
                    raise TimeoutError("injected create transport failure")
                return original(*args, **kwargs)

            with self.subTest(lost_response=lost_response):
                with patch.object(self.app.gateway, "cli", side_effect=fail_create):
                    with self.assertRaises(TimeoutError):
                        self.prepare(request=request)
                directory = self.app.private / ("request-" + request)
                change = (directory / "change.json").read_bytes()
                credential = directory / "credential.json"
                saved = credential.read_bytes() if credential.exists() else None
                with patch.object(self.app.gateway, "cli", wraps=original) as cli:
                    with self.assertRaisesRegex(worker.InputError, "Creation outcome.*uncertain"):
                        self.prepare(request=request)
                    cli.assert_not_called()
                self.assertEqual((directory / "change.json").read_bytes(), change)
                self.assertEqual(credential.read_bytes() if credential.exists() else None, saved)
                if created:
                    self.assertEqual(original("run", "get", "--key-file", str(self.app.gateway.key),
                                              created[0]["id"])["status"], "OPEN")
                self.assertEqual(self.names(), set(worker.demo.SEEDS))

    def test_submit_lost_ack_retains_credential_until_identical_retry(self):
        prepared = self.prepare()
        credential = self.app.records[prepared["runId"]]["directory"] / "credential.json"
        original = self.app.gateway.cli

        def lose_ack(*args, **kwargs):
            result = original(*args, **kwargs)
            if args[:2] == ("run", "submit"):
                raise TimeoutError("injected lost acknowledgement")
            return result

        with patch.object(self.app.gateway, "cli", side_effect=lose_ack):
            with self.assertRaises(TimeoutError):
                self.app.submit(prepared["runId"], self.checks())
        self.assertTrue(credential.exists())
        final = self.app.submit(prepared["runId"], self.checks())
        self.assertFalse(credential.exists())
        with patch.object(self.app.gateway, "cli", wraps=original) as cli:
            replay = self.app.submit(prepared["runId"], self.checks())
        self.assertEqual(replay["run"]["finalSubmissionId"], final["run"]["finalSubmissionId"])
        self.assertFalse(any(call.args[:2] == ("run", "submit") for call in cli.call_args_list))
        self.app.execute(prepared["runId"], prepared["targets"])
        self.assertEqual(self.names(), {"orders.csv", "config.yaml"})

    def test_changed_arguments_and_changed_resource_stop_execution(self):
        prepared = self.prepare()
        self.app.submit(prepared["runId"], self.checks())
        with self.assertRaises(ValueError):
            self.app.execute(prepared["runId"], ["orders.csv"])
        (self.app.tool.path / "cache.tmp").write_text("changed after observation")
        with self.assertRaises(ValueError):
            self.app.execute(prepared["runId"], prepared["targets"])
        self.assertEqual(self.names(), set(worker.demo.SEEDS))

    def test_service_offline_and_missing_submission_stop_execution(self):
        prepared = self.prepare()
        with self.assertRaises(ValueError):
            self.app.execute(prepared["runId"], prepared["targets"])
        self.app.submit(prepared["runId"], self.checks())
        self.app.process.terminate()
        self.app.process.wait(timeout=5)
        with self.assertRaises(RuntimeError):
            self.app.execute(prepared["runId"], prepared["targets"])
        self.assertEqual(self.names(), set(worker.demo.SEEDS))

    def test_real_expiry_stops_execution(self):
        self.app.policy = self.app.policy.replace("validForSeconds: 60", "validForSeconds: 1")
        folder = self.app.private / "short-policy"
        folder.mkdir()
        self.app.gateway.publish(self.app.policy, folder)
        prepared = self.prepare()
        final = self.app.submit(prepared["runId"], self.checks())
        time.sleep(max(0, final["run"]["decisionExpiresAt"] - time.time()) + .1)
        with self.assertRaises(ValueError):
            self.app.execute(prepared["runId"], prepared["targets"])
        self.assertEqual(self.names(), set(worker.demo.SEEDS))

    def test_isolation_and_execution_retry(self):
        other = worker.Cleanup(os.environ["OPEN_GUARDRAIL_BINARIES"])
        self.addCleanup(other.close)
        prepared = self.prepare()
        with self.assertRaises(KeyError):
            other.execute(prepared["runId"], prepared["targets"])
        self.app.submit(prepared["runId"], self.checks())
        self.app.execute(prepared["runId"], prepared["targets"])
        with self.assertRaises(ValueError):
            self.app.execute(prepared["runId"], prepared["targets"])
        self.assertEqual({f["name"] for f in other.tool.inventory()}, set(worker.demo.SEEDS))

    def test_arbitrary_paths_and_incomplete_checks_are_refused(self):
        for targets in (["../cache.tmp"], ["/tmp/cache.tmp"], ["cache.tmp", "cache.tmp"]):
            with self.assertRaises(ValueError):
                self.prepare(targets)
        prepared = self.prepare()
        with self.assertRaises(ValueError):
            self.app.submit(prepared["runId"], self.checks()[:1])
        self.assertEqual(self.names(), set(worker.demo.SEEDS))

    def test_partial_failure_preserves_effects_and_refuses_reexecution(self):
        prepared = self.prepare()
        self.app.submit(prepared["runId"], self.checks())
        unlink = os.unlink

        def fail_second(name, **kwargs):
            if name == "download.tmp":
                raise OSError("injected test filesystem failure")
            return unlink(name, **kwargs)

        with patch.object(worker.demo.os, "unlink", side_effect=fail_second):
            with self.assertRaises(OSError):
                self.app.execute(prepared["runId"], prepared["targets"])
        self.assertEqual(self.app.records[prepared["runId"]]["removed"], ["cache.tmp"])
        self.assertEqual(self.names(), {"download.tmp", "orders.csv", "config.yaml"})
        with self.assertRaises(ValueError):
            self.app.execute(prepared["runId"], prepared["targets"])


if __name__ == "__main__":
    unittest.main()
