#!/usr/bin/env python3
"""Outer-Agent side of the Jenkins handoff; never runs inside Jenkins."""

import argparse
import base64
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import time
from urllib.error import HTTPError
from urllib.parse import urlencode, urljoin, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener

from integrations.recovery_agent import gate


MAX_RESPONSE_BYTES = 1 << 20


def child_env(**updates):
    environment = {key: value for key, value in os.environ.items()
                   if key not in ("JENKINS_API_TOKEN", "GUARDRAIL_KEY_FILE",
                                  "OPEN_GUARDRAIL_SESSION_FILE")}
    environment.update(updates)
    return environment


def safe_origin(url):
    parts = urlsplit(url)
    if parts.username or parts.password or parts.query or parts.fragment or not parts.hostname:
        raise ValueError("Jenkins URL must not contain credentials, query or fragment")
    if parts.scheme != "https" and not (parts.scheme == "http" and parts.hostname in
                                        ("127.0.0.1", "::1", "localhost")):
        raise ValueError("Jenkins URL must be HTTPS or loopback HTTP")
    return (parts.scheme, parts.netloc)


def jenkins_request(url, *, data=None):
    safe_origin(url)
    username = os.environ.get("JENKINS_USER", "")
    token = os.environ.get("JENKINS_API_TOKEN", "")
    if not username or not token:
        raise ValueError("JENKINS_USER and JENKINS_API_TOKEN are required")
    auth = base64.b64encode(f"{username}:{token}".encode()).decode("ascii")
    headers = {"Authorization": "Basic " + auth}
    if data is not None:
        headers["Content-Type"] = "application/x-www-form-urlencoded"
    class NoRedirect(HTTPRedirectHandler):
        def redirect_request(self, request, fp, code, msg, headers, newurl):
            return None

    request = Request(url, data=data, headers=headers)
    try:
        with build_opener(NoRedirect).open(request, timeout=10) as response:
            body = response.read(MAX_RESPONSE_BYTES + 1)
            if len(body) > MAX_RESPONSE_BYTES:
                raise ValueError("Jenkins response exceeds 1 MiB")
            return response.geturl(), response.headers, body
    except HTTPError as error:
        # HTTPError owns the response stream even when it represents an
        # intentionally rejected redirect. Close it on success and error paths.
        with error:
            if data is not None and error.code in (201, 302, 303):
                return url, error.headers, b""
            raise


def same_origin(base, other):
    if safe_origin(base) != safe_origin(other):
        raise ValueError("Jenkins redirected to a different origin")
    return other


def start(args):
    safe_origin(args.job_url)
    work = Path(args.work_dir).resolve()
    try:
        work.mkdir(mode=0o700)
    except FileExistsError as error:
        raise ValueError("work directory already exists; choose a fresh --work-dir for this build") from error
    if stat.S_IMODE(work.stat().st_mode) != 0o700:
        raise ValueError("private work directory must have mode 0700")
    identity = work / "agent-identity.txt"
    gate.command("age-keygen", "-o", str(identity), env=child_env())
    identity.chmod(0o600)
    recipient = gate.command("age-keygen", "-y", str(identity), env=child_env()).strip()
    if not recipient.startswith("age1"):
        raise ValueError("age-keygen did not return an age recipient")
    params = urlencode({"CANARY_CASE": args.case, "AGENT_AGE_RECIPIENT": recipient}).encode()
    target = args.job_url.rstrip("/") + "/buildWithParameters"
    final_url, headers, _ = jenkins_request(target, data=params)
    same_origin(target, final_url)
    queue = headers.get("Location", "")
    queue = urljoin(args.job_url, queue)
    same_origin(args.job_url, queue)
    if "/queue/item/" not in urlsplit(queue).path:
        raise ValueError("Jenkins did not return a queue item Location")
    gate.private_json(work / "state.json", {"jobUrl": args.job_url, "queueUrl": queue})
    print(f"Jenkins queued build; private handoff directory: {work}")


def receive(args):
    work = Path(args.work_dir).resolve()
    handoff = work / "handoff.json"
    if handoff.exists():
        show_handoff(work, handoff)
        return
    state = json.loads((work / "state.json").read_text(encoding="utf-8"))
    safe_origin(state["jobUrl"])
    deadline = time.monotonic() + args.timeout_seconds
    build_url = None
    while time.monotonic() < deadline:
        queue_url = same_origin(state["jobUrl"], state["queueUrl"])
        _, _, raw = jenkins_request(queue_url.rstrip("/") + "/api/json")
        item = json.loads(raw)
        if item.get("cancelled"):
            raise RuntimeError("Jenkins queue item was cancelled")
        build_url = item.get("executable", {}).get("url")
        if build_url:
            same_origin(state["jobUrl"], build_url)
            break
        time.sleep(3)
    if not build_url:
        raise RuntimeError("Jenkins build did not start before timeout")
    artifact_url = build_url.rstrip("/") + "/artifact/.demo-bin/gate-envelope.age"
    while time.monotonic() < deadline:
        try:
            _, _, encrypted = jenkins_request(artifact_url)
            break
        except HTTPError as error:
            if error.code != 404:
                raise
            _, _, raw = jenkins_request(build_url.rstrip("/") + "/api/json")
            build = json.loads(raw)
            if not build.get("building", True):
                raise RuntimeError(f"Jenkins build ended before handoff: {build.get('result')}")
            time.sleep(3)
    else:
        raise RuntimeError("encrypted Run handoff was not published before timeout")
    envelope = work / "gate-envelope.age"
    if len(encrypted) > (1 << 20):
        raise ValueError("encrypted Run handoff exceeds 1 MiB")
    descriptor, temporary_path = tempfile.mkstemp(dir=work, prefix="gate-envelope-", suffix=".age")
    temporary_envelope = Path(temporary_path)
    try:
        with os.fdopen(descriptor, "wb") as output:
            output.write(encrypted)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary_envelope, envelope)
    finally:
        temporary_envelope.unlink(missing_ok=True)
    gate.command("age", "-d", "-i", str(work / "agent-identity.txt"), "-o",
                 str(handoff), str(envelope), env=child_env())
    handoff.chmod(0o600)
    show_handoff(work, handoff)


def show_handoff(work, handoff):
    bundle = json.loads(handoff.read_text(encoding="utf-8"))
    if bundle["runId"] != bundle["runToken"]["runId"]:
        raise ValueError("decrypted Run ID does not match token scope")
    print(json.dumps({"runId": bundle["runId"], "specSnapshot": bundle["specSnapshot"],
                      "observation": bundle["observation"],
                      "baseSubmissionNo": bundle["baseSubmissionNo"],
                      "policyRevisionId": bundle["policyRevisionId"], "specDigest": bundle["specDigest"],
                      "submissionFile": str(work / "submission.json")}, ensure_ascii=False))


def submit(args):
    work = Path(args.work_dir).resolve()
    bundle = json.loads((work / "handoff.json").read_text(encoding="utf-8"))
    submission = json.loads(Path(args.submission).read_text(encoding="utf-8"))
    gate.validate_submission(submission, bundle["observation"])
    token_file = work / "run-credential.json"
    if not token_file.exists():
        gate.private_json(token_file, bundle["runToken"])
    environment = child_env(OPEN_GUARDRAIL_URL=bundle["runToken"]["server"])
    gate.command(args.cli, "run", "submit", "--submission-credential-file", str(token_file),
                 "--submission", str(Path(args.submission).resolve()), "--operation-id",
                 bundle["operationPrefix"] + "-submit", bundle["runId"], env=environment)
    token_file.unlink()
    (work / "handoff.json").unlink()
    print(f"Submitted Run {bundle['runId']}; Jenkins will independently read its Decision")


def wait_build(args):
    state = json.loads((Path(args.work_dir).resolve() / "state.json").read_text(encoding="utf-8"))
    deadline = time.monotonic() + args.timeout_seconds
    queue_url = same_origin(state["jobUrl"], state["queueUrl"])
    _, _, raw = jenkins_request(queue_url.rstrip("/") + "/api/json")
    build_url = json.loads(raw).get("executable", {}).get("url")
    if not build_url:
        raise RuntimeError("Jenkins build has not started")
    same_origin(state["jobUrl"], build_url)
    while time.monotonic() < deadline:
        _, _, raw = jenkins_request(build_url.rstrip("/") + "/api/json")
        build = json.loads(raw)
        if not build.get("building", True):
            print(f"Jenkins build {build_url}: {build.get('result')}")
            return 0 if build.get("result") == "SUCCESS" else 1
        time.sleep(3)
    raise RuntimeError("Jenkins build did not finish before timeout")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    trigger = commands.add_parser("start", help="generate a private key and trigger Jenkins")
    trigger.add_argument("--job-url", required=True)
    trigger.add_argument("--case", choices=("healthy", "unhealthy", "unknown"), required=True)
    trigger.add_argument("--work-dir", required=True)
    wait = commands.add_parser("receive", help="wait for and decrypt the handoff artifact")
    wait.add_argument("--work-dir", required=True)
    wait.add_argument("--timeout-seconds", type=int, default=600)
    send = commands.add_parser("submit", help="submit an Agent-authored Check with the Run Submission Credential")
    send.add_argument("--work-dir", required=True)
    send.add_argument("--submission", required=True)
    send.add_argument("--cli", default="guardrailctl")
    finish = commands.add_parser("wait", help="wait for Jenkins to verify the Decision")
    finish.add_argument("--work-dir", required=True)
    finish.add_argument("--timeout-seconds", type=int, default=1200)
    args = parser.parse_args()
    try:
        return {"start": start, "receive": receive, "submit": submit,
                "wait": wait_build}[args.command](args) or 0
    except (OSError, ValueError, KeyError, RuntimeError, HTTPError, subprocess.SubprocessError) as error:
        print(f"Agent handoff stopped: {type(error).__name__}; inspect the private work directory "
              "and Jenkins build before retrying", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
