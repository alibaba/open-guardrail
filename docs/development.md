# Development and source export

Work from the repository root. To try the project before contributing, follow
[Getting started](getting-started.md).

Use the [documentation index](README.md) to find user guides and technical
references. Documentation-only contributions follow the
[English documentation and diagram conventions](../CONTRIBUTING.md#documentation-and-diagrams).

## Toolchains and checks

Go 1.27.4 or newer is used to build and test. CI reads the version from `go.mod`,
and the container build uses the same default baseline. All dependencies are public
Go modules.

```sh
make generate
make test
make build
make smoke
./bin/open-guardrail serve
```

`make test` runs Go format/vet/race checks, generated-code checks, Node.js UI
tests and Python integration contract tests. Development requires Node.js 22+ and
Python 3.12+ in addition to Go; released binaries do not require them. The suites
can also be run separately with `make test-go`, `make test-web`, `make test-python`
and `make test-scripts`. `make smoke` exercises the local server/CLI Recovery loop
without invoking a model or Jenkins.

`api/openapi/openapi.yaml` is the HTTP contract source. `make generate` uses the
version-pinned Go tool to refresh the committed models, standard-library server
interface and Go client. `make test` rejects generated-code drift.

PostgreSQL tests require a **disposable empty test database**:

```sh
TEST_DATABASE_URL='postgres://USER:PASSWORD@localhost:5432/TEST_DATABASE?sslmode=disable' go test ./internal/adapters/storage -count=1
```

These tests modify migration metadata and must never target an existing database.
Without `TEST_DATABASE_URL`, the PostgreSQL lifecycle test is explicitly skipped.
CI runs it against an isolated PostgreSQL service.

`make preview-release` packages CGO-free preview binaries for Linux and macOS
(amd64/arm64). `VERSION=<version> make public-release` (also exposed as
`make release`) runs the license, module-path and dependency gates sequentially
before creating public artifacts.
The archives contain both commands, the embedded web assets/migrations, documentation,
security guidance, third-party license inventory, a configuration example, OpenAPI,
examples, integration recipes and the Agent demo skill. Only tracked public assets
are included; untracked local files and credentials are excluded. A `LICENSE` file
is included when present, and is mandatory for `public-release`. Each archive has
a SHA-256 checksum sidecar. No release is published automatically.
Assembly requires a Git checkout, Go and Python 3.12+; running the packaged service
and CLI requires neither Go nor Python. Archive ownership, permissions and
timestamps are normalized with `SOURCE_DATE_EPOCH` (the commit timestamp by default).

```sh
make smoke-release VERSION=0.1.0-alpha.0
```

This builds preview archives and verifies the native one outside the checkout, with
Go and Node unavailable. From an extracted archive, `python3 examples/agent-demo/smoke.py`
runs the self-check with the packaged binaries. See
[release verification](releases.md) for checksum, Golden Path and reproducibility details.

See [architecture](architecture.md), [development roadmap](development-roadmap.md),
[security policy](../SECURITY.md) and [contribution guidance](../CONTRIBUTING.md). The
[dependency license review](dependency-licenses.md) is checked against the
release-binary module graph with `make check-dependency-licenses`.
The project uses Apache-2.0. Its canonical source repository is
[alibaba/open-guardrail](https://github.com/alibaba/open-guardrail), and its Go module
path is `github.com/alibaba/open-guardrail`.

## Parser fuzzing

`make test-fuzz` runs four Go fuzz targets for Spec YAML/JSON validation and
ChangeSet JSON canonicalization. Each target runs for 10 seconds with at most two
workers by default; compilation and seed replay take additional time. Override
these limits for a longer investigation:

```sh
make test-fuzz FUZZ_TIME=60s FUZZ_PARALLEL=2
```

The targets check deterministic canonical bytes and SHA-256 digests, formatting
invariance, Spec text/validity preservation across YAML and JSON, and rejection of
duplicate ChangeSet keys, including escaped spellings inside nested objects.
ChangeSet number lexemes remain significant: `1` and `1.0` have different identities.
Structured inputs keep exercising valid documents while raw inputs explore parser
errors and size/depth limits. Round trips stay within the parser's input byte limit.

The GitHub CI fuzz job runs the bounded campaign and uploads failure corpus files
as `fuzz-regressions`. Review and commit minimized failures under the package's
`testdata/fuzz/<target>/` directory after fixing them. Ordinary `go test` (including
`make test-go`) replays seed and regression inputs without starting a fuzz campaign.
See the [Go fuzzing guide](https://go.dev/doc/security/fuzz/) for corpus and
reproduction commands.

## SQL extensions

The public module is independently buildable with `GOWORK=off`. Compiled-in SQL
extensions can use `storage/driver`; extensions are not fetched or loaded at runtime.
