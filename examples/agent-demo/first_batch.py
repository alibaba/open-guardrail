#!/usr/bin/env python3
"""Start a local first-batch Run or query its archived, synthetic observations."""

import argparse
import hashlib
from http.client import HTTPConnection
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import secrets
import shlex
import socket
import subprocess
import tempfile
from urllib.parse import urlsplit

from smoke import EXAMPLE, ROOT, private_json, run, wait_for_service


def read(path):
    return json.loads(path.read_text(encoding="utf-8"))


def observe(data, scope, source, record_id=None):
    if source == "changes":
        return [row for row in data[source] if row["service"] == scope["service"]
                and row["candidate"] == scope["candidate"]]
    if source == "records":
        return [row for row in data[source] if row["orderId"] == record_id
                and all(row[key] == scope[key] for key in ("service", "environment"))
                and row["asOf"] == scope["to"]]
    for window in data["windows"]:
        if all(window[key] == scope[key] for key in ("service", "environment", "from", "to")):
            if source not in window:
                break
            return [row for row in window[source] if row["instance"] in scope["instances"]
                    and (source != "logs" or scope["from"] <= row["at"] < scope["to"])]
    return {"unavailable": "No source coverage for this observation window"}


def query(work, source, record_id):
    scope = read(work / "run.json")["changeSet"]
    data = read(work / "observations.json")
    if source == "records" and not record_id:
        raise ValueError("Pass --record-id from the scoped log query.")
    if source != "records" and record_id:
        raise ValueError("--record-id applies only to records.")
    if source == "records":
        logs = observe(data, scope, "logs")
        if not isinstance(logs, list) or not any(row.get("orderId") == record_id for row in logs):
            raise ValueError("Choose a record ID returned by this batch's log query.")
    result = observe(data, scope, source, record_id)
    observation = {
        "source": source, "scope": scope, "observedAt": datetime.now(timezone.utc).isoformat(),
        "result": result,
    }
    if record_id:
        observation["recordId"] = record_id

    evidence_dir = work / "queries"
    evidence_dir.mkdir(mode=0o700, exist_ok=True)
    suffix = "-" + hashlib.sha256(record_id.encode()).hexdigest() if record_id else ""
    with tempfile.NamedTemporaryFile(mode="w", dir=evidence_dir, delete=False) as tmp:
        json.dump(observation, tmp)
    os.replace(tmp.name, evidence_dir / (source + suffix + ".json"))
    return observation


def run_request(work, suffix="", submission=None):
    port = urlsplit(read(work / "connection.json")["server"]).port
    run_id = read(work / "run.json")["id"]
    headers = {}
    body = None
    if submission is not None:
        headers["Authorization"] = "Bearer " + read(work / "run-key.json")["token"]
        body = json.dumps(submission).encode("utf-8")
        headers.update({"Content-Type": "application/json", "Idempotency-Key": "first-batch-submit"})
    conn = HTTPConnection("127.0.0.1", port, timeout=10)
    try:
        conn.request("POST" if body else "GET", f"/runtime/v1alpha1/runs/{run_id}{suffix}", body, headers)
        response = conn.getresponse()
        if not 200 <= response.status < 300:
            raise RuntimeError(f"Run request failed (HTTP {response.status}).")
        return json.load(response)
    finally:
        conn.close()


def save_record(work, run_id, accepted):
    report = run_request(work, "/report")
    if report["run"]["id"] != run_id or Path(run_id).name != run_id:
        raise ValueError("Unexpected Run ID in the diagnostic report.")
    observations = [
        observation
        for check in accepted["checks"]
        for evidence in check["evidence"]
        for observation in json.loads(evidence["resultSummary"])
    ]
    directory = ROOT / "data" / "first-batch-reports"
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    destination = directory / f"{run_id}.json"
    with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=directory, delete=False) as output:
        try:
            json.dump({"report": report, "observations": observations}, output, indent=2)
            output.write("\n")
            output.close()
            os.replace(output.name, destination)
        finally:
            Path(output.name).unlink(missing_ok=True)
    return destination


def submit(work, verdict, summary):
    state = run_request(work)
    if state["status"] == "OPEN":
        snapshot = work / "submission.json"
        if not snapshot.exists():
            evidence = []
            for source in ("metrics", "logs", "alerts"):
                path = work / "queries" / (source + ".json")
                if not path.is_file():
                    raise ValueError(f"Query {source} before submitting.")
                observations = [read(path)]
                if source == "logs":
                    for pattern in ("changes.json", "records-*.json"):
                        observations.extend(read(p) for p in sorted(path.parent.glob(pattern)))
                evidence.append({"requirementId": source, "observedAt": observations[0]["observedAt"],
                                 "querySummary": f"Read {source} for the frozen first-batch scope",
                                 "resultSummary": json.dumps(observations)})
            private_json(snapshot, {"baseSubmissionNo": state["latestSubmissionNo"], "finishRun": True,
                         "verdict": verdict, "summary": summary, "checks": [{"checkId": "batch-health",
                         "verdict": verdict, "summary": summary, "evidence": evidence}]})
        saved = read(snapshot)
        if (saved["verdict"], saved["summary"]) != (verdict, summary):
            raise ValueError("Retry the saved submission with the same verdict and summary.")
        run_request(work, "/submissions", saved)
        state = run_request(work)
    if state["status"] != "FINISHED":
        raise ValueError(f"Run is {state['status']}. Start a new trial.")
    stored = run_request(work, "/submissions/" + state["finalSubmissionId"])
    accepted = stored["snapshot"]
    if (accepted["verdict"], accepted["summary"]) != (verdict, summary):
        raise ValueError("This Run already has a different final assessment.")
    record = save_record(work, state["id"], accepted)
    return {"recordPath": str(record), "runId": state["id"], "result": state["result"], "decision": state["decision"],
            "checks": accepted["checks"]}


def start(case, show_console=False):
    with tempfile.TemporaryDirectory(prefix="first-batch-recovery-") as tmp:
        work = Path(tmp)
        binaries = ROOT / "bin" if (ROOT / "go.mod").is_file() else ROOT
        server_bin, cli_bin = binaries / "open-guardrail", binaries / "guardrailctl"
        if binaries == ROOT / "bin" and not (server_bin.is_file() and cli_bin.is_file()):
            print("Building server and CLI from source…", flush=True)
            subprocess.run(["make", "build"], check=True, cwd=ROOT)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            address = "127.0.0.1:" + str(listener.getsockname()[1])
        origin = "http://" + address
        env = {k: v for k, v in os.environ.items()
               if not k.startswith("OPEN_GUARDRAIL_") and k != "DATABASE_URL"}
        env.update(OPEN_GUARDRAIL_URL=origin)

        def cli(*args, **kwargs):
            return run(str(cli_bin), *args, env=env, **kwargs)

        with (work / "server.log").open("w") as log:
            service = subprocess.Popen([server_bin, "serve", "--listen", address,
                                        "--data-dir", work / "db"], env=env, cwd=work,
                                       stdout=log, stderr=subprocess.STDOUT)
            try:
                wait_for_service(service, origin)
                cli("scene", "apply", "--key", "first-batch-recovery", "--name", "First batch",
                    "--spec", str(EXAMPLE / "first-batch.yaml"), "--publish")
                data = read(EXAMPLE / "first-batch-data.json")
                scope = data.pop("changeSet")
                if case == "logs-unavailable":
                    del data["windows"][0]["logs"]
                elif case == "clean":
                    data["windows"][0]["logs"] = [row for row in data["windows"][0]["logs"]
                                                  if row["instance"] not in scope["instances"]]
                private_json(work / "observations.json", data)
                private_json(work / "scope.json", scope)
                created = json.loads(cli("run", "create", "--scene", "first-batch-recovery",
                     "--change-set", str(work / "scope.json"),
                    "--operation-id", "first-batch-create", "--submission-credential-file", str(work / "run-key.json")))
                private_json(work / "run.json", created)
                private_json(work / "connection.json", {"server": origin})
                print(f"Run {created['id']} is ready. Keep this terminal open.\n"
                      f"In your coding agent, from this checkout, ask:\n\n"
                      f"Use $open-guardrail-demo for the first-batch Run in {shlex.quote(tmp)}. "
                      "Show each query and its findings, then submit your assessment.", flush=True)
                if show_console:
                    print(f"Optional local console: {origin} — open directly.", flush=True)
                service.wait()
                raise RuntimeError("Local service exited before the trial ended.")
            finally:
                service.terminate()
                try:
                    service.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    service.kill()
                    service.wait()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    actions = parser.add_subparsers(dest="action", required=True)
    launch = actions.add_parser("start")
    launch.add_argument("--case", choices=("write-error", "clean", "logs-unavailable"), default="write-error")
    launch.add_argument("--console", action="store_true", help="show browser access details for this temporary Run")
    lookup = actions.add_parser("query")
    lookup.add_argument("--work-dir", type=Path, required=True)
    lookup.add_argument("source", choices=("metrics", "logs", "alerts", "changes", "records"))
    lookup.add_argument("--record-id")
    inspect = actions.add_parser("spec", help="read the Run and its frozen Spec")
    inspect.add_argument("--work-dir", type=Path, required=True)
    finish = actions.add_parser("submit", help="submit the agent's assessment with its queried evidence")
    finish.add_argument("--work-dir", type=Path, required=True)
    finish.add_argument("--verdict", choices=("PASS", "BLOCKED", "UNKNOWN"), required=True)
    finish.add_argument("--summary", required=True)
    args = parser.parse_args()
    if args.action == "start":
        start(args.case, args.console)
    elif args.action == "spec":
        state = run_request(args.work_dir)
        revision = run_request(args.work_dir, "/policy-revision")
        print(json.dumps({"runId": state["id"], "scope": state["changeSet"],
                          "specDigest": state["specDigest"]}, indent=2))
        print(revision["yaml"])
    elif args.action == "submit":
        print(json.dumps(submit(args.work_dir, args.verdict, args.summary), indent=2))
    else:
        print(json.dumps(query(args.work_dir, args.source, args.record_id), indent=2))


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        pass
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        raise SystemExit(f"First-batch trial: {error}")
