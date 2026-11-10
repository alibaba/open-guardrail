# Reference integrations

OpenGuardrail's policy, evidence and decision model can be used in any scenario.
Production delivery and recovery are the first reference scenarios represented
in this directory. Other domains can use the same HTTP API or CLI with their own
Scene, policies, evaluation context and evidence sources.

Status: the shared Recovery Agent gate, Jenkins adapter and Argo Rollouts Analysis
Job are implemented as synthetic CD-side demos. GitHub Actions and Argo CD sync
hooks remain planned.

Start with [Getting started](../docs/getting-started.md) and the
[local Agent demo](../examples/agent-demo/README.md) before configuring a delivery
platform. See [core concepts](../docs/concepts.md) for the policy, Run and
Submission model, and the [CLI guide](../cmd/guardrailctl/README.md) for commands.

The current delivery recipes run on the CD side and call the common HTTP API or
CLI at explicit checkpoints. They preserve platform build/change identity, wait
for an authoritative decision and stop when checks fail or the outcome is uncertain.

The CD platform owns deployment and continuation. The
[shared Recovery gate](recovery_agent/README.md) owns Run creation, encrypted
handoff and Decision verification; the outside Agent owns assessment and
Submission. Each platform adapter maps its own build/checkpoint identity,
observation source and credential injection. Server-side context lookup, when
needed, belongs in a separate delivery provider. Keep examples runnable with
public dependencies and synthetic data so every adopter can reproduce the same flow.

| Platform | Directory | Integration status |
| --- | --- | --- |
| pi / DeepSeek Harness | [agent-plugins](agent-plugins/README.md) | Internal preview: native tools and Skill for the isolated file-cleanup scenario |
| Jenkins | [jenkins](jenkins/README.md) | Synthetic Jenkinsfile, build-context adapter and outer-Agent client using encrypted handoff |
| GitHub Actions | [github-actions](github-actions/README.md) | Planned deployment workflow calling the shared CLI or API |
| Argo Rollouts | [argo](argo/README.md) | Inline Analysis Job, encrypted handoff, layered failure diagnostics and repeatable eight-case acceptance |
| Argo CD | [argo](argo/README.md) | Sync hooks remain planned |

The shared CLI lives in [cmd/guardrailctl](../cmd/guardrailctl/README.md).
Scenario source and its installable status are listed in
[scenarios](../scenarios/README.md). The pi adapter is a single host entrypoint;
new scenarios are registered through that package.
For planned server-side context contracts and reusable provider implementations,
see [core and adapter responsibilities](../docs/extensions.md#core-and-adapter-responsibilities).
Their source-checkout design notes live under `extension/delivery/` and
`internal/adapters/delivery/`; those source-only directories are not included in
native archives. CD-side recipes use the HTTP API or CLI independently of those
planned provider APIs.
The project's own `.github/workflows` runs its development checks and is separate
from these adopter-facing deployment recipes.
