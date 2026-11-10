#!/usr/bin/env python3
"""Policy, observations and guarded file operations for the file-cleanup scenario."""

from contextlib import nullcontext
from datetime import datetime, timezone
import errno
import hashlib
import json
import os
from pathlib import Path
import re
import stat
from string import Template
import subprocess
import time
import uuid


HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
SEEDS = {"cache.tmp": b"temporary cache\n", "download.tmp": b"temporary download\n",
         "orders.csv": b"order_id,amount\n1001,42\n", "config.yaml": b"environment: demo\n"}
TEMPORARY = frozenset(("cache.tmp", "download.tmp"))


def identity(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False)


def snapshot_identity(value):
    value = json.loads(identity(value))
    for check in value["checks"]:
        for evidence in check["evidence"]:
            # Normalize RFC3339Nano without losing precision on Python 3.9.
            raw = evidence["observedAt"]
            stamp = re.fullmatch(r"(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})",
                                 raw) if isinstance(raw, str) else None
            if stamp is None:
                raise ValueError("Evidence time requires RFC3339 with a timezone")
            whole, fraction, offset = stamp.groups()
            observed = datetime.fromisoformat(whole + offset.replace("Z", "+00:00"))
            seconds = observed.astimezone(timezone.utc).isoformat(timespec="seconds")
            evidence["observedAt"] = seconds[:-6] + "." + (fraction or "").ljust(9, "0") + "+00:00"
    return identity(value)


def private_json(path, value):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as output:
        json.dump(value, output)


class CommandFailed(RuntimeError):
    def __init__(self, operation, exit_code):
        self.operation = operation if operation in ("build", "serve", "bootstrap", "auth", "scene", "run") else "request"
        self.exit_code = exit_code
        component = {"build": "Go", "serve": "open-guardrail"}.get(self.operation, "guardrailctl")
        super().__init__(f"{component} {self.operation} failed (exit {exit_code}); no action was authorized by this command")


def public_error(error, stage):
    """Return bounded diagnostics without exception arguments, paths or output."""
    known = {
        "Install the Go version specified in go.mod to run from source": ("go_unavailable", "Install the Go version specified in go.mod, then retry the task."),
        "The isolated OpenGuardrail service did not become ready": ("service_unavailable", "The local OpenGuardrail service did not become ready. Check its startup and loopback port."),
        "Execution stopped: Run identity, decision or evidence mismatch": ("decision_mismatch", "The authoritative Run or accepted evidence does not match this request."),
        "Execution stopped: files changed, checks failed or decision expired": ("execution_conditions_changed", "The files, check results or decision validity changed before execution."),
        "Execution stopped: decision expired during deletion": ("decision_expired", "The decision expired during deletion. Inspect any completed file changes."),
        "Run policy differs from the policy selected for this task": ("policy_mismatch", "The Run's frozen policy does not match the policy selected for this task."),
        "The service did not persist the expected assessment": ("assessment_mismatch", "The stored assessment does not match the submitted checks."),
    }
    if isinstance(error, CommandFailed):
        code, message = "command_failed", str(error)
    elif isinstance(error, subprocess.TimeoutExpired):
        code, message = "command_timeout", "A service command timed out. Inspect existing Runs before retrying."
    elif isinstance(error, OSError):
        code, message = {
            errno.EADDRINUSE: ("port_in_use", "The selected loopback port is already in use."),
            errno.ENOENT: ("file_missing", "A required local file or executable is missing."),
            errno.EACCES: ("access_denied", "A required local file or executable could not be accessed."),
            errno.EPERM: ("access_denied", "A required local file or executable could not be accessed."),
        }.get(error.errno, ("file_operation_failed", "A local file operation failed. Inspect the actual file changes."))
    else:
        code, message = known.get(str(error), ("invalid_data", "Required data could not be verified.")
                                  if isinstance(error, ValueError) else ("unexpected_error", "The task stopped unexpectedly. Inspect existing Runs and file changes."))
    detail = {"code": code, "message": message, "stage": stage, "errorType": type(error).__name__}
    if isinstance(error, CommandFailed):
        detail["exitCode"] = error.exit_code
    return detail


def command(*args, input_text=None, cwd=None, timeout=40):
    # Do not inherit another service, database or administrator session.
    env = {k: v for k, v in os.environ.items()
           if not k.startswith("OPEN_GUARDRAIL_") and k != "DATABASE_URL"}
    env.update(GOWORK="off", CGO_ENABLED="0")
    result = subprocess.run(args, cwd=cwd or ROOT, env=env, input=input_text, text=True,
                            capture_output=True, timeout=timeout)
    if result.returncode:
        raise CommandFailed(args[1], result.returncode)
    return result.stdout


def policy_yaml(limit):
    if type(limit) is not int or limit not in (1, 2):
        raise ValueError("The file-cleanup batch limit must be 1 or 2")
    template = (HERE / "policy.yaml").read_text(encoding="utf-8")
    return Template(template).substitute(max_files=limit)


class FileTool:
    """Owns a disposable directory; accepts fixed basenames, never client paths."""

    def __init__(self, parent):
        self.workspace_id = uuid.uuid4().hex
        self.path = Path(parent) / self.workspace_id
        self.path.mkdir(mode=0o700)
        self.fd = os.open(self.path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        for name, data in SEEDS.items():
            with os.fdopen(os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL,
                                  0o600, dir_fd=self.fd), "wb") as output:
                output.write(data)

    def close(self):
        os.close(self.fd)

    def inventory(self):
        files = []
        for name in SEEDS:
            try:
                fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=self.fd)
            except FileNotFoundError:
                continue
            with os.fdopen(fd, "rb") as source:
                info = os.fstat(source.fileno())
                if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_size > 32768:
                    raise ValueError("The scenario workspace contains an unexpected file")
                content = source.read(32769)
                if len(content) > 32768:
                    raise ValueError("A sample file changed while being read")
                files.append({"name": name, "temporary": name in TEMPORARY, "bytes": len(content),
                              "sha256": hashlib.sha256(content).hexdigest(),
                              "inode": info.st_ino, "modifiedNs": info.st_mtime_ns})
        return files

    def observe(self, targets):
        if (not isinstance(targets, list) or len(targets) > len(SEEDS)
            or any(not isinstance(x, str) or x not in SEEDS for x in targets)
            or len(set(targets)) != len(targets)):
            raise ValueError("Select unique sample filenames; arbitrary paths are not accepted")
        return {"workspaceId": self.workspace_id, "targets": sorted(targets), "files": self.inventory()}

    def execute(self, gateway, record, state_lock=None):
        # Re-read the authoritative Run and accepted evidence at the tool boundary.
        final = gateway.cli("run", "get", "--key-file", str(gateway.key), record["runId"])
        stored = gateway.cli("run", "submission", "--key-file", str(gateway.key),
                             "--submission-id", final.get("finalSubmissionId", ""), record["runId"])
        # Remote reads must not block state polling. Keep local verification,
        # deletion and the per-file record together in a short critical section.
        with (state_lock if state_lock is not None else nullcontext()):
            return self._apply(record, final, stored)

    def _apply(self, record, final, stored):
        expected = record["observation"]
        if (final["id"] != record["runId"] or final["status"] != "FINISHED"
            or final.get("decision") != "ALLOW" or final.get("result") != "PASS"
            or final["policyRevisionId"] != record["revision"]["id"]
            or final["specDigest"] != record["revision"]["digest"]
            or identity(final["changeSet"]) != identity(record["changeSet"])
            or stored["id"] != final["finalSubmissionId"] or stored["runId"] != final["id"]
            or stored["submissionNo"] != final["latestSubmissionNo"]
            or stored["verdict"] != "PASS" or stored["finishRun"] is not True
            or snapshot_identity(stored["snapshot"]) != snapshot_identity(record["submission"])):
            raise ValueError("Execution stopped: Run identity, decision or evidence mismatch")
        current = self.observe(expected["targets"])
        limit = record["changeSet"]["maxFiles"]
        if (record["changeSet"]["workspaceId"] != self.workspace_id
            or record["changeSet"]["action"] != "delete-files"
            or record["changeSet"]["targets"] != expected["targets"]
            or record["changeSet"]["observationDigest"] != hashlib.sha256(identity(expected).encode()).hexdigest()
            or any(identity(json.loads(c["evidence"][0]["resultSummary"])) != identity(expected)
                   for c in stored["snapshot"]["checks"])
            or identity(current) != identity(expected)
            or any(x["verdict"] != "PASS" for x in assess(current, limit))
            or final.get("decisionExpiresAt", 0) <= time.time()):
            raise ValueError("Execution stopped: files changed, checks failed or decision expired")
        removed = []
        for name in expected["targets"]:
            if final["decisionExpiresAt"] <= time.time():
                raise ValueError("Execution stopped: decision expired during deletion")
            os.unlink(name, dir_fd=self.fd)
            removed.append(name)
            record.setdefault("removed", []).append(name)
        return removed


def assess(observation, limit):
    targets = observation["targets"]
    existing = {x["name"] for x in observation["files"]}
    safety = ("BLOCKED" if any(x not in TEMPORARY for x in targets) else
              "UNKNOWN" if not targets or not set(targets) <= existing else "PASS")
    batch = "UNKNOWN" if not targets else "BLOCKED" if len(targets) > limit else "PASS"
    return [{"checkId": "temporary-only", "verdict": safety},
            {"checkId": "batch-limit", "verdict": batch}]


class Gateway:
    def __init__(self, cli, origin, private):
        self.binary, self.origin, self.private = str(cli), origin, Path(private)
        self.session, self.key = self.private / "session.json", self.private / "key.json"

    def cli(self, *args, input_text=None):
        output = command(self.binary, *args[:2], "--server", self.origin, *args[2:], input_text=input_text)
        return json.loads(output) if args[0] in ("run", "scene") else None

    def setup(self, password):
        self.cli("bootstrap", "init", "--username", "demo", "--password-stdin", input_text=password + "\n")
        self.cli("auth", "login", "--session-file", str(self.session), "--username", "demo",
                 "--password-stdin", input_text=password + "\n")
        self.cli("auth", "create-key", "--session-file", str(self.session), "--expires-in", "1h",
                 "--result-file", str(self.key))

    def publish(self, spec_yaml, directory):
        path = directory / "policy.yaml"
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w", encoding="utf-8") as output:
            output.write(spec_yaml)
        self.cli("scene", "apply", "--session-file", str(self.session), "--key", "file-cleanup",
                 "--name", "File cleanup", "--spec", str(path), "--publish")

    def evaluate(self, tool, targets, limit, spec_yaml, directory, on_created=None):
        directory.mkdir(mode=0o700)
        observation = tool.observe(targets)
        change = {"action": "delete-files", "workspaceId": tool.workspace_id,
                  "targets": observation["targets"], "maxFiles": limit,
                  "observationDigest": hashlib.sha256(identity(observation).encode()).hexdigest()}
        private_json(directory / "change.json", change)
        credential = directory / "credential.json"
        operation = directory.name
        created = self.cli("run", "create", "--key-file", str(self.key), "--scene", "file-cleanup",
                           "--change-set", str(directory / "change.json"), "--operation-id", operation + "-create",
                           "--submission-credential-file", str(credential))
        if on_created:
            on_created(created["id"])
        revision = self.cli("run", "revision", "--key-file", str(self.key), created["id"])
        if (revision["id"] != created["policyRevisionId"] or revision["digest"] != created["specDigest"]
            or revision["yaml"] != spec_yaml):
            raise ValueError("Run policy differs from the policy selected for this task")
        checks = assess(observation, limit)
        verdict = next((x for x in ("BLOCKED", "UNKNOWN") if any(c["verdict"] == x for c in checks)), "PASS")
        for check in checks:
            check.update(summary=check["verdict"] + ": " + check["checkId"], evidence=[{
                "requirementId": "files", "observedAt": datetime.now(timezone.utc).isoformat(),
                "querySummary": "Read the scenario workspace using no-follow file descriptors",
                "resultSummary": identity(observation)}])
        submission = {"baseSubmissionNo": created["latestSubmissionNo"], "finishRun": True,
                      "verdict": verdict, "summary": "File cleanup checks: " + verdict, "checks": checks}
        private_json(directory / "submission.json", submission)
        self.cli("run", "submit", "--submission-credential-file", str(credential),
                 "--submission", str(directory / "submission.json"), "--operation-id", operation + "-submit", created["id"])
        final = self.cli("run", "get", "--key-file", str(self.key), created["id"])
        stored = self.cli("run", "submission", "--key-file", str(self.key), "--submission-id", final["finalSubmissionId"], created["id"])
        if (final["status"] != "FINISHED" or final["result"] != verdict
            or final["decision"] != ("ALLOW" if verdict == "PASS" else "DENY")
            or snapshot_identity(stored["snapshot"]) != snapshot_identity(submission)):
            raise ValueError("The service did not persist the expected assessment")
        credential.unlink()  # Terminal Run; retain no local submission credential.
        return {"runId": created["id"], "decision": final["decision"], "result": final["result"],
                "decisionExpiresAt": final["decisionExpiresAt"], "targets": targets,
                "checks": checks, "revision": revision, "changeSet": change, "submission": submission,
                "storedSubmission": stored, "run": final, "observation": observation, "removed": []}

    def close(self):
        if self.key.is_file():
            self.cli("auth", "revoke-key", "--session-file", str(self.session),
                     "--key-id", json.loads(self.key.read_text())["keyId"])
