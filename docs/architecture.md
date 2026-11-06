# Runtime architecture

This document describes the code that exists today. Target product decisions are
recorded in [ADR 0001](adr/0001-product-baseline.md), and their implementation order
is tracked in the [development roadmap](development-roadmap.md).

For the caller-facing model and reading paths, see [core concepts](concepts.md)
and the [documentation index](README.md). The
[README diagram](../README.md#target-architecture) includes planned components;
the [getting-started diagram](getting-started.md#evaluation-flow) shows the
implemented evaluation loop.

The core evaluation model is scenario-independent. A caller supplies its Scene,
policy and JSON evaluation context, evaluators submit checks and evidence, and
the caller consumes the recorded decision. Production delivery and recovery are
the first reference integrations.

## Runtime

`cmd/open-guardrail` delegates to `internal/servercmd`, which parses startup flags
and creates a signal-aware context.
`internal/config` loads strict YAML and environment settings.
`internal/bootstrap` connects storage, applies or verifies migrations according to
the configured mode, then opens the HTTP listener.
`internal/adapters/httpapi` implements the generated standard-library HTTP server
interface and serves diagnostics, identity APIs, OpenAPI and embedded assets.
`cmd/guardrailctl` delegates to `internal/ctlcmd`, which communicates only over HTTP.

`api/openapi/openapi.yaml` is the source for public HTTP models, route interfaces
and the Go client. Generated code is committed and checked for drift. The generated
router does not replace authorization or complete request validation: the HTTP
adapter retains the origin, CSRF, credential, body-size and unknown-field checks.
Request bodies must be UTF-8 JSON objects with unique keys at every nesting
level; duplicate keys, including escaped spellings of the same key, are rejected
before application commands run.
The outer HTTP middleware generates an opaque trace identifier for every response;
error envelopes repeat it without returning internal error details.

Storage initialization has a 30-second deadline; readiness queries have a two-second
deadline. SIGINT/SIGTERM allow ten seconds to drain HTTP requests before storage
closes. A listener opens only after the database schema is ready.

## Storage

The application selects exactly one backend. `internal/adapters/storage/sqlite` uses a
pure-Go driver, WAL, foreign keys and a bounded busy timeout.
`internal/adapters/storage/postgres` uses pgx through database/sql with a bounded pool.
`internal/adapters/storage` owns lifecycle and Goose migrations embedded from
`migrations/sqlite` or `migrations/postgres`. Compiled-in extensions register a
connection factory, migration filesystem, dialect and conflict handling through
`storage/driver`. The shared repositories depend on these operations rather than
on a particular extension or its third-party driver.

Built-in persistence infrastructure lives under `internal/adapters/storage`. Its explicit
driver registry selects the backend and records its migration dialect, support
level and current multi-instance guarantee. It provides separate connection
entrypoints for startup and maintenance of existing databases.
The root package manages migrations/readiness and implements shared
identity and Spec repository SQL. Backend subpackages own database-specific
connections and dialect behavior. Keep portable repository SQL shared; isolate
backend-specific operations only where their behavior differs.
Repository and transaction interfaces belong with the application use cases that
consume them. Domain and application packages must not import storage adapters.

The initial schema creates all 15 business tables, including identity, Policy,
Revision, Run, Submission, Evidence and scoped credentials. Final results and
decisions are fields on Run. Each database has one initialization script and the
same version. The server rejects a database with a different schema version.

With `database.migrations: auto`, startup installs the bundled schema. With
`external`, the schema and initialization version must already exist. See
[database initialization](configuration.md#schema-migration-mode).

The current preview supports one service instance per database. PostgreSQL multi-instance
coordination and SQLite business-write contention must be verified as those features
are introduced. A single SQLite connection reduces local contention; future business
correctness must still come from transactions, conditional writes and constraints.
The public capability response therefore reports `multiInstance: false` for all
current adapters. It exposes only support level and the verified operating envelope,
never the selected driver, connection string, file path or database topology.

The accepted v0.1 target keeps the backend configurable: SQLite is official for
local/single-instance deployments and PostgreSQL is official for shared/multi-instance
deployments. Both must preserve identical domain behavior through driver-specific
implementations and a shared conformance suite. Additional drivers use the same ports
and can enter a compatibility promise only after passing the suite. No driver
selection may silently fall back to SQLite.

## Deliberate scope

The UI is embedded static management pages with no frontend build requirement.
Future composition and provider directories contain design READMEs only; they do
not define supported Go APIs or add runtime behavior. See the
[package layout](extensions.md).
The current scope includes local administrator bootstrap and sessions; see the
[administrator guide](local-administrator.md).
Scene settings live in `scene`; Policy metadata lives in `policy`.
Both editable Drafts and published content live in `policy_revision`. `internal/domain/spec` owns document validation,
canonical JSON and the digest. Drafts may be saved before validation; publishing
validates and freezes a Revision. Activation is a separate compare-and-swap update.
Draft edits update the same Revision row with last-write-wins semantics.
See [ADR 0002](adr/0002-persistence-model.md) for keys, constraints and initialization.

`internal/application/spec.Service` validates and canonicalizes documents and
prepares the request identity for Scene initialization and Revision publication.
HTTP handlers call this service. The storage port accepts prepared commands and
retains project/Scene/Policy lock ordering, exact reviewed-Draft comparisons,
idempotent replay checks and atomic audit writes. Validation no longer requires
holding database write locks. Publication timestamps are still sampled after
the required persistence locks through a deferred application clock.

The runtime API creates a Run from `sceneKey` and an arbitrary JSON object
`changeSet`. Machine roles are issuance templates; each key persists its complete
effective scopes and endpoint authorization checks those scopes. A Project-scoped
`RUN_ORCHESTRATOR` key receives `runs:create`, `runs:read` and
`submission-credential:rotate`. A trusted integration key may additionally receive
`runs:create-with-revision` so it can select an immutable Revision already frozen by
an upstream system. Ordinary orchestrator keys always use the Scene default active
Revision. A separate `POLICY_PUBLISHER` key receives `policies:create` and
`revisions:publish`; it cannot create Runs or edit an administrator's Draft.
The Scene default Policy's active Revision and Scene TTL are frozen in the Run
with its `policyId`, `policyRevisionId`, Spec digest and normalized ChangeSet.
A Run starts OPEN and creation returns its plaintext Submission Credential once;
only a digest is persisted. A Project key can rotate the credential while OPEN.
The matching Run credential submits complete Snapshots with a baseline number.
Intermediate Snapshots advance the baseline. A final Snapshot atomically creates
one Result and Decision and moves the Run to FINISHED. An overdue unfinished Run
moves to EXPIRED. A finished Run retains its terminal state. The Policy Revision
contains the immutable YAML and canonical Spec, so the Run does not copy them.

The identity application layer owns actual initialization and login use cases.
It also owns Project-key lifetime and transition rules. Key creation defaults to
90 days; rotation preserves the existing expiry unless explicitly changed.
Administrator sessions are rechecked in the same identity transaction as key
issuance, version-checked rotation or idempotent revocation and their audit writes.
Metadata pages never contain secrets or stored hashes. Authentication samples
time after database reads, so pool waits do not extend a credential's lifetime.
The current bootstrap and HTTP handlers still depend on the local identity service.
Public enterprise composition and replaceable identity remain planned, as documented
in the directory READMEs.
No internal platform dependency, provider credential, private endpoint or telemetry
is built into this service. Public APIs are documented by api/openapi/openapi.yaml.

## Build choices

The public module path is `github.com/alibaba/open-guardrail`, matching the
[canonical source repository](https://github.com/alibaba/open-guardrail).
This module identity does not make the planned server-composition or provider
interfaces supported Go APIs.

The release matrix currently includes Linux/macOS amd64/arm64. Both commands build
with CGO disabled. PostgreSQL 17 is the shared-backend integration-test baseline.
Docker is an optional packaging tool; extracted binaries run directly on the host.

The code uses Go 1.27.3, `/livez` for process liveness and minimal `/readyz` for
dependency readiness.
Anonymous version/capability discovery excludes commit hashes and dependency
details.

## Request execution budget

HTTP handlers receive a 10-second context deadline, within the server's
15-second socket read/write timeouts. Repository calls pass that context to
`database/sql`, bounding connection-pool and row-lock waits. An earlier client
cancellation/deadline is preserved. Cancellation is cooperative: a handler does
not run in a detached goroutine, and writes retain their existing atomic
transaction and idempotent retry behavior. Storage cancellation maps to the
existing unavailable response; callers should retry writes with the same key
when the outcome is uncertain.

## Continuous integration

The repository workflow runs the shared test suite against disposable PostgreSQL,
alongside SQLite, and checks the frontend, Python integrations,
generated API client, dependency inventory and release archives. In a source
checkout, the workflow is `.github/workflows/ci.yaml`. Native archives contain
the [development guide](development.md) and [release verification](releases.md)
but do not include source-only CI configuration.
Deployment-specific runner setup belongs in the integrating platform, outside
the public source distribution.
