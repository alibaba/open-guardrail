"""Argo identity and enforcing-gate contracts; no cluster, model or credentials."""

import base64
from contextlib import redirect_stderr, redirect_stdout
import io
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

from integrations.argo import adapter, agent_client


class ArgoAdapterTest(unittest.TestCase):
    def context(self, **updates):
        options = dict(instance_id="cluster-1", rollout="checkout", revision="hash-1",
                       checkpoint="canary", observation={"status": "healthy"},
                       key_file="/private/key.json", cli="guardrailctl")
        options.update(updates)
        return adapter.context_from_argo({"POD_NAMESPACE": "demo", "JOB_UID": "job-1"}, **options)

    def test_identity_binds_attempt_and_candidate_without_credentials(self):
        context = self.context()
        self.assertNotIn("/private/key.json", context.operation_identity)
        self.assertEqual(context.operation_identity, self.context().operation_identity)
        for field in ("instance_id", "rollout", "revision", "checkpoint"):
            self.assertNotEqual(context.operation_identity,
                                self.context(**{field: "other"}).operation_identity)
        delivery = context.change_set["delivery"]
        self.assertEqual(delivery["jobUid"], "job-1")
        self.assertEqual(delivery["namespace"], "demo")
        with self.assertRaises(ValueError):
            self.context(revision="")

    def test_waits_for_gate_and_never_converts_gate_failure_to_success(self):
        for outcome in ([False, True], RuntimeError("DENY"), RuntimeError("UNKNOWN"),
                        RuntimeError("expired"), OSError("read failed")):
            with self.subTest(outcome=outcome), tempfile.TemporaryDirectory() as root:
                args = SimpleNamespace(instance_id="cluster-1", rollout="checkout",
                    revision="hash-1", checkpoint="canary", case="healthy",
                    recipient="age1test", state_dir=str(Path(root) / "state"),
                    cli="guardrailctl", timeout_seconds=780)
                mounted_key = Path(root) / "mounted-key.json"
                target_key = Path(root) / "secret-data.json"
                target_key.write_text('{"server":"https://guardrail.example","token":"synthetic"}')
                target_key.chmod(0o440)
                mounted_key.symlink_to(target_key)
                def prepare(context, state_dir, recipient, envelope):
                    materialized = Path(context.key_file)
                    self.assertFalse(materialized.is_symlink())
                    self.assertEqual(materialized.stat().st_mode & 0o777, 0o600)
                    Path(envelope).write_bytes(b"encrypted-not-plaintext")
                output = io.StringIO()
                with mock.patch.dict(adapter.os.environ, {"GUARDRAIL_KEY_FILE": str(mounted_key),
                         "OPEN_GUARDRAIL_URL": "https://guardrail.example",
                         "POD_NAMESPACE": "demo", "JOB_UID": "job-1"}), \
                     mock.patch.object(adapter.gate, "prepare_gate", side_effect=prepare), \
                     mock.patch.object(adapter.gate, "poll_gate", side_effect=outcome), \
                     mock.patch.object(adapter.time, "sleep"), redirect_stdout(output):
                    if isinstance(outcome, Exception):
                        with self.assertRaises(type(outcome)):
                            adapter.run(args)
                    else:
                        self.assertEqual(adapter.run(args), 0)
                line = output.getvalue().splitlines()[0]
                self.assertEqual(base64.b64decode(line[len(adapter.ENVELOPE_MARKER):]),
                                 b"encrypted-not-plaintext")
                self.assertFalse((Path(root) / "project-key.json").exists())

    def test_timeout_never_allows(self):
        args = SimpleNamespace(recipient="age1test", timeout_seconds=0)
        with mock.patch.dict(adapter.os.environ, {"GUARDRAIL_KEY_FILE": __file__,
                             "OPEN_GUARDRAIL_URL": "https://guardrail.example"}):
            with self.assertRaises(ValueError):
                adapter.run(args)

    def test_receive_rejects_missing_or_ambiguous_handoff(self):
        for logs in ("not ready", (adapter.ENVELOPE_MARKER + "YWdl\n") * 2,
                     adapter.ENVELOPE_MARKER + "!invalid!"):
            with self.subTest(logs=logs), tempfile.TemporaryDirectory() as root:
                args = SimpleNamespace(work_dir=root, namespace="demo", pod="selected-pod")
                with mock.patch.object(agent_client.gate, "command", return_value=logs):
                    with self.assertRaises(ValueError):
                        agent_client.receive(args)
                self.assertFalse((Path(root) / "handoff.json").exists())

    def test_termination_message_distinguishes_decision_and_business_rejection(self):
        for error, layer, reason in (
            (adapter.gate.GateStopped("EVIDENCE_REJECTED", "business", "private-token"), "business", "EVIDENCE_REJECTED"),
            (adapter.gate.GateStopped("DECISION_EXPIRED", "decision", "private-token"), "decision", "DECISION_EXPIRED"),
            (OSError("private-token"), "verification", "VERIFICATION_FAILED"),
        ):
            with self.subTest(reason=reason), tempfile.TemporaryDirectory() as root:
                result = Path(root) / "termination.json"
                argv = ["adapter", "--instance-id", "test", "--rollout", "test", "--revision", "hash",
                        "--case", "healthy", "--recipient", "age1test", "--result-file", str(result)]
                output = io.StringIO()
                with mock.patch.object(adapter.sys, "argv", argv), \
                     mock.patch.object(adapter, "run", side_effect=error), \
                     redirect_stdout(output), redirect_stderr(output):
                    self.assertEqual(adapter.main(), 1)
                actual = json.loads(result.read_text())
                self.assertEqual((actual["outcome"], actual["layer"], actual["reason"]), ("DENY", layer, reason))
                self.assertEqual(actual["businessEvidence"], "REJECTED" if layer == "business" else "NOT_CHECKED")
                self.assertNotIn("private-token", output.getvalue() + result.read_text())

    def test_success_reports_both_conditions_passed(self):
        with tempfile.TemporaryDirectory() as root:
            result = Path(root) / "termination.json"
            with redirect_stdout(io.StringIO()):
                adapter.report_result(result, allowed=True)
            actual = json.loads(result.read_text())
            self.assertEqual(actual["serverDecision"], "ALLOW")
            self.assertEqual(actual["businessEvidence"], "ACCEPTED")

    def test_agent_deadline_stops_without_an_allow_poll(self):
        with tempfile.TemporaryDirectory() as root:
            args = SimpleNamespace(instance_id="test", rollout="test", revision="hash", checkpoint="canary",
                case="healthy", state_dir=str(Path(root) / "state"), recipient="age1test", cli="unused")
            def prepare(context, state_dir, recipient, envelope):
                Path(envelope).write_bytes(b"ciphertext")
            with mock.patch.dict(adapter.os.environ, {"POD_NAMESPACE": "demo", "JOB_UID": "job-1"}), \
                 mock.patch.object(adapter.gate, "prepare_gate", side_effect=prepare), \
                 mock.patch.object(adapter.gate, "poll_gate") as poll, \
                 mock.patch.object(adapter.time, "monotonic", return_value=100), \
                 redirect_stdout(io.StringIO()):
                with self.assertRaises(adapter.gate.GateStopped) as caught:
                    adapter.wait_gate(args, "unused", deadline=99)
            self.assertEqual(caught.exception.reason, "AGENT_TIMEOUT")
            poll.assert_not_called()


if __name__ == "__main__":
    unittest.main()
