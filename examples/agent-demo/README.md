# Recovery Agent demo

The console, CLI and management API use direct access. Only assessment submission requires a Run credential. See [Access](../../docs/access.md).

Replay the [first-batch review](../../docs/media/first-batch-run.mp4), then run it in your checkout. The launcher sets up the local service and Run.

## Try it with your coding agent

Open this checkout in your coding agent (such as Claude Code, Cursor, Codex, or Antigravity) with Make, Python 3.9+ and the Go version in `go.mod` available. Ask:

> Use $open-guardrail-demo to check the first batch and tell me whether the next batch can proceed.

The agent starts the local trial, reads the frozen [Spec](first-batch.yaml), shows its queries and reports the recorded decision in chat. If the host does not discover skills, ask it to follow [First-batch recovery](../../.agents/skills/open-guardrail-demo/SKILL.md#first-batch-recovery).

The telemetry comes from a [synthetic local recording](first-batch-data.json).
The query tool filters by the recorded window and first-batch instances.
The monitoring and alert snapshots also contain an instance outside that batch.
The agent should investigate a write error by correlating its handler with the
candidate change and its order ID with a read-only record lookup. The query
tool returns observations; the agent chooses the verdict. The local CLI creates the Run. Submission and decision readback use the service's HTTP API.

In the default case, payment for `order-42` was captured while its state is still `pending`. Expect `BLOCKED / DENY`. The final command prints the Run ID, decision and `recordPath`. Open that file to see the service's diagnostic report and the accepted synthetic observations. Each Run has its own file under `data/first-batch-reports/` in the checkout or extracted archive. It remains available after Ctrl-C removes the temporary database and credentials. If export fails, leave the launcher running and retry the same submission. For optional browser inspection, run `python3 examples/agent-demo/first_batch.py start --console` in your own terminal. Paste the printed Run instruction into your coding agent. Open the printed local URL directly to inspect the [Run report](../../docs/audit-and-reports.md#run-report). Ctrl-C in that terminal removes its database and credentials.

To compare different inputs, start a fresh trial with `--case clean` or
`--case logs-unavailable`. Check whether the agent distinguishes an empty
log result from a log source that could not be queried. This walkthrough is
available from this checkout or a native archive containing `first_batch.py`.

## Original three health observations

The three JSON health observations below exercise the Recovery Spec shared
by the Jenkins and Argo examples. They can also be assessed independently.

This path uses one local service, the CLI,
and one instruction to the Agent. Jenkins, public-key encryption and a
deployment pipeline are not involved.

Start the local service and make `guardrailctl` available (from a release archive
or by building this repository). Then ask your coding agent, from the checkout
or extracted release:

> Use `$open-guardrail-demo` to evaluate all three observations in
> `examples/agent-demo/`. Create and submit three Runs, show me their Run IDs and
> Decisions, and check that I can inspect them on the Runs page.

The service address defaults to `http://127.0.0.1:8080`. For another local
address, set `OPEN_GUARDRAIL_URL` to the exact origin before
running the demo. The files in this directory contain synthetic observations
only; no real service is queried.

## Jenkins pipeline example

See [the advanced Jenkins + outer Agent example](../../integrations/jenkins/README.md).
There the Agent triggers Jenkins and submits directly with its encrypted Run
token handoff, while Jenkins independently waits for the Decision. Jenkins does
not run Codex. The local `smoke.py` exercises the actual API/CLI and management
list with deterministic synthetic checks; it does **not** evaluate model quality.
Run it with `python3 examples/agent-demo/smoke.py` if you want an isolated
technical self-check without touching your existing service.

## Isolated technical self-check

For a guided local trial that keeps all four synthetic Runs available in the
browser, use:

```sh
python3 examples/agent-demo/smoke.py --keep-running
```

After the checks pass, open the printed URL directly and select **Runs**.
Ctrl-C stops the service and removes its temporary data and credentials.
The source-only `make demo` target starts the separate
[file cleanup example](../cleanup-demo/README.md). See
[troubleshooting](../../docs/getting-started.md#troubleshooting) for dependency,
startup and connection issues.

From either the source checkout or an extracted native release, run:

```sh
python3 examples/agent-demo/smoke.py
```

Python 3.9+ is needed for this optional self-check. In a source checkout it builds
the server and CLI with Go; in a release it uses the two packaged binaries and
needs no Go or Node.js. It starts its own loopback service, uses a temporary data
and credential directory, and leaves an existing service untouched. It verifies
healthy ALLOW, unhealthy DENY, unknown DENY, and a stored ALLOW whose forged
Evidence must still be blocked by the delivery gate. These are deterministic
contract checks and do not measure an Agent's reasoning quality.


Release maintainers use `make smoke-release VERSION=...` to run this from the
actual archive with an empty executable search path. See
[release verification](../../docs/releases.md).
