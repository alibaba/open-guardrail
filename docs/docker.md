# Docker deployment behind HTTPS

Use Docker Engine with the Compose v2 plugin, plus OpenSSL for the local example.
Run these commands from the repository root. The Compose file builds the public
distribution with SQLite by default; PostgreSQL is an explicit configuration
choice. For a native local setup, start with [Getting started](getting-started.md).

The service listens on a private container network. Only the TLS proxy publishes
a host port. An explicit HTTPS public origin is required even when testing on
localhost; container networking does not enable token-free local setup.

## Local HTTPS example

Create a disposable certificate valid only for localhost and 127.0.0.1:

```sh
umask 077
mkdir -p deploy/tls
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -keyout deploy/tls/privkey.pem -out deploy/tls/fullchain.pem \
  -subj /CN=localhost -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1'
chmod 600 deploy/tls/privkey.pem
export OPEN_GUARDRAIL_TLS_DIR="$(pwd)/deploy/tls"
export OPEN_GUARDRAIL_PUBLIC_URL=https://localhost:8443
docker compose -f deploy/compose.yaml up -d --build
curl --fail --retry 30 --retry-connrefused --retry-delay 1 \
  --cacert deploy/tls/fullchain.pem https://localhost:8443/readyz
```

Open <https://localhost:8443> after trusting this disposable certificate in your
local browser. The default published address is `127.0.0.1:8443`, so other machines
cannot connect. The public URL must exactly match the browser origin, including a
nonstandard port, with no path or trailing slash. Do not disable TLS verification
in automation. Both required variables fail Compose interpolation when missing;
the server also rejects missing or invalid HTTPS origins before opening storage.

## Create the first administrator

The runtime image has no shell or `cat`. Copy the private token with Docker's
file-copy command; do not put it in Compose environment variables or logs:

```sh
umask 077
setup_dir=$(mktemp -d)
docker compose -f deploy/compose.yaml cp guardrail:/data/bootstrap.token "$setup_dir/bootstrap.token"
chmod 600 "$setup_dir/bootstrap.token"
```

Read that file locally and enter its contents on the browser setup page together
with the administrator username and password. Keep it private. It expires after
one hour; restarting an uninitialized service renews an expired token. The token
cannot initialize a second administrator after setup has completed. Delete the
host copy after setup:

```sh
rm -f "$setup_dir/bootstrap.token"
rmdir "$setup_dir"
```

Alternatively, a separately installed `guardrailctl bootstrap init --server URL
--username admin --token-file PATH` can initialize the server; its host must trust
the TLS certificate. See [administrator setup](local-administrator.md).

## Persistent data and permissions

The image includes `LICENSE`, `NOTICE` and the original third-party notices under
`/usr/share/licenses/open-guardrail/`.

The image runs as UID/GID `65532:65532`. `/data` is created with that ownership and
mode `0700`; Docker populates a **new named volume** from this directory. The
Compose example mounts `guardrail-data` there. SQLite, its WAL files and the
`0600` bootstrap token therefore survive container recreation. Do not mount the
same SQLite volume into multiple service instances.

`docker compose -f deploy/compose.yaml down` keeps the named volume. Adding `-v`
deletes it, including the administrator, policies and Runs. Back up the database
with the service stopped before upgrades. To rebuild and recreate both the
backend and its proxy while keeping data:

```sh
docker compose -f deploy/compose.yaml up -d --build --force-recreate
```

If substituting a Linux bind mount, create its directory **before** starting the
container, and set ownership to `65532:65532` and mode `0700`, for example with
`sudo install -d -o 65532 -g 65532 -m 0700 /srv/open-guardrail/data`. A bind mount
hides the image's directory permissions; Docker does not fix existing volume or
host-directory ownership. Rootless Docker/user-namespace deployments must map
that container UID/GID to the corresponding host owner. Do not use `chmod 777` or
run the application as root to bypass permission failures.

## A remote host

Provide your real hostname and a certificate trusted by your clients:

```sh
export OPEN_GUARDRAIL_PUBLIC_URL=https://guardrail.example.com
export OPEN_GUARDRAIL_TLS_DIR=/absolute/path/to/tls
export OPEN_GUARDRAIL_HTTPS_BIND=0.0.0.0:443
docker compose -f deploy/compose.yaml up -d --build --force-recreate
```

The TLS directory must contain `fullchain.pem` and `privkey.pem`; it is mounted
read-only into NGINX. Supply the real DNS name and restrict host/firewall access to
trusted operators. Keep keys outside the source tree in remote deployments. Local
`deploy/tls/` is ignored by Git; PEM/key files and local environment files are
excluded from the Docker build context. No certificate or token is built into the
image. Reload/recreate the proxy after certificate renewal.

### Optional HSTS for a stable remote hostname

The shared proxy example leaves HSTS disabled because it also serves the local
demo with a disposable certificate. For a stable remote hostname, enable HSTS
only after its certificate is trusted by clients and HTTPS will remain available.
Start with a short policy in the TLS `server` block of `deploy/nginx.conf`:

```nginx
add_header Strict-Transport-Security "max-age=300" always;
```

Verify the deployment before increasing `max-age` to a longer period. Removing
the header alone does not clear a policy already cached by a browser. Leave
`includeSubDomains` and `preload` out unless you deliberately manage that wider
HTTPS commitment. See [NGINX HSTS guidance](https://blog.nginx.org/blog/http-strict-transport-security-hsts-and-nginx).

### Proxy and application timeouts

The proxy's `proxy_read_timeout 20s` limits the idle interval between response
reads. It exceeds the application's 10-second cooperative request budget and
15-second socket write timeout. The current synchronous API is designed around
that budget; longer operations need an asynchronous API or coordinated changes
to both server and proxy timeouts. Increasing the proxy timeout alone does not
extend the application's budget.

The supplied NGINX config preserves the external `Host` **including its port**,
passes `Origin` and authorization headers, and removes common client-supplied
forwarding headers. The application validates the configured origin and always
requires a bootstrap token in this deployment. Do not publish the backend's
plaintext port 8080, attach unrelated containers to its private network, or expose
the default native loopback mode through a tunnel. The administrator remains a
preview identity feature; apply the access controls described in
[remote access](local-administrator.md#remote-access).

For PostgreSQL, explicitly select `OPEN_GUARDRAIL_DATABASE_DRIVER=postgres` and
inject `DATABASE_URL` through your private deployment configuration. Keep `/data`
private and persistent for bootstrap state even when using an external database;
arrange database network access separately from this isolated SQLite example.

## Verify the deployment

`make smoke-docker` requires a Docker daemon, Compose v2 and OpenSSL. It builds the
actual image and uses temporary certificates, a unique Compose project and a
fresh volume. A test-local Compose override gives the image an explicit tag that
differs from Compose's default naming, so image lookup must use the created
container's actual image ID. It checks configuration rejection, TLS readiness,
non-root storage, original license/notice file hashes, token-required setup,
origin/CSRF rejection, administrator login and persistence
after container recreation. Cleanup removes only that disposable project's
containers and volume. Public CI runs the same target on its Docker runner.

References: [Docker volume initialization](https://docs.docker.com/engine/storage/volumes/)
and [NGINX proxy header behavior](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_set_header).
