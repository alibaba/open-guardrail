# Package layout and extension boundaries

OpenGuardrail owns the scenario-independent policy, evidence and decision model.
Production delivery and recovery are the first reference scenarios. Domain-specific
observation sources and assessment logic belong in evaluators and integrations.
Custom distributions consume the same core and supply adapters for their platform
through public integration boundaries.

Directories marked **planned** below contain design READMEs only. They are not
importable Go packages or implemented extension APIs. The current server still
wires local identity directly; a custom distribution cannot replace it yet.

HTTP and CLI integration work today and are described in the
[integration guide](../integrations/README.md). Compiled-in storage registration
is also implemented through `storage/driver`; it does not make the planned
identity, authorization, delivery or server-composition APIs available.

Provider adapters are planned in-process Go implementations selected during
composition and compiled into the service. The identity, permission or delivery
systems they consult may be external. Evaluators run outside the service and
submit assessments through the HTTP API. See the
[ADR clarification](adr/0001-product-baseline.md#provider-execution-clarification).

```text
cmd/
  open-guardrail/             Standard server and host maintenance commands
  guardrailctl/              Shared HTTP CLI and credential-file handling
server/                      Public composition and lifecycle API [planned]
extension/                   Public provider contracts [planned]
  identity/                  Verified actors and authentication
  authorization/             External role and permission facts
  delivery/                  Change and checkpoint context from delivery systems
internal/
  servercmd/                 Server command parsing and host maintenance
  ctlcmd/                    Shared HTTP CLI implementation
  domain/
    spec/                    Policy document rules, canonical JSON and digest
    run/                     Arbitrary ChangeSet canonicalization and identity
  application/
    identity/                Local administrator and credential use cases
    spec/                    Scene drafts, validated publication commands and repository ports
    run/                     Immutable Run creation/query and repository port
    authorization/           Guardrail resource and operation rules [planned]
  adapters/
    httpapi/                 HTTP routes, credential transport and responses
    storage/                 Shared SQL, repositories and migration lifecycle
      sqlite/                SQLite connection and dialect-specific behavior
      postgres/              PostgreSQL connection and dialect-specific behavior
    security/                Local password and host-file implementations
    identity/                Built-in external identity providers [planned]
    delivery/                Built-in delivery context providers [planned]
    observability/           Logging, metrics and tracing integrations [planned]
  bootstrap/                 Standard distribution wiring and lifecycle
  config/                    Standard file, environment and flag configuration
  buildinfo/                 Build metadata
api/openapi/                 Versioned HTTP contract and embedded document
storage/driver/              Compiled-in SQL storage registration
migrations/                  Initial schemas for supported databases
web/                         Embedded management UI
integrations/                CD-side integration recipes and synthetic demos
  jenkins/                   Pipeline/Shared Library and CLI recipe
  github-actions/            Deployment workflow and CLI recipe
  argo/                      Rollouts Analysis Job demo; Argo CD sync hooks planned
examples/
  policies/                  Policy examples
  extensions/                External Go consumer example [planned]
deploy/                      Standard distribution configuration and packaging
scripts/                     Build and release tooling
docs/                        User guides and runtime details
```

## Dependency boundaries

- `internal/domain` owns document and decision rules. It has no HTTP, SQL,
  configuration-center or provider SDK dependency. The Spec parser uses the public
  YAML library as part of its document format.
- `internal/application` owns use cases and defines the internal ports it consumes.
  It depends on domain rules and, when implemented, public extension contracts.
  It does not import adapters or bootstrap.
- `internal/adapters` implements IO and maps external facts into core inputs.
  Repository transactions preserve the atomicity required by application commands.
- `internal/bootstrap` assembles the standard distribution. The planned `server`
  facade will assemble the same implementation with explicitly supplied providers.
- Once the planned composition APIs exist, custom distributions will import
  `server` and the relevant `extension` packages.
  Public signatures must use public contract types; they cannot expose `internal`
  types, database handles or corporate SDK types.

Keep ports beside the use case that consumes them. Repository and transaction
interfaces remain internal; they do not become enterprise extension APIs merely
because they are interfaces. Split a package when its responsibilities diverge,
without adding parallel `core`, `service`, `manager` or generic `utils` layers.

## Core and adapter responsibilities

The core owns Spec validation, Policy Revision identity, Run references and TTL,
Snapshot Evidence acceptance, verdicts, expiry, idempotency, resource authorization
and required audit records. Multiple complete Submissions, final Result/Decision
evaluation and Decision validity are implemented; Claims remain planned.

Read [core concepts](concepts.md) for the public terminology and
[runtime architecture](architecture.md) for the current implementation.

An enterprise adapter verifies identities, obtains external permission facts,
maps a delivery platform's change/checkpoint context, or connects a standard
telemetry backend. It does not implement another Run state machine or rewrite
the acceptance rules. Provider failures must have explicit error semantics and
must not silently grant access or pass a check.

Organization-specific integrations can live in separate modules or repositories.
Keep private SDKs, endpoints and deployment conventions outside the public core.
Reusable capabilities belong upstream so future adopters need less integration
code.

The public server API must reuse standard storage, HTTP handlers and the UI while
allowing explicit provider selection and enterprise login routes. Enterprise
authentication must not require implementing local passwords, the single local
administrator or local bootstrap. Current handler coupling must be removed when
that API is implemented; a wrapper around `bootstrap.Serve` alone is insufficient.

Deployment settings can be translated into public server configuration by the
enterprise entrypoint. Prefer standard `slog` and telemetry APIs over a new logging
framework. Required audit writes remain part of core command correctness.

Applications, services, scripts and agents can integrate through the HTTP API and
shared `guardrailctl` CLI without a compiled provider. The
[CLI directory](../cmd/guardrailctl/README.md) contains
implemented diagnostic, identity and Run create/get/revision/submission/list/rotate-credential/submit commands.
Keep Jenkins, GitHub Actions and Argo Pipeline/YAML recipes in `integrations`,
separate from Go context adapters. Deployment and batch/traffic control remain with
the CD system. This project's own build/test workflows stay in `.github/workflows`.

## Making the boundary usable

Implement the first provider contract together with its public server wiring,
default implementation and an example built as a separate Go module. That example
must work through public imports and without copying handlers or core code.
Select the public module path before publishing supported Go integration APIs.

Compatibility checks should run the same core behavior through default and test
providers, including identity failures, authorization denial and retries. Enterprise
upgrades pin the new core version and run those checks against their own adapters.
HTTP integration should exercise the same commands and must not determine a
second, reduced guardrail model. See the directory READMEs for the planned boundaries
and [runtime architecture](architecture.md) for implemented behavior.
