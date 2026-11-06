# ADR 0001: Product and engineering baseline

- Status: Accepted
- Date: 2026-10-04
- Scope: OpenGuardrail public core

This ADR records the baseline context and target contracts at acceptance, rather
than the current feature inventory. Apache-2.0 and the public repository
[alibaba/open-guardrail](https://github.com/alibaba/open-guardrail) have since been
selected. See the [README](../../README.md) and [roadmap](../development-roadmap.md)
for the current implementation.

## Context at acceptance

At acceptance on 2026-10-04, OpenGuardrail contained a preview service and CLI
scaffold. It could start with SQLite or PostgreSQL, run migrations, expose
diagnostics and provide local administrator bootstrap. Policies, Runs,
Submissions, Decisions and controlled actions had not yet been implemented.
The [roadmap](../development-roadmap.md) describes capabilities added since then.

Development needs a single baseline that separates current implementation facts
from target product contracts. In particular, an open-source deployment cannot
assume that every user has the same database resources, while choosing a database
must not change externally observable guardrail behavior.

## Decision

### Product boundary

The public core is a standalone Go service with:

- a versioned HTTP/OpenAPI contract;
- `guardrailctl` as a first-class client for users who do not call HTTP directly;
- process-external evaluators;
- provider adapters compiled into the service to obtain identity, authorization
  or delivery facts;
- storage adapters behind application ports; and
- no dependency on internal identity, database, configuration or release systems.

The service binary is named `open-guardrail`; the CLI is named `guardrailctl`.
The HTTP API remains `v1alpha1` until compatibility rules and business contracts
are proven.

### Provider execution clarification

Clarified on 2026-11-03. The earlier wording "process-external providers and
evaluators" conflated an adapter with the source of its facts. Evaluators run
outside OpenGuardrail and submit assessments over HTTP. Planned provider adapters
implement Go contracts, are selected during server composition and are compiled
into the service binary. Their fact sources may be external identity, permission
or delivery systems; those systems need not run in the service process.

This clarifies the existing [extension design](../extensions.md) without adding
runtime behavior or a supported Go provider API. Public server composition and
replaceable identity, authorization and delivery contracts remain planned.

### Database support

The database backend is configurable. The documented local default is SQLite when
no driver is selected. This default is not a fallback: an explicitly selected
driver that is unknown, invalid or unavailable fails startup and never silently
switches to SQLite.

The v0.1 support matrix is:

| Driver | Support level | Intended deployment | Required behavior |
| --- | --- | --- | --- |
| SQLite | Official local | Local evaluation, personal use and one service instance | Full v0.1 business semantics within the documented single-instance boundary |
| PostgreSQL | Official shared | Shared environments and multiple service instances | Full v0.1 business semantics, concurrency and recovery guarantees |
| MySQL and other first-party drivers | Planned or experimental | Not part of the initial compatibility promise | Must pass the same conformance suite before becoming official |
| Third-party drivers | Community | Community-defined | Must advertise their actual compatibility; no default project endorsement |

All official drivers expose the same API, state transitions and stable error
semantics. Implementations may use different transactions, locks, conditional
updates and indexes. Migration versions and logical schemas stay aligned across
official drivers even when SQL differs.

Application and domain code must not import driver-specific packages or branch on
driver type. Every official driver must pass the shared storage conformance suite,
including migrations, immutable policy revisions, Run idempotency, concurrent
submissions, Claim exclusivity and ActionResult finality.

### Toolchain and diagnostic contract

Go 1.27.3 is the toolchain baseline across module, CI and container builds.

The target public diagnostic contract is:

| Endpoint | Contract |
| --- | --- |
| `GET /livez` | Process liveness only; no database dependency |
| `GET /readyz` | Minimal ready/not-ready result; no raw dependency details |
| `GET /api/v1alpha1/meta/version` | Product version and compatibility information; no commit hash |
| `GET /api/v1alpha1/meta/capabilities` | Supported API versions, CLI range, enabled features and non-sensitive storage operating envelope |

Detailed dependency diagnostics require authentication. Anonymous metadata never
includes the source commit.

### Distribution and onboarding

Extracted binaries remain a supported zero-container path. Docker Compose becomes
the documented Golden Path only after a real business loop can be demonstrated; it
is not restored for diagnostics alone.

At acceptance, the public module path, hosting organization and license were owner
and open-source approval decisions. Apache-2.0 has since been selected. On
2026-11-05, the public repository was set to `alibaba/open-guardrail` and the Go
module path to `github.com/alibaba/open-guardrail`.

## Consequences

- SQLite and PostgreSQL remain useful without forcing the same deployment model.
- Supporting two official databases requires synchronized migrations and a shared
  conformance suite for every business capability.
- API, CLI and generated clients must derive from one OpenAPI contract.
- Current endpoints and toolchain are explicitly transitional and are aligned in a
  later change rather than being misrepresented by documentation.
- Adding an official database driver is a compatibility commitment, not only a new
  connection factory.

## Non-goals of the original decision

This decision does not implement Policy, Run, Submission, Decision, Claim or
ActionResult. The original decision did not add business tables, restore Compose
or select a public namespace or license. Current implementation and licensing
status are documented in the [README](../../README.md).

## Changing this decision

A material change to product boundaries, an official support tier, API versioning
or public distribution requires a superseding ADR. Small implementation details may
be refined in detailed designs as long as they preserve this decision's observable
contracts.
