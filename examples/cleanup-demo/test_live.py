"""Opt-in live-service and HTTP checks: CLEANUP_DEMO_BINARIES=/absolute/bin/."""

import importlib.util
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import threading
import time
import unittest
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


spec = importlib.util.spec_from_file_location("cleanup_live_demo", Path(__file__).with_name("demo.py"))
demo = importlib.util.module_from_spec(spec)
spec.loader.exec_module(demo)


@unittest.skipUnless(os.environ.get("CLEANUP_DEMO_BINARIES"), "set CLEANUP_DEMO_BINARIES for live checks")
class LiveDemoTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="cleanup-live-test-")
        cls.private = Path(cls.temporary.name)
        binaries = Path(os.environ["CLEANUP_DEMO_BINARIES"])
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        cls.runtime = f"http://127.0.0.1:{port}"
        cls.process = subprocess.Popen((str(binaries / "open-guardrail"), "serve", "--listen",
                                        f"127.0.0.1:{port}", "--data-dir", str(cls.private / "data")),
                                       cwd=cls.private, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                       env={k: v for k, v in os.environ.items()
                                            if not k.startswith("OPEN_GUARDRAIL_") and k != "DATABASE_URL"})
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.addClassCleanup(cls.stop_process)
        deadline = time.monotonic() + 15
        while True:
            try:
                with urlopen(cls.runtime + "/readyz", timeout=.2):
                    break
            except (URLError, TimeoutError):
                if cls.process.poll() is not None or time.monotonic() >= deadline:
                    raise RuntimeError("test service did not start")
                time.sleep(.05)
        cls.gateway = demo.Gateway(binaries / "guardrailctl", cls.runtime, cls.private)
        cls.gateway.setup(secrets.token_urlsafe(24))
        cls.addClassCleanup(cls.gateway.close)
        cls.app = demo.Demo(cls.gateway, cls.private)
        cls.addClassCleanup(lambda: cls.app.tool.close())
        cls.web = demo.ThreadingHTTPServer(("127.0.0.1", 0), demo.handler(cls.app))
        cls.origin = f"http://127.0.0.1:{cls.web.server_port}"
        cls.thread = threading.Thread(target=cls.web.serve_forever, daemon=True)
        cls.thread.start()
        cls.addClassCleanup(cls.stop_web)

    @classmethod
    def stop_process(cls):
        cls.process.terminate()
        cls.process.wait(timeout=5)

    @classmethod
    def stop_web(cls):
        cls.web.shutdown()
        cls.web.server_close()
        cls.thread.join(timeout=5)

    def request(self, path, value=None, headers=None):
        defaults = {"Origin": self.origin, "X-Demo-Token": self.app.csrf, "Content-Type": "application/json"}
        defaults.update(headers or {})
        body = None if value is None else json.dumps(value).encode()
        with urlopen(Request(self.origin + path, data=body, headers=defaults), timeout=10) as response:
            return json.load(response)

    def task(self, targets, limit=2, repair=True):
        self.request("/api/reset", {})
        self.request("/api/tasks", {"targets": targets, "maxFiles": limit, "repair": repair})
        deadline = time.monotonic() + 20
        while self.app.busy:
            if time.monotonic() > deadline:
                self.fail("task did not finish")
            time.sleep(.05)
        state = self.request("/api/state")
        self.assertNotEqual(state["jobs"][-1]["status"], "error")
        return state, state["jobs"][-1]

    def test_denial_keeps_actual_files_and_persists_evidence(self):
        state, job = self.task(list(demo.SEEDS), repair=False)
        self.assertEqual(len(state["files"]), 4)
        self.assertEqual(job["status"], "denied")
        record = job["records"][0]
        self.assertEqual((record["result"], record["decision"]), ("BLOCKED", "DENY"))
        self.assertEqual(record["storedSubmission"]["snapshot"]["verdict"], "BLOCKED")
        self.assertTrue(all((Path(state["workspace"]) / name).is_file() for name in demo.SEEDS))

    def test_repair_creates_two_real_runs_and_deletes_only_temporary_files(self):
        state, job = self.task(list(demo.SEEDS))
        self.assertEqual([r["decision"] for r in job["records"]], ["DENY", "ALLOW"])
        self.assertNotEqual(job["records"][0]["runId"], job["records"][1]["runId"])
        self.assertEqual({f["name"] for f in state["files"]}, {"orders.csv", "config.yaml"})
        for record in job["records"]:
            final = self.gateway.cli("run", "get", "--key-file", str(self.gateway.key), record["runId"])
            self.assertEqual(final["decision"], record["decision"])
            self.assertEqual(final["policyRevisionId"], record["revision"]["id"])
            self.assertEqual(record["revision"]["yaml"], demo.policy_yaml(2))
            self.assertTrue(record["revision"]["yaml"].startswith("apiVersion: guardrail.open/v1alpha1\n"))
        self.assertEqual((Path(state["workspace"]) / "orders.csv").read_bytes(), demo.SEEDS["orders.csv"])

    def test_rule_change_publishes_a_revision_and_changes_the_actual_effect(self):
        state, job = self.task(["cache.tmp", "download.tmp"], limit=1)
        self.assertEqual([r["decision"] for r in job["records"]], ["DENY", "ALLOW"])
        self.assertEqual(job["removed"], ["cache.tmp"])
        self.assertEqual(len(state["files"]), 3)
        frozen = job["records"][0]["revision"]["yaml"]
        self.assertEqual(frozen, demo.policy_yaml(1))
        self.assertIn("The maximum number of files in one request is 1.", frozen)
        self.assertNotIn("${max_files}", frozen)
        self.assertEqual(len({r["revision"]["id"] for r in job["records"]}), 1)

    def test_run_with_different_frozen_yaml_stops_before_submission(self):
        directory = self.private / "policy-mismatch"
        directory.mkdir(mode=0o700)
        self.gateway.publish(demo.policy_yaml(1), directory)
        tool = demo.FileTool(self.private)
        try:
            with self.assertRaisesRegex(ValueError, "Run policy differs"):
                self.gateway.evaluate(tool, ["cache.tmp", "download.tmp"], 2,
                                      demo.policy_yaml(2), directory / "mismatch-attempt")
            self.assertEqual(len(tool.inventory()), 4)
            opened = self.gateway.cli("run", "list", "--key-file", str(self.gateway.key),
                                      "--scene", "file-cleanup", "--status", "OPEN")["items"]
            self.assertEqual(len(opened), 1)
            run = self.gateway.cli("run", "get", "--key-file", str(self.gateway.key), opened[0]["id"])
            self.assertEqual(run["changeSet"]["workspaceId"], tool.workspace_id)
            self.assertEqual(run["latestSubmissionNo"], 0)
        finally:
            tool.close()

    def test_http_boundary_rejects_cross_origin_wrong_host_token_paths_and_large_bodies(self):
        before = len(self.app.jobs)
        for headers in ({"Origin": "https://attacker.invalid"}, {"X-Demo-Token": "wrong"},
                        {"Host": "attacker.invalid"}, {"Sec-Fetch-Site": "cross-site"}):
            with self.assertRaises(HTTPError) as rejected:
                self.request("/api/reset", {}, headers)
            self.assertEqual(rejected.exception.code, 403)
        with self.assertRaises(HTTPError) as rejected:
            self.request("/api/tasks", {"targets": ["../outside.tmp"], "maxFiles": 2, "repair": False})
        self.assertEqual(rejected.exception.code, 409)
        with self.assertRaises(HTTPError) as rejected:
            self.request("/api/tasks", {"oversized": "a" * 4096})
        self.assertEqual(rejected.exception.code, 400)
        with self.assertRaises(HTTPError) as rejected:
            self.request("/key.json")
        self.assertEqual(rejected.exception.code, 404)
        self.assertEqual(len(self.app.jobs), before)


if __name__ == "__main__":
    unittest.main()
