"""Run inside an Argo Rollouts Analysis Job; only a verified ALLOW exits zero."""

import argparse
import base64
import json
import os
from pathlib import Path
import sys
import time

from integrations.recovery_agent import gate


ENVELOPE_MARKER = "GUARDRAIL_ENVELOPE_AGE_BASE64="


def context_from_argo(environment, *, instance_id, rollout, revision, checkpoint,
                      observation, key_file, cli):
    delivery = {"type": "argo-rollouts", "instanceId": instance_id,
                "namespace": environment.get("POD_NAMESPACE", ""),
                "jobUid": environment.get("JOB_UID", ""), "rollout": rollout,
                "revision": revision, "checkpoint": checkpoint}
    if not all(isinstance(value, str) and value.strip() for value in delivery.values()):
        raise ValueError("Argo cluster, namespace, Job UID, rollout, revision and checkpoint are required")
    if not key_file:
        raise ValueError("GUARDRAIL_KEY_FILE is required")
    return gate.GateInput(
        change_set={"service": rollout, "environment": "synthetic-argo",
                    "candidate": revision, "delivery": delivery},
        operation_identity=json.dumps(delivery, sort_keys=True, separators=(",", ":")),
        observation=observation, key_file=key_file, cli=cli)


def run(args):
    key_file = os.environ.get("GUARDRAIL_KEY_FILE", "")
    if not os.environ.get("OPEN_GUARDRAIL_URL") or not Path(key_file).is_file():
        raise ValueError("configure the service URL and mount the Project Key file")
    if not args.recipient.startswith("age1"):
        raise ValueError("an age public recipient is required")
    if not 0 < args.timeout_seconds <= 840:
        raise ValueError("timeout must be between 1 and 840 seconds")
    deadline = time.monotonic() + args.timeout_seconds
    # Kubernetes Secret volumes expose symlinks and group-readable files. The
    # CLI intentionally accepts only regular 0600 credentials. Copy into this
    # Pod's private memory volume without weakening that CLI contract.
    private_key = Path(args.state_dir).parent / "project-key.json"
    with Path(key_file).open("rb") as source:
        raw = source.read(8193)
    if len(raw) > 8192:
        raise ValueError("Project Key file exceeds 8 KiB")
    gate.private_json(private_key, json.loads(raw))
    try:
        return wait_gate(args, str(private_key), deadline)
    finally:
        private_key.unlink(missing_ok=True)


def wait_gate(args, key_file, deadline):
    context = context_from_argo(
        os.environ, instance_id=args.instance_id, rollout=args.rollout,
        revision=args.revision, checkpoint=args.checkpoint,
        observation=gate.load_observation(str(gate.ROOT / "examples/agent-demo" / (args.case + ".json"))),
        key_file=key_file, cli=args.cli)
    envelope = Path(args.state_dir).parent / "gate-envelope.age"
    try:
        gate.prepare_gate(context, args.state_dir, args.recipient, envelope)
    except gate.GateStopped:
        raise
    except (OSError, ValueError, KeyError, RuntimeError) as error:
        raise gate.GateStopped("PREPARE_FAILED", "orchestration", "Gate preparation failed") from error
    # Logs carry ciphertext only. The Job has no Kubernetes API credentials;
    # the outside Agent needs pods/log read permission, never pods/exec.
    print(ENVELOPE_MARKER + base64.b64encode(envelope.read_bytes()).decode("ascii"), flush=True)
    while time.monotonic() < deadline:
        if gate.poll_gate(args.cli, key_file, args.state_dir):
            print("Verified current ALLOW; Analysis Job may complete", flush=True)
            return 0
        time.sleep(min(5, max(0, deadline - time.monotonic())))
    raise gate.GateStopped("AGENT_TIMEOUT", "orchestration",
                           "Agent submission timed out; rollout must stop")


def report_result(result_file, *, allowed=False, error=None):
    """Expose only fixed classifications, never exception text or credentials."""
    if allowed:
        result = {"outcome": "ALLOW", "layer": "combined", "reason": "GATE_PASSED",
                  "serverDecision": "ALLOW", "businessEvidence": "ACCEPTED"}
    else:
        reason = error.reason if isinstance(error, gate.GateStopped) else "VERIFICATION_FAILED"
        layer = error.layer if isinstance(error, gate.GateStopped) else "verification"
        result = {"outcome": "DENY", "layer": layer, "reason": reason,
                  "serverDecision": (getattr(error, "server_decision", None) or
                                     ("ALLOW" if layer == "business" else "UNVERIFIED")),
                  "businessEvidence": getattr(error, "business_evidence", "NOT_CHECKED")}
    payload = json.dumps(result, sort_keys=True)
    if result_file:
        Path(result_file).write_text(payload, encoding="utf-8")
    print("GUARDRAIL_GATE_RESULT=" + payload, flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--instance-id", required=True)
    parser.add_argument("--rollout", required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--checkpoint", default="recovery-canary")
    parser.add_argument("--case", choices=("healthy", "unhealthy", "unknown"), required=True)
    parser.add_argument("--recipient", required=True)
    parser.add_argument("--state-dir", default="/state/gate")
    parser.add_argument("--cli", default="guardrailctl")
    parser.add_argument("--timeout-seconds", type=int, default=780)
    parser.add_argument("--result-file", help="Kubernetes termination message file")
    args = parser.parse_args()
    try:
        run(args)
        report_result(args.result_file, allowed=True)
        return 0
    except (OSError, ValueError, KeyError, RuntimeError) as error:
        # Do not log subprocess output, credentials or the encrypted bundle's plaintext.
        report_result(args.result_file, error=error)
        print("Argo gate stopped; inspect the Run and Analysis Job before retrying", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
