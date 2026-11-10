#!/usr/bin/env python3
"""Browser demonstration for the file-cleanup scenario."""

import argparse
from datetime import datetime, timezone
import getpass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import importlib.util
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
from urllib.error import URLError
from urllib.request import urlopen
import uuid


HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
scenario_spec = importlib.util.spec_from_file_location(
    "file_cleanup_scenario", ROOT / "scenarios" / "file-cleanup" / "runtime.py")
scenario = importlib.util.module_from_spec(scenario_spec)
scenario_spec.loader.exec_module(scenario)

# Keep these runtime exports for the demo's Python API and its existing tests.
SEEDS = scenario.SEEDS
TEMPORARY = scenario.TEMPORARY
identity = scenario.identity
snapshot_identity = scenario.snapshot_identity
CommandFailed = scenario.CommandFailed
public_error = scenario.public_error
command = scenario.command
policy_yaml = scenario.policy_yaml
FileTool = scenario.FileTool
assess = scenario.assess
Gateway = scenario.Gateway

class Demo:
    def __init__(self, gateway, private):
        self.gateway, self.private = gateway, Path(private)
        self.lock = threading.RLock()
        self.tool = FileTool(self.private)
        self.busy, self.jobs = False, []
        self.csrf = secrets.token_urlsafe(24)

    def state(self):
        with self.lock:
            return {"busy": self.busy, "files": self.tool.inventory(), "workspace": str(self.tool.path),
                    "workspaceId": self.tool.workspace_id, "jobs": json.loads(json.dumps(self.jobs)),
                    "consoleUrl": self.gateway.origin, "csrfToken": self.csrf}

    def reset(self):
        with self.lock:
            if self.busy:
                raise ValueError("A task is still running")
            self.tool.close()
            self.tool = FileTool(self.private)

    def start(self, request):
        if (not isinstance(request, dict) or set(request) != {"targets", "maxFiles", "repair"}
            or type(request["maxFiles"]) is not int or request["maxFiles"] not in (1, 2)
            or type(request["repair"]) is not bool):
            raise ValueError("Invalid cleanup task")
        with self.lock:
            if self.busy:
                raise ValueError("A task is still running")
            self.tool.observe(request["targets"])
            self.busy = True
            job = {"id": uuid.uuid4().hex, "status": "running", "events": [], "records": [], "removed": [],
                   "workspaceId": self.tool.workspace_id,
                   "maxFiles": request["maxFiles"], "initialTargets": request["targets"]}
            self.jobs = [*self.jobs[-9:], job]
            threading.Thread(target=self.work, args=(job, request), daemon=True).start()
            return job["id"]

    def event(self, job, kind, **data):
        with self.lock:
            job["events"].append({"kind": kind, "at": datetime.now(timezone.utc).isoformat(), **data})

    def work(self, job, request):
        stage = "prepare"
        try:
            directory = self.private / ("task-" + job["id"])
            directory.mkdir(mode=0o700)
            spec_yaml = policy_yaml(request["maxFiles"])
            self.event(job, "started", targets=request["targets"])
            stage = "publish"
            self.gateway.publish(spec_yaml, directory)
            targets = request["targets"]
            for attempt in range(2):
                stage = "check"
                self.event(job, "checking", targets=targets)
                record = self.gateway.evaluate(self.tool, targets, request["maxFiles"], spec_yaml,
                                               directory / f"attempt-{attempt}-{uuid.uuid4().hex}",
                                               on_created=lambda run_id: self.event(job, "created", runId=run_id))
                with self.lock:
                    job["records"].append(record)
                self.event(job, "decision", runId=record["runId"], decision=record["decision"], targets=targets)
                if record["decision"] == "ALLOW":
                    stage = "execute"
                    removed = self.tool.execute(self.gateway, record, state_lock=self.lock)
                    with self.lock:
                        record["removed"] = removed
                        job["removed"] = removed
                    self.event(job, "executed", targets=removed)
                    break
                repaired = [x for x in targets if x in TEMPORARY and
                            x in {f["name"] for f in self.tool.inventory()}][:request["maxFiles"]]
                if not request["repair"] or attempt or not repaired or repaired == targets:
                    break
                targets = repaired
                self.event(job, "revised", targets=targets)
            with self.lock:
                job["status"] = "completed" if job["removed"] else "denied"
        except Exception as error:
            # An uncertain service write is not retried under a new operation ID.
            detail = public_error(error, stage)
            with self.lock:
                job["error"] = detail
                self.event(job, "error", error=detail)
                job["status"] = "error"
                job["removed"] = [name for record in job["records"] for name in record["removed"]]
        finally:
            with self.lock:
                self.busy = False


def handler(demo):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def respond(self, status, content, mime="application/json"):
            payload = content.encode() if isinstance(content, str) else json.dumps(content).encode()
            self.send_response(status)
            self.send_header("Content-Type", mime + "; charset=utf-8")
            self.send_header("Content-Length", str(len(payload)))
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Content-Type-Options", "nosniff")
            self.send_header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
            self.end_headers()
            self.wfile.write(payload)

        def valid_host(self):
            return self.headers.get("Host") == f"127.0.0.1:{self.server.server_port}"

        def do_GET(self):
            if not self.valid_host():
                return self.respond(403, {"error": "Invalid Host"})
            try:
                if self.path == "/":
                    self.respond(200, (HERE / "index.html").read_text(), "text/html")
                elif self.path == "/api/state":
                    self.respond(200, demo.state())
                else:
                    self.respond(404, {"error": "Not found"})
            except (OSError, ValueError):
                self.respond(503, {"error": "Demo files cannot be inspected. Stop and restart the demo."})

        def do_POST(self):
            expected_origin = f"http://127.0.0.1:{self.server.server_port}"
            if (not self.valid_host() or self.headers.get("Origin") != expected_origin
                or self.headers.get("Sec-Fetch-Site") == "cross-site"
                or not secrets.compare_digest(self.headers.get("X-Demo-Token", ""), demo.csrf)):
                return self.respond(403, {"error": "Use the local demo page"})
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if self.headers.get("Content-Type") != "application/json" or not 0 < length <= 4096:
                    return self.respond(400, {"error": "Expected a small JSON request"})
                request = json.loads(self.rfile.read(length))
                if self.path == "/api/tasks":
                    self.respond(202, {"jobId": demo.start(request)})
                elif self.path == "/api/reset" and request == {}:
                    demo.reset()
                    self.respond(200, {"ok": True})
                else:
                    self.respond(404, {"error": "Not found"})
            except (ValueError, OSError):
                self.respond(409, {"error": "The task could not start. Check the selection and running task."})
    return Handler


def binaries(directory, supplied=None):
    if supplied:
        return Path(supplied).resolve() / "open-guardrail", Path(supplied).resolve() / "guardrailctl"
    if not (ROOT / "go.mod").is_file():
        return ROOT / "open-guardrail", ROOT / "guardrailctl"
    if shutil.which(os.environ.get("GO", "go")) is None:
        raise ValueError("Install the Go version specified in go.mod to run from source")
    print("Building OpenGuardrail and guardrailctl…", flush=True)
    results = []
    for name in ("open-guardrail", "guardrailctl"):
        target = Path(directory) / name
        command(os.environ.get("GO", "go"), "build", "-o", str(target), "./cmd/" + name, timeout=300)
        results.append(target)
    return results


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--password-stdin", action="store_true")
    parser.add_argument("--binaries", type=Path, help="use existing native server/CLI binaries")
    parser.add_argument("--port", type=int, default=0, help="loopback demo page port (default: random)")
    args = parser.parse_args(argv)
    if sys.version_info < (3, 9):
        parser.error("Python 3.9+ is required")
    if args.password_stdin:
        password = sys.stdin.readline().rstrip("\r\n")
    else:
        if not sys.stdin.isatty():
            parser.error("Use a terminal for the hidden password prompts, or --password-stdin")
        password = getpass.getpass("Choose a password for the demo console (username: demo): ")
        if password != getpass.getpass("Confirm password: "):
            parser.error("Passwords do not match")
    if len(password) < 6 or len(password.encode()) > 128:
        parser.error("Use at least 6 characters and at most 128 UTF-8 bytes")
    with tempfile.TemporaryDirectory(prefix="open-guardrail-cleanup-") as temporary:
        private = Path(temporary)
        server_bin, cli_bin = binaries(private, args.binaries)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            runtime_port = listener.getsockname()[1]
        origin = f"http://127.0.0.1:{runtime_port}"
        environment = {k: v for k, v in os.environ.items()
                       if not k.startswith("OPEN_GUARDRAIL_") and k != "DATABASE_URL"}
        with (private / "server.log").open("w") as log:
            process = subprocess.Popen((str(server_bin), "serve", "--listen", f"127.0.0.1:{runtime_port}",
                                        "--data-dir", str(private / "data")), cwd=private,
                                       env=environment, stdout=log, stderr=log, start_new_session=True)
        gateway = Gateway(cli_bin, origin, private)
        demo = web = None
        try:
            deadline = time.monotonic() + 40
            while True:
                if process.poll() is not None:
                    raise CommandFailed("serve", process.returncode)
                if time.monotonic() > deadline:
                    raise RuntimeError("The isolated OpenGuardrail service did not become ready")
                try:
                    with urlopen(origin + "/readyz", timeout=.5) as response:
                        if response.status == 200:
                            break
                except (URLError, TimeoutError):
                    time.sleep(.1)
            gateway.setup(password)
            demo = Demo(gateway, private)
            web = ThreadingHTTPServer(("127.0.0.1", args.port), handler(demo))
            print(f"Demo: http://127.0.0.1:{web.server_port}/", flush=True)
            print(f"Console: {origin} — username demo; use the password you chose.", flush=True)
            print("The demo page needs no login. It reads real Runs and a disposable directory.", flush=True)
            print("Keep this terminal open. Ctrl-C stops both services and removes temporary data.", flush=True)
            web.serve_forever(poll_interval=.2)
        except KeyboardInterrupt:
            pass
        finally:
            if web:
                web.server_close()
            if demo:
                # Do not remove files or revoke the key while an execution is in flight.
                while demo.busy:
                    time.sleep(.1)
                demo.tool.close()
            try:
                gateway.close()
            finally:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, RuntimeError, subprocess.TimeoutExpired) as error:
        detail = public_error(error, "startup")
        sys.exit(f"Demo stopped [{detail['code']}]: {detail['message']}")
