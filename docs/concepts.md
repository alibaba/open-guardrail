# Core concepts

The console, CLI and management API use direct access. Only assessment submission requires a Run credential. See [Access](access.md).

OpenGuardrail records a policy, evaluation context, assessment and decision for
one evaluation. The caller defines the scenario; external evaluators investigate
it, and the calling system decides how to use the recorded outcome.

## Policy and evaluation records

| Term | Meaning |
| --- | --- |
| Scene | A scenario with a stable key, a Run timeout (`runTtlSeconds`) and an optional default Policy. A Scene may contain multiple Policies. |
| Policy | Strategy identity, ownership, name, lifecycle and the active Revision pointer within a Scene. |
| GuardrailSpec | The YAML policy document defining instructions, Checks, evidence requirements and decision validity. Its document version is `guardrail.open/v1alpha1`. |
| Policy Revision | Rule content in `DRAFT` or `PUBLISHED` state. One editable Draft is allowed per Policy; publication assigns its number and freezes it. Runs select published Revisions. Publication and activation are separate operations. |
| Run | One independent evaluation. It freezes the Scene, Policy Revision reference, Spec digest, caller-supplied context and Run deadline. |
| Evaluation context | The arbitrary JSON object carried in the API field `changeSet`. The caller defines its business fields; the service normalizes and records it. |
| Check | One assessment required by the frozen Spec, with a `PASS`, `BLOCKED` or `UNKNOWN` verdict and applicable evidence. |
| Submission | One complete Snapshot of all Checks and their applicable evidence. A Run may accept intermediate Snapshots and one final Snapshot. |
| Evidence | Structured observations attached to a Check: a requirement ID, observation time, query summary, result summary and optional reference. |
| Result | The server-validated aggregate Check verdict: `PASS`, `BLOCKED` or `UNKNOWN`. |
| Decision | The stored `ALLOW` or `DENY` outcome derived from the Result, with its own validity window. |

The server verifies that a Submission matches its frozen Spec and aggregates the
Check verdicts. It does not independently attest that the observations are true.
Integrations verify the evidence they rely on before applying a decision.

## Run state and time

A new Run is `OPEN`. Complete intermediate Submissions advance its baseline
number. A final Submission moves it to `FINISHED` and creates its Result and
Decision atomically. An unfinished Run observed at or after its deadline becomes
`EXPIRED` with `UNKNOWN / DENY`.

| Time constraint | Source | Meaning |
| --- | --- | --- |
| Run deadline (`deadlineAt`) | Scene `runTtlSeconds`, frozen when creating the Run | Latest time a new Submission may be accepted. Intermediate Submissions and credential rotation do not extend it. |
| Decision validity (`decisionExpiresAt`) | Frozen Spec `spec.validForSeconds` | Lifetime of the stored Decision after the terminal outcome. Passing this time does not rewrite historical Run state. |

`PASS` maps to `ALLOW`; `BLOCKED` and `UNKNOWN` map to `DENY`. A stored `ALLOW`
must still be current and associated with the intended evaluation before a caller
uses it. See [Runs and submissions](runs.md) for concurrency and retry behavior.

## Credentials and responsibilities

| Credential | Used by | Purpose |
| --- | --- | --- |
| Run Submission Credential | An external evaluator | Submit complete Snapshots to exactly one Run. It is accepted only by that Run's Submission endpoint. |

## Current scope and future execution control

Applying a decision belongs to the calling system. The preview's delivery demos
wait for a current authoritative decision and verify persisted evidence before
continuing. They are synthetic integration examples.

Execution Claims, ActionResults, evaluator attestation and replaceable
identity/provider APIs are planned. The [target architecture](../README.md#target-architecture)
includes that future scope; the [getting-started flow](getting-started.md#evaluation-flow)
shows the implemented evaluation loop. See the [roadmap](development-roadmap.md)
for remaining work.

Return to the [documentation index](README.md).
