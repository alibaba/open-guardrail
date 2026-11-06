# Shared Recovery Agent gate

This is the shared implementation reference for the synthetic
[Jenkins](../jenkins/README.md) and [Argo Rollouts](../argo/README.md) demos.
For a first local trial, use [Getting started](../../docs/getting-started.md)
or the [local Agent demo](../../examples/agent-demo/README.md).

`gate.py` is the pipeline-neutral part of the advanced demo. A delivery adapter
supplies one `GateInput` with an immutable `change_set`, stable
`operation_identity`, observation and Project Key file path. Prepare uses the
Project Key to create a Run and fetch its immutable PolicyRevision through the
Run-scoped reader, then encrypts the Submission Credential, Spec and observation
for the **outer Agent**. The Agent submits directly with that credential, without
the Project Key. The delivery adapter polls the authoritative Decision and permits
continuation only for a `FINISHED`, unexpired `PASS / ALLOW`.
The gate does not deploy, resume, or invoke a model on the worker.

Adapters include [Jenkins](../jenkins/README.md) and
[Argo Rollouts](../argo/README.md). Another CD system can map
its own build identity, checkpoint, observation source and credential injection
to the same `GateInput`; it must not add its own Decision aggregation or bypass the
authoritative Run query. Keep platform API clients and environment-variable names
in that adapter. This shared gate is **Recovery-demo-specific** (`readiness` Check,
`health` Evidence), not a general policy engine or a new server-side provider API.

The operation identity is hashed into stable create/submit idempotency keys. An
uncertain create/submit result is never retried with a new identity. Inspect the
Run and follow [credential recovery](../../docs/runs.md) before any retry.

`guardrailctl run revision` reads only the Revision pinned to that Run.
`guardrailctl run submission --submission-id <id>` reads a stored Snapshot within
that Run. Both require the Project Key; a Submission Credential grants neither reader.
The final gate verifies the stored Snapshot identity, verdict and health Evidence.

The final release condition combines a current authoritative ALLOW with the
business adapter's evidence rule. The Recovery demo uses exact structured health
equality; other business adapters can define their own bounded evidence checks.
`GateStopped` retains RuntimeError compatibility and exposes a fixed failure
layer/reason to adapters. Argo records these in logs and the Pod termination
message; it never prints raw error bodies or credentials.
