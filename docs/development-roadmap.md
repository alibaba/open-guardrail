# Development roadmap

This roadmap describes implemented capabilities and planned product work.
Start with the [documentation index](README.md); the
[baseline ADR](adr/0001-product-baseline.md) records the original design context.

## Available in the development preview

| Area | Implemented behavior |
| --- | --- |
| Service and storage | Native service and CLI; strict configuration; SQLite/PostgreSQL initial schemas, including externally prepared schema mode; readiness and version/capability discovery |
| Identity | Local administrator setup and recovery, browser/CLI sessions, scoped Project keys with bounded lifetimes, metadata listing, rotation and revocation |
| Policy management | Exact resource bindings with fixed/active Revision selection and audit; Scenes, editable Draft Revisions, immutable Published Revisions, reviewed-Draft publication, activation and bounded Revision history |
| Evaluation | Independent Runs with immutable Revision references and context, complete intermediate/final Snapshots, structured evidence, fixed Result/Decision aggregation, lazy expiry and decision validity |
| Machine integration | OpenAPI-generated HTTP clients, Run-scoped Submission Credentials, Revision and stored-Submission readers, bounded Run listing and endpoint-scope authorization |
| User interface | Embedded management pages for administrator setup, Policies and read-only Run inspection |
| Examples and packaging | Synthetic local Agent, Jenkins and Argo Rollouts demos; native archives; optional Docker Compose behind HTTPS |

See [current limits](../README.md#current-capabilities-and-limits) and
[reference integration status](../integrations/README.md).

## Remaining product work

1. **Execution control.** Define and implement Claims, execution attempts,
   ActionResults and manual resolution. Preserve decision validity, idempotency,
   authorization and audit requirements across every transition.
2. **Evaluator identity.** Establish process-external evaluator registration and
   attestation. Keep evidence acceptance and the calling system's verification
   responsibility explicit.
3. **Public composition and providers.** Deliver the server composition API
   together with the first replaceable identity/provider contract, its default
   implementation and a separate-module consumer example. Current design READMEs
   are not supported Go APIs.
4. **Shared deployment guarantees.** Prove cross-process concurrency and recovery
   for PostgreSQL before advertising multi-instance operation. Continue to
   document SQLite's single-instance boundary.
5. **Operational completeness.** Extend audit, retention, observability and
   supported import/export interfaces alongside their user guides and CLI paths.
6. **Additional delivery examples.** Implement and validate GitHub Actions and
   Argo CD sync-hook recipes. Keep the existing Jenkins and Argo Rollouts examples
   labeled as synthetic demos.

The [extension guide](extensions.md) defines the intended provider boundaries.
Organization-specific adapters must consume public contracts and preserve the
shared policy, evidence and decision semantics. Private SDKs and deployment
conventions stay outside the exported public core.

## Conformance for each capability

Every vertical slice must carry matching SQLite and PostgreSQL migrations and
exercise the same observable API, state-machine and error behavior on both
drivers. Changes need evidence for idempotency, concurrent writes, expiry,
credential separation, rollback and required audit records where applicable.
A capability needs supported CLI and documented end-to-end usage alongside its
HTTP contract.

The current preview still advertises `multiInstance: false`. Do not infer a
cross-process guarantee from a successful connection or a single-process test.
Future execution-control work also needs Claim exclusivity and ActionResult
finality checks.

## Production readiness

Production adoption additionally requires backend concurrency and recovery
evidence, SLO/capacity validation, backup and recovery drills, and review of the
deployment's identity, access and evidence-verification boundaries. A migration
from another implementation also needs external behavior-differential tests.
These requirements do not add another runtime or a public SDK commitment to the
Go core.
