#!/usr/bin/env python3
"""Run the real Action script against a temporary local service; no account or model needed."""

import argparse
import json
import os
from pathlib import Path
import shlex
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from urllib.error import URLError
from urllib.request import urlopen


HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]


def outputs(path):
    return dict(line.split("=", 1) for line in path.read_text().splitlines() if "=" in line)


def options(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--case", choices=("all", "pass", "blocked", "unknown"), default="all")
    parser.add_argument("--binaries-dir", type=Path, default=ROOT / "bin")
    parser.add_argument("--keep-running", action="store_true", help="keep the temporary console open until Ctrl-C")
    parser.add_argument("--spec-file", type=Path, help="your own Spec YAML (requires --assessment-command)")
    parser.add_argument("--assessment-command", help="your trusted evaluator command, run from the current directory")
    parser.add_argument("--expect-result", choices=("PASS", "BLOCKED", "UNKNOWN"),
                        help="expected custom assessment result (default: PASS)")
    args = parser.parse_args(argv)
    if bool(args.spec_file) != bool(args.assessment_command):
        parser.error("provide both --spec-file and --assessment-command for a custom scenario")
    if args.spec_file:
        args.spec_file = args.spec_file.resolve()
        if not args.spec_file.is_file():
            parser.error("--spec-file must point to a readable Spec YAML file")
        if args.case != "all":
            parser.error("--case selects built-in observations; omit it for a custom scenario")
    elif args.expect_result:
        parser.error("--expect-result is only used with a custom Spec and assessment command")
    binaries = args.binaries_dir.resolve()
    for name in ("open-guardrail", "guardrailctl"):
        if not os.access(binaries / name, os.X_OK):
            parser.error("build the server and CLI first with make build, or set --binaries-dir")
    for name in ("bash", "jq"):
        if not shutil.which(name):
            parser.error(name + " must be installed")
    return args


def main(argv=None):
    args = options(argv)
    binaries = args.binaries_dir.resolve()

    with tempfile.TemporaryDirectory(prefix="og-github-actions-") as temporary:
        work = Path(temporary)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        origin = f"http://127.0.0.1:{port}"
        environment = {key: value for key, value in os.environ.items()
                       if not key.startswith(("OPEN_GUARDRAIL_", "OG_")) and key != "DATABASE_URL"}
        with (work / "server.log").open("w", encoding="utf-8") as log:
            server = subprocess.Popen(
                [str(binaries / "open-guardrail"), "serve", "--listen", f"127.0.0.1:{port}",
                 "--data-dir", str(work / "data")],
                cwd=work, env=environment, stdout=log, stderr=log,
            )
        try:
            deadline = time.monotonic() + 40
            while True:
                if server.poll() is not None:
                    diagnostic = (work / "server.log").read_text(encoding="utf-8")[-2000:]
                    raise RuntimeError(f"temporary service exited before readiness (exit {server.returncode}): " + diagnostic)
                try:
                    with urlopen(origin + "/readyz", timeout=0.2) as response:
                        if response.status == 200:
                            break
                except (URLError, OSError):
                    pass
                if time.monotonic() >= deadline:
                    diagnostic = (work / "server.log").read_text(encoding="utf-8")[-2000:]
                    raise RuntimeError("temporary service did not become ready within 40 seconds: " + diagnostic)
                time.sleep(0.1)

            print("Temporary console: " + origin, flush=True)
            cases = (("custom",) if args.spec_file else
                     ("pass", "blocked", "unknown") if args.case == "all" else (args.case,))
            for case in cases:
                expected = (args.expect_result or "PASS") if args.spec_file else {
                    "pass": "PASS", "blocked": "BLOCKED", "unknown": "UNKNOWN",
                }[case]
                allowed = expected == "PASS"
                output = work / (case + ".outputs")
                output.touch(mode=0o600)
                env = dict(environment, OG_MODE="evaluate", OG_SERVER=origin,
                           OG_SCENE="github-actions-demo", OG_RUN_ID="",
                           OG_SPEC_FILE_INPUT=str(args.spec_file or HERE / "release-gate.yaml"),
                           OG_ASSESSMENT_COMMAND=args.assessment_command or shlex.join([
                               sys.executable, str(HERE / "example/assess.py"),
                               "--signals", str(HERE / "example" / (case + ".json")),
                           ]),
                           OG_BUSINESS_ID="demo-" + case, OG_CHANGE_SET_JSON="",
                           OG_DEPLOYMENT_ENVIRONMENT="synthetic", OG_OPERATION_ID="demo-" + case,
                           OG_WAIT_TIMEOUT="30s", OG_CTL=str(binaries / "guardrailctl"),
                           GITHUB_OUTPUT=str(output), RUNNER_TEMP=str(work), TMPDIR=str(work),
                           GITHUB_REPOSITORY="example/sample-app", GITHUB_RUN_ID="local-demo-" + case,
                           GITHUB_RUN_ATTEMPT="1", GITHUB_SHA="sample-commit", GITHUB_REF="refs/heads/main")
                evaluated = subprocess.run(["bash", str(HERE / "gate.sh")], env=env, timeout=60,
                                           check=False, capture_output=True, text=True)
                values = outputs(output)
                if values.get("result") != expected or (evaluated.returncode == 0) != allowed:
                    raise RuntimeError(case + ": unexpected Action result: " + evaluated.stdout + evaluated.stderr)

                # A fresh Action invocation simulates the independent deployment job.
                verify_output = work / (case + "-verify.outputs")
                verify_output.touch(mode=0o600)
                verify_env = dict(env, OG_MODE="verify", OG_RUN_ID=values["run-id"],
                                  OG_SPEC_FILE_INPUT="", OG_ASSESSMENT_COMMAND="",
                                  GITHUB_OUTPUT=str(verify_output))
                verified = subprocess.run(["bash", str(HERE / "gate.sh")], env=verify_env, timeout=30,
                                          check=False, capture_output=True, text=True)
                if (verified.returncode == 0) != allowed:
                    raise RuntimeError(case + ": independent verification disagrees")
                with urlopen(origin + "/runtime/v1alpha1/runs/" + values["run-id"], timeout=5) as response:
                    run = json.load(response)
                with urlopen(origin + "/runtime/v1alpha1/runs/" + values["run-id"] +
                             "/submissions/" + run["finalSubmissionId"], timeout=5) as response:
                    accepted = json.load(response)
                if run["status"] != "FINISHED" or accepted["snapshot"]["verdict"] != expected:
                    raise RuntimeError(case + ": final assessment was not persisted")
                if any(work.rglob("submission-credential.json")):
                    raise RuntimeError("Action left a local Submission Credential behind")
                print(f"{case}: {values['run-id']} {expected}/{values['decision']} "
                      f"deployment={'continued (simulation)' if verified.returncode == 0 else 'blocked'}", flush=True)

            print("All selected cases matched their expected results. No production changes were made.", flush=True)
            if args.keep_running:
                print("Inspect Runs in the console above; press Ctrl-C to remove the temporary service and data.", flush=True)
                try:
                    while server.poll() is None:
                        time.sleep(0.2)
                except KeyboardInterrupt:
                    pass
        finally:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait(timeout=5)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.TimeoutExpired) as error:
        print("GitHub Actions demo failed: " + str(error), file=sys.stderr)
        sys.exit(1)
