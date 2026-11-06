"""Contract tests for the shared Recovery gate; no model or service is called."""

import json
from io import BytesIO
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from integrations.recovery_agent import gate


def snapshot(observation, verdict="PASS", summary="Healthy"):
    return {"baseSubmissionNo": 0, "finishRun": True, "verdict": verdict, "summary": summary,
            "checks": [{"checkId": "readiness", "verdict": verdict, "summary": summary,
                        "evidence": [{"requirementId": "health", "observedAt": "2026-10-26T00:00:00Z",
                                      "querySummary": "Read health observation",
                                      "resultSummary": json.dumps(observation)}]}]}


class RecoveryGateTest(unittest.TestCase):
    def test_empty_submission_summary_keeps_check_and_evidence_requirements(self):
        observation = {"ready": True}
        submission = snapshot(observation)
        submission["summary"] = ""
        gate.validate_submission(submission, observation)
        for field in ("summary", "querySummary", "resultSummary"):
            with self.subTest(field=field):
                invalid = snapshot(observation)
                invalid["summary"] = ""
                target = (invalid["checks"][0] if field == "summary"
                          else invalid["checks"][0]["evidence"][0])
                target[field] = ""
                with self.assertRaises(ValueError):
                    gate.validate_submission(invalid, observation)

    def test_fixtures_contain_observations_without_answers(self):
        for name in ("healthy", "unhealthy", "unknown"):
            with self.subTest(name=name):
                fixture = json.loads((gate.ROOT / "examples/agent-demo" / (name + ".json")).read_text())
                self.assertIsInstance(fixture["observations"]["health"], dict)
                self.assertNotIn("verdict", fixture)
                self.assertNotIn("decision", fixture)

    def test_validate_submission_rejects_mismatched_evidence(self):
        observation = {"probeError": "TimeoutError"}
        submission = snapshot({"status": "healthy"}, "UNKNOWN", "Health could not be observed")
        with self.assertRaisesRegex(ValueError, "differs"):
            gate.validate_submission(submission, observation)

    def test_evidence_preserves_json_types(self):
        for observed, submitted in ((True, 1), (False, 0), (1, 1.0), (1.0, True)):
            with self.subTest(observed=observed, submitted=submitted):
                submission = snapshot({"ready": submitted})
                with self.assertRaisesRegex(ValueError, "differs"):
                    gate.validate_submission(submission, {"ready": observed})
        self.assertEqual(gate.json_identity({"a": 1, "b": True}),
                         gate.json_identity({"b": True, "a": 1}))
        with self.assertRaisesRegex(ValueError, "finite"):
            gate.json_identity({"latency": float("nan")})

    def test_summary_limit_counts_utf8_bytes(self):
        submission = snapshot({"ready": True}, summary="界" * 1366)
        with self.assertRaisesRegex(ValueError, "invalid Submission"):
            gate.validate_submission(submission, {"ready": True})

    def test_command_times_out_without_exposing_output(self):
        with mock.patch.object(gate.subprocess, "run", side_effect=
                               gate.subprocess.TimeoutExpired(("codex", "exec"), 300,
                                                               output="sensitive output")) as run:
            with self.assertRaisesRegex(RuntimeError, "codex exec timed out after 300s") as caught:
                gate.command("codex", "exec", input_text="sensitive prompt")
        self.assertEqual(run.call_args.kwargs["timeout"], gate.COMMAND_TIMEOUT_SECONDS)
        self.assertNotIn("sensitive", str(caught.exception))

    def test_http_failure_is_observed_not_hidden_as_timeout(self):
        response = BytesIO(b'{"status":"unhealthy","readyReplicas":1}')
        response.status = 503
        observed = gate.read_health_response(response, "https://canary.example/health")
        self.assertEqual(observed["httpStatus"], 503)
        self.assertEqual(observed["status"], "unhealthy")

    def test_observation_read_is_bounded(self):
        source = mock.MagicMock()
        source.__enter__.return_value = source
        source.read.return_value = b"x" * (gate.MAX_OBSERVATION_BYTES + 1)
        with mock.patch.object(Path, "open", return_value=source):
            with self.assertRaisesRegex(ValueError, "exceeds 32 KiB"):
                gate.load_observation("growing.json")
            source.read.assert_called_once_with(gate.MAX_OBSERVATION_BYTES + 1)

    def test_observation_at_limit_and_invalid_shape(self):
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / "health.json"
            content = json.dumps({"observations": {"health": {"status": "healthy"}}})
            path.write_text(content + " " * (gate.MAX_OBSERVATION_BYTES - len(content)))
            self.assertEqual(gate.load_observation(path), {"status": "healthy"})
            path.write_text('{"observations": []}')
            with self.assertRaisesRegex(ValueError, "observations.health"):
                gate.load_observation(path)

    def test_prepare_encrypts_handoff_and_removes_plaintext_token(self):
        observation = {"status": "healthy", "httpStatus": 200}
        input_data = gate.GateInput(
            change_set={"service": "sample-checkout", "environment": "test",
                        "candidate": "candidate-1"},
            operation_identity="test-pipeline-build-1-checkpoint-1",
            observation=observation, key_file="secret-file",
        )
        def fake_command(*command, **kwargs):
            if command[0] == "age":
                bundle = json.loads(Path(command[-1]).read_text())
                self.assertEqual(bundle["observation"], observation)
                self.assertEqual(bundle["runToken"]["token"], "test-only")
                Path(command[command.index("-o") + 1]).write_text("encrypted-test")
                return ""
            if command[2] == "create":
                gate.private_json(Path(command[command.index("--submission-credential-file") + 1]),
                                  {"runId": "run_test", "token": "test-only"})
                return json.dumps({"id": "run_test", "status": "OPEN"})
            if command[2] == "revision":
                return json.dumps({"id": "rev_test", "digest": "digest", "yaml": "kind: GuardrailSpec"})
            return json.dumps({"policyRevisionId": "rev_test", "specDigest": "digest", "latestSubmissionNo": 0})

        with tempfile.TemporaryDirectory() as root, \
             mock.patch.object(gate, "command", side_effect=fake_command):
            state_dir = Path(root) / "state"
            envelope = Path(root) / "handoff.age"
            gate.prepare_gate(input_data, state_dir, "age1recipient", envelope)
            self.assertTrue(envelope.exists())
            self.assertFalse((state_dir / "run-credential.json").exists())
            self.assertFalse((state_dir / "handoff.json").exists())
            self.assertNotIn("test-only", (state_dir / "state.json").read_text())
            self.assertEqual(json.loads((state_dir / "state.json").read_text())["observation"],
                             observation)
            previous_commands = gate.command.call_count
            with self.assertRaisesRegex(gate.GateStopped, "cannot restart prepare") as caught:
                gate.prepare_gate(input_data, state_dir, "age1recipient", envelope)
            self.assertEqual(caught.exception.reason, "STATE_EXISTS")
            self.assertEqual(gate.command.call_count, previous_commands)

    def test_uncertain_final_read_preserves_run_token(self):
        with tempfile.TemporaryDirectory() as root:
            state_dir = Path(root) / "gate-state-42"
            state_dir.mkdir()
            gate.private_json(state_dir / "state.json", {"runId": "run_test"})
            gate.private_json(state_dir / "run-credential.json", {"token": "test-only"})
            with mock.patch.object(gate, "command", return_value=json.dumps({"status": "OPEN"})):
                with self.assertRaisesRegex(RuntimeError, "promotion stopped"):
                    gate.finalize_gate("guardrailctl", "key-file", state_dir)
            self.assertTrue((state_dir / "run-credential.json").exists())

    def test_poll_only_allows_current_pass(self):
        observation = {"status": "healthy"}
        submission = snapshot(observation)
        for status, result, decision, expires, allowed in (
            ("FINISHED", "PASS", "ALLOW", 4102444800, True),
            ("FINISHED", "BLOCKED", "DENY", 4102444800, False),
            ("FINISHED", "UNKNOWN", "DENY", 4102444800, False),
            ("FINISHED", "PASS", "ALLOW", 1, False),
        ):
            with self.subTest(result=result, expires=expires):
                with tempfile.TemporaryDirectory() as root:
                    state_dir = Path(root) / "state"
                    state_dir.mkdir()
                    gate.private_json(state_dir / "state.json", {"runId": "run_test",
                                                                "observation": observation, "policyRevisionId": "rev_test"})
                    final = json.dumps({"status": status, "result": result,
                                        "decision": decision, "decisionExpiresAt": expires,
                                        "policyRevisionId": "rev_test", "finalSubmissionId": "sub_test", "latestSubmissionNo": 1})
                    stored = json.dumps({"id": "sub_test", "runId": "run_test", "submissionNo": 1,
                                         "verdict": result, "finishRun": True, "snapshot": submission})
                    with mock.patch.object(gate, "command", side_effect=[final, final, stored] if allowed else [final, final]):
                        if allowed:
                            self.assertTrue(gate.poll_gate("cli", "key", state_dir))
                        else:
                            with self.assertRaisesRegex(RuntimeError, "promotion stopped"):
                                gate.poll_gate("cli", "key", state_dir)
                    self.assertFalse(state_dir.exists())

    def test_poll_rejects_forged_or_missing_snapshot_despite_allow(self):
        for forged in (snapshot({"status": "healthy"}), None):
            with self.subTest(forged=forged), tempfile.TemporaryDirectory() as root:
                state_dir = Path(root) / "state"
                state_dir.mkdir(mode=0o700)
                gate.private_json(state_dir / "state.json", {"runId": "run_test",
                    "policyRevisionId": "rev_test", "observation": {"status": "unhealthy"}})
                final = {"status": "FINISHED", "result": "PASS", "decision": "ALLOW",
                         "decisionExpiresAt": 4102444800, "policyRevisionId": "rev_test",
                         "finalSubmissionId": "sub_test", "latestSubmissionNo": 1}
                stored = {"id": "sub_test", "runId": "run_test", "submissionNo": 1,
                          "verdict": "PASS", "finishRun": True, "snapshot": forged}
                with mock.patch.object(gate, "command", side_effect=[json.dumps(final), json.dumps(final), json.dumps(stored)]):
                    with self.assertRaisesRegex(RuntimeError, "does not match pipeline observation"):
                        gate.poll_gate("cli", "key", state_dir)
                self.assertTrue((state_dir / "state.json").exists())

    def test_decision_expiring_during_snapshot_read_is_rejected(self):
        with tempfile.TemporaryDirectory() as root:
            state_dir = Path(root) / "state"
            state_dir.mkdir(mode=0o700)
            observation = {"status": "healthy"}
            gate.private_json(state_dir / "state.json", {"runId": "run_test",
                "policyRevisionId": "rev_test", "observation": observation})
            final = {"status": "FINISHED", "result": "PASS", "decision": "ALLOW",
                     "decisionExpiresAt": 200, "policyRevisionId": "rev_test",
                     "finalSubmissionId": "sub_test", "latestSubmissionNo": 1}
            stored = {"id": "sub_test", "runId": "run_test", "submissionNo": 1,
                      "verdict": "PASS", "finishRun": True, "snapshot": snapshot(observation)}
            with mock.patch.object(gate, "command", side_effect=[json.dumps(final), json.dumps(stored)]), \
                 mock.patch.object(gate.time, "time", side_effect=[100, 300]):
                with self.assertRaisesRegex(gate.GateStopped, "promotion stopped") as caught:
                    gate.finalize_gate("cli", "key", state_dir)
                self.assertEqual(caught.exception.reason, "DECISION_EXPIRED")
                self.assertEqual(caught.exception.business_evidence, "ACCEPTED")

    def test_jenkinsfile_waits_without_running_agent(self):
        pipeline = (gate.ROOT / "integrations/jenkins/Jenkinsfile").read_text()
        self.assertNotIn("codex exec", pipeline)
        self.assertNotIn("--phase submit", pipeline)
        self.assertIn("archiveArtifacts", pipeline)
        self.assertIn("waitUntil", pipeline)

    def test_jenkinsfile_normalizes_secret_file_permissions(self):
        pipeline = (gate.ROOT / "integrations/jenkins/Jenkinsfile").read_text()
        self.assertEqual(pipeline.count('trap \'rm -f "$private_key"\' EXIT'), 2)
        self.assertEqual(pipeline.count("trap 'exit 1' HUP INT TERM"), 2)
        self.assertEqual(
            pipeline.count('mktemp "${TMPDIR:-/tmp}/open-guardrail-key.XXXXXX"'),
            2,
        )
        self.assertEqual(
            pipeline.count('(umask 077; cp "$GUARDRAIL_KEY_FILE" "$private_key")'),
            2,
        )
        self.assertEqual(pipeline.count('chmod 600 "$private_key"'), 2)
        self.assertNotIn('$WORKSPACE/.demo-bin/guardrail-key-', pipeline)
        self.assertEqual(
            pipeline.count('GUARDRAIL_KEY_FILE="$private_key" python3 -m integrations.jenkins.adapter'),
            2,
        )


if __name__ == "__main__":
    unittest.main()
