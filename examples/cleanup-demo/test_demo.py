"""File-tool enforcement tests; no running service or model is required."""

import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import threading
import time
import unittest
from unittest.mock import Mock


spec = importlib.util.spec_from_file_location("cleanup_demo", Path(__file__).with_name("demo.py"))
demo = importlib.util.module_from_spec(spec)
spec.loader.exec_module(demo)


class FileToolTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.tool = demo.FileTool(self.temporary.name)
        self.gateway = Mock()
        self.gateway.key = "unused-test-key"

    def tearDown(self):
        self.tool.close()
        self.temporary.cleanup()

    def test_policy_parameter_rejects_invalid_types_and_yaml_injection(self):
        for limit in (True, False, 0, 3, 1.0, "1", "1\nkind: unexpected"):
            with self.subTest(limit=limit), self.assertRaises(ValueError):
                demo.policy_yaml(limit)

    def approved(self, targets=None, tool=None):
        tool = tool or self.tool
        observation = tool.observe(targets or ["cache.tmp", "download.tmp"])
        change = {"action": "delete-files", "workspaceId": tool.workspace_id,
                  "targets": observation["targets"], "maxFiles": 2,
                  "observationDigest": demo.hashlib.sha256(demo.identity(observation).encode()).hexdigest()}
        checks = demo.assess(observation, 2)
        for check in checks:
            check["evidence"] = [{"resultSummary": demo.identity(observation), "observedAt": "2026-11-06T00:00:00Z"}]
        submission = {"checks": checks, "verdict": "PASS"}
        final = {"id": "run-test", "status": "FINISHED", "result": "PASS", "decision": "ALLOW",
                 "policyRevisionId": "rev-test", "specDigest": "digest-test", "changeSet": change,
                 "finalSubmissionId": "sub-test", "latestSubmissionNo": 1,
                 "decisionExpiresAt": time.time() + 60}
        stored = {"id": "sub-test", "runId": "run-test", "submissionNo": 1,
                  "verdict": "PASS", "finishRun": True, "snapshot": submission}
        record = {"runId": "run-test", "changeSet": change, "submission": submission,
                  "revision": {"id": "rev-test", "digest": "digest-test"}, "observation": observation}
        self.gateway.cli.side_effect = lambda *args: copy.deepcopy(final if args[1] == "get" else stored)
        return record, final, stored

    def test_real_deletion_retains_business_files(self):
        record, _, _ = self.approved()
        self.assertEqual(self.tool.execute(self.gateway, record), ["cache.tmp", "download.tmp"])
        self.assertEqual({f["name"] for f in self.tool.inventory()}, {"orders.csv", "config.yaml"})
        self.assertTrue((self.tool.path / "orders.csv").is_file())

    def test_denied_expired_open_wrong_run_or_revision_never_deletes(self):
        for mutation in ({"decision": "DENY"}, {"decisionExpiresAt": 0}, {"status": "OPEN"},
                         {"id": "other-run"}, {"policyRevisionId": "other-revision"},
                         {"specDigest": "other-digest"}):
            with self.subTest(mutation=mutation):
                record, final, _ = self.approved()
                final.update(mutation)
                with self.assertRaises(ValueError):
                    self.tool.execute(self.gateway, record)
                self.assertEqual(len(self.tool.inventory()), 4)

    def test_changed_files_rejected_after_allow(self):
        record, _, _ = self.approved()
        (self.tool.path / "cache.tmp").write_text("changed after assessment")
        with self.assertRaises(ValueError):
            self.tool.execute(self.gateway, record)
        self.assertEqual(len(self.tool.inventory()), 4)

    def test_service_timestamp_normalization_preserves_evidence_identity(self):
        record, _, stored = self.approved()
        stored["snapshot"] = copy.deepcopy(stored["snapshot"])
        stored["snapshot"]["checks"][0]["evidence"][0]["observedAt"] = "2026-11-06T00:00:00.000000+00:00"
        self.assertEqual(len(self.tool.execute(self.gateway, record)), 2)

    def test_fractional_timestamp_widths_are_supported_on_python_39(self):
        for fraction in ("1", "12", "123", "1234", "12345", "123456", "1234567", "12345678", "123456789"):
            with self.subTest(fraction=fraction):
                a = {"checks": [{"evidence": [{"observedAt": "2026-11-06T00:00:00." + fraction + "Z"}]}]}
                b = copy.deepcopy(a)
                b["checks"][0]["evidence"][0]["observedAt"] = "2026-11-06T00:00:00." + fraction.ljust(9, "0") + "+00:00"
                self.assertEqual(demo.snapshot_identity(a), demo.snapshot_identity(b))

    def test_nanoseconds_preserve_distinct_evidence_and_normalize_timezones(self):
        def evidence(stamp):
            return {"checks": [{"evidence": [{"observedAt": stamp}]}]}
        a = evidence("2026-11-06T00:00:00.123456789Z")
        self.assertEqual(demo.snapshot_identity(a), demo.snapshot_identity(evidence("2026-11-06T08:00:00.123456789+08:00")))
        self.assertNotEqual(demo.snapshot_identity(a), demo.snapshot_identity(evidence("2026-11-06T00:00:00.123456788Z")))
        for stamp in ("2026-11-06T00:00:00.1234567890Z", "2026-11-06T00:00:00.123456789", None):
            with self.subTest(stamp=stamp), self.assertRaises(ValueError):
                demo.snapshot_identity(evidence(stamp))

    def test_changed_nanosecond_in_accepted_evidence_prevents_deletion(self):
        record, _, stored = self.approved()
        stored["snapshot"] = copy.deepcopy(stored["snapshot"])
        stored["snapshot"]["checks"][0]["evidence"][0]["observedAt"] = "2026-11-06T00:00:00.000000001Z"
        with self.assertRaises(ValueError):
            self.tool.execute(self.gateway, record)
        self.assertEqual(len(self.tool.inventory()), 4)

    def test_public_errors_do_not_expose_exception_arguments_or_captured_output(self):
        secret = "fake-private-sentinel"
        cases = [RuntimeError(secret), ValueError(secret),
                 OSError(2, "missing " + secret, "/private/" + secret),
                 demo.subprocess.TimeoutExpired(["guardrailctl", secret], 40, output=secret, stderr=secret)]
        for error in cases:
            with self.subTest(kind=type(error).__name__):
                detail = demo.public_error(error, "check")
                self.assertNotIn(secret, json.dumps(detail))
                self.assertEqual(detail["stage"], "check")
                self.assertEqual(detail["errorType"], type(error).__name__)
        detail = demo.public_error(demo.CommandFailed("run", 7), "check")
        self.assertEqual(detail["code"], "command_failed")
        self.assertEqual(detail["exitCode"], 7)
        self.assertIn("exit 7", detail["message"])
        self.assertEqual(demo.public_error(ValueError("Install Go from go.mod, or use a native release archive"), "startup")["code"], "go_unavailable")

    def test_command_failure_exposes_exit_code_without_arguments_or_output(self):
        from unittest.mock import patch
        private = "fake-private-command-sentinel"
        result = demo.subprocess.CompletedProcess([], 9, stdout=private, stderr=private)
        with patch.object(demo.subprocess, "run", return_value=result):
            with self.assertRaises(demo.CommandFailed) as raised:
                demo.command("/private/" + private, "run", "--key-file", private)
        detail = demo.public_error(raised.exception, "check")
        self.assertEqual(detail["exitCode"], 9)
        self.assertNotIn(private, json.dumps(detail))
        self.assertNotIn("/private", json.dumps(detail))

    def test_state_polling_does_not_wait_for_remote_execution_reads(self):
        app = demo.Demo(self.gateway, self.temporary.name)
        entered, release, returned = threading.Event(), threading.Event(), threading.Event()
        record, _, _ = self.approved(tool=app.tool)
        record.update(decision="ALLOW", removed=[])
        self.gateway.evaluate.return_value = record
        read = self.gateway.cli.side_effect
        def delayed(*args):
            entered.set()
            if not release.wait(3):
                raise RuntimeError("test read timed out")
            return read(*args)
        self.gateway.cli.side_effect = delayed
        result = []
        def poll():
            result.append(app.state())
            returned.set()
        poller = threading.Thread(target=poll, daemon=True)
        try:
            app.start({"targets": ["cache.tmp", "download.tmp"], "maxFiles": 2, "repair": False})
            self.assertTrue(entered.wait(1))
            poller.start()
            self.assertTrue(returned.wait(.5), "state polling waited for a remote command")
            self.assertTrue(result[0]["busy"])
            self.assertEqual(len(result[0]["files"]), 4)
        finally:
            release.set()
            if poller.ident is not None:
                poller.join(timeout=2)
            deadline = time.monotonic() + 3
            while app.busy and time.monotonic() < deadline:
                time.sleep(.01)
            app.tool.close()
        self.assertFalse(app.busy)
        self.assertEqual(app.jobs[-1]["status"], "completed")
        self.assertEqual(app.jobs[-1]["removed"], ["cache.tmp", "download.tmp"])

    def test_forged_evidence_or_different_final_submission_rejected(self):
        for mutation in ("evidence", "identity"):
            record, _, stored = self.approved()
            if mutation == "evidence":
                stored["snapshot"] = {"checks": []}
            else:
                stored["runId"] = "other-run"
            with self.assertRaises(ValueError):
                self.tool.execute(self.gateway, record)
            self.assertEqual(len(self.tool.inventory()), 4)

    def test_targets_cannot_be_substituted_after_approval(self):
        record, _, _ = self.approved(["cache.tmp"])
        record["observation"] = self.tool.observe(["download.tmp"])
        with self.assertRaises(ValueError):
            self.tool.execute(self.gateway, record)
        self.assertEqual(len(self.tool.inventory()), 4)

    def test_second_execution_of_the_same_allow_does_not_delete_anything_else(self):
        record, _, _ = self.approved()
        self.tool.execute(self.gateway, record)
        with self.assertRaises(ValueError):
            self.tool.execute(self.gateway, record)
        self.assertEqual(len(self.tool.inventory()), 2)

    def test_arbitrary_paths_duplicates_and_symlinks_are_rejected(self):
        for targets in (["../outside.tmp"], ["/tmp/outside.tmp"], ["cache.tmp", "cache.tmp"], "cache.tmp"):
            with self.assertRaises(ValueError):
                self.tool.observe(targets)
        outside = Path(self.temporary.name) / "outside.tmp"
        outside.write_text("must remain")
        (self.tool.path / "cache.tmp").unlink()
        (self.tool.path / "cache.tmp").symlink_to(outside)
        with self.assertRaises(OSError):
            self.tool.observe(["cache.tmp"])
        self.assertEqual(outside.read_text(), "must remain")

    def test_assess_distinguishes_protected_missing_and_batch_limit(self):
        self.assertEqual([c["verdict"] for c in demo.assess(self.tool.observe(["orders.csv"]), 2)], ["BLOCKED", "PASS"])
        self.assertEqual([c["verdict"] for c in demo.assess(self.tool.observe(["cache.tmp", "download.tmp"]), 1)], ["PASS", "BLOCKED"])
        (self.tool.path / "cache.tmp").unlink()
        self.assertEqual(demo.assess(self.tool.observe(["cache.tmp"]), 2)[0]["verdict"], "UNKNOWN")

    def test_transport_failure_fails_closed(self):
        record, _, _ = self.approved()
        self.gateway.cli.side_effect = RuntimeError("service unavailable")
        with self.assertRaises(RuntimeError):
            self.tool.execute(self.gateway, record)
        self.assertEqual(len(self.tool.inventory()), 4)

    def test_an_allow_label_cannot_authorize_protected_files(self):
        record, _, _ = self.approved(["orders.csv"])
        with self.assertRaises(ValueError):
            self.tool.execute(self.gateway, record)
        self.assertEqual(len(self.tool.inventory()), 4)

    def test_partial_tool_failure_records_completed_deletions(self):
        record, _, _ = self.approved()
        original = demo.os.unlink
        def unlink(name, **kwargs):
            if name == "download.tmp":
                raise OSError("injected file operation failure")
            original(name, **kwargs)
        from unittest.mock import patch
        with patch.object(demo.os, "unlink", side_effect=unlink), self.assertRaises(OSError):
            self.tool.execute(self.gateway, record)
        self.assertEqual(record["removed"], ["cache.tmp"])
        self.assertEqual({f["name"] for f in self.tool.inventory()}, {"download.tmp", "orders.csv", "config.yaml"})

    def test_reset_creates_a_different_workspace_and_preserves_history(self):
        app = demo.Demo(self.gateway, self.temporary.name)
        try:
            before = app.tool.workspace_id
            app.jobs.append({"id": "previous"})
            app.reset()
            self.assertNotEqual(before, app.tool.workspace_id)
            self.assertEqual(len(app.state()["files"]), 4)
            self.assertEqual(app.state()["jobs"], [{"id": "previous"}])
        finally:
            app.tool.close()

    def test_failure_before_run_creation_remains_bound_to_its_original_workspace(self):
        app = demo.Demo(self.gateway, self.temporary.name)
        self.gateway.publish.side_effect = demo.CommandFailed("scene", 2)
        try:
            before = app.tool.workspace_id
            job_id = app.start({"targets": ["cache.tmp"], "maxFiles": 1, "repair": True})
            deadline = time.monotonic() + 2
            while app.busy and time.monotonic() < deadline:
                time.sleep(.01)
            self.assertFalse(app.busy)
            job = app.state()["jobs"][-1]
            self.assertEqual(job["id"], job_id)
            self.assertEqual(job["status"], "error")
            self.assertEqual(job["records"], [])
            self.assertEqual(job["workspaceId"], before)
            self.assertEqual(job["error"]["stage"], "publish")
            self.assertEqual(job["error"]["code"], "command_failed")
            self.assertEqual(job["error"]["exitCode"], 2)
            self.assertEqual(job["events"][-1]["error"], job["error"])
            app.reset()
            self.assertNotEqual(app.state()["workspaceId"], job["workspaceId"])
            self.assertEqual(app.state()["jobs"][-1], job)
        finally:
            app.tool.close()


if __name__ == "__main__":
    unittest.main()
