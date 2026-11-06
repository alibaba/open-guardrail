# Configuration and storage

For a first local trial, follow [Getting started](getting-started.md). This guide
covers persistent storage, configuration precedence and remote deployment.

Commands below assume a source checkout after `make build`. In an extracted
native archive, replace `./bin/open-guardrail` with `./open-guardrail`.

## Configuration

Precedence: built-in defaults → explicit YAML file → environment → command flags.
An explicitly empty environment value overrides the file and is validated.

| Setting | YAML | Environment | Default |
| --- | --- | --- | --- |
| HTTP address | `listen` | `OPEN_GUARDRAIL_LISTEN` | `127.0.0.1:8080` |
| External HTTPS origin | `public_url` | `OPEN_GUARDRAIL_PUBLIC_URL` | empty (direct loopback mode) |
| SQLite directory | `data_dir` | `OPEN_GUARDRAIL_DATA_DIR` | `./data` |
| Database backend | `database.driver` | `OPEN_GUARDRAIL_DATABASE_DRIVER` | `sqlite` |
| Database DSN (PostgreSQL) | `database.url` | `DATABASE_URL` | empty |
| Database pool size | `database.max_connections` | — | `10` |
| Schema migration mode | `database.migrations` | `OPEN_GUARDRAIL_DATABASE_MIGRATIONS` | `auto` |

`--listen` and `--data-dir` override corresponding settings. `DATABASE_URL`
requires explicitly selecting `postgres`. Unknown YAML fields fail validation.
Configuration files must contain exactly one YAML document and be at most 1 MiB.
Credentials and raw driver errors are excluded from logs and API errors.
Non-loopback listening requires an explicit HTTPS `public_url` (or
`OPEN_GUARDRAIL_PUBLIC_URL`) and a trusted TLS reverse proxy. Do not expose the
unencrypted backend directly. The default loopback address needs no proxy.

`public_url` must be an HTTPS origin with no path, trailing slash, user information,
query or fragment. Include a nonstandard port when clients use one, for example
`https://guardrail.example.com:8443`. Configuring a public origin requires the
remote bootstrap-token flow even when the backend listener is loopback.

### Schema migration mode

`auto` connects to storage and installs the bundled initial schema before opening
the HTTP listener. `external` requires that schema and initialization version to
be prepared in advance and does not create tables. A missing or different schema
version fails startup.
Both modes require the current schema for readiness. See the
[runtime storage design](architecture.md#storage).

### Docker behind HTTPS

For a complete Docker Compose deployment with TLS, private backend networking,
persistent SQLite storage and first-administrator setup, see
[Docker deployment](docker.md). The image uses UID/GID `65532:65532`, listens
on `0.0.0.0:8080` inside its private network, and stores data in `/data`. Supply
`OPEN_GUARDRAIL_PUBLIC_URL`; there is deliberately no built-in public origin.

## Database choices

The public distribution supports SQLite and PostgreSQL. SQLite targets local
evaluation, personal use and a single service instance; PostgreSQL targets shared
deployments. Released behavior must expose identical API, state-machine
and error semantics on every supported driver and pass a shared conformance suite.

The current model defines identity, Policy, Run expiry, Snapshot Submission,
Evidence and Decision semantics across the preview drivers. SQLite is exercised
locally; PostgreSQL needs a disposable test instance for its backend suite.

The public capability response reports only the active adapter's support level and
verified multi-instance guarantee. It does not reveal the driver name or connection
details. All current adapters report `multiInstance: false`; shared storage is not
advertised as multi-instance-ready until the future business invariants pass that
conformance boundary.

### PostgreSQL configuration

Use an existing PostgreSQL database and configure the same server:

```sh
export OPEN_GUARDRAIL_DATABASE_DRIVER=postgres
export DATABASE_URL='postgres://USER:PASSWORD@HOST:5432/DATABASE?sslmode=verify-full'
./bin/open-guardrail serve
```

The DSN above is a placeholder. Supply actual credentials using your deployment's
secret mechanism; never commit them. A YAML example is available at
[`deploy/config.example.yaml`](../deploy/config.example.yaml):

```sh
./bin/open-guardrail serve --config /etc/open-guardrail/config.yaml
```

PostgreSQL does not require Docker. Direct installations and managed services work
with the same API. Connection and migration failures stop startup; the server never
falls back to SQLite. Switching configuration does **not** migrate existing data.

For remote PostgreSQL connections, configure TLS with certificate verification,
as in the `sslmode=verify-full` example above. The service remains a development preview.
