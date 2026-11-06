"""Contract tests for the outer Agent client; no Jenkins or model is contacted."""

import json
from io import BytesIO
from urllib.error import HTTPError
import stat
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest import mock

from integrations.jenkins import agent_client
from integrations.recovery_agent import gate
from integrations.recovery_agent.test_gate import snapshot


class AgentClientTest(unittest.TestCase):
    def test_start_passes_only_public_key_and_case(self):
        with tempfile.TemporaryDirectory() as root:
            work = Path(root) / "private"

            def fake_command(*args, **kwargs):
                if args[1] == "-o":
                    Path(args[2]).write_text("private-test-key")
                    return ""
                return "age1testrecipient\n"

            headers = {"Location": "https://jenkins.example/queue/item/42/"}
            with mock.patch.object(agent_client.gate, "command", side_effect=fake_command), \
                 mock.patch.object(agent_client, "jenkins_request",
                                   return_value=("https://jenkins.example/job/demo/buildWithParameters",
                                                 headers, b"")) as request:
                agent_client.start(SimpleNamespace(job_url="https://jenkins.example/job/demo",
                                                   case="healthy", work_dir=str(work)))
            self.assertIn(b"AGENT_AGE_RECIPIENT=age1testrecipient", request.call_args.kwargs["data"])
            self.assertNotIn(b"private-test-key", request.call_args.kwargs["data"])
            self.assertNotIn(b"GUARDRAIL_URL", request.call_args.kwargs["data"])
            self.assertEqual(stat.S_IMODE((work / "agent-identity.txt").stat().st_mode), 0o600)

    def test_start_requires_a_fresh_work_directory(self):
        with tempfile.TemporaryDirectory() as root:
            with self.assertRaisesRegex(ValueError, "fresh --work-dir"):
                agent_client.start(SimpleNamespace(job_url="https://jenkins.example/job/demo",
                                                   case="healthy", work_dir=root))

    def test_receive_and_submit_use_run_token_without_project_key(self):
        with tempfile.TemporaryDirectory() as root:
            work = Path(root) / "private"
            work.mkdir(mode=0o700)
            gate.private_json(work / "state.json", {
                "jobUrl": "https://jenkins.example/job/demo",
                "queueUrl": "https://jenkins.example/queue/item/42/",
            })
            (work / "agent-identity.txt").write_text("test-only")
            observation = {"status": "healthy", "httpStatus": 200}
            bundle = {"runId": "run_test", "operationPrefix": "gate-test",
                      "baseSubmissionNo": 0, "policyRevisionId": "rev_test", "specDigest": "digest",
                      "specSnapshot": "kind: GuardrailSpec", "observation": observation,
                      "runToken": {"runId": "run_test", "token": "ogrt_test",
                                   "server": "https://guardrail.example"}}

            def fake_jenkins(url, **kwargs):
                if "/queue/item/" in url:
                    return url, {}, json.dumps({"executable": {
                        "url": "https://jenkins.example/job/demo/42/"}}).encode()
                return url, {}, b"ciphertext"

            def fake_command(*args, **kwargs):
                if args[0] == "age":
                    gate.private_json(Path(args[args.index("-o") + 1]), bundle)
                    return ""
                self.assertEqual(args[1:3], ("run", "submit"))
                self.assertNotIn("--key-file", args)
                self.assertEqual(kwargs["env"]["OPEN_GUARDRAIL_URL"], "https://guardrail.example")
                self.assertEqual(json.loads(Path(args[args.index("--submission-credential-file") + 1]).read_text()),
                                 bundle["runToken"])
                return "{}"

            with mock.patch.object(agent_client, "jenkins_request", side_effect=fake_jenkins), \
                 mock.patch.object(agent_client.gate, "command", side_effect=fake_command):
                agent_client.receive(SimpleNamespace(work_dir=str(work), timeout_seconds=10))
                submission = work / "submission.json"
                gate.private_json(submission, snapshot(observation))
                agent_client.submit(SimpleNamespace(work_dir=str(work), submission=str(submission),
                                                    cli="guardrailctl"))
            self.assertFalse((work / "handoff.json").exists())
            self.assertFalse((work / "run-token.json").exists())

    def test_receive_replaces_interrupted_envelope_with_fresh_ciphertext(self):
        with tempfile.TemporaryDirectory() as root:
            work = Path(root) / "private"
            work.mkdir(mode=0o700)
            gate.private_json(work / "state.json", {
                "jobUrl": "https://jenkins.example/job/demo",
                "queueUrl": "https://jenkins.example/queue/item/42/",
            })
            (work / "agent-identity.txt").write_text("test-only")
            (work / "gate-envelope.age").write_bytes(b"damaged")

            def fake_jenkins(url, **kwargs):
                if "/queue/item/" in url:
                    return url, {}, json.dumps({"executable": {
                        "url": "https://jenkins.example/job/demo/42/"}}).encode()
                return url, {}, b"fresh ciphertext"

            def fake_command(*args, **kwargs):
                self.assertEqual(Path(args[-1]).read_bytes(), b"fresh ciphertext")
                gate.private_json(Path(args[args.index("-o") + 1]), {
                    "runId": "run_test", "runToken": {"runId": "run_test"},
                    "baseSubmissionNo": 0, "policyRevisionId": "rev_test", "specDigest": "digest",
                      "specSnapshot": "kind: GuardrailSpec", "observation": {"status": "healthy"}})
                return ""

            with mock.patch.object(agent_client, "jenkins_request", side_effect=fake_jenkins), \
                 mock.patch.object(agent_client.gate, "command", side_effect=fake_command):
                agent_client.receive(SimpleNamespace(work_dir=str(work), timeout_seconds=10))
            self.assertEqual((work / "gate-envelope.age").read_bytes(), b"fresh ciphertext")
            self.assertEqual(stat.S_IMODE((work / "gate-envelope.age").stat().st_mode), 0o600)

    def test_receive_preserves_previous_envelope_if_atomic_replace_fails(self):
        with tempfile.TemporaryDirectory() as root:
            work = Path(root) / "private"
            work.mkdir(mode=0o700)
            gate.private_json(work / "state.json", {
                "jobUrl": "https://jenkins.example/job/demo",
                "queueUrl": "https://jenkins.example/queue/item/42/",
            })
            envelope = work / "gate-envelope.age"
            envelope.write_bytes(b"previous ciphertext")

            def fake_jenkins(url, **kwargs):
                if "/queue/item/" in url:
                    return url, {}, json.dumps({"executable": {
                        "url": "https://jenkins.example/job/demo/42/"}}).encode()
                return url, {}, b"fresh ciphertext"

            with mock.patch.object(agent_client, "jenkins_request", side_effect=fake_jenkins), \
                 mock.patch.object(agent_client.os, "replace", side_effect=OSError("disk error")):
                with self.assertRaisesRegex(OSError, "disk error"):
                    agent_client.receive(SimpleNamespace(work_dir=str(work), timeout_seconds=10))
            self.assertEqual(envelope.read_bytes(), b"previous ciphertext")
            self.assertEqual(list(work.glob("gate-envelope-*.age")), [])

    def test_jenkins_url_rejects_remote_http_and_credentials(self):
        for url in ("http://jenkins.example/job/demo", "https://user:pass@jenkins.example/",
                    "https://jenkins.example/?token=secret"):
            with self.subTest(url=url), self.assertRaises(ValueError):
                agent_client.safe_origin(url)

    def test_request_rejects_unsafe_url_before_sending_credentials(self):
        with mock.patch.object(agent_client, "build_opener") as opener:
            with self.assertRaises(ValueError):
                agent_client.jenkins_request("http://jenkins.example/api/json")
            opener.assert_not_called()

    def test_request_bounds_response_and_closes_stream(self):
        response = mock.MagicMock()
        response.__enter__.return_value = response
        response.read.return_value = b"x" * (agent_client.MAX_RESPONSE_BYTES + 1)
        with mock.patch.dict(agent_client.os.environ, {"JENKINS_USER": "test", "JENKINS_API_TOKEN": "test-only"}), \
             mock.patch.object(agent_client, "build_opener") as opener:
            opener.return_value.open.return_value = response
            with self.assertRaisesRegex(ValueError, "exceeds 1 MiB"):
                agent_client.jenkins_request("https://jenkins.example/api/json")
            response.read.assert_called_once_with(agent_client.MAX_RESPONSE_BYTES + 1)
            response.__exit__.assert_called_once()

    def test_request_closes_http_error_streams(self):
        for code in (303, 403):
            with self.subTest(code=code):
                stream = BytesIO(b"test-only response")
                error = HTTPError("https://jenkins.example/", code, "test", {"Location": "/queue/item/42/"}, stream)
                with mock.patch.dict(agent_client.os.environ, {"JENKINS_USER": "test", "JENKINS_API_TOKEN": "test-only"}), \
                     mock.patch.object(agent_client, "build_opener") as opener:
                    opener.return_value.open.side_effect = error
                    if code == 303:
                        agent_client.jenkins_request("https://jenkins.example/", data=b"case=healthy")
                    else:
                        with self.assertRaises(HTTPError):
                            agent_client.jenkins_request("https://jenkins.example/", data=b"case=healthy")
                self.assertTrue(stream.closed)

    def test_wait_reports_jenkins_result_without_claiming_decision(self):
        with tempfile.TemporaryDirectory() as root:
            work = Path(root)
            gate.private_json(work / "state.json", {
                "jobUrl": "https://jenkins.example/job/demo",
                "queueUrl": "https://jenkins.example/queue/item/42/",
            })
            for result, expected in (("SUCCESS", 0), ("FAILURE", 1)):
                def fake_request(url, **kwargs):
                    if "/queue/item/" in url:
                        body = {"executable": {"url": "https://jenkins.example/job/demo/42/"}}
                    else:
                        body = {"building": False, "result": result}
                    return url, {}, json.dumps(body).encode()

                with mock.patch.object(agent_client, "jenkins_request", side_effect=fake_request):
                    self.assertEqual(agent_client.wait_build(SimpleNamespace(
                        work_dir=str(work), timeout_seconds=1)), expected)


if __name__ == "__main__":
    unittest.main()
