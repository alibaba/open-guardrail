#!/usr/bin/env python3
"""Exercise the shipped Compose/TLS deployment using disposable Docker resources."""

import csv
import hashlib
import io
import json
import os
from pathlib import Path
import secrets
import socket
import ssl
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.request


ROOT = Path(__file__).resolve().parent.parent


def run(args, *, env=None, capture=False, check=True, timeout=300):
    return subprocess.run(args, cwd=ROOT, env=env, check=check, timeout=timeout,
                          stdout=subprocess.PIPE if capture else None,
                          stderr=subprocess.PIPE if capture else None)


def smoke():
    run(["docker", "info"], capture=True)
    run(["docker", "compose", "version"], capture=True)
    project = "guardrail-smoke-" + secrets.token_hex(6)
    # Colima shares /Users by default, but macOS system temporary directories
    # may be outside its mounts. Keep the bind-mounted test certificates here.
    scratch = ROOT / ".cache"
    scratch.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="guardrail-docker-", dir=scratch) as temporary:
        tls = Path(temporary)
        run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-sha256", "-nodes",
             "-days", "1", "-keyout", str(tls / "privkey.pem"),
             "-out", str(tls / "fullchain.pem"), "-subj", "/CN=localhost",
             "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1"], capture=True)
        with socket.socket() as reserved:
            reserved.bind(("127.0.0.1", 0))
            port = reserved.getsockname()[1]
        origin = f"https://127.0.0.1:{port}"
        environment = {k: v for k, v in os.environ.items()
                       if not k.startswith("OPEN_GUARDRAIL_") and not k.startswith("COMPOSE_")}
        environment.update(OPEN_GUARDRAIL_TLS_DIR=str(tls),
                           OPEN_GUARDRAIL_PUBLIC_URL=origin,
                           OPEN_GUARDRAIL_HTTPS_BIND=f"127.0.0.1:{port}")
        command = ["docker", "compose", "--project-name", project,
                   "--env-file", "/dev/null", "-f", "deploy/compose.yaml"]
        # Reproduce non-default/legacy image naming without requiring an old
        # Compose binary. Only the test image tag changes; deployment settings
        # still come from the shipped Compose file.
        image_override = tls / "image-name.json"
        image_override.write_text(json.dumps({
            "services": {"guardrail": {"image": project + "_explicit:smoke"}}
        }))
        command += ["-f", str(image_override)]

        def compose(*args, **kwargs):
            return run(command + list(args), env=environment, **kwargs)

        # Fail configuration before any resources are created, rather than
        # silently inventing an external host or starting plaintext-only mode.
        for key in ("OPEN_GUARDRAIL_PUBLIC_URL", "OPEN_GUARDRAIL_TLS_DIR"):
            missing = dict(environment)
            missing.pop(key)
            result = run(command + ["config"], env=missing, capture=True, check=False)
            assert result.returncode != 0 and key.encode() in result.stderr
        configuration = json.loads(compose("config", "--format", "json", capture=True).stdout)
        assert not configuration["services"]["guardrail"].get("ports")
        assert configuration["networks"]["backend"]["internal"] is True
        context = ssl.create_default_context(cafile=str(tls / "fullchain.pem"))
        # Do not let host HTTP(S)_PROXY redirect the private smoke requests.
        client = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                            urllib.request.HTTPSHandler(context=context))

        def request(path, body=None, headers=None, expected=200):
            values = {"Content-Type": "application/json", "Origin": origin,
                      "X-Guardrail-CSRF": "1"}
            values.update(headers or {})
            data = None if body is None else json.dumps(body).encode()
            req = urllib.request.Request(origin + path, data=data, headers=values)
            try:
                response = client.open(req, timeout=5)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                assert response.status == expected, f"{path}: {response.status}, expected {expected}"
                return json.load(response), response.headers

        def ready():
            deadline = time.monotonic() + 60
            while time.monotonic() < deadline:
                try:
                    payload, _ = request("/readyz")
                    if payload["status"] == "ready":
                        return
                except (OSError, ValueError, AssertionError):
                    pass
                time.sleep(1)
            raise RuntimeError("Compose TLS endpoint did not become ready")

        try:
            compose("build", timeout=1800)
            # Images are reported for created containers, not merely builds.
            # Inspect the created container instead of guessing Compose's tag
            # separator or suppressing errors from image discovery.
            compose("create", "--no-build", "guardrail")
            created = compose("ps", "--all", "-q", "guardrail", capture=True).stdout.decode().split()
            if len(created) != 1:
                raise RuntimeError("expected exactly one created guardrail container")
            image = json.loads(run(["docker", "inspect", created[0]], capture=True).stdout)[0]["Image"]
            rejected = run(["docker", "run", "--rm", image], env=environment,
                           capture=True, check=False, timeout=30)
            assert rejected.returncode != 0 and b"HTTPS public_url" in rejected.stderr
            compose("up", "-d")
            ready()
            container = compose("ps", "-q", "guardrail", capture=True).stdout.decode().strip()
            inspection = json.loads(run(["docker", "inspect", container], capture=True).stdout)[0]
            assert inspection["Image"] == image
            assert inspection["Config"]["User"] == "65532:65532"
            assert not inspection["HostConfig"]["PortBindings"]
            legal_archive = run(["docker", "cp", container + ":/usr/share/licenses/open-guardrail", "-"],
                                capture=True).stdout
            with tarfile.open(fileobj=io.BytesIO(legal_archive)) as archive:
                for name in ("LICENSE", "NOTICE"):
                    assert archive.extractfile("open-guardrail/" + name).read() == (ROOT / name).read_bytes()
                with (ROOT / "third_party/notices.tsv").open() as inventory:
                    for row in csv.DictReader(inventory, delimiter="|"):
                        content = archive.extractfile("open-guardrail/" + row["path"]).read()
                        assert hashlib.sha256(content).hexdigest() == row["sha256"]
            volume = next(m["Name"] for m in inspection["Mounts"] if m["Destination"] == "/data")
            token_archive = run(["docker", "cp", container + ":/data/bootstrap.token", "-"],
                                capture=True).stdout
            with tarfile.open(fileobj=io.BytesIO(token_archive)) as archive:
                member = archive.getmembers()[0]
                assert member.mode == 0o600 and member.uid == 65532 and member.gid == 65532
                token = archive.extractfile(member).read().decode().strip()
            assert token.startswith("ogb_")
            status, _ = request("/api/v1alpha1/bootstrap")
            assert status == {"initialized": False, "requiresToken": True}
            # Backend networking and spoofed forwarding headers cannot opt into
            # token-free local setup. Exact Host (including port) and Origin pass.
            password = secrets.token_urlsafe(24)
            account = {"username": "admin", "password": password}
            request("/api/v1alpha1/bootstrap", account, expected=401,
                    headers={"Forwarded": "for=127.0.0.1", "X-Forwarded-For": "127.0.0.1"})
            auth = {"Authorization": "Bearer " + token}
            request("/api/v1alpha1/bootstrap", account, expected=403,
                    headers={**auth, "Origin": "https://untrusted.example"})
            request("/api/v1alpha1/bootstrap", account, expected=403,
                    headers={**auth, "Host": "untrusted.example"})
            request("/api/v1alpha1/bootstrap", account, expected=403,
                    headers={**auth, "X-Guardrail-CSRF": ""})
            request("/api/v1alpha1/bootstrap", account, headers=auth, expected=201)
            _, headers = request("/api/v1alpha1/auth/session", account, expected=201)
            cookie = headers["Set-Cookie"]
            assert all(flag in cookie for flag in ("Secure", "HttpOnly", "SameSite=Strict"))
            compose("up", "-d", "--force-recreate", "guardrail", "proxy")
            ready()
            replacement = compose("ps", "-q", "guardrail", capture=True).stdout.decode().strip()
            assert replacement != container
            new_inspection = json.loads(run(["docker", "inspect", replacement], capture=True).stdout)[0]
            assert any(m.get("Name") == volume for m in new_inspection["Mounts"])
            status, _ = request("/api/v1alpha1/bootstrap")
            assert status["initialized"] is True
            request("/api/v1alpha1/auth/session", account, expected=201)
            request("/api/v1alpha1/bootstrap", account, headers=auth, expected=409)
            print("Docker smoke passed: TLS, non-root volume, license notices, private setup, origin/CSRF and recreation")
        finally:
            # Startup logs contain token paths, never the token or test password.
            compose("logs", "--no-color", "--tail", "20", check=False, timeout=30)
            compose("down", "--volumes", "--remove-orphans", check=False, timeout=60)


if __name__ == "__main__":
    smoke()
