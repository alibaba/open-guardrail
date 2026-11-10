# Agent plugins (cleanup preview)


For a matching versioned installation and independent trial, see the [installation guide](https://github.com/alibaba/open-guardrail/blob/main/docs/plugin-trial.md).

[简体中文](README.zh-CN.md)

Native pi and DeepSeek Harness tools let an agent assess a cleanup request
against a frozen OpenGuardrail policy. They operate on four real sample files in
a private disposable directory. The agent chooses verdicts and summaries; the
adapter attaches the exact tool observation as evidence. The existing cleanup
tool independently verifies the decision, evidence and actual files before
deletion. No arbitrary-path deletion tool or new public service API is added.

## Install

Requirements: Node.js 22+, Python 3.9+, native `open-guardrail` and `guardrailctl`
binaries, and a separately authenticated model in the host. A source checkout
also needs Go from `go.mod` to build the binaries. Linux/macOS only: the reused
file tool depends on POSIX APIs.

Tested hosts: **pi 1.5.0** (`@earendil-works/pi-coding-agent`) and **DeepSeek
Harness 0.2.2-rc.1** (`@deepseek-ai/dsh`). DSH needs pnpm for plugin installation.
The candidate package name has not been published or reserved on a registry.

From the checkout root:

```sh
make build
export OPEN_GUARDRAIL_BINARIES="$PWD/bin"
# Optional: export OPEN_GUARDRAIL_PYTHON=/absolute/path/to/python3
plugin_install_dir=$(mktemp -d)
(cd integrations/agent-plugins && npm pack --pack-destination "$plugin_install_dir")
```

The archive contains both adapters, the Skill, shared worker, the original
cleanup implementation/policy copied during packing, LICENSE and NOTICE. It
requires the configured native binaries but no source checkout or Go at runtime.
Use a stable installation directory; do not remove it while registered in pi.

For pi, extract the archive and install the directory. A raw local `.tgz` is not
an extension directory:

```sh
tar -xzf "$plugin_install_dir/openguardrail-cleanup-preview-0.1.0-alpha.0.tgz" -C "$plugin_install_dir"
pi install "$plugin_install_dir/package"
pi --no-mcp --tools 'read,guardrail_cleanup_*'
```

Use a private `PI_CODING_AGENT_DIR` when trialling without changing normal pi
settings. The explicit tool allowlist limits this invocation to Skill reading
and cleanup tools; it does not change permissions for other invocations.

For DSH, choose the profile you actually use:

```sh
dsh plugin --profile headless add "$plugin_install_dir/openguardrail-cleanup-preview-0.1.0-alpha.0.tgz" --ignore-scripts
dsh --profile headless 'Use guardrail-cleanup to demonstrate a refused broad cleanup, then reassess a permitted narrower request and verify the actual outcome.'
```

Use a private `DSH_HOME` for an isolated profile. The bundle adds four tools and
registers its Skill when the host skill service is available. It preserves the
host's model, permission checks, sandbox and other plugins. No inference model,
endpoint or credential is embedded in this package.

## Try

Ask: **Use the guardrail-cleanup Skill. Assess all four sample files and attempt
execution to demonstrate refusal. Then narrow the request under a new requestId,
reassess and execute. Verify actual files and both persisted decisions.**

| Tool | Purpose |
| --- | --- |
| `guardrail_cleanup_status` | Readiness, actual files and authoritative Runs |
| `guardrail_cleanup_prepare` | Run, frozen targets/policy and actual observation |
| `guardrail_cleanup_submit` | Agent check verdicts/summaries and server decision |
| `guardrail_cleanup_execute` | Final verification and actual permitted deletion |

The broad request should produce BLOCKED/DENY and retain all files. A new request
for the two permitted temporary files should produce PASS/ALLOW and delete only
those files, preserving orders and configuration. The execution tool also
refuses a false PASS for protected files even when the server stored ALLOW.

Status returns the latest ten owned Run summaries. Pass `runId` to read one
owned Run's frozen revision and accepted Submission snapshot without duplicating
large evidence in the list. The reported removed-file
list is local execution state, distinct from the authoritative decision/evidence.

Each pi conversation or DSH Agent owns its own worker, local service, one-hour
machine key and directory. No existing administrator session or database is
reused. Tools return no credentials or private paths. Shutdown, pi session switch
and plugin unload stop owned services and remove temporary data. Records are
**ephemeral**: inspect/capture non-secret status results while the session lives.

## Failure and trust boundary

- Missing assessment, DENY, expiry, changed targets/files, mismatched evidence or
  unavailable service prevent deletion. Errors do not echo arguments, captured
  command output or credentials.
- After a successful prepare, retries with the same requestId/targets return the
  same Run. If creation or revision verification failed before prepare returned,
  that requestId reports an uncertain creation outcome. Inspect existing Runs
  before continuing. The original operation directory and any credential are
  retained; do not automatically retry under a new ID.
- Submit retries must carry the same checks. An acknowledged terminal submission
  deletes its local credential; subsequent retries read the authoritative Run.
  An uncertain submit retains the credential and frozen snapshot for an identical
  retry. Session shutdown removes any remaining credentials and private data.
- The first call has a seven-minute budget for service startup, setup commands
  and preparation. Once initialization is confirmed, calls have a 90-second
  budget. Expiry cancels the session and requires inspecting prior outcomes.
- Execution is serialized within this preview worker. After any deletion, the
  same Run is refused locally; inspect actual files after partial failure.
  This is not a global Claim or server execution receipt. Multi-file deletion
  is not atomic.
- Cancellation/timeout drains the owned worker and stops further calls in that
  session. It cannot undo completed effects. Inspect returned outcomes before
  starting another session.
- Only the plugin's operation is protected. Other shell/filesystem tools and
  processes under the same OS user are outside this boundary. The directory is
  not an OS sandbox, and the service does not independently attest observations.
- At most 64 Runs are retained per preview session. Operators must trust their
  configured binaries/Python and the host-side code they install.

## Verify

`make test-python` and `make test` intentionally include `test-agent-plugins`.
This gate builds both native binaries and runs isolated real service/file tests
because mocked responses cannot verify evidence checks or actual deletions.
It requires Go, Python and Node 22+. pi/DSH SDK tests remain opt-in through
`OPEN_GUARDRAIL_HOST_MODULES`. Direct test commands without native binary
configuration skip live cases.

After building, from the checkout root:

```sh
OPEN_GUARDRAIL_BINARIES="$PWD/bin" python3 -B -m unittest discover \
  -s integrations/agent-plugins -p 'test_*.py' -v
OPEN_GUARDRAIL_BINARIES="$PWD/bin" node --test integrations/agent-plugins/test/*.test.mjs
```

For actual host-loader and DSH native-pipeline tests, install hosts separately:

```sh
npm install --prefix /absolute/private/host-tests --ignore-scripts --no-audit --no-fund \
  @earendil-works/pi-coding-agent@1.5.0 @deepseek-ai/dsh@0.2.2-rc.1
OPEN_GUARDRAIL_HOST_MODULES=/absolute/private/host-tests/node_modules \
OPEN_GUARDRAIL_BINARIES="$PWD/bin" node --test integrations/agent-plugins/test/*.test.mjs
```

Host tests explicitly skip without that variable. Coverage includes real
service/file effects, false PASS, UNKNOWN, expiry, offline service, changed
arguments/resources, retries, concurrent execution, cancellation, owner isolation,
host policy rejection and native Skill discovery. Loader/deterministic tests
do not establish model behavior: also run the real-model task above against the
packed installation before claiming support for a host version.

## Upgrade or remove

Stop the host before replacing the package/binaries. Repack, reinstall using the
same host commands, and check status in a new session. Remove with
`pi remove /absolute/path/to/the/installed/package` or
`dsh plugin --profile headless remove @openguardrail/cleanup-preview`.
Unregister pi before deleting its installation directory. Normal model sign-in
and other plugins remain owned by the host.

## Observed host limitations

DSH 0.2.2-rc.1 with pi-ai 1.5.0 can retain Codex subscription WebSockets
and referenced five-minute cache timers after the task finishes. The adapter
omits session cleanup. Concurrent first connections from the main reply and
background title generation can also overwrite a cache entry, leaving a
connection outside the cleanup registry. The pure-text, no-plugin control
reproduces the exit delay. See [upstream discussion](https://github.com/deepseek-ai/deepseek-harness/discussions/4190).

For this version combination, use the supported SSE transport in your existing
`llm-pi-ai` provider configuration when running unattended Codex tasks. Add
`transport: sse` to the existing `openai-codex` provider entry. For example, the
relevant part of that configuration is

```yaml
- id: llm-pi-ai
  config:
    providers:
      openai-codex:
        transport: sse
        # Keep this provider's existing credentials, models and other settings.
```

Edit the complete existing provider configuration. DSH patches can replace the
`providers` map, so a separate patch containing only this snippet can discard
other providers and authentication settings. The cleanup bundle leaves model
configuration under host control. Both title generation and main inference must
use the configured transport. SSE passed repeated real-model natural-exit
controls on this pinned combination. Recheck after host or model upgrades.
Native API-key model routes have not been validated in this trial.

The packed package includes an exit verifier. Run it from the extracted package
using your already configured host and model login. Choose a fresh output path
and a timeout long enough for the real task

```sh
python3 "$plugin_install_dir/package/verify-host-exit.py" \
  --output-dir /absolute/private/new-dsh-trial --timeout 180 -- \
  dsh --profile headless --json \
  'Use guardrail-cleanup to demonstrate a refused broad cleanup, then reassess a permitted narrower request and verify the actual outcome.'
```

The verifier stores host output and `exit.json` in a private directory, inherits
the host environment, and records whether it sent SIGTERM or SIGKILL. A timeout
fails verification even if the host handles SIGTERM and returns zero. Inspect
host logs before sharing them. Natural exit is one acceptance criterion. Also
inspect tool results for DENY followed by ALLOW, accepted evidence, deletion of
only the two temporary files, and unchanged protected files. The verifier does
not infer those business outcomes from an exit code.

On networks with an existing private CA, configure the host to trust that CA
through its supported Node/system certificate settings. Keep TLS verification
enabled; a model transport failure does not authorize cleanup execution.
