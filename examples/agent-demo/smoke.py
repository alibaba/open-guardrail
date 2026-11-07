#!/usr/bin/env python3
"""Live service/CLI/Jenkins gate smoke for outcomes and forged Evidence (no model)."""

import argparse
import getpass
import json
from datetime import datetime, timezone
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


ROOT = Path(__file__).resolve().parents[2]
EXAMPLE = Path(__file__).resolve().parent
READINESS_TIMEOUT_SECONDS = 40


def run(*args, env=None, input_text=None, timeout=120):
    result = subprocess.run(args, cwd=ROOT, env=env, input=input_text, text=True,
                            capture_output=True, timeout=timeout)
    if result.returncode:
        hint = (" Check the Go version in go.mod and run make build for compiler diagnostics."
                if args[1] == "build" else
                " See docs/getting-started.md#troubleshooting; captured credentials are not printed.")
        raise RuntimeError(f"{Path(args[0]).name} {args[1]} failed (exit {result.returncode})." + hint)
    return result.stdout


def private_json(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as output:
        json.dump(value, output)


def read_json(url, token=None):
    headers = {"Authorization": "Bearer " + token} if token else {}
    with urlopen(Request(url, headers=headers), timeout=2) as response:
        return json.load(response)


def options(argv=None):
    parser = argparse.ArgumentParser(
        description="Run four synthetic Recovery cases against a temporary local service.")
    parser.add_argument("--keep-running", action="store_true",
                        help="keep the service and Runs available in the browser until Ctrl-C")
    parser.add_argument("--password-stdin", action="store_true",
                        help="read the temporary demo administrator password from stdin")
    args = parser.parse_args(argv)
    if sys.version_info < (3, 9):
        parser.error("Python 3.9+ is required. Use a newer Python, or run the native "
                     "binaries with the manual walkthrough in docs/getting-started.md.")
    if args.password_stdin and not args.keep_running:
        parser.error("--password-stdin requires --keep-running")
    return args


def administrator_password(args):
    if not args.keep_running:
        return secrets.token_urlsafe(24)
    if args.password_stdin:
        password = sys.stdin.readline().rstrip("\r\n")
    else:
        if not sys.stdin.isatty():
            raise ValueError("--keep-running needs a terminal for the hidden password prompt; "
                             "use --password-stdin for automation")
        password = getpass.getpass("Choose a password for the temporary demo administrator: ")
        if password != getpass.getpass("Confirm password: "):
            raise ValueError("passwords do not match")
    if len(password) < 6 or len(password.encode("utf-8")) > 128:
        raise ValueError("password must contain at least 6 characters and at most 128 UTF-8 bytes")
    return password


def wait_for_service(service, origin):
    deadline = time.monotonic() + READINESS_TIMEOUT_SECONDS
    while time.monotonic() < deadline:
        if service.poll() is not None:
            raise RuntimeError(f"local server exited before readiness (exit {service.returncode}). "
                               "Try starting open-guardrail serve in a terminal; "
                               "see docs/getting-started.md#troubleshooting.")
        try:
            with urlopen(origin + "/readyz", timeout=0.2) as response:
                if response.status == 200:
                    return
        except (HTTPError, URLError, TimeoutError):
            time.sleep(0.1)
    raise RuntimeError("local server did not become ready within "
                       f"{READINESS_TIMEOUT_SECONDS}s. Try starting open-guardrail serve "
                       "in a terminal to inspect its startup diagnostics.")


def inspect_in_browser(service, origin):
    print(f"Open {origin} and sign in as demo with the password you chose.", flush=True)
    print("Select Runs to inspect the four synthetic assessments and their evidence.", flush=True)
    print("Press Ctrl-C to stop the service and remove its temporary data and credentials.",
          flush=True)
    try:
        while service.poll() is None:
            time.sleep(0.2)
    except KeyboardInterrupt:
        return
    raise RuntimeError(f"local server stopped while waiting for inspection (exit {service.returncode})")


def main(argv=None):
    args = options(argv)
    password = administrator_password(args)
    started = time.monotonic()
    with tempfile.TemporaryDirectory(prefix="open-guardrail-demo-") as temporary:
        work = Path(temporary)
        if (ROOT / "go.mod").is_file():
            go = os.environ.get("GO", "go")
            if shutil.which(go) is None:
                raise RuntimeError("source demo needs Go from go.mod. Install Go, or use a "
                                   "native release archive; see docs/getting-started.md.")
            print("Building server and CLI from source; the first build may download Go dependencies.",
                  flush=True)
            server_bin, cli_bin = work / "open-guardrail", work / "guardrailctl"
            build_environment = {**os.environ, "GOWORK": "off", "CGO_ENABLED": "0"}
            run(go, "build", "-o", str(server_bin), "./cmd/open-guardrail",
                env=build_environment, timeout=300)
            run(go, "build", "-o", str(cli_bin), "./cmd/guardrailctl",
                env=build_environment, timeout=300)
        else:
            server_bin, cli_bin = ROOT / "open-guardrail", ROOT / "guardrailctl"
            if not server_bin.is_file() or not cli_bin.is_file():
                raise RuntimeError("release archive must contain both server and CLI binaries")
        print("Starting an isolated loopback service with temporary SQLite data.", flush=True)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        origin = f"http://127.0.0.1:{port}"
        environment = {key: value for key, value in os.environ.items()
                       if not key.startswith("OPEN_GUARDRAIL_") and key != "DATABASE_URL"}
        environment.update(OPEN_GUARDRAIL_URL=origin,
                           OPEN_GUARDRAIL_SESSION_FILE=str(work / "session.json"))
        server_log_path = work / "server.log"
        with server_log_path.open("w", encoding="utf-8") as server_log:
            service = subprocess.Popen((str(server_bin), "serve", "--listen", f"127.0.0.1:{port}",
                                        "--data-dir", str(work / "data")), cwd=work, env=environment,
                                       stdout=subprocess.DEVNULL, stderr=server_log)
        try:
            wait_for_service(service, origin)

            try:
                read_json(origin + "/runtime/v1alpha1/runs")
            except HTTPError as failure:
                with failure:
                    denied = json.load(failure)
                    trace_id = failure.headers.get("X-Trace-ID")
                if failure.code != 401 or not trace_id or denied.get("traceId") != trace_id:
                    raise AssertionError("unauthenticated response lost its correlation ID")
            else:
                raise AssertionError("Run orchestration accepted an unauthenticated request")

            def cli(*args, input_text=None):
                return run(str(cli_bin), *args, env=environment, input_text=input_text)

            cli("bootstrap", "init", "--username", "demo", "--password-stdin",
                input_text=password + "\n")
            cli("auth", "login", "--username", "demo", "--password-stdin",
                input_text=password + "\n")
            scene_args = ("scene", "apply", "--key", "recovery", "--name", "Recovery",
                          "--spec", str(ROOT / "examples/policies/recovery.yaml"), "--publish")
            cli(*scene_args)
            repeated_scene = json.loads(cli(*scene_args))
            if repeated_scene["draftChanged"] or repeated_scene["revision"]["revisionNumber"] != 1:
                raise AssertionError("repeated Scene application changed the active Revision")
            key_path = work / "project-key.json"
            cli("auth", "create-key", "--result-file", str(key_path))
            private_values = [password, json.loads(key_path.read_text())["token"]]
            completed = {}
            for case in ("healthy", "unhealthy", "unknown", "forged"):
                fixture = json.loads((EXAMPLE / (("healthy" if case == "forged" else case) + ".json")).read_text(encoding="utf-8"))
                health = fixture["observations"]["health"]
                verdict = ("UNKNOWN" if "probeError" in health else
                           "PASS" if health.get("status") == "healthy" and
                           health.get("httpStatus") == 200 and
                           health.get("readyReplicas") == health.get("desiredReplicas") and
                           health.get("errorRatePercent", 100) < 1 else "BLOCKED")
                expected = "ALLOW" if verdict == "PASS" else "DENY"
                change = work / (case + "-change.json")
                private_json(change, fixture["changeSet"])
                token = work / (case + "-token.json")
                created = json.loads(cli("run", "create", "--key-file", str(key_path),
                                         "--scene", "recovery", "--change-set", str(change),
                                         "--operation-id", "create-" + case,
                                         "--submission-credential-file", str(token)))
                private_values.append(json.loads(token.read_text())["token"])
                if created["status"] != "OPEN":
                    raise AssertionError(f"{case}: Run was not OPEN")
                submission = work / (case + "-submission.json")
                private_json(submission, {"baseSubmissionNo": 0, "finishRun": True,
                    "verdict": verdict, "summary": "Recovery health evaluated",
                    "checks": [{"checkId": "readiness", "verdict": verdict,
                    "summary": "Health verified" if verdict == "PASS" else
                               "Health failed" if verdict == "BLOCKED" else "Health unverified",
                    "evidence": [{"requirementId": "health",
                        "observedAt": datetime.now(timezone.utc).isoformat(),
                        "querySummary": "Read synthetic health observation",
                        "resultSummary": json.dumps(health, sort_keys=True)}]}]})
                revision = json.loads(cli("run", "revision", "--key-file", str(key_path), created["id"]))
                if revision["id"] != created["policyRevisionId"] or revision["digest"] != created["specDigest"]:
                    raise AssertionError("Run-scoped Revision disagrees with frozen reference")
                agent_work = work / (case + "-agent")
                agent_work.mkdir(mode=0o700)
                private_json(agent_work / "handoff.json", {"runId": created["id"],
                    "operationPrefix": "smoke-" + case, "specSnapshot": revision["yaml"],
                    "baseSubmissionNo": 0, "policyRevisionId": revision["id"], "specDigest": revision["digest"],
                    "observation": health, "runToken": json.loads(token.read_text())})
                run(sys.executable, "-m", "integrations.jenkins.agent_client", "submit",
                    "--work-dir", str(agent_work), "--submission", str(submission),
                    "--cli", str(cli_bin), env=environment)
                actual = json.loads(cli("run", "get", "--key-file", str(key_path), created["id"]))
                accepted = json.loads(cli("run", "submission", "--key-file", str(key_path),
                    "--submission-id", actual["finalSubmissionId"], created["id"]))
                gate_state = work / (case + "-gate")
                gate_state.mkdir(mode=0o700)
                private_json(gate_state / "state.json", {"runId": created["id"],
                    "policyRevisionId": created["policyRevisionId"],
                    "observation": {"status": "unhealthy"} if case == "forged" else health})
                gate_env = {**environment, "GUARDRAIL_KEY_FILE": str(key_path)}
                polled = subprocess.run((sys.executable, "-m", "integrations.jenkins.adapter",
                    "--phase", "poll", "--state-dir", str(gate_state), "--cli", str(cli_bin)),
                    cwd=ROOT, env=gate_env, capture_output=True, text=True, timeout=30)
                should_allow = verdict == "PASS" and case != "forged"
                if polled.returncode != (0 if should_allow else 1):
                    raise AssertionError(f"{case}: Jenkins gate returned {polled.returncode}")
                if case == "forged" and not (gate_state / "state.json").exists():
                    raise AssertionError("forged Evidence did not preserve investigation state")
                if (actual["status"], actual["result"], actual["decision"]) != (
                    "FINISHED", verdict, expected
                ) or actual["finalSubmissionId"] != accepted["id"] or actual["latestSubmissionNo"] != 1:
                    raise AssertionError(f"{case}: wrong terminal Run or Evidence")
                completed[case] = (created["id"], expected)

            admin_token = json.loads(Path(environment["OPEN_GUARDRAIL_SESSION_FILE"]).read_text())["token"]
            listed = read_json(origin + "/api/v1alpha1/runs", admin_token)
            listed_ids = {item["id"] for item in listed["items"]}
            for case, (run_id, decision) in completed.items():
                if run_id not in listed_ids:
                    raise AssertionError(f"{case}: missing from management list")
                detail = read_json(origin + "/api/v1alpha1/runs/" + run_id, admin_token)
                if detail["decision"] != decision:
                    raise AssertionError(f"{case}: management detail disagrees")
                print(f"{case}: {run_id} server={decision} gate={'ALLOW' if decision == 'ALLOW' and case != 'forged' else 'DENY'}",
                      flush=True)
            logs = server_log_path.read_text(encoding="utf-8")
            records = [json.loads(line) for line in logs.splitlines()]
            if not any(record.get("trace_id") == trace_id and record.get("status") == 401
                       and record.get("route") == "GET /runtime/v1alpha1/runs" for record in records):
                raise AssertionError("CLI failure cannot be found in standard server access logs")
            if any(value in logs for value in private_values):
                raise AssertionError("server access logs exposed a password or credential")
            print(f"All four synthetic cases passed in {time.monotonic() - started:.1f}s.",
                  flush=True)
            if args.keep_running:
                inspect_in_browser(service, origin)
            else:
                print("Temporary service and Runs are removed on exit. "
                      "Add --keep-running to inspect them in the browser.", flush=True)
        finally:
            service.terminate()
            try:
                service.wait(timeout=5)
            except subprocess.TimeoutExpired:
                service.kill()
                service.wait(timeout=5)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("Demo interrupted; temporary service and data cleaned up.", file=sys.stderr)
        sys.exit(130)
    except (AssertionError, RuntimeError, OSError, KeyError, ValueError,
            subprocess.TimeoutExpired) as error:
        print(f"agent demo smoke failed: {error}", file=sys.stderr)
        sys.exit(1)
