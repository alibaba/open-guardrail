# Install and try the Agent plugins

[简体中文](plugin-trial.zh-CN.md)

Use a matching native/plugin release to try evidence-driven cleanup in pi or
DeepSeek Harness. The tools operate on four disposable sample files. They
start a private loopback service and remove their temporary data on shutdown.

## Requirements and compatibility

| Component | Requirement or validated baseline |
| --- | --- |
| Native binaries | Linux/macOS, amd64 or arm64; server and CLI from the selected release |
| Python | 3.9+ |
| Node.js | 22+ for pi and the standalone trial; 24.9+ for DeepSeek Harness 0.2.2-rc.1 |
| pi | 1.5.0, `@earendil-works/pi-coding-agent` |
| DeepSeek Harness | 0.2.2-rc.1, `@deepseek-ai/dsh`; pnpm for installation |
| Model | Authenticate separately in your chosen host |

The pinned hosts previously passed real-model cleanup trials on macOS arm64.
The release workflow checks native installations on Linux and macOS for each
supported architecture. Check the actual CI results for the source revision you
use. A model route or host upgrade requires a new real-model trial.

For Codex subscriptions in the pinned DSH version, configure SSE in the existing
provider. Follow the [transport instructions](../integrations/agent-plugins/README.md#observed-host-limitations),
including its natural-exit check.

## Obtain a version

For `v0.1.0-alpha.1` and later, download these from the same approved
[GitHub Release](https://github.com/alibaba/open-guardrail/releases):

- The native `open-guardrail-VERSION-PLATFORM.tar.gz` for your platform.
- `openguardrail-agent-plugins-preview-VERSION-install.tar.gz`.
- `SHA256SUMS`.

Replace VERSION and PLATFORM with the selected release and platform. Follow
[archive and provenance verification](releases.md#verify-release-provenance)
before extracting the installation archive. In your download directory:

```sh
tar -xzf openguardrail-agent-plugins-preview-VERSION-install.tar.gz
```

This creates `install.py`, `plugin-release.json` and the matching npm `.tgz` in that
directory. Keep your native archive beside these files. The installer and manifest
are included in the plugin installation archive, rather than separate downloads.
The inner npm archive remains suitable for DSH; the outer `-install.tar.gz` is
the complete installation bundle and is not an npm package.

For the older `alpha.0` layout, use its
[versioned installation guide](https://github.com/alibaba/open-guardrail/blob/v0.1.0-alpha.0/docs/plugin-trial.md).
For a maintainer's local candidate, retain its source revision and acceptance
results. From a reviewed checkout with Go, Python 3.12+, Node 22+ and npm:

```sh
make smoke-plugin-release VERSION=0.1.0-alpha.1
```

This preserves the raw local native/plugin bundle under `dist/plugin-release-VERSION/`
for development. The public Tag workflow authenticates all four native jobs,
assembles the six user download files and repeats installation and the packaged
doctor's live readiness check before signing them. Native jobs already run the
full deterministic cleanup trial. Existing local bundle directories are preserved; use
a fresh `PLUGIN_RELEASE_DIR=/absolute/path/to/new-bundle` for a rebuild.

## Install and diagnose

From the downloaded asset directory

```sh
python3 install.py --assets-dir . --prefix "$HOME/.local/share/open-guardrail" --version VERSION
source "$HOME/.local/share/open-guardrail/VERSION/env.sh"
plugin_dir="$HOME/.local/share/open-guardrail/VERSION/plugin"
node "$plugin_dir/doctor.mjs" --live
```

The installer verifies the selected archive hashes, rejects unsafe tar entries
and creates a new version directory. An existing version is preserved. Use a
checksum manifest from the same trusted distribution as the installer. Hashes
check integrity; publisher authentication requires a trusted download channel
or separately verified provenance/signatures.

The doctor checks Node, Python and binary access. `--live` additionally starts
an isolated service, negotiates through the real CLI and checks the service build
against the plugin version. `--host pi` or `--host dsh` checks the pinned host
command, and pnpm for DSH. Model login and actual tool/Skill loading are checked
inside the chosen host in the next step.

Run the deterministic self-check before using a model

```sh
node "$plugin_dir/trial.mjs"
```

It creates real Runs, verifies DENY, checks that refusal preserves every file,
then narrows the request and verifies ALLOW, actual deletion of only the two
`.tmp` files, unchanged protected-file metadata/content hashes and accepted
Submission references. It prints a credential-free summary. The session records
are ephemeral. This self-check uses programmed verdicts; model participation is
validated separately below.

## Try in your host

For pi, install the extracted plugin directory

```sh
node "$plugin_dir/doctor.mjs" --host pi
pi install "$plugin_dir"
pi --no-mcp --tools 'read,guardrail_cleanup_*'
```

For DSH, install the inner `.tgz` extracted from the installation archive into
the profile you actually use

```sh
node "$plugin_dir/doctor.mjs" --host dsh
dsh plugin --profile headless add /absolute/path/to/openguardrail-agent-plugins-preview-VERSION.tgz --ignore-scripts
dsh --profile headless
```

Use a private `PI_CODING_AGENT_DIR` or `DSH_HOME` for an isolated trial, and
configure model access in that environment through the host's normal login.
Submit this task

> Use the guardrail-cleanup Skill. Assess all four sample files and attempt
> execution to demonstrate refusal. Then narrow the request under a new
> requestId, reassess and execute. Verify actual files and both persisted
> decisions, including each Run's accepted evidence with status(runId).

Inspect the tool results. The first request should be denied with all files
present. The narrower request should remove `cache.tmp` and `download.tmp`,
while `orders.csv` and `config.yaml` remain unchanged. Confirm both authority
records before ending the session. For unattended DSH trials also use the
packaged `verify-host-exit.py` as described in the plugin guide.

## Independent user acceptance

Ask participants who did not implement the integration to follow this guide in
their own test environment. Record each person's result separately

| Field | Record |
| --- | --- |
| Participant and date | To be recorded |
| Service/plugin version, platform, host and model route | To be recorded |
| Time to install / first successful task | To be recorded |
| First refusal understood and corrected | Pass / fail, with reason |
| Two temporary files deleted, protected files unchanged | Actual tool results |
| Frozen policy, accepted evidence and decisions inspected | Actual Run IDs and findings |
| Host exited naturally for unattended use | Exit verifier result |
| Maintainer assistance and blockers | Steps, errors and assistance needed |

A maintainer's clean-environment test and a real-model trial provide separate
technical evidence. Record participant trials only after they actually happen.
Before sharing logs, remove credentials, personal paths and private model data.

## Upgrade, uninstall and troubleshooting

Install an upgrade into its own version directory, stop the host, source the new
`env.sh`, then register the new plugin and repeat the checks. Retain the previous
version for rollback. For pi, remove the old registration before installing a
new directory, otherwise both copies register the same tool names

```sh
pi remove /absolute/path/to/OLD-VERSION/plugin
pi install /absolute/path/to/NEW-VERSION/plugin
```

DSH replaces a package only when its name is unchanged. If upgrading from the
earlier `@openguardrail/cleanup-preview` preview, stop DSH and remove that old
package from the same profile before adding `@openguardrail/agent-plugins-preview`:

```sh
dsh plugin --profile headless remove @openguardrail/cleanup-preview
```

Check the selected profile after installation. Stop the host and unregister the plugin before removing
its installation directory. DSH plugin removal is by package name
`@openguardrail/agent-plugins-preview`; pi removal uses the installed directory.

If diagnosis fails, follow its `fix` field. If model login or Skill discovery
fails, verify the chosen profile and the host's own diagnostics. If execution
fails or times out, inspect actual state and prior Runs before retrying. Report
versions, failing step, Run IDs and a redacted diagnostic summary through the
[issue template](https://github.com/alibaba/open-guardrail/issues/new?template=bug_report.md).
