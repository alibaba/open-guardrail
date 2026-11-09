#!/usr/bin/env python3
"""Private, serial stdio adapter for the isolated cleanup example (not a server API)."""

from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import signal
import socket
import subprocess
import sys
import tempfile
import time
from urllib.error import URLError
from urllib.request import urlopen


HERE = Path(__file__).resolve().parent
source = HERE.parents[1] / "examples" / "cleanup-demo" / "demo.py"
if not source.exists():
    source = HERE / "assets" / "cleanup-demo" / "demo.py"
spec = importlib.util.spec_from_file_location("guardrail_cleanup", source)
demo = importlib.util.module_from_spec(spec)
spec.loader.exec_module(demo)


class InputError(ValueError):
    """A static, credential-free diagnostic intended for the caller."""


class Cleanup:
    def __init__(self, binaries):
        self.temporary = tempfile.TemporaryDirectory(prefix="guardrail-plugin-")
        self.private = Path(self.temporary.name)
        self.process = self.tool = self.gateway = None
        self.records = {}
        self.requests = {}
        try:
            if os.name != "posix":
                raise InputError("This preview requires Linux or macOS POSIX file operations")
            binaries = Path(binaries)
            if not binaries.is_absolute():
                raise InputError("Configure an absolute OPEN_GUARDRAIL_BINARIES directory")
            for name in ("open-guardrail", "guardrailctl"):
                if not (binaries / name).is_file() or not os.access(binaries / name, os.X_OK):
                    raise InputError("Build both native binaries before starting the plugin")
            with socket.socket() as listener:
                listener.bind(("127.0.0.1", 0))
                port = listener.getsockname()[1]
            self.origin = f"http://127.0.0.1:{port}"
            with (self.private / "server.log").open("w") as log:
                self.process = subprocess.Popen(
                    [str(binaries / "open-guardrail"), "serve", "--listen", f"127.0.0.1:{port}",
                     "--data-dir", str(self.private / "data")], cwd=self.private,
                    env={k: os.environ[k] for k in ("PATH", "HOME", "LANG", "TMPDIR") if k in os.environ},
                    stdout=log, stderr=log)
            deadline = time.monotonic() + 30
            while True:
                if self.process.poll() is not None or time.monotonic() > deadline:
                    raise RuntimeError("The isolated OpenGuardrail service did not become ready")
                try:
                    with urlopen(self.origin + "/readyz", timeout=.5):
                        break
                except (URLError, TimeoutError):
                    time.sleep(.05)
            self.gateway = demo.Gateway(binaries / "guardrailctl", self.origin, self.private)
            self.capabilities = json.loads(demo.command(str(binaries / "guardrailctl"),
                                                       "--server", self.origin, "capabilities"))
            self.gateway.setup(secrets.token_urlsafe(32))
            self.policy = demo.policy_yaml(2)
            self.gateway.publish(self.policy, self.private)
            self.tool = demo.FileTool(self.private)
        except BaseException:
            self.close()
            raise

    def status(self, runId=None):
        with urlopen(self.origin + "/readyz", timeout=2):
            pass
        selected = [self.records[runId]] if runId is not None else list(self.records.values())[-10:]
        records = []
        for record in selected:
            final = self.gateway.cli("run", "get", "--key-file", str(self.gateway.key), record["runId"])
            item = {"runId": record["runId"], "removed": record["removed"]}
            if runId is None:
                item["run"] = {key: final[key] for key in ("id", "status", "result", "decision",
                    "decisionExpiresAt", "policyRevisionId", "specDigest", "finalSubmissionId") if key in final}
            else:
                item.update(run=final, revision=record["revision"], acceptedSubmission=(
                    self.gateway.cli("run", "submission", "--key-file", str(self.gateway.key),
                        "--submission-id", final["finalSubmissionId"], record["runId"])
                    if final.get("finalSubmissionId") else None))
            records.append(item)
        return {"mode": "isolated-cleanup-preview", "server": self.origin,
                "capabilities": self.capabilities,
                "files": self.tool.inventory(), "maxFiles": 2,
                "retainedRunCount": len(self.records), "runs": records}

    def prepare(self, requestId, targets):
        if not isinstance(requestId, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,80}", requestId):
            raise InputError("Use a stable requestId of 1-80 letters, digits, underscores or hyphens")
        observation = self.tool.observe(targets)
        targets = observation["targets"]
        if requestId in self.requests:
            record = self.requests[requestId]
            if record["targets"] != targets:
                raise InputError("This requestId already belongs to different targets")
            return self.view(record)
        if len(self.records) >= 64:
            raise InputError("This preview session reached 64 Runs; inspect results before starting another session")
        # Keep uncertain creation state under the same operation identity; never
        # replace it automatically with a new Run or Submission credential.
        directory = self.private / ("request-" + requestId)
        try:
            directory.mkdir(mode=0o700)
        except FileExistsError:
            raise InputError("Creation outcome for this requestId is uncertain; inspect existing Runs before "
                             "continuing. Do not automatically retry under a new requestId") from None
        change = {"action": "delete-files", "workspaceId": self.tool.workspace_id,
                  "targets": targets, "maxFiles": 2,
                  "observationDigest": hashlib.sha256(demo.identity(observation).encode()).hexdigest()}
        demo.private_json(directory / "change.json", change)
        created = self.gateway.cli("run", "create", "--key-file", str(self.gateway.key),
                                  "--scene", "file-cleanup", "--change-set", str(directory / "change.json"),
                                  "--operation-id", requestId + "-create", "--submission-credential-file",
                                  str(directory / "credential.json"))
        revision = self.gateway.cli("run", "revision", "--key-file", str(self.gateway.key), created["id"])
        if (revision["id"] != created["policyRevisionId"] or revision["digest"] != created["specDigest"]
                or revision["yaml"] != self.policy):
            raise InputError("Run policy differs from the policy selected for this task")
        record = {"runId": created["id"], "targets": targets, "changeSet": change,
                  "revision": revision, "observation": observation, "directory": directory,
                  "requestId": requestId, "created": created, "removed": [],
                  "observedAt": datetime.now(timezone.utc).isoformat()}
        self.records[created["id"]] = record
        self.requests[requestId] = record
        return self.view(record)

    def view(self, record):
        return {"runId": record["runId"], "targets": record["targets"],
                "policy": record["revision"]["yaml"], "observation": record["observation"],
                "observedAt": record["observedAt"], "changeSet": record["changeSet"],
                "run": self.gateway.cli("run", "get", "--key-file", str(self.gateway.key), record["runId"])}

    def submit(self, runId, checks):
        record = self.records[runId]
        if not isinstance(checks, list) or len(checks) != 2:
            raise InputError("Supply both frozen policy checks with checkId, verdict and summary")
        if {c.get("checkId") for c in checks if isinstance(c, dict)} != {"temporary-only", "batch-limit"}:
            raise InputError("Supply exactly the frozen policy check IDs")
        supplied = []
        for check in sorted(checks, key=lambda c: ("temporary-only", "batch-limit").index(c["checkId"])):
            if (set(check) != {"checkId", "verdict", "summary"}
                    or check["verdict"] not in ("PASS", "BLOCKED", "UNKNOWN")
                    or not isinstance(check["summary"], str) or not check["summary"].strip()
                    or len(check["summary"]) > 1000):
                raise InputError("Each check needs a valid verdict and a nonempty summary up to 1000 characters")
            supplied.append({**check, "evidence": [{"requirementId": "files",
                "observedAt": record["observedAt"],
                "querySummary": "Read fixed demo filenames through the existing no-follow file tool",
                "resultSummary": demo.identity(record["observation"])}]})
        verdict = next((v for v in ("BLOCKED", "UNKNOWN") if any(c["verdict"] == v for c in supplied)), "PASS")
        snapshot = {"baseSubmissionNo": record["created"]["latestSubmissionNo"], "finishRun": True,
                    "verdict": verdict, "summary": "Agent cleanup assessment: " + verdict, "checks": supplied}
        path = record["directory"] / "submission.json"
        if path.exists():
            if demo.snapshot_identity(json.loads(path.read_text())) != demo.snapshot_identity(snapshot):
                raise InputError("A submission already exists; retry exactly the same checks")
        else:
            demo.private_json(path, snapshot)
        credential = record["directory"] / "credential.json"
        if "submission" not in record:
            self.gateway.cli("run", "submit", "--submission-credential-file", str(credential),
                             "--submission", str(path), "--operation-id", record["requestId"] + "-submit", runId)
            record["submission"] = snapshot
        # Acknowledged terminal submissions need only an authoritative read on
        # retry. Keep the credential only while the submit outcome is uncertain.
        credential.unlink(missing_ok=True)
        return self.view(record)

    def execute(self, runId, targets):
        record = self.records[runId]
        if self.tool.observe(targets)["targets"] != record["targets"]:
            raise InputError("Execution targets differ from the frozen request")
        if record["removed"]:
            raise InputError("Execution already started; inspect actual files before any retry")
        if "submission" not in record:
            raise InputError("Submit the frozen checks before executing")
        # The reused tool independently rechecks current files, business rules,
        # revision, authoritative Run and accepted evidence immediately before unlink.
        removed = self.tool.execute(self.gateway, record)
        return {"runId": runId, "removed": removed, "files": self.tool.inventory()}

    def call(self, method, params):
        if method not in ("status", "prepare", "submit", "execute") or not isinstance(params, dict):
            raise InputError("Unknown cleanup operation")
        return getattr(self, method)(**params)

    def close(self):
        if self.tool:
            self.tool.close()
            self.tool = None
        try:
            if self.gateway and self.process and self.process.poll() is None:
                self.gateway.close()
        except (OSError, RuntimeError, subprocess.TimeoutExpired):
            # This key exists only in the database that is being removed below.
            pass
        finally:
            if self.process and self.process.poll() is None:
                self.process.terminate()
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait()
            self.temporary.cleanup()


def main():
    signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt()))
    app = None
    try:
        for line in iter(lambda: sys.stdin.buffer.readline(1024 * 1024 + 1), b""):
            request_id = None
            try:
                if len(line) > 1024 * 1024:
                    raise InputError("Request is too large")
                request = json.loads(line)
                request_id = request["id"]
                if app is None:
                    app = Cleanup(os.environ.get("OPEN_GUARDRAIL_BINARIES", ""))
                value = app.call(request["method"], request.get("params", {}))
                result = {"id": request_id, "value": value}
            except Exception as error:
                detail = ({"code": "cleanup_refused", "message": str(error), "stage": "plugin"}
                          if isinstance(error, InputError) else demo.public_error(error, "plugin"))
                result = {"id": request_id, "error": detail}
            result["initialized"] = app is not None
            print(json.dumps(result, allow_nan=False), flush=True)
            if len(line) > 1024 * 1024:
                break
    except (KeyboardInterrupt, BrokenPipeError):
        pass
    finally:
        if app:
            app.close()


if __name__ == "__main__":
    main()
