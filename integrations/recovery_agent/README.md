# Shared Recovery Agent gate

The console, CLI and management API use direct access. Only assessment submission requires a Run credential. See [Access](../../docs/access.md).

This is the shared implementation reference for the synthetic
[Jenkins](../jenkins/README.md) and [Argo Rollouts](../argo/README.md) demos.
For a first local trial, use [Getting started](../../docs/getting-started.md)
or the [local Agent demo](../../examples/agent-demo/README.md).

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

The final release condition combines a current authoritative ALLOW with the
business adapter's evidence rule. The Recovery demo uses exact structured health
equality; other business adapters can define their own bounded evidence checks.
`GateStopped` retains RuntimeError compatibility and exposes a fixed failure
layer/reason to adapters. Argo records these in logs and the Pod termination
message; it never prints raw error bodies or credentials.
