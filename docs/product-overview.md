# Meet OpenGuardrail

Don't rely on system prompts alone to stop risky AI agent actions.
**OpenGuardrail** puts an evidence-based admission gate in front of destructive
MCP tool calls, database mutations, and production rollouts.

## Why use it?

When an AI agent can touch production databases, delete files, or promote a
release, asking it to "be careful" in a prompt is not an enforceable control.
OpenGuardrail binds the **policy revision**, **check verdicts**, **structured
evidence**, and **final decision** into one immutable, auditable record.

- **For humans and teams:** Review exactly what an agent checked and why an
  action was allowed or blocked.
- **For tools and pipelines:** Independently verify an unexpired `ALLOW`
  decision bound to the exact target before executing a risky action.

## How it works

1. **Define the checks.** Publish a versioned `GuardrailSpec` YAML describing
   required checks, pass/block/unknown criteria, and evidence fields.
2. **Investigate and submit.** The agent or CI step inspects live signals and
   submits a complete Snapshot using a Run-scoped submission credential.
3. **Verify, then act.** OpenGuardrail records `ALLOW` (all checks `PASS`) or
   `DENY` (`BLOCKED`, `UNKNOWN`, or expired). The proxy or deployment job
   verifies the decision before acting.

## Start where it fits your workflow

- **Guard MCP tools in Cursor, Claude Code, or Cline:** Use
  `guardrailctl mcp-proxy` for zero-overhead pass-through on unmatched ordinary
  calls and argument-bound gates on configured or destructive calls. See the
  [MCP Proxy guide](../integrations/mcp-proxy/README.md).
- **Gate production deployments in GitHub Actions:** Add the composite Action
  and three-check `release-gate.yaml` Spec to your CD workflow. See the
  [GitHub Actions guide](../integrations/github-actions/README.md).
- **Try the 30-second first-batch rollout check:**
  [Run the first-batch check with your coding agent](getting-started.md#run-the-first-batch-check)
  to watch an agent trace a simulated checkout write failure to a captured order
  still marked `pending`.
- **Try local binary or plugin demos:** Download the
  [native binary archive](native-install.md) (no Go/Node/Python runtime
  required), or try the [Agent plugin](plugin-trial.md) in pi / DeepSeek Harness.

Ready to connect your own tool or pipeline? Read the
[integration guide](../integrations/README.md).
