#!/usr/bin/env python3
"""Correlate a release order with a Run through the CLI against a real disposable service."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import time
from datetime import datetime, timezone
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
    with tempfile.TemporaryDirectory(prefix="guardrail-business-") as temporary:
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
            def cli(*args, input_text=None, expected=0):
                result = subprocess.run([str(binaries / "guardrailctl"), *args[:2], "--server", origin, *args[2:]],
                                        cwd=work, env=environment, input=input_text, text=True,
                                        capture_output=True, timeout=40)
                if result.returncode != expected:
                    raise RuntimeError("CLI " + " ".join(args[:2]) + " returned " +
                                       str(result.returncode) + "; expected " + str(expected))
                if expected:
                    return result.stderr
                return json.loads(result.stdout) if result.stdout.strip().startswith("{") else None

            for scene in ("release-demo", "other-release"):
                cli("scene", "apply",  "--key", scene,
                    "--name", "Release order checks", "--spec", str(ROOT / "examples/policies/recovery.yaml"), "--publish")
            business = "发布/42 %?# +"
            change = work / "change.json"
            change.write_text(json.dumps({"releaseOrder": business, "batch": 1}))

            def create(name, business_id=business, scene="release-demo", expected=0):
                return cli("run", "create",  "--scene", scene,
                           "--business-id", business_id, "--change-set", str(change),
                           "--operation-id", name, "--submission-credential-file", str(work / (name + ".json")), expected=expected)

            first = create("first")
            credential = (work / "first.json").read_bytes()
            replay = create("retry", expected=5)
            if first["id"] not in replay or (work / "retry.json").exists():
                raise RuntimeError("Replay did not identify the original Run without reissuing credentials")
            create("first", expected=2)
            if (work / "first.json").read_bytes() != credential:
                raise RuntimeError("Existing credential was overwritten")
            listing = cli("run", "list",  "--scene", "release-demo", "--business-id", business)
            if [item["id"] for item in listing["items"]] != [first["id"]]:
                raise RuntimeError("Business ID lookup/encoding mismatch")
            change.write_text('{"releaseOrder":"different content"}')
            conflict = create("conflict", expected=5)
            if "BUSINESS_ID_CONFLICT" not in conflict or (work / "conflict.json").exists():
                raise RuntimeError("Conflicting content was not rejected")
            change.write_text(json.dumps({"releaseOrder": business, "batch": 1}))
            other = create("other-scene", scene="other-release")
            if other["id"] == first["id"]:
                raise RuntimeError("Business ID leaked across scene boundaries")
            # Distinct operation IDs and output files emulate independent callers.
            # Exactly one request receives the one-time credential.
            def concurrent(index):
                name = "parallel-" + str(index)
                result = subprocess.run([str(binaries / "guardrailctl"), "run", "create", "--server", origin,
                                          "--scene", "release-demo", "--business-id", "release-43",
                                         "--change-set", str(change), "--operation-id", name,
                                         "--submission-credential-file", str(work / (name + ".json"))],
                                        cwd=work, env=environment, capture_output=True, text=True, timeout=40)
                return name, result
            with ThreadPoolExecutor(max_workers=8) as executor:
                results = list(executor.map(concurrent, range(8)))
            winners = [(name, json.loads(result.stdout)) for name, result in results if result.returncode == 0]
            if len(winners) != 1:
                raise RuntimeError("Concurrent business ID creation did not have one winner")
            winner = winners[0][1]["id"]
            for name, result in results:
                if result.returncode != 0 and (result.returncode != 5 or winner not in result.stderr or (work / (name + ".json")).exists()):
                    raise RuntimeError("Concurrent replay lost its Run or retained a credential")
            snapshot = {"baseSubmissionNo": 0, "finishRun": True, "verdict": "BLOCKED", "summary": "Release blocked",
                        "checks": [{"checkId": "readiness", "verdict": "BLOCKED", "summary": "Health failure",
                                    "evidence": [{"requirementId": "health", "observedAt": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
                                                  "querySummary": "Release health", "resultSummary": "Health failed"}]}]}
            submission = work / "submission.json"
            submission.write_text(json.dumps(snapshot))
            cli("run", "submit", "--submission-credential-file", str(work / "first.json"), "--submission", str(submission), first["id"])
            terminal = cli("run", "list",  "--scene", "release-demo", "--business-id", business, "--status", "FINISHED")
            if len(terminal["items"]) != 1 or terminal["items"][0]["decision"] != "DENY":
                raise RuntimeError("Terminal business lookup failed")
            replay = create("terminal-retry", expected=5)
            if first["id"] not in replay or "rotate-credential" in replay:
                raise RuntimeError("Terminal replay gave invalid recovery guidance")
            report = work / "report.json"
            cli("run", "report",  "--output", str(report), first["id"])
            saved = json.loads(report.read_text())
            if saved["run"]["businessId"] != business or saved["revision"]["id"] != first["policyRevisionId"] or not saved.get("finalSubmission"):
                raise RuntimeError("Report lost the business, frozen policy or evidence linkage")
            # Short TTL is configured directly through the Scene endpoint.

            request = Request(origin + "/api/v1alpha1/scenes/release-demo", method="PATCH",
                              data=b'{"runTtlSeconds":1}', headers={"Content-Type":"application/json",
                              "X-Guardrail-CSRF":"1"})
            with urlopen(request, timeout=15) as response:
                json.load(response)
            expired = create("expiry", business_id="release-44")
            time.sleep(max(0, expired["deadlineAt"] - time.time()) + 0.1)
            expiry = cli("run", "list",  "--scene", "release-demo", "--business-id", "release-44", "--status", "EXPIRED")
            if len(expiry["items"]) != 1 or expiry["items"][0]["id"] != expired["id"]:
                raise RuntimeError("Expired business ID lookup failed")
            create("expiry-retry", business_id="release-44", expected=5)
            fresh = create("new-attempt", business_id="release-45")
            if fresh["id"] == expired["id"]:
                raise RuntimeError("New attempt reused the expired Run")
            print(json.dumps({"result":"PASS", "checks":["exact-encoded-lookup","credential-preservation","conflict",
                             "scene-isolation","eight-concurrent-callers","terminal-replay","report-linkage","expired-lookup","new-attempt"],
                             "runId":first["id"]}))
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
