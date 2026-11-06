#!/usr/bin/env python3
"""Exercise real local kind/Rollouts gates with isolated synthetic observations.

Requires installed Rollouts, a loaded demo image, native server/CLI binaries,
kubectl, its argo-rollouts plugin, age/age-keygen, and openssl. Credentials stay
in a fresh private work directory. Only sanitized results may be shared.
"""

import argparse
import base64
from datetime import datetime, timezone
import http.client
import http.server
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import socket
import ssl
import subprocess
import sys
import threading
import time
from urllib.parse import urlsplit
import urllib.request
import uuid

from integrations.recovery_agent import gate


CASES = ("healthy", "unhealthy", "unknown", "forged", "timeout", "expired", "unavailable", "pod-loss")


class Smoke:
    def __init__(self, args):
        self.args = args
        self.work = Path(args.work_dir).resolve()
        if self.work == gate.ROOT or gate.ROOT in self.work.parents:
            raise ValueError("private smoke data must be outside the repository")
        self.namespace = args.namespace or "guardrail-smoke-" + uuid.uuid4().hex[:8]
        if not re.fullmatch(r"[a-z0-9]([-a-z0-9]*[a-z0-9])?", self.namespace) or len(self.namespace) > 63:
            raise ValueError("namespace must be a Kubernetes DNS label")
        if not args.context.startswith("kind-"):
            raise ValueError("this smoke supports an explicit local kind context only")
        ipaddress.IPv4Address(args.host_ip)
        self.env = {key: value for key, value in os.environ.items()
                    if not key.startswith("OPEN_GUARDRAIL_") and key != "DATABASE_URL"}
        self.env["OPEN_GUARDRAIL_SESSION_FILE"] = str(self.work / "admin-session.json")
        self.cli = str(Path(args.cli).resolve())
        self.server_bin = str(Path(args.server_bin).resolve())
        self.service = self.proxy = self.server_log = None
        self.results = []

    def command(self, *args, input_text=None, check=True):
        result = subprocess.run(args, cwd=gate.ROOT, env=self.env, input=input_text,
                                text=True, capture_output=True, timeout=240)
        if check and result.returncode:
            # Never echo bodies, parameters, passwords, tokens or raw errors.
            raise RuntimeError(f"{Path(args[0]).name} {args[1]} failed (exit {result.returncode})")
        return result

    def kubectl(self, *args, **kwargs):
        return self.command("kubectl", "--context", self.args.context,
                            "--namespace", self.namespace, *args, **kwargs)

    def cli_json(self, *args):
        return json.loads(self.command(self.cli, *args).stdout)

    def apply(self, obj):
        self.kubectl("apply", "-f", "-", input_text=json.dumps(obj))

    def resource(self, kind, name=None):
        return json.loads(self.kubectl("get", kind, *([name] if name else []), "-o", "json").stdout)

    def manifest(self, name):
        # kubectl converts the checked-in YAML; no separate YAML dependency.
        return json.loads(self.kubectl("create", "--dry-run=client", "-f",
            str(gate.ROOT / "integrations/argo" / name), "-o", "json").stdout)

    @staticmethod
    def wait(description, predicate, seconds=180):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            value = predicate()
            if value:
                return value
            time.sleep(1)
        raise RuntimeError("Timed out: " + description)

    def preflight(self):
        server = self.command("kubectl", "--context", self.args.context, "config", "view",
                              "--minify", "-o", "jsonpath={.clusters[0].cluster.server}").stdout
        if urlsplit(server).hostname not in ("127.0.0.1", "localhost", "::1"):
            raise ValueError("kind API server must use a loopback endpoint")
        if self.kubectl("get", "namespace", self.namespace, "--ignore-not-found", "-o", "name").stdout.strip():
            raise ValueError("namespace already exists; choose a fresh test namespace")
        self.work.mkdir(mode=0o700)
        print("Private work directory:", self.work, flush=True)
        print("Test namespace:", self.namespace, flush=True)

    def start_service(self):
        cert, key = self.work / "tls.crt", self.work / "tls.key"
        conf = self.work / "openssl.cnf"
        conf.write_text("[req]\ndistinguished_name=dn\nx509_extensions=ext\nprompt=no\n"
            "[dn]\nCN=OpenGuardrail local smoke\n[ext]\nsubjectAltName=IP:" + self.args.host_ip +
            "\nbasicConstraints=critical,CA:TRUE\nkeyUsage=critical,digitalSignature,keyEncipherment,keyCertSign\n")
        self.command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "2",
                     "-config", str(conf), "-keyout", str(key), "-out", str(cert))
        key.chmod(0o600)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            backend_port = listener.getsockname()[1]

        class Proxy(http.server.BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *args):
                pass

            def handle_request(self):
                size = int(self.headers.get("Content-Length", "0"))
                if size < 0 or size > (1 << 20):
                    self.send_error(413)
                    return
                body = self.rfile.read(size)
                if not self.server.available:
                    self.send_error(503)
                    return
                if self.command == "GET" and self.path.startswith("/runtime/v1alpha1/runs/"):
                    time.sleep(self.server.run_read_delay)
                connection = http.client.HTTPConnection("127.0.0.1", backend_port, timeout=20)
                try:
                    connection.request(self.command, self.path, body, dict(self.headers))
                    response = connection.getresponse()
                    data = response.read()
                    self.send_response(response.status)
                    for name, value in response.getheaders():
                        if name.lower() not in ("transfer-encoding", "connection", "content-length"):
                            self.send_header(name, value)
                    self.send_header("Content-Length", str(len(data)))
                    self.end_headers()
                    self.wfile.write(data)
                finally:
                    connection.close()

            do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = handle_request

        self.proxy = http.server.ThreadingHTTPServer((self.args.host_ip, 0), Proxy)
        self.proxy.available = True
        self.proxy.run_read_delay = 0
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(cert, key)
        self.proxy.socket = context.wrap_socket(self.proxy.socket, server_side=True)
        threading.Thread(target=self.proxy.serve_forever, daemon=True).start()
        self.origin = f"https://{self.args.host_ip}:{self.proxy.server_port}"
        self.env.update(OPEN_GUARDRAIL_URL=self.origin, OPEN_GUARDRAIL_PUBLIC_URL=self.origin,
                        SSL_CERT_FILE=str(cert))
        self.server_log = (self.work / "server.log").open("w")
        self.service = subprocess.Popen((self.server_bin, "serve", "--listen", f"127.0.0.1:{backend_port}",
            "--data-dir", str(self.work / "data")), cwd=self.work, env=self.env,
            stdout=subprocess.DEVNULL, stderr=self.server_log)

        def ready():
            if self.service.poll() is not None:
                raise RuntimeError("temporary service exited")
            try:
                with urllib.request.urlopen(f"http://127.0.0.1:{backend_port}/readyz", timeout=1) as response:
                    return response.status == 200
            except OSError:
                return False
        self.wait("temporary service readiness", ready, 30)

    def publish_spec(self, validity=900):
        path = self.work / f"recovery-{validity}.yaml"
        if not path.exists():
            path.write_text((gate.ROOT / "examples/policies/recovery.yaml").read_text().replace(
                "validForSeconds: 900", f"validForSeconds: {validity}"))
        self.command(self.cli, "scene", "apply", "--key", "recovery", "--name", "Recovery",
                     "--spec", str(path), "--publish")

    def setup(self):
        password = secrets.token_urlsafe(24)
        self.command(self.cli, "bootstrap", "init", "--token-file", str(self.work / "data/bootstrap.token"),
                     "--username", "demo", "--password-stdin", input_text=password + "\n")
        self.command(self.cli, "auth", "login", "--username", "demo", "--password-stdin", input_text=password + "\n")
        self.publish_spec()
        self.key_path = self.work / "project-key.json"
        self.command(self.cli, "auth", "create-key", "--result-file", str(self.key_path))
        # create, rather than apply, refuses to overwrite an existing namespace.
        self.kubectl("create", "namespace", self.namespace)
        self.apply({"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "open-guardrail-project-key"},
                    "data": {"project-key.json": base64.b64encode(self.key_path.read_bytes()).decode()}})
        self.apply({"apiVersion": "v1", "kind": "ConfigMap", "metadata": {"name": "guardrail-test-ca"},
                    "data": {"ca.crt": (self.work / "tls.crt").read_text()}})
        self.install_template()
        rollout = self.manifest("rollout.yaml")
        rollout["spec"]["analysis"] = {"successfulRunHistoryLimit": len(CASES),
                                        "unsuccessfulRunHistoryLimit": len(CASES)}
        self.apply(rollout)
        self.status()

    def install_template(self, timeout=780):
        template = self.manifest("analysis-template.yaml")
        pod = template["spec"]["metrics"][0]["provider"]["job"]["spec"]["template"]["spec"]
        container = pod["containers"][0]
        container["image"] = self.args.image
        container["args"] += ["--timeout-seconds", str(timeout)]
        for item in container["env"]:
            if item["name"] == "OPEN_GUARDRAIL_URL":
                item["value"] = self.origin
        container["env"].append({"name": "SSL_CERT_FILE", "value": "/test-ca/ca.crt"})
        container["volumeMounts"].append({"name": "test-ca", "mountPath": "/test-ca", "readOnly": True})
        pod["volumes"].append({"name": "test-ca", "configMap": {"name": "guardrail-test-ca"}})
        self.apply(template)

    def status(self):
        self.command("kubectl", "argo", "rollouts", "status", "guardrail-demo", "-n", self.namespace,
                     "--context", self.args.context, "--timeout", "120s")

    def receive(self, case):
        agent = self.work / (case + "-agent")
        fixture = case if case in ("healthy", "unhealthy", "unknown") else "unhealthy" if case == "forged" else "healthy"
        self.command(sys.executable, "-m", "integrations.argo.agent_client", "init", "--case", fixture,
                     "--work-dir", str(agent))
        known = {pod["metadata"]["uid"] for pod in self.resource("pods")["items"]}
        self.kubectl("patch", "rollout", "guardrail-demo", "--type=merge", "--patch-file", str(agent / "rollout-patch.json"))

        def handoff():
            for pod in self.resource("pods")["items"]:
                if pod["metadata"]["uid"] in known or pod["metadata"].get("labels", {}).get("app.kubernetes.io/component") != "open-guardrail-analysis":
                    continue
                logs = self.kubectl("logs", pod["metadata"]["name"], "-c", "guardrail", check=False)
                if "GUARDRAIL_ENVELOPE_AGE_BASE64=" in logs.stdout:
                    return pod
                if pod.get("status", {}).get("phase") == "Failed":
                    raise RuntimeError(case + " Pod failed before handoff; inspect its termination message")
            return None
        pod = self.wait(case + " encrypted handoff", handoff, 180)
        job = self.resource("job", pod["metadata"]["ownerReferences"][0]["name"])
        analysis = self.resource("analysisrun", job["metadata"]["ownerReferences"][0]["name"])
        rollout = self.resource("rollout", "guardrail-demo")
        assert pod["metadata"]["ownerReferences"][0]["uid"] == job["metadata"]["uid"]
        assert job["metadata"]["ownerReferences"][0]["uid"] == analysis["metadata"]["uid"]
        assert analysis["metadata"]["ownerReferences"][0]["uid"] == rollout["metadata"]["uid"]
        self.command(sys.executable, "-m", "integrations.argo.agent_client", "receive", "--work-dir", str(agent),
                     "--namespace", self.namespace, "--pod", pod["metadata"]["name"])
        return agent, json.loads((agent / "handoff.json").read_text()), pod, job, analysis

    def submit(self, case, agent, bundle):
        health = bundle["observation"]
        if case == "forged":
            health = json.loads((gate.ROOT / "examples/agent-demo/healthy.json").read_text())["observations"]["health"]
        verdict = {"unhealthy": "BLOCKED", "unknown": "UNKNOWN"}.get(case, "PASS")
        submission = {"baseSubmissionNo": bundle["baseSubmissionNo"], "finishRun": True, "verdict": verdict,
            "summary": "Synthetic " + case, "checks": [{"checkId": "readiness", "verdict": verdict,
            "summary": "Synthetic health", "evidence": [{"requirementId": "health",
            "observedAt": datetime.now(timezone.utc).isoformat(), "querySummary": "Read synthetic fixture",
            "resultSummary": json.dumps(health, sort_keys=True)}]}]}
        path = agent / "submission.json"
        gate.private_json(path, submission)
        if case == "forged":
            token = agent / "run-credential.json"
            gate.private_json(token, bundle["runToken"])
            self.command(self.cli, "run", "submit", "--submission-credential-file", str(token),
                "--submission", str(path), "--operation-id", bundle["operationPrefix"] + "-submit", bundle["runId"])
        else:
            self.command(sys.executable, "-m", "integrations.argo.agent_client", "submit", "--work-dir", str(agent),
                         "--submission", str(path), "--cli", self.cli)

    def run_case(self, case):
        self.publish_spec(1 if case == "expired" else 900)
        self.install_template(10 if case == "timeout" else 780)
        agent, bundle, pod, job, analysis = self.receive(case)
        pod_name = pod["metadata"]["name"]
        before_logs = self.kubectl("logs", pod_name, "-c", "guardrail").stdout
        if case == "expired":
            self.proxy.run_read_delay = 3
        if case in ("healthy", "unhealthy", "unknown", "forged", "expired"):
            self.submit(case, agent, bundle)
        elif case == "unavailable":
            self.proxy.available = False
        elif case == "pod-loss":
            self.kubectl("delete", "pod", pod_name, "--wait=false")

        def terminal():
            current = self.resource("analysisrun", analysis["metadata"]["name"])
            return current if current.get("status", {}).get("phase") in ("Successful", "Failed", "Error", "Inconclusive") else None
        try:
            final_analysis = self.wait(case + " terminal analysis", terminal, 120)
        finally:
            self.proxy.available = True
            self.proxy.run_read_delay = 0
        phase = final_analysis["status"]["phase"]
        assert phase == ("Successful" if case == "healthy" else "Failed"), (case, phase)
        if case == "healthy":
            self.status()
        else:
            self.wait(case + " rollout abort", lambda: self.resource("rollout", "guardrail-demo").get("status", {}).get("abort"), 60)
        final = self.cli_json("run", "get", "--key-file", str(self.key_path), bundle["runId"])
        expected_decision = "ALLOW" if case in ("healthy", "forged", "expired") else "DENY" if case in ("unhealthy", "unknown") else None
        assert final.get("decision") == expected_decision, (case, final.get("decision"))
        if case in ("timeout", "unavailable", "pod-loss"):
            assert final["status"] == "OPEN", (case, final["status"])
        if case == "pod-loss":
            # Kubernetes may fail the Job directly or attempt a replacement Pod.
            # Either path must fail; a replacement cannot replay the one-time credential.
            assert any(c["type"] == "Failed" and c["status"] == "True" for c in self.resource("job", job["metadata"]["name"])["status"]["conditions"])
            gate_result = {"outcome": "DENY", "layer": "infrastructure", "reason": "POD_LOST", "source": "Kubernetes Job"}
            logs = before_logs
        else:
            finished_pod = self.resource("pod", pod_name)
            terminated = next(s["state"]["terminated"] for s in finished_pod["status"]["containerStatuses"] if s["name"] == "guardrail")
            gate_result = json.loads(terminated["message"])
            expected_reason = {"healthy": "GATE_PASSED", "unhealthy": "DECISION_REJECTED", "unknown": "DECISION_REJECTED",
                "forged": "EVIDENCE_REJECTED", "timeout": "AGENT_TIMEOUT", "expired": "DECISION_EXPIRED", "unavailable": "VERIFICATION_FAILED"}[case]
            assert gate_result["reason"] == expected_reason, (case, gate_result)
            logs = self.kubectl("logs", pod_name, "-c", "guardrail").stdout
        assert json.loads(self.key_path.read_text())["token"] not in logs
        assert bundle["runToken"]["token"] not in logs
        listed = self.cli_json("run", "list", "--key-file", str(self.key_path), "--limit", "100")
        assert len(listed["items"]) == len(self.results) + 1, "duplicate Run created during recovery"
        result = {"case": case, "runId": bundle["runId"], "runStatus": final["status"], "decision": final.get("decision"),
            "analysisRun": analysis["metadata"]["name"], "analysisPhase": phase, "job": job["metadata"]["name"], "gate": gate_result}
        self.results.append(result)
        (self.work / "results.json").write_text(json.dumps({"context": self.args.context, "namespace": self.namespace,
            "cases": self.results}, indent=2))
        print(json.dumps(result), flush=True)

    def close(self):
        if self.proxy:
            self.proxy.shutdown()
            self.proxy.server_close()
        if self.service:
            self.service.terminate()
            try:
                self.service.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.service.kill()
                self.service.wait()
        if self.server_log:
            self.server_log.close()


def main():
    if not __debug__:
        raise RuntimeError("run the smoke without Python optimization so all assertions execute")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--context", required=True)
    parser.add_argument("--namespace", help="must not already exist; random name by default")
    parser.add_argument("--work-dir", required=True, help="fresh private directory; never publish it")
    parser.add_argument("--host-ip", required=True, help="host IPv4 address reachable from local kind Pods")
    parser.add_argument("--server-bin", required=True)
    parser.add_argument("--cli", required=True)
    parser.add_argument("--image", default="open-guardrail-argo-demo:local")
    parser.add_argument("--cases", nargs="+", choices=CASES, default=list(CASES))
    args = parser.parse_args()
    if len(set(args.cases)) != len(args.cases):
        parser.error("cases must be distinct")
    smoke = Smoke(args)
    try:
        smoke.preflight()
        smoke.start_service()
        smoke.setup()
        for case in args.cases:
            smoke.run_case(case)
        print("REAL ARGO ROLLOUTS SMOKE PASSED", flush=True)
    finally:
        smoke.close()


if __name__ == "__main__":
    main()
