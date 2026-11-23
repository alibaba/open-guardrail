import json
import importlib.util
import os
import pathlib
import re
import shlex
import stat
import subprocess
import sys
import tempfile
import time
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
GHA_DIR = REPO_ROOT / "integrations" / "github-actions"
WORKFLOW_CONTEXT = {
    "repository": "alibaba/open-guardrail",
    "workflowRunId": "12345",
    "workflowRunAttempt": "1",
    "sha": "abc123",
    "ref": "refs/heads/main",
    "environment": "production",
}


def extract_action_script():
    return (GHA_DIR / "gate.sh").read_text(encoding="utf-8")


def action_command():
    # Inspect this asset's single-line entrypoint, not arbitrary YAML. The live
    # demo uses the same gate.sh; the service parses the real Spec with its YAML parser.
    text = (GHA_DIR / "action.yml").read_text(encoding="utf-8")
    commands = re.findall(r"^      run: (.+)$", text, re.MULTILINE)
    if len(commands) != 1:
        raise ValueError("expected one single-line composite Action entrypoint")
    return commands[0]


def parse_github_output(path):
    outputs = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if "=" in line:
            key, val = line.split("=", 1)
            outputs[key] = val
    return outputs


class GitHubActionsAssetsTest(unittest.TestCase):
    def test_custom_demo_reports_configuration_errors_before_starting_service(self):
        cases = [
            (["--spec-file", str(GHA_DIR / "release-gate.yaml")], "provide both"),
            (["--assessment-command", "true"], "provide both"),
            (["--expect-result", "UNKNOWN"], "only used with a custom"),
            (["--spec-file", str(GHA_DIR / "missing.yaml"), "--assessment-command", "true"],
             "readable Spec YAML"),
        ]
        for args, expected in cases:
            with self.subTest(args=args):
                result = subprocess.run([sys.executable, "-B", str(GHA_DIR / "demo.py"), *args],
                                        capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 2)
                self.assertIn(expected, result.stderr)

    def test_release_gate_spec_structure(self):
        spec_path = GHA_DIR / "release-gate.yaml"
        text = spec_path.read_text(encoding="utf-8")
        self.assertIn("apiVersion: guardrail.open/v1alpha1\n", text)
        self.assertIn("kind: GuardrailSpec\n", text)
        self.assertIn("metadata:\n  name: release-gate\n", text)
        check_ids = re.findall(r"^    - id: (.+)$", text, re.MULTILINE)
        self.assertEqual(
            check_ids,
            ["freeze-window", "compatibility-and-blast-radius", "rollback-readiness"],
        )
        self.assertEqual(re.findall(r"^        - id: (.+)$", text, re.MULTILINE),
                         ["freeze-status", "change-assessment", "rollback-target"])

    def test_action_yaml_contract(self):
        action_path = GHA_DIR / "action.yml"
        text = action_path.read_text(encoding="utf-8")
        self.assertIn("runs:\n  using: composite\n", text)
        inputs = text.split("\ninputs:\n", 1)[1].split("\noutputs:\n", 1)[0]
        outputs = text.split("\noutputs:\n", 1)[1].split("\nruns:\n", 1)[0]
        input_names = re.findall(r"^  ([\w-]+):$", inputs, re.MULTILINE)
        output_names = re.findall(r"^  ([\w-]+):$", outputs, re.MULTILINE)
        for required_input in ("mode", "server", "scene", "run-id", "business-id"):
            self.assertIn(required_input, input_names)
        self.assertNotIn("project-key-json", input_names)
        for required_output in ("run-id", "decision", "result", "submission-credential-file"):
            self.assertIn(required_output, output_names)
        self.assertEqual(action_command(), 'bash "$GITHUB_ACTION_PATH/gate.sh"')
        for name in input_names:
            self.assertIn("${{ inputs." + name + " }}", text)
        script = extract_action_script()
        for removed_flag in ("--key-file", "--token-file", "--run-id"):
            self.assertNotIn(removed_flag, script)
        self.assertIn("--submission-credential-file", script)
        self.assertIn('"$decision" != "ALLOW"', script)
        self.assertIn('"$result" != "PASS"', script)
        self.assertIn('"$status" != "FINISHED"', script)

    def test_example_workflow_has_independent_verify_job(self):
        wf_path = GHA_DIR / "release-gate-example.yml"
        raw = wf_path.read_text(encoding="utf-8")
        self.assertNotIn("project-key-json", raw)
        jobs = raw.split("\njobs:\n", 1)[1]
        self.assertIn("  release-gate:\n", jobs)
        self.assertIn("  deploy-example:\n    needs: release-gate\n", jobs)
        self.assertIn("options: [pass, blocked, unknown]", raw)


class ReleaseAssessmentTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location("release_assessment", GHA_DIR / "example/assess.py")
        cls.evaluator = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.evaluator)

    def test_release_fixtures_have_expected_check_outcomes(self):
        for case, expected, checks in (
            ("pass", "PASS", ["PASS", "PASS", "PASS"]),
            ("blocked", "BLOCKED", ["BLOCKED", "PASS", "PASS"]),
            ("unknown", "UNKNOWN", ["PASS", "UNKNOWN", "PASS"]),
        ):
            with self.subTest(case=case):
                data = json.loads((GHA_DIR / "example" / (case + ".json")).read_text())
                verdict, actual = self.evaluator.assess(data)
                self.assertEqual(verdict, expected)
                self.assertEqual([check["verdict"] for check in actual], checks)
                self.assertTrue(all(check["evidence"] for check in actual))

    def test_known_block_dominates_missing_signals(self):
        verdict, checks = self.evaluator.assess({"freezeWindow": {"active": True}})
        self.assertEqual(verdict, "BLOCKED")
        self.assertEqual([check["verdict"] for check in checks], ["BLOCKED", "UNKNOWN", "UNKNOWN"])

    def test_unverified_values_and_missing_rollback_target_are_unknown(self):
        data = json.loads((GHA_DIR / "example/pass.json").read_text())
        data["freezeWindow"]["active"] = "false"
        data["changeAssessment"]["canaryHealthy"] = 1
        data["rollback"]["target"] = ""
        verdict, checks = self.evaluator.assess(data)
        self.assertEqual(verdict, "UNKNOWN")
        self.assertEqual([check["verdict"] for check in checks], ["UNKNOWN", "UNKNOWN", "UNKNOWN"])


class GitHubActionsScriptExecutionTest(unittest.TestCase):
    def setUp(self):
        self.script = action_command()

    def _run_action(self, mode, mock_responses, extra_env=None):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = pathlib.Path(tmp)
            args_log = tmp_path / "args.jsonl"
            responses_file = tmp_path / "responses.json"
            responses_file.write_text(json.dumps(mock_responses), encoding="utf-8")

            fake_ctl = tmp_path / "guardrailctl"
            fake_ctl.write_text(
                "#!/usr/bin/env python3\n"
                "import json, pathlib, sys\n"
                f"args_log = pathlib.Path({str(args_log)!r})\n"
                f"responses = json.loads(pathlib.Path({str(responses_file)!r}).read_text())\n"
                "argv = sys.argv[1:]\n"
                "with args_log.open('a', encoding='utf-8') as f:\n"
                "    f.write(json.dumps(argv) + '\\n')\n"
                "if argv[:2] == ['scene', 'apply']:\n"
                "    print('{}')\n"
                "elif len(argv) >= 2 and argv[0] == 'run' and argv[1] == 'create':\n"
                "    idx = argv.index('--submission-credential-file')\n"
                "    cred_path = pathlib.Path(argv[idx + 1])\n"
                "    cred_path.write_text('{\"token\":\"ogrt_mock\"}', encoding='utf-8')\n"
                "    cred_path.chmod(0o600)\n"
                "    print(json.dumps(responses['create']))\n"
                "elif argv[:2] == ['run', 'revision']:\n"
                "    print(json.dumps(responses['revision']))\n"
                "elif argv[:2] == ['run', 'submit']:\n"
                "    snapshot = pathlib.Path(argv[argv.index('--submission') + 1]).read_text()\n"
                "    (args_log.parent / 'submitted.json').write_text(snapshot)\n"
                "    print('{}')\n"
                "elif len(argv) >= 2 and argv[0] == 'run' and argv[1] == 'wait':\n"
                "    if responses.get('wait_exit_code'):\n"
                "        print(responses.get('wait_error', 'run wait failed'), file=sys.stderr)\n"
                "        sys.exit(responses['wait_exit_code'])\n"
                "    print(json.dumps(responses['wait']))\n"
                "elif len(argv) >= 2 and argv[0] == 'run' and argv[1] == 'get':\n"
                "    print(json.dumps(responses['get']))\n"
                "else:\n"
                "    sys.exit(2)\n",
                encoding="utf-8",
            )
            fake_ctl.chmod(0o755)

            gh_out = tmp_path / "github_output"
            gh_out.write_text("", encoding="utf-8")
            runner_temp = tmp_path / "runner_temp"
            runner_temp.mkdir(mode=0o700)

            env = os.environ.copy()
            env.update(
                {
                    "OG_MODE": mode,
                    "OG_SERVER": "http://127.0.0.1:8080",
                    "OG_SCENE": "release-gate",
                    "OG_SPEC_FILE_INPUT": "",
                    "OG_ASSESSMENT_COMMAND": "",
                    "OG_RUN_ID": "",
                    "OG_BUSINESS_ID": "",
                    "OG_CHANGE_SET_JSON": "",
                    "OG_DEPLOYMENT_ENVIRONMENT": "production",
                    "OG_OPERATION_ID": "",
                    "OG_WAIT_TIMEOUT": "2m",
                    "OG_CTL": str(fake_ctl),
                    "GITHUB_OUTPUT": str(gh_out),
                    "GITHUB_ACTION_PATH": str(GHA_DIR),
                    "RUNNER_TEMP": str(runner_temp),
                    "TMPDIR": str(tmp_path),
                    "GITHUB_REPOSITORY": "alibaba/open-guardrail",
                    "GITHUB_RUN_ID": "12345",
                    "GITHUB_RUN_ATTEMPT": "1",
                    "GITHUB_SHA": "abc123",
                    "GITHUB_REF": "refs/heads/main",
                }
            )
            if extra_env:
                env.update(extra_env)

            proc = subprocess.run(
                ["bash", "-c", self.script],
                env=env,
                capture_output=True,
                text=True,
                check=False,
            )
            outputs = parse_github_output(gh_out)
            self.output_lines = gh_out.read_text(encoding="utf-8").splitlines()
            calls = []
            if args_log.exists():
                for line in args_log.read_text(encoding="utf-8").splitlines():
                    calls.append(json.loads(line))
            cred_mode = None
            cred_file = outputs.get("submission-credential-file")
            if cred_file and pathlib.Path(cred_file).exists():
                cred_mode = stat.S_IMODE(pathlib.Path(cred_file).stat().st_mode)
            submitted = tmp_path / "submitted.json"
            self.accepted_snapshot = json.loads(submitted.read_text()) if submitted.exists() else None
            self.remaining_credentials = list(tmp_path.rglob("submission-credential.json"))
            return proc, outputs, calls, cred_mode

    def _evaluation_responses(self, verdict="PASS"):
        return {
            "create": {"id": "run_eval", "status": "OPEN", "latestSubmissionNo": 0,
                       "policyRevisionId": "rev_1", "specDigest": "digest_1"},
            "revision": {"id": "rev_1", "digest": "digest_1",
                         "yaml": (GHA_DIR / "release-gate.yaml").read_text()},
            "get": {"id": "run_eval", "status": "FINISHED", "sceneKey": "release-gate",
                    "changeSet": WORKFLOW_CONTEXT, "result": verdict,
                    "decision": "ALLOW" if verdict == "PASS" else "DENY",
                    "decisionExpiresAt": int(time.time()) + 600},
        }

    def _sample_command(self, filename):
        return shlex.join([sys.executable, str(GHA_DIR / "example/assess.py"),
                           "--signals", str(GHA_DIR / "example" / filename)])

    def test_evaluate_complete_flow_submits_every_outcome_and_cleans_credential(self):
        for sample, verdict in (("pass", "PASS"), ("blocked", "BLOCKED"), ("unknown", "UNKNOWN")):
            with self.subTest(sample=sample):
                proc, values, calls, _ = self._run_action(
                    "evaluate", self._evaluation_responses(verdict),
                    extra_env={"OG_ASSESSMENT_COMMAND": self._sample_command(sample + ".json"),
                               "OG_SPEC_FILE_INPUT": str(GHA_DIR / "release-gate.yaml"),
                               "OG_OPERATION_ID": "attempt-1"},
                )
                self.assertEqual(proc.returncode == 0, verdict == "PASS", proc.stdout + proc.stderr)
                self.assertEqual(values["result"], verdict)
                self.assertEqual([call[:2] for call in calls], [
                    ["scene", "apply"], ["run", "create"], ["run", "revision"],
                    ["run", "submit"], ["run", "get"],
                ])
                self.assertIn("attempt-1-submit", calls[3])
                self.assertEqual(self.accepted_snapshot["verdict"], verdict)
                self.assertEqual([c["checkId"] for c in self.accepted_snapshot["checks"]],
                                 ["freeze-window", "compatibility-and-blast-radius", "rollback-readiness"])
                self.assertEqual(self.output_lines.count("run-id=run_eval"), 1)
                credential = pathlib.Path(calls[1][calls[1].index("--submission-credential-file") + 1])
                submission = pathlib.Path(calls[3][calls[3].index("--submission") + 1])
                self.assertNotEqual(credential.parent, submission.parent)
                self.assertEqual(self.remaining_credentials, [])
                self.assertNotIn("submission-credential-file", values)

    def test_evaluate_missing_source_is_unknown_and_is_submitted(self):
        proc, _, _, _ = self._run_action(
            "evaluate", self._evaluation_responses("UNKNOWN"),
            extra_env={"OG_ASSESSMENT_COMMAND": self._sample_command("missing.json")},
        )
        self.assertNotEqual(proc.returncode, 0)
        self.assertEqual(self.accepted_snapshot["verdict"], "UNKNOWN")

    def test_evaluate_accepts_user_defined_spec_and_assessment(self):
        responses = self._evaluation_responses()
        responses["revision"]["yaml"] = "kind: GuardrailSpec\nspec:\n  checks:\n    - id: artifact-scan\n"
        command = shlex.join([sys.executable, "-c", (
            "import json,os,pathlib; "
            "assert 'artifact-scan' in pathlib.Path(os.environ['OG_SPEC_FILE']).read_text(); "
            "run=json.loads(pathlib.Path(os.environ['OG_RUN_FILE']).read_text()); "
            "snapshot={'baseSubmissionNo':run['latestSubmissionNo'],'finishRun':True,"
            "'verdict':'PASS','summary':'Artifact verified','checks':[{'checkId':'artifact-scan',"
            "'verdict':'PASS','summary':'Scan passed','evidence':[]}]}; "
            "pathlib.Path(os.environ['OG_SUBMISSION_FILE']).write_text(json.dumps(snapshot))"
        )])
        proc, _, calls, _ = self._run_action(
            "evaluate", responses, extra_env={"OG_ASSESSMENT_COMMAND": command},
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertEqual(self.accepted_snapshot["checks"][0]["checkId"], "artifact-scan")
        self.assertNotIn(["scene", "apply"], [call[:2] for call in calls])

    def test_evaluate_does_not_create_without_assessment_command(self):
        proc, _, calls, _ = self._run_action("evaluate", {})
        self.assertEqual(proc.returncode, 2)
        self.assertEqual(calls, [])

    def test_invalid_context_never_publishes_a_policy(self):
        proc, _, calls, _ = self._run_action(
            "evaluate", {}, extra_env={
                "OG_ASSESSMENT_COMMAND": "true", "OG_CHANGE_SET_JSON": "not-json",
                "OG_SPEC_FILE_INPUT": str(GHA_DIR / "release-gate.yaml"),
            },
        )
        self.assertEqual(proc.returncode, 2)
        self.assertEqual(calls, [])

    def test_missing_spec_is_reported_before_any_service_write(self):
        proc, _, calls, _ = self._run_action(
            "evaluate", {}, extra_env={"OG_ASSESSMENT_COMMAND": "true",
                                       "OG_SPEC_FILE_INPUT": str(GHA_DIR / "missing.yaml")},
        )
        self.assertEqual(proc.returncode, 2)
        self.assertIn("spec-file must point", proc.stdout)
        self.assertEqual(calls, [])

    def test_missing_server_or_cli_has_actionable_error(self):
        for env, expected in (({"OG_SERVER": ""}, "server is required"),
                              ({"OG_CTL": "/nonexistent/guardrailctl"}, "Required command")):
            with self.subTest(env=env):
                proc, _, calls, _ = self._run_action("create", {}, extra_env=env)
                self.assertEqual(proc.returncode, 2)
                self.assertIn(expected, proc.stdout)
                self.assertEqual(calls, [])

    def test_evaluate_failure_or_missing_snapshot_never_submits(self):
        for command in ("exit 7", "true", "printf '{\"finishRun\":false}' > \"$OG_SUBMISSION_FILE\""):
            with self.subTest(command=command):
                proc, _, calls, _ = self._run_action(
                    "evaluate", self._evaluation_responses(),
                    extra_env={"OG_ASSESSMENT_COMMAND": command},
                )
                self.assertNotEqual(proc.returncode, 0)
                self.assertNotIn(["run", "submit"], [call[:2] for call in calls])
                self.assertEqual(self.remaining_credentials, [])

    def test_evaluate_rejects_wrong_frozen_revision_before_assessment(self):
        responses = self._evaluation_responses()
        responses["revision"]["digest"] = "wrong"
        proc, _, calls, _ = self._run_action(
            "evaluate", responses, extra_env={"OG_ASSESSMENT_COMMAND": "exit 9"},
        )
        self.assertEqual(proc.returncode, 1)
        self.assertIn("frozen identity", proc.stdout)
        self.assertEqual([call[:2] for call in calls], [["run", "create"], ["run", "revision"]])
        self.assertEqual(self.remaining_credentials, [])

    def test_verify_cannot_publish_a_spec(self):
        proc, _, calls, _ = self._run_action(
            "verify", {}, extra_env={"OG_SPEC_FILE_INPUT": str(GHA_DIR / "release-gate.yaml")},
        )
        self.assertEqual(proc.returncode, 2)
        self.assertEqual(calls, [])

    def test_create_mode_persists_private_credential_and_forwards_flags(self):
        proc, outputs, calls, cred_mode = self._run_action(
            "create",
            {"create": {"id": "run_123", "status": "OPEN"}},
            extra_env={
                "OG_BUSINESS_ID": "gha-12345-1",
                "OG_OPERATION_ID": "op-12345-create",
            },
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(outputs["run-id"], "run_123")
        self.assertEqual(cred_mode, 0o600)
        self.assertEqual(len(calls), 1)
        self.assertIn("--submission-credential-file", calls[0])
        self.assertIn("--business-id", calls[0])
        self.assertIn("gha-12345-1", calls[0])
        self.assertIn("--operation-id", calls[0])

    def test_gate_mode_allows_unexpired_pass(self):
        future = int(time.time()) + 600
        proc, outputs, calls, _ = self._run_action(
            "gate",
            {
                "wait": {
                    "id": "run_456",
                    "sceneKey": "release-gate",
                    "changeSet": WORKFLOW_CONTEXT,
                    "status": "FINISHED",
                    "result": "PASS",
                    "decision": "ALLOW",
                    "decisionExpiresAt": future,
                },
            },
            extra_env={"OG_RUN_ID": "run_456"},
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(outputs["run-id"], "run_456")
        self.assertEqual(outputs["decision"], "ALLOW")
        self.assertEqual(outputs["result"], "PASS")
        self.assertEqual(calls, [["run", "wait", "--server", "http://127.0.0.1:8080", "--timeout", "2m", "run_456"]])

    def test_gate_requires_existing_run_without_creating_one(self):
        proc, _, calls, _ = self._run_action("gate", {})
        self.assertNotEqual(proc.returncode, 0)
        self.assertEqual(calls, [])

    def test_gate_mode_wait_failure_reports_summary_and_preserves_exit_code(self):
        for code, error in ((1, "Run EXPIRED"), (2, "invalid timeout: 300"), (3, "wait timed out")):
            with self.subTest(error=error):
                proc, outputs, calls, _ = self._run_action(
                    "gate",
                    {"wait_exit_code": code, "wait_error": error},
                    extra_env={"OG_RUN_ID": "run_failed"},
                )
                self.assertEqual(proc.returncode, code)
                self.assertIn(error, proc.stderr)
                self.assertIn("::error::OpenGuardrail blocked deployment: run wait failed", proc.stdout)
                self.assertIn("runId=run_failed", proc.stdout)
                self.assertEqual(outputs["decision"], "DENY")
                self.assertEqual(outputs["result"], "UNKNOWN")
                self.assertEqual(len(calls), 1)

    def test_verify_mode_blocks_on_deny_or_expired_or_missing_expiry(self):
        now = int(time.time())
        cases = [
            {"id": "run_1", "status": "FINISHED", "result": "BLOCKED", "decision": "DENY", "decisionExpiresAt": now + 600},
            {"id": "run_2", "status": "FINISHED", "result": "PASS", "decision": "ALLOW", "decisionExpiresAt": now - 10},
            {"id": "run_3", "status": "FINISHED", "result": "PASS", "decision": "ALLOW"},
            {"id": "run_4", "status": "FINISHED", "result": "PASS", "decision": "ALLOW", "decisionExpiresAt": 10**100},
        ]
        for payload in cases:
            payload.update(sceneKey="release-gate", changeSet=WORKFLOW_CONTEXT)
            proc, _, calls, _ = self._run_action(
                "verify",
                {"get": payload},
                extra_env={"OG_RUN_ID": payload["id"]},
            )
            self.assertNotEqual(proc.returncode, 0, f"expected failure for {payload}")
            self.assertEqual(calls[0], ["run", "get", "--server", "http://127.0.0.1:8080", payload["id"]])

    def test_verify_matches_trusted_deployment_identity(self):
        valid = {
            "id": "run_1", "sceneKey": "release-gate", "businessId": "gha-12345-1",
            "changeSet": WORKFLOW_CONTEXT, "status": "FINISHED", "result": "PASS",
            "decision": "ALLOW", "decisionExpiresAt": int(time.time()) + 600,
        }
        cases = [
            ({}, True),
            ({"id": "run_other"}, False),
            ({"sceneKey": "weaker-policy"}, False),
            ({"businessId": "another-release"}, False),
            ({"changeSet": dict(WORKFLOW_CONTEXT, sha="other-commit")}, False),
            ({"changeSet": dict(WORKFLOW_CONTEXT, workflowRunAttempt="2")}, False),
            ({"changeSet": dict(WORKFLOW_CONTEXT, environment="staging")}, False),
            ({"changeSet": None}, False),
        ]
        for changes, allowed in cases:
            with self.subTest(changes=changes):
                proc, _, _, _ = self._run_action(
                    "verify", {"get": dict(valid, **changes)},
                    extra_env={"OG_RUN_ID": "run_1", "OG_BUSINESS_ID": "gha-12345-1"},
                )
                self.assertEqual(proc.returncode == 0, allowed, proc.stdout + proc.stderr)

    def test_verify_custom_context_includes_exact_target_and_artifact(self):
        context = dict(WORKFLOW_CONTEXT, environment="production", artifact="sha256:123")
        payload = {
            "id": "run_1", "sceneKey": "release-gate", "changeSet": context,
            "status": "FINISHED", "result": "PASS", "decision": "ALLOW",
            "decisionExpiresAt": int(time.time()) + 600,
        }
        for expected, allowed in ((context, True), (dict(context, environment="staging"), False)):
            with self.subTest(expected=expected):
                proc, _, _, _ = self._run_action(
                    "verify", {"get": payload},
                    extra_env={"OG_RUN_ID": "run_1", "OG_CHANGE_SET_JSON": json.dumps(expected)},
                )
                self.assertEqual(proc.returncode == 0, allowed, proc.stdout + proc.stderr)

    def test_invalid_mode_fails_closed(self):
        proc, _, _, _ = self._run_action("invalid-mode", {})
        self.assertEqual(proc.returncode, 2)


if __name__ == "__main__":
    unittest.main()
