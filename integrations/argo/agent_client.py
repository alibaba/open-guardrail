"""Outer-Agent client for the Argo Rollouts encrypted handoff."""

import argparse
import base64
import binascii
import json
from pathlib import Path
import sys
import uuid

from integrations.argo.adapter import ENVELOPE_MARKER
from integrations.jenkins.agent_client import child_env, show_handoff
from integrations.recovery_agent import gate


def init(args):
    work = Path(args.work_dir).resolve()
    work.mkdir(mode=0o700)
    identity = work / "agent-identity.txt"
    gate.command("age-keygen", "-o", str(identity), env=child_env())
    identity.chmod(0o600)
    recipient = gate.command("age-keygen", "-y", str(identity), env=child_env()).strip()
    if not recipient.startswith("age1"):
        raise ValueError("age-keygen did not return an age recipient")
    gate.private_json(work / "rollout-patch.json", {
        "metadata": {"annotations": {"guardrail-demo/case": args.case,
                                     "guardrail-demo/recipient": recipient}},
        "spec": {"template": {"metadata": {
            "annotations": {"guardrail-demo/attempt": str(uuid.uuid4())}}}}})
    print(recipient)


def receive(args):
    work = Path(args.work_dir).resolve()
    handoff = work / "handoff.json"
    if not handoff.exists():
        # Explicit pod selection prevents accidentally reading a previous attempt.
        raw = gate.command("kubectl", "--namespace", args.namespace, "logs", args.pod,
                           "--container", "guardrail", "--limit-bytes=2097152", env=child_env())
        lines = [line[len(ENVELOPE_MARKER):] for line in raw.splitlines()
                 if line.startswith(ENVELOPE_MARKER)]
        if len(lines) != 1 or len(lines[0]) > (1 << 20):
            raise ValueError("expected exactly one bounded encrypted handoff; check the selected Pod")
        envelope = work / "gate-envelope.age"
        encrypted = base64.b64decode(lines[0], validate=True)
        # Replace ciphertext on receive retry; preserve plaintext on submit failure.
        envelope.write_bytes(encrypted)
        envelope.chmod(0o600)
        gate.command("age", "-d", "-i", str(work / "agent-identity.txt"), "-o",
                     str(handoff), str(envelope), env=child_env())
        handoff.chmod(0o600)
    show_handoff(work, handoff)


def submit(args):
    work = Path(args.work_dir).resolve()
    bundle = json.loads((work / "handoff.json").read_text(encoding="utf-8"))
    if bundle["runId"] != bundle["runToken"]["runId"]:
        raise ValueError("Run ID differs from token scope")
    submission = json.loads(Path(args.submission).read_text(encoding="utf-8"))
    gate.validate_submission(submission, bundle["observation"])
    token = work / "run-credential.json"
    if not token.exists():
        gate.private_json(token, bundle["runToken"])
    gate.command(args.cli, "run", "submit", "--submission-credential-file", str(token),
                 "--submission", str(Path(args.submission).resolve()), "--operation-id",
                 bundle["operationPrefix"] + "-submit", bundle["runId"],
                 env=child_env(OPEN_GUARDRAIL_URL=bundle["runToken"]["server"]))
    token.unlink()
    (work / "handoff.json").unlink()
    print(f"Submitted Run {bundle['runId']}; Argo independently verifies the Decision")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("init", "receive", "submit"):
        command = commands.add_parser(name)
        command.add_argument("--work-dir", required=True)
        if name == "init":
            command.add_argument("--case", choices=("healthy", "unhealthy", "unknown"), required=True)
        if name == "receive":
            command.add_argument("--namespace", required=True)
            command.add_argument("--pod", required=True)
        if name == "submit":
            command.add_argument("--submission", required=True)
            command.add_argument("--cli", default="guardrailctl")
    args = parser.parse_args()
    try:
        return {"init": init, "receive": receive, "submit": submit}[args.command](args) or 0
    except (OSError, ValueError, KeyError, RuntimeError, binascii.Error):
        print("Argo handoff stopped; inspect the private directory, Run and Analysis Job before retrying",
              file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
