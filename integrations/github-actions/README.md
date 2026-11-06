# GitHub Actions deployment integration

Status: planned. There is no reusable workflow or Action implementation yet.

Deployment jobs will call the shared guardrail CLI or HTTP API before deployment
and, where a workflow exposes batches, before the next batch continues. Keep build
and deployment execution in the workflow; the shared core accepts evidence and
returns the authoritative check result.

Correlate the change with the workflow run and attempt. Reuse `--operation-id`
only when retrying the same Run-creation or evidence-submission request.
Run APIs and the shared CLI are already available;
the adopter-facing workflow is the planned work. Its implementation must define
credential handling, wait/timeout behavior and blocked/unknown outcomes using
those existing contracts. Start with [Runs and submissions](../../docs/runs.md)
and the [CLI guide](../../cmd/guardrailctl/README.md).

Keep this deployment recipe in `integrations/github-actions`. The repository's own
`.github/workflows` contains development checks and build automation. A platform
that calls the shared CLI/API needs no separate guardrail binary or core fork.
