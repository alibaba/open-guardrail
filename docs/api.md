# API overview

The console, CLI and management API use direct access. Only assessment submission requires a Run credential. See [Access](access.md).

The versioned [OpenAPI document](../api/openapi/openapi.yaml) is the contract
source. This page summarizes the current routes; consult the contract for request
schemas and the full route list.

For the domain model, read [core concepts](concepts.md). For a runnable sequence,
follow [Getting started](getting-started.md).

## API surfaces and credentials

Management routes use `/api/v1alpha1`; machine evaluation routes use
`/runtime/v1alpha1`. The policy document's `guardrail.open/v1alpha1` is the
GuardrailSpec format identifier, not an HTTP path.

| Operation | Access |
| --- | --- |
| Scene, Spec, binding, Run management and audit | Direct access in the default workspace/project |
| Run assessment submission | Submission credential bound to the selected Run |
| Enterprise operator APIs | Enterprise transport and per-Policy maintainer checks |

## CLI transport

`open-guardrail serve` starts the service; `guardrailctl` calls an existing service:

```sh
guardrailctl --server http://127.0.0.1:8080 status
guardrailctl --server https://guardrail.example.com version
```

The CLI also accepts `OPEN_GUARDRAIL_URL` and `OPEN_GUARDRAIL_TOKEN`. It uses the
client generated from the published OpenAPI contract, and management commands
verify the server's API/CLI compatibility window before sending credentials or
changing state. Stable automation exit codes are documented in the
[CLI guide](../cmd/guardrailctl/README.md).
Diagnostic commands only attach the optional token over HTTPS and never follow
redirects; the public diagnostic endpoints do not require it. Run submission uses a private credential file and allows plain HTTP only for literal loopback addresses.
Every HTTP response includes a server-generated `X-Trace-ID`. Error responses
repeat that value as `traceId` so operators can correlate a CLI failure without
exposing database or credential details.

## Current endpoint summary

| Endpoint | Purpose |
| --- | --- |
| `GET /livez` | Process liveness, independent of database availability |
| `GET /readyz` | Database connection readiness; 503 on failure |
| `GET /api/v1alpha1/meta/version` | Public product version; never includes commit metadata |
| `GET /api/v1alpha1/meta/capabilities` | API/CLI compatibility, features and non-sensitive storage operating envelope |
| `GET /openapi.yaml` | Public OpenAPI contract |
| `GET /runtime/v1alpha1/runs` | List Runs in the default Project |
| `POST /runtime/v1alpha1/runs/{runId}/submissions` | Submit a complete intermediate or final Snapshot using the bound Run credential |
| `GET /runtime/v1alpha1/runs/{runId}` | Query a Run in the default Project |
| `GET /runtime/v1alpha1/runs/{runId}/policy-revision` | Read the immutable Policy Revision selected by a Run |
| `GET /runtime/v1alpha1/runs/{runId}/submissions/{submissionId}` | Read a persisted Submission and its complete Snapshot within a Run |
| `GET /api/v1alpha1/audit-events` | Project audit query with filters, cursor pagination and optional redaction |
| `GET /api/v1alpha1/runs/{runId}/report` | Run diagnostic report |
| `GET /api/v1alpha1/runs` | List Runs for the default project's read-only management view |
| `GET /api/v1alpha1/runs/{runId}` | Inspect one Run from the management view |
| `POST /api/v1alpha1/scenes/{key}:initialize` | Atomically create Scene, default Policy and Revision 1 |
| `GET /api/v1alpha1/scenes/{key}/bindings` | List exact resource bindings with limit/after pagination |
| `PUT /api/v1alpha1/scenes/{key}/bindings/{kind}/{resourceId}` | Idempotently create/replace a binding; optional published Revision pin |
| `DELETE /api/v1alpha1/scenes/{key}/bindings/{kind}/{resourceId}` | Remove a binding; absent bindings return 204 |
| `POST /api/v1alpha1/scenes/{key}/policies` | Create another Policy |
| `PUT /api/v1alpha1/policies/{policyId}/maintainers` | Replace the nonempty maintainer list; trusted-operator mode requires a current maintainer |
| `GET /api/v1alpha1/policies/{policyId}/draft` | Read the current Draft Revision, or 404 when absent |
| `PUT /api/v1alpha1/policies/{policyId}/draft` | Save and return the Draft Revision |
| `POST /api/v1alpha1/policies/{policyId}/draft:publish` | Publish the reviewed Draft as the same Revision |
| `POST /api/v1alpha1/policies/{policyId}/revisions` | Publish a Revision directly |
| `GET /api/v1alpha1/policies/{policyId}/revisions?pageSize=20` | Page through Revision summaries; follow `nextBefore` and fetch YAML by ID |
| `PUT /api/v1alpha1/policies/{policyId}/active-revision` | Activate a Revision |
| `GET /` | Embedded management shell |

Run creation and Submission require `Idempotency-Key`; retry the same logical
operation with the same key and content. Read the Run's `finalSubmissionId`, then
use the Submission reader to inspect accepted evidence. A `FINISHED` Run exposes
its Result, Decision and `decisionExpiresAt`; callers must also verify the evidence
and evaluation identity they rely on. Execution Claims and ActionResults remain
planned. See [Runs and submissions](runs.md) for the full lifecycle.

See [audit and report exports](audit-and-reports.md) for privacy boundaries and bounded history.

Each Policy records the caller's actor ID as its initial maintainer. Draft
writes, publication, activation and archival check the current maintainer list in
the same transaction as the write. In enterprise mode, platform administrator grants do not bypass
this check. Maintainers may replace the list with `{"maintainers":["actor-id"]}`;
the list cannot be empty. Existing publisher keys keep their existing scopes;
`drafts:write`, `revisions:activate` and `policies:maintainers-write` are opt-in
grants and do not substitute for membership in the Spec's maintainer list.
In trusted-operator mode, the actor ID is `<namespace>:<operator>`, derived directly
from the caller-supplied operator identifier without identity verification.
