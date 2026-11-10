#!/usr/bin/env python3
"""Pipeline-neutral Recovery demo gate: observe, submit, enforce a Run Decision."""

from datetime import datetime
import hashlib
import json
import os
import re
from dataclasses import dataclass
from pathlib import Path
import subprocess
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


ROOT = Path(__file__).resolve().parents[2]
MAX_OBSERVATION_BYTES = 32 << 10
COMMAND_TIMEOUT_SECONDS = 300
MAX_SUMMARY_BYTES = 4096


class GateStopped(RuntimeError):
    """Safe, stable failure classification shared by delivery adapters."""

    def __init__(self, reason, layer, message, server_decision=None, business_evidence=None):
        super().__init__(message)
        self.reason = reason
        self.layer = layer
        self.server_decision = server_decision if server_decision in ("ALLOW", "DENY") else None
        self.business_evidence = (business_evidence if business_evidence in ("ACCEPTED", "REJECTED", "NOT_CHECKED")
                                  else "REJECTED" if layer == "business" else "NOT_CHECKED")


def private_json(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as output:
        json.dump(value, output)


def command(*args, input_text=None, env=None):
    try:
        result = subprocess.run(args, cwd=ROOT, input=input_text, text=True,
                                capture_output=True, check=False, env=env,
                                timeout=COMMAND_TIMEOUT_SECONDS)
    except subprocess.TimeoutExpired as error:
        # The gate fails closed without logging captured output or credentials.
        raise RuntimeError(f"{Path(args[0]).name} {args[1]} timed out after "
                           f"{COMMAND_TIMEOUT_SECONDS}s") from error
    if result.returncode:
        # CLI failures deliberately do not echo arguments, response bodies or credentials.
        raise RuntimeError(f"{Path(args[0]).name} {args[1]} failed (exit {result.returncode})")
    return result.stdout


def observation_from_url(url):
    parts = urlsplit(url)
    if parts.username or parts.password or parts.query or parts.fragment or (
        parts.scheme != "https" and not (
            parts.scheme == "http" and parts.hostname in ("127.0.0.1", "::1")
        )
    ):
        raise ValueError("health URL must be HTTPS or literal loopback HTTP without credentials or query")

    class NoRedirect(HTTPRedirectHandler):
        def redirect_request(self, request, fp, code, msg, headers, newurl):
            return None

    try:
        with build_opener(NoRedirect).open(
            Request(url, headers={"Accept": "application/json"}), timeout=5
        ) as response:
            return read_health_response(response, url)
    except HTTPError as error:
        with error:
            return read_health_response(error, url)
    except (URLError, TimeoutError) as error:
        return {"source": url, "probeError": type(error).__name__}


def read_health_response(response, url):
    raw = response.read(MAX_OBSERVATION_BYTES + 1)
    if len(raw) > MAX_OBSERVATION_BYTES:
        raise ValueError("health response exceeds 32 KiB")
    try:
        observation = json.loads(raw)
    except json.JSONDecodeError:
        observation = {"responseError": "non-JSON health response"}
    if not isinstance(observation, dict):
        raise ValueError("health response must be a JSON object")
    return {**observation, "source": url, "httpStatus": response.status}


def load_observation(observation_file=None, health_url=None):
    if health_url:
        return observation_from_url(health_url)
    with Path(observation_file).open("rb") as source:
        raw = source.read(MAX_OBSERVATION_BYTES + 1)
    if len(raw) > MAX_OBSERVATION_BYTES:
        raise ValueError("observation exceeds 32 KiB")
    fixture = json.loads(raw)
    observations = fixture.get("observations") if isinstance(fixture, dict) else None
    if not isinstance(observations, dict) or not isinstance(observations.get("health"), dict):
        raise ValueError("observation fixture needs observations.health object")
    return observations["health"]


def validate_submission(submission, observation):
    if not isinstance(submission, dict) or set(submission) != {"baseSubmissionNo", "finishRun", "verdict", "summary", "checks"}:
        raise ValueError("Agent returned an invalid Submission")
    if (type(submission["baseSubmissionNo"]) is not int or submission["baseSubmissionNo"] < 0
        or submission["finishRun"] is not True
        or (submission["summary"] != "" and not valid_summary(submission["summary"]))):
        raise ValueError("Agent returned an invalid Submission")
    checks = submission["checks"]
    if not isinstance(checks, list) or len(checks) != 1 or not isinstance(checks[0], dict):
        raise ValueError("Agent must return exactly one Check")
    check = checks[0]
    if (set(check) != {"checkId", "verdict", "summary", "evidence"}
        or check["checkId"] != "readiness"
        or check["verdict"] not in ("PASS", "BLOCKED", "UNKNOWN")
        or not valid_summary(check["summary"])):
        raise ValueError("Agent returned an invalid Check")
    if submission["verdict"] != check["verdict"]:
        raise ValueError("Submission verdict differs from Check")
    evidence = check["evidence"]
    if not isinstance(evidence, list) or len(evidence) != 1 or not isinstance(evidence[0], dict):
        raise ValueError("Agent Evidence differs from the observed health")
    item = evidence[0]
    if (set(item) != {"requirementId", "observedAt", "querySummary", "resultSummary"}
        or item["requirementId"] != "health" or not valid_summary(item["querySummary"])
        or not valid_summary(item["resultSummary"])):
        raise ValueError("Agent returned invalid Evidence")
    try:
        raw_time = item["observedAt"]
        stamp = re.fullmatch(
            r"([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2})"
            r"(?:\.[0-9]+)?(Z|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])",
            raw_time,
        )
        if stamp is None:
            raise ValueError("observation time needs RFC3339 and a timezone")
        # Validate the calendar and offset independently of fractional precision.
        # Python 3.9/3.10 only parse 3 or 6 fractional digits. Keep the original
        # timestamp in Evidence, which is compared without rewriting its bytes.
        whole, offset = stamp.groups()
        datetime.fromisoformat(whole + offset.replace("Z", "+00:00"))
        actual = json.loads(item["resultSummary"])
    except (TypeError, AttributeError, ValueError) as error:
        raise ValueError("Agent returned invalid Evidence") from error
    # Preserve JSON types: Python considers True == 1 and 1 == 1.0.
    if json_identity(actual) != json_identity(observation):
        raise ValueError("Agent Evidence differs from the observed health")


def valid_summary(value):
    return isinstance(value, str) and bool(value.strip()) and len(value) <= 2048 and len(value.encode("utf-8")) <= MAX_SUMMARY_BYTES


def json_identity(value):
    try:
        return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False)
    except (TypeError, ValueError) as error:
        raise ValueError("Evidence must contain finite JSON values") from error


@dataclass(frozen=True)
class GateInput:
    change_set: dict
    operation_identity: str
    observation: dict
    key_file: str
    cli: str = "guardrailctl"


def prepare_gate(input_data, state_dir, recipient, envelope_path, age="age"):
    """Create a Run and encrypt its one-time credential for the outside Agent."""
    state_dir = Path(state_dir)
    try:
        state_dir.mkdir(mode=0o700)
    except FileExistsError as error:
        raise GateStopped("STATE_EXISTS", "orchestration",
                           "Gate state already exists; inspect the existing Run and private "
                           "state before retrying. This demo cannot restart prepare in the "
                           "same state directory") from error
    operation_prefix = "gate-" + hashlib.sha256(
        input_data.operation_identity.encode("utf-8")
    ).hexdigest()[:32]
    change_path = state_dir / "change.json"
    credential_path = state_dir / "run-credential.json"
    private_json(change_path, input_data.change_set)
    created = json.loads(command(input_data.cli, "run", "create", "--key-file", input_data.key_file,
                                 "--scene", "recovery", "--change-set", str(change_path),
                                 "--operation-id", operation_prefix + "-create",
                                 "--submission-credential-file", str(credential_path)))
    run_id = created["id"]
    print(f"Guardrail Run: {run_id}", flush=True)
    if created["status"] != "OPEN":
        raise RuntimeError(f"Run {run_id} is not OPEN; inspect it before retrying")
    frozen = json.loads(command(input_data.cli, "run", "get", "--key-file", input_data.key_file,
                                run_id))
    revision = json.loads(command(input_data.cli, "run", "revision", "--key-file", input_data.key_file, run_id))
    if revision["id"] != frozen["policyRevisionId"] or revision["digest"] != frozen["specDigest"]:
        raise RuntimeError("Run Revision does not match its frozen digest")
    private_json(state_dir / "state.json", {"runId": run_id, "policyRevisionId": frozen["policyRevisionId"],
                                            "operationPrefix": operation_prefix,
                                            "observation": input_data.observation})
    bundle_path = state_dir / "handoff.json"
    private_json(bundle_path, {
        "runId": run_id,
        "operationPrefix": operation_prefix,
        "specSnapshot": revision["yaml"],
        "policyRevisionId": revision["id"], "specDigest": revision["digest"],
        "baseSubmissionNo": frozen["latestSubmissionNo"],
        "observation": input_data.observation,
        "runToken": json.loads(credential_path.read_text(encoding="utf-8")),
    })
    try:
        command(age, "-r", recipient, "-o", str(envelope_path), str(bundle_path))
    finally:
        bundle_path.unlink(missing_ok=True)
    credential_path.unlink()
    print(f"Guardrail operation prefix: {operation_prefix}", flush=True)


def finalize_gate(cli, key_file, state_dir):
    """Read the authoritative Decision after the Agent phase has ended."""
    state_dir = Path(state_dir)
    state = json.loads((state_dir / "state.json").read_text(encoding="utf-8"))
    run_id = state["runId"]
    final = json.loads(command(cli, "run", "get", "--key-file", key_file, run_id))
    print(f"Run {run_id}: status={final['status']} result={final.get('result')} "
          f"decision={final.get('decision')}")
    decision_expired = final.get("decisionExpiresAt", 0) <= time.time()
    business_evidence = "NOT_CHECKED"
    allowed = (final["status"] == "FINISHED" and final.get("result") == "PASS"
               and final.get("decision") == "ALLOW"
               and not decision_expired)
    if allowed:
        try:
            if final["policyRevisionId"] != state["policyRevisionId"]:
                raise ValueError("Run Revision changed")
            submission_id = final["finalSubmissionId"]
            stored = json.loads(command(cli, "run", "submission", "--key-file", key_file,
                                        "--submission-id", submission_id, run_id))
            if (stored["id"] != submission_id or stored["runId"] != run_id
                or stored["submissionNo"] != final["latestSubmissionNo"]
                or stored["verdict"] != final["result"] or stored["finishRun"] is not True):
                raise ValueError("Final Submission identity does not match Run")
            validate_submission(stored["snapshot"], state["observation"])
            if stored["snapshot"]["verdict"] != final["result"]:
                raise ValueError("Final Snapshot verdict does not match Run")
            business_evidence = "ACCEPTED"
        except (KeyError, ValueError) as error:
            # Keep the private state for investigation; an Agent-side check is
            # not a trust boundary once the Agent possesses the Submission Credential.
            raise GateStopped("EVIDENCE_REJECTED", "business",
                               f"Run {run_id} Submission does not match pipeline observation; "
                               "promotion stopped", server_decision="ALLOW") from error
    # The stored Snapshot read can take time; freshness must hold at release too.
    if allowed:
        decision_expired = final.get("decisionExpiresAt", 0) <= time.time()
    allowed = allowed and not decision_expired
    if final["status"] in ("FINISHED", "EXPIRED"):
        # The pipeline is done with this Submission Credential; erase local copies.
        # Preserve all files when the result is still OPEN or unknown.
        for name in ("change.json", "run-credential.json", "state.json"):
            (state_dir / name).unlink(missing_ok=True)
        state_dir.rmdir()
    if not allowed:
        expired = (final["status"] == "EXPIRED" or
                   (final.get("decision") == "ALLOW" and
                    decision_expired))
        raise GateStopped("DECISION_EXPIRED" if expired else "DECISION_REJECTED", "decision",
                          f"Run {run_id} did not return a current ALLOW; promotion stopped",
                          server_decision=final.get("decision"), business_evidence=business_evidence)


def poll_gate(cli, key_file, state_dir):
    """Return false while OPEN; permit rollout only after a fresh server read."""
    state = json.loads((Path(state_dir) / "state.json").read_text(encoding="utf-8"))
    final = json.loads(command(cli, "run", "get", "--key-file", key_file, state["runId"]))
    if final["status"] == "OPEN":
        return False
    finalize_gate(cli, key_file, state_dir)
    return True
