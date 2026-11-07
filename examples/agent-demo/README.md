# Recovery Agent demo

Start with [Getting started](../../docs/getting-started.md) if you need a local
service, administrator account or CLI session. The
[documentation index](../../docs/README.md) links the full set of guides.

This demo lets an Agent evaluate three synthetic health observations against the
published Recovery Spec. The observation files contain no verdicts or prepared
Submissions. The Agent creates three independent Runs and supplies its own Check
summaries and Evidence; the server produces the final Decisions.

## Try it with Codex

This is the **first-time, no-Jenkins path**: one local service, one CLI login,
then one instruction to the Agent. Jenkins, public-key encryption and a
deployment pipeline are not involved.

Start the local service and make `guardrailctl` available (from a release archive
or by building this repository). Initialize the administrator if this is a new
service. Sign in to the CLI once with `guardrailctl auth login --username <name>`;
the password is entered through its hidden prompt. The browser session does not
authenticate the CLI. Then ask Codex, from the checkout or extracted release:

> Use `$open-guardrail-demo` to evaluate all three observations in
> `examples/agent-demo/`. Create and submit three Runs, show me their Run IDs and
> Decisions, and check that I can inspect them on the Runs page.

The Agent uses `guardrailctl scene apply`, `auth create-key`, `run create`,
`run submit`, and `run get`. It writes private credentials and working JSON
outside the repository. Expect `PASS / ALLOW` for the healthy case,
`BLOCKED / DENY` for the unhealthy case, and `UNKNOWN / DENY` when the probe
times out. Open the local management page and select **Runs** to inspect the
three immutable records. A Guardrail Decision does
not itself pause or resume a release; the integrating business system chooses
whether to enforce it.

The service address defaults to `http://127.0.0.1:8080`. For another local
address, set `OPEN_GUARDRAIL_URL` to the exact origin before signing in and
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

Choose the temporary administrator password at the hidden prompts. After the
checks pass, open the printed URL, sign in as `demo` and select **Runs**.
Ctrl-C stops the service and removes its temporary data and credentials.
The source-only `make demo` target starts the separate
[file cleanup example](../cleanup-demo/README.md). See
[troubleshooting](../../docs/getting-started.md#troubleshooting) for dependency,
startup and login issues.

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

The optional `--password-stdin` flag reads the temporary administrator password
from stdin for automated `--keep-running` trials. No password is accepted as a
command-line argument.

Release maintainers use `make smoke-release VERSION=...` to run this from the
actual archive with an empty executable search path. See
[release verification](../../docs/releases.md).
