# File cleanup scenario

Version: `0.1.0-alpha.0` (bundled internal preview).

This scenario owns the four `guardrail_cleanup_*` tool definitions in
[`scenario.mjs`](scenario.mjs), its isolated serial
[`worker.py`](worker.py), the
[`guardrail-cleanup` Skill](skills/guardrail-cleanup/SKILL.md), the
[`policy.yaml`](policy.yaml) GuardrailSpec template and the
[`runtime.py`](runtime.py) file observation and execution gate.
The plugin pack copies these sources together, so the installed package needs
no source checkout. The [browser example](../../examples/cleanup-demo/README.md)
imports this runtime and adds only its page, task orchestration and HTTP entry.

The worker creates its own local OpenGuardrail service and disposable files.
It freezes the proposed targets, observes them, attaches evidence to the
agent's assessment, then checks the authoritative Run and current files before
deletion. It accepts only the fixed sample filenames. The plugin adapter
controls host registration and session shutdown; the scenario controls all
cleanup behavior.

To try it, follow the [plugin installation guide](../../integrations/agent-plugins/README.md).
This is an internal preview, not a general file deletion guard.
