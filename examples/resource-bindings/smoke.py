#!/usr/bin/env python3
"""Exercise resource bindings through the CLI against a disposable real service."""
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("demo_smoke", ROOT / "examples/agent-demo/smoke.py")
demo = importlib.util.module_from_spec(spec)
spec.loader.exec_module(demo)


def verify(binaries):
    for name in ("open-guardrail", "guardrailctl"):
        if not (binaries / name).is_file():
            raise ValueError("Build or extract both native binaries before running this example")
    environment = {key: os.environ[key] for key in ("PATH", "HOME", "LANG", "TMPDIR") if key in os.environ}
    with tempfile.TemporaryDirectory(prefix="guardrail-binding-") as temporary:
        work = Path(temporary)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        origin = "http://127.0.0.1:" + str(port)
        with (work / "server.log").open("w") as log:
            process = subprocess.Popen([str(binaries / "open-guardrail"), "serve", "--listen",
                                        "127.0.0.1:" + str(port), "--data-dir", str(work / "data")],
                                       cwd=work, env=environment, stdout=log, stderr=log)
        try:
            demo.wait_for_service(process, origin)
            session, key = work / "session.json", work / "key.json"

            def cli(*args, input_text=None, expected=0):
                result = subprocess.run([str(binaries / "guardrailctl"), *args[:2], "--server", origin, *args[2:]],
                                        cwd=work, env=environment, input=input_text, text=True,
                                        capture_output=True, timeout=40)
                if result.returncode != expected:
                    raise RuntimeError("CLI " + " ".join(args[:2]) + " returned " +
                                       str(result.returncode) + "; expected " + str(expected))
                return json.loads(result.stdout) if result.stdout.strip().startswith("{") else None

            password = secrets.token_urlsafe(24) + "\n"
            cli("bootstrap", "init", "--username", "demo", "--password-stdin", input_text=password)
            cli("auth", "login", "--username", "demo", "--password-stdin", "--session-file", str(session), input_text=password)
            cli("auth", "create-key", "--session-file", str(session), "--expires-in", "1h", "--result-file", str(key))
            initialized = cli("scene", "apply", "--session-file", str(session), "--key", "binding-demo",
                              "--name", "Resource binding demo", "--spec", str(ROOT / "examples/policies/recovery.yaml"), "--publish")
            admin = json.loads(session.read_text())["token"]

            def api(method, path, body):
                request = Request(origin + "/api/v1alpha1/" + path, method=method,
                                  data=json.dumps(body).encode(), headers={"Content-Type": "application/json",
                                  "Authorization": "Bearer " + admin, "X-Guardrail-CSRF": "1",
                                  "Idempotency-Key": secrets.token_hex(16)})
                with urlopen(request, timeout=15) as response:
                    return json.load(response)

            # Existing policy-publication HTTP APIs prepare a non-default Policy.
            policy = api("POST", "scenes/binding-demo/policies", {"name": "Repository checks"})
            yaml = (ROOT / "examples/policies/recovery.yaml").read_text()
            first = api("POST", "policies/" + policy["id"] + "/revisions", {"yaml": yaml})
            api("PUT", "policies/" + policy["id"] + "/active-revision", {"policyRevisionId": first["id"]})
            prefix = ("--session-file", str(session), "--scene", "binding-demo")
            for resource_id in (".", ".."):
                cli("binding", "put", *prefix, "--kind", "repository", "--resource-id", resource_id, "--policy-id", policy["id"])
                cli("binding", "delete", *prefix, "--kind", "repository", "--resource-id", resource_id)
            resource_id = "demo/repository %2F?#中文"
            target = ("--kind", "repository", "--resource-id", resource_id)
            cli("binding", "put", *prefix, *target, "--policy-id", policy["id"])
            cli("binding", "put", *prefix, "--kind", "repository", "--resource-id", "z-last", "--policy-id", policy["id"])
            page = cli("binding", "list", *prefix, "--limit", "1")
            if page["items"][0]["resource"]["id"] != resource_id or not page.get("nextAfter"):
                raise RuntimeError("Encoded resource or first page mismatch")
            page = cli("binding", "list", *prefix, "--limit", "1", "--after", page["nextAfter"])
            if page["items"][0]["resource"]["id"] != "z-last":
                raise RuntimeError("Binding cursor did not select the second page")
            change = work / "change.json"
            change.write_text('{"changeSetRef":"binding-demo"}')
            refs = [{"kind": "repository", "id": resource_id}]

            def create(name, resources=None, expected=0):
                args = ["run", "create", "--key-file", str(key), "--scene", "binding-demo",
                        "--change-set", str(change), "--operation-id", name,
                        "--submission-credential-file", str(work / (name + ".json"))]
                if resources is not None:
                    path = work / "resources.json"
                    path.write_text(json.dumps(resources))
                    args.extend(["--resources", str(path)])
                result = cli(*args, expected=expected)
                if expected and (work / (name + ".json")).exists():
                    raise RuntimeError("Rejected Run creation retained a credential output")
                return result

            old = create("first", refs)
            second = api("POST", "policies/" + policy["id"] + "/revisions",
                         {"yaml": yaml.replace("Recovery readiness", "Updated recovery readiness")})
            api("PUT", "policies/" + policy["id"] + "/active-revision",
                {"policyRevisionId": second["id"], "expectedActiveRevisionId": first["id"]})
            active = create("active", refs)
            cli("binding", "put", *prefix, *target, "--policy-id", policy["id"], "--revision-id", first["id"])
            pinned = create("pinned", refs)
            frozen = cli("run", "get", "--key-file", str(key), old["id"])
            if (old["policyId"] != policy["id"] or frozen["policyRevisionId"] != first["id"] or
                    active["policyRevisionId"] != second["id"] or second["id"] == first["id"] or
                    active["selection"]["revisionMode"] != "active" or pinned["policyRevisionId"] != first["id"] or
                    pinned["selection"]["revisionMode"] != "pinned"):
                raise RuntimeError("Frozen/active/pinned selection mismatch")
            # Existing CLI retries preserve a saved credential and report that
            # the plaintext credential cannot be replayed into a new file.
            original = work / "first.json"
            original.rename(work / "first.saved.json")
            create("first", refs, expected=5)
            create("partial", refs + [{"kind": "repository", "id": "unbound"}], expected=5)
            cli("binding", "put", *prefix, "--kind", "repository", "--resource-id", "other",
                "--policy-id", initialized["scene"]["defaultPolicyId"])
            create("conflict", refs + [{"kind": "repository", "id": "other"}], expected=5)
            for name, resources in (("omitted", None), ("unbound", [{"kind": "repository", "id": "unknown"}])):
                result = create(name, resources)
                if result["policyId"] != initialized["scene"]["defaultPolicyId"] or "selection" in result:
                    raise RuntimeError("Default fallback mismatch")
            cli("binding", "delete", *prefix, *target)
            cli("binding", "delete", *prefix, *target)
            removed = create("after-delete", refs)
            if removed["policyId"] != initialized["scene"]["defaultPolicyId"]:
                raise RuntimeError("Deleted binding still selected")
            # Audit every binding mutation, including before/after values, then
            # export without the private resource identifiers or stored details.
            audit_args = ("--session-file", str(session), "--resource-type", "policy-binding", "--limit", "2")
            events, cursor = [], None
            while True:
                page = cli("audit", "list", *audit_args, *(("--cursor", cursor) if cursor else ()))
                events.extend(page["items"])
                cursor = page.get("nextCursor")
                if not cursor:
                    break
            ids = [item["id"] for item in events]
            if len(ids) != len(set(ids)) or not {"policy_binding_created", "policy_binding_updated", "policy_binding_deleted"}.issubset({item["eventType"] for item in events}):
                raise RuntimeError("Audit mutation chain or pagination mismatch")
            export_path = work / "audit-export.json"
            cli("audit", "export", *audit_args, "--output", str(export_path))
            exported = json.loads(export_path.read_text())
            if not exported["redacted"] or any(any(key in item for key in ("actorId", "resourceId", "details")) for item in exported["items"]):
                raise RuntimeError("Audit export contains private fields")
            if export_path.stat().st_mode & 0o777 != 0o600:
                raise RuntimeError("Audit export has unsafe permissions")
            original_export = export_path.read_bytes()
            cli("audit", "export", *audit_args, "--output", str(export_path), expected=2)
            if export_path.read_bytes() != original_export:
                raise RuntimeError("Existing export overwritten")
            snapshot = {"baseSubmissionNo": 0, "finishRun": True, "verdict": "BLOCKED", "summary": "Private diagnostic context",
                        "checks": [{"checkId": "readiness", "verdict": "BLOCKED", "summary": "Private health details",
                                    "evidence": [{"requirementId": "health", "observedAt": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
                                                  "querySummary": "Private source query", "resultSummary": "Private observed failure"}]}]}
            snapshot_path = work / "snapshot.json"
            snapshot_path.write_text(json.dumps(snapshot))
            cli("run", "submit", "--submission-credential-file", str(work / "after-delete.json"),
                "--submission", str(snapshot_path), "--operation-id", "diagnose", removed["id"])
            report_path = work / "run-report.json"
            cli("run", "report", "--key-file", str(key), "--output", str(report_path), removed["id"])
            report_text = report_path.read_text()
            report = json.loads(report_text)
            evidence = report["finalSubmission"]["checks"][0]["evidence"][0]
            if (report["run"].get("decision") != "DENY" or report["reasons"] != ["CHECK_BLOCKED"] or
                    report["revision"]["id"] != removed["policyRevisionId"] or "Private" in report_text or
                    evidence["resultDigest"] != hashlib.sha256(b"Private observed failure").hexdigest()):
                raise RuntimeError("Redacted Run report lost decision or evidence linkage")
            print(json.dumps({"result": "PASS", "checks": ["resource-encoding", "pagination", "active-revision",
                             "pinned-revision", "frozen-run", "idempotent-replay", "partial-binding-denied",
                             "conflicting-bindings-denied", "default-fallback", "idempotent-delete", "audit-pagination", "audit-redaction", "run-diagnostics"],
                             "runId": old["id"], "policyId": policy["id"]}))
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binaries", type=Path, default=ROOT / "bin", help="directory containing both native binaries")
    args = parser.parse_args()
    try:
        verify(args.binaries.resolve())
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    main()
