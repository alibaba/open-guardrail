# API overview

The versioned [OpenAPI document](../api/openapi/openapi.yaml) is the contract
source. This page summarizes the current routes; consult the contract for request
schemas and the full route list.

For the domain model, read [core concepts](concepts.md). For a runnable sequence,
follow [Getting started](getting-started.md).

## API surfaces and credentials

Management routes use `/api/v1alpha1`; machine evaluation routes use
`/runtime/v1alpha1`. The policy document's `guardrail.open/v1alpha1` is the
GuardrailSpec format identifier, not an HTTP path.

| Caller | Credential | Access |
| --- | --- | --- |
| Administrator | Browser or CLI administrator session | Manage Scenes, Policies and keys; inspect management Run views |
| Orchestrator | Project key with effective endpoint scopes | Create/read Runs and rotate an open Run's Submission Credential |
| Policy publisher | Project key with publication scopes | Create Policies and publish Revisions; no administrator Draft or activation access |
| Evaluator | Submission Credential bound to one Run | Submit complete intermediate or final Snapshots to that Run |

Scopes are checked on the server. A Run credential cannot use the Run, Revision
or Submission readers; a Project key cannot submit an assessment. See
[credential management](local-administrator.md#project-machine-keys) and
[Run recovery](runs.md#run-retry-and-credential-recovery).

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
redirects; the public diagnostic endpoints do not require it. Identity and Run
commands use explicit credential files and allow plain HTTP only for literal
loopback addresses.
Every HTTP response includes a server-generated `X-Trace-ID`. Error responses
repeat that value as `traceId` so operators can correlate a CLI failure without
exposing database or credential details.

## Current endpoint summary

| Endpoint | Purpose |
| --- | --- |
| `GET /livez` | Process liveness, independent of database availability |
| `GET /readyz` | Database connection and schema readiness; 503 on failure |
| `GET /api/v1alpha1/meta/version` | Public product version; never includes commit metadata |
| `GET /api/v1alpha1/meta/capabilities` | API/CLI compatibility, features and non-sensitive storage operating envelope |
| `GET /openapi.yaml` | Public OpenAPI contract |
| `GET /runtime/v1alpha1/identity` | Project machine identity, role and effective scopes |
| `GET/POST /api/v1alpha1/auth/api-keys` | List secret-free key metadata or issue an expiring Project key |
| `DELETE /api/v1alpha1/auth/api-keys/{keyId}` | Revoke a Project key using an administrator session |
| `POST /api/v1alpha1/auth/api-keys/{keyId}/rotation` | Replace an active key secret using its current version |
| `POST /runtime/v1alpha1/runs` | Create an immutable Run using a Project key and `Idempotency-Key` |
| `GET /runtime/v1alpha1/runs` | List Runs in the authenticated Project |
| `POST /runtime/v1alpha1/runs/{runId}/submission-credential:rotate` | Replace an open Run's Submission Credential using its Project key |
| `POST /runtime/v1alpha1/runs/{runId}/submissions` | Submit a complete intermediate or final Snapshot using the bound Run credential |
| `GET /runtime/v1alpha1/runs/{runId}` | Query a Run in the authenticated Project |
| `GET /runtime/v1alpha1/runs/{runId}/policy-revision` | Read the immutable Policy Revision selected by a Run |
| `GET /runtime/v1alpha1/runs/{runId}/submissions/{submissionId}` | Read a persisted Submission and its complete Snapshot within a Run |
| `GET /api/v1alpha1/audit-events` | Administrator audit query with filters, cursor pagination and optional redaction |
| `GET /api/v1alpha1/runs/{runId}/report` | Administrator diagnostic report |
| `GET /runtime/v1alpha1/runs/{runId}/report` | Diagnostic report using a Project key with `runs:read` |
| `GET /api/v1alpha1/runs` | List Runs for the authenticated administrator's read-only management view |
| `GET /api/v1alpha1/runs/{runId}` | Inspect one Run from the management view |
| `POST /api/v1alpha1/scenes/{key}:initialize` | Atomically create Scene, default Policy and Revision 1 |
| `GET /api/v1alpha1/scenes/{key}/bindings` | List exact resource bindings with limit/after pagination |
| `PUT /api/v1alpha1/scenes/{key}/bindings/{kind}/{resourceId}` | Idempotently create/replace a binding; optional published Revision pin |
| `DELETE /api/v1alpha1/scenes/{key}/bindings/{kind}/{resourceId}` | Remove a binding; absent bindings return 204 |
| `POST /api/v1alpha1/scenes/{key}/policies` | Create another Policy |
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

Binding management uses existing administrator sessions and the write CSRF header.
Encode resource IDs in paths, including slash. Run `resources` are trusted inputs
and cannot be combined with an explicit Revision override. Optional
`selection` evidence is recorded only for matched bindings; default/explicit paths
omit it. Use `guardrailctl binding list|put|delete` with an administrator session and
`guardrailctl run create --resources FILE` with a Project key. See the
[CLI walkthrough](../examples/resource-bindings/README.md) and the
[contract and trust boundaries](extensions.md#resource-bindings).

See [audit and report exports](audit-and-reports.md) for privacy boundaries and bounded history.
