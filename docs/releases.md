# Release verification

A preview archive contains the native `open-guardrail` server and `guardrailctl`,
configuration and user guides, the OpenAPI document, policy/observation examples,
CD integration recipes and `.agents/skills/open-guardrail-demo`. Archives also retain
the Apache-2.0 LICENSE, project NOTICE and original third-party legal files and their
version/checksum inventory. The service and
CLI require no Go, Node.js or Python runtime. The optional self-check below needs
Python 3.9+.

See [release notes](release-notes.md) for the capabilities, compatibility and
limitations of a release. For official binary releases, see
[GitHub Releases](https://github.com/alibaba/open-guardrail/releases).

For a matched service/CLI/plugin bundle, see the [Agent plugin installation and trial guide](plugin-trial.md).

## Check and try a downloaded archive

Verify its sidecar before extracting. In the download directory, use the command
available on your platform:

```sh
sha256sum -c open-guardrail-VERSION-linux-amd64.tar.gz.sha256
# macOS:
shasum -a 256 -c open-guardrail-VERSION-darwin-arm64.tar.gz.sha256
```

Replace VERSION and the platform with the archive you downloaded. Extract into
an empty directory and run `./open-guardrail serve`; initialize the administrator
in the browser. Use a persistent `--data-dir` for data you want to keep.

Obtain the archive and checksum from the same official release. A matching
checksum checks file integrity; publisher authentication additionally requires
verification of the release's signatures or provenance.

For an interactive trial from the extraction root:

```sh
python3 examples/cleanup-demo/demo.py
```

Choose a console password, then open the printed Demo URL. The predefined runner
uses the packaged service and CLI to create real Runs, while the controlled tool
acts on files in an isolated temporary directory. The demo page needs no login;
the separate Console uses username `demo` and the chosen password. Keep the
terminal open; Ctrl-C stops both services and removes temporary state. See the
[cleanup guide](../examples/cleanup-demo/README.md) for checks and limits.

For the original isolated Recovery contract self-check:

```sh
python3 examples/agent-demo/smoke.py
```

This starts a separate loopback service on a free port with temporary data and
credentials, bootstraps a temporary administrator, publishes the Recovery policy,
creates Runs, submits Snapshots through the delivery adapter, reads stored
Evidence and verifies the management queries. It checks healthy ALLOW, unhealthy
DENY, unknown DENY and delivery-side rejection of forged Evidence even when
the service reports ALLOW. It cleans up its process and temporary files.
An existing service is not used or modified.

To inspect those Runs in the browser before cleanup, use:

```sh
python3 examples/agent-demo/smoke.py --keep-running
```

Choose a password at the hidden prompts, then open the printed URL and sign in
as `demo`. Select **Runs** to inspect the four records. Keep the terminal open;
Ctrl-C stops the temporary service and removes its data and credentials.
Use Python 3.9+ for the demo. Release assembly still requires Python 3.12+. See
[troubleshooting](getting-started.md#troubleshooting) for dependency and startup
problems.

The test uses the packaged binaries and fixtures. It does not invoke a model,
Jenkins Controller or age encryption. The Agent demo skill is available for a
separate reasoning-based trial; see [the demo guide](../examples/agent-demo/README.md).

## Public artifact verification policy

Local package scripts produce archives, SHA-256 sidecars and a matching plugin
manifest. They do not sign artifacts or generate SBOMs. The dependency-license
inventory is not an artifact SBOM.

The public [preview release workflow](https://github.com/alibaba/open-guardrail/blob/main/.github/workflows/release.yaml) adds
SPDX 2.3 JSON SBOMs and GitHub OIDC attestations for the exact native/plugin
archives verified on native runners. It signs the final manifest, installer and
`SHA256SUMS`, then creates a **draft** pre-release. Workflow code alone is not
evidence that a release job ran or an artifact was signed. Check the release's
actual run, assets and verification results.

### Verify release provenance

Download the native archive and checksum for your platform, its `.spdx.json`,
`.provenance.sigstore.json` and `.sbom.sigstore.json`, plus `plugin-release.json`,
`install.py`, `SHA256SUMS` and `release-manifest.sigstore.json` from the same
official Release. Plugin users also need the matching `.tgz` and its SBOM/bundles.
Use a current GitHub CLI with `gh attestation verify`.

Replace the version, platform and expected 40-character public commit below with
the values independently checked against the Release tag. Authenticate the
manifest and installer before running downloaded scripts:

```sh
release_version=0.1.0-alpha.0
release_commit=EXPECTED_PUBLIC_COMMIT
release_archive="open-guardrail-$release_version-linux-amd64.tar.gz"
for release_file in plugin-release.json install.py SHA256SUMS; do
  gh attestation verify "$release_file" --bundle release-manifest.sigstore.json \
    --repo alibaba/open-guardrail \
    --signer-workflow alibaba/open-guardrail/.github/workflows/release.yaml \
    --source-ref "refs/tags/v$release_version" --source-digest "$release_commit" \
    --deny-self-hosted-runners
done
gh attestation verify "$release_archive" --bundle "$release_archive.provenance.sigstore.json" \
  --repo alibaba/open-guardrail \
  --signer-workflow alibaba/open-guardrail/.github/workflows/release.yaml \
  --source-ref "refs/tags/v$release_version" --source-digest "$release_commit" \
  --deny-self-hosted-runners
gh attestation verify "$release_archive" --bundle "$release_archive.sbom.sigstore.json" \
  --repo alibaba/open-guardrail \
  --signer-workflow alibaba/open-guardrail/.github/workflows/release.yaml \
  --source-ref "refs/tags/v$release_version" --source-digest "$release_commit" \
  --deny-self-hosted-runners --predicate-type https://spdx.dev/Document/v2.3
```

Repeat the archive commands for the plugin `.tgz`. Verify the native sidecar as
shown above. If all release assets were downloaded, also run `sha256sum -c
SHA256SUMS` (macOS: `shasum -a 256 -c SHA256SUMS`). That file covers the payload,
archive proofs and manifest; its own final signing bundle is authenticated
separately and is not recursively listed in the checksums.

An SBOM attestation binds the SBOM to its archive digest. An attestation establishes
the signer and source, not the absence of vulnerabilities or sandbox coverage.

## Prepare the public preview draft

After syncing the reviewed internal changes to the public repository, wait for
the final public `main` commit's CI to succeed. Commit notes at
`docs/releases/vVERSION.md` before tagging. From the public repository, create a
preview tag such as `v0.1.0-alpha.0` at that exact commit and push that tag. The
workflow accepts `alpha.N`, `beta.N` and `rc.N` tags and only runs in
`alibaba/open-guardrail`. It requires the tagged commit to belong to `main` and
to have a successful main push CI; an unfinished check stops preflight. Rerun
the failed jobs after CI succeeds rather than moving the tag. Preserve artifacts
from successful native jobs when retrying a partially completed run.

One shared plugin job packs the npm archive once, generates its SBOM and signs
the archive, installer and plugin manifest. The four native jobs download and
authenticate that same artifact before using it. Each builds only its own native
server/CLI archive, runs the source-free native and shared-plugin checks, and
stages its archive plus a manifest identifying the tested plugin hash. Their
SBOM/provenance generation follows those checks. Collection verifies all four
native archives and the shared plugin against the same version, source and
workflow identity. No cross-platform npm tarball reproducibility assumption is
needed; all platforms install the same bytes.

Preflight, shared plugin and draft jobs each have a 15-minute execution limit;
native jobs have 45 minutes each, and collection has 30 minutes. Native builds
must match their runner's OS/architecture. Local `make plugin-release` retains
its full-matrix behavior. The workflow uses these narrower script modes:

```sh
python3 scripts/plugin-release.py --version 0.1.0-alpha.0 --output-dir shared-plugin --plugin-only
python3 scripts/plugin-release.py --version 0.1.0-alpha.0 --output-dir native-pair \
  --platform linux-amd64 --plugin-assets shared-plugin
```

The second command produces one native pair without running `node pack.mjs` or
`npm pack`; authenticate downloaded shared assets first as the workflow does.
`PACKAGE_PLATFORMS=linux/amd64 make public-release VERSION=0.1.0-alpha.0` selects
one native archive without a plugin. Omitting that setting preserves all four
platforms for local packaging.

The final job verifies the manifest, installer, checksums and unchanged tag, then
creates a new draft. It does not replace an existing Release or its assets.
If creation/upload fails, inspect the draft and workflow state before retrying;
do not delete or overwrite an existing draft to make a rerun pass. Native jobs
are deterministic technical checks, not real-model or independent-user trials.
Record any real-model acceptance separately and describe the actual validated
host/platform scope in the release. Complete the repository distribution review
before publishing the draft. Creation of a draft does not publish the version.
For the first Alpha, require all four native jobs, proof verification, draft
creation and draft readback to succeed before publishing. A code fix changes the
source commit: use a new preview tag (for example `alpha.1`) with matching notes,
rather than moving the old tag. Network-only failures can retry failed jobs.
Stable tags such as `v0.1.0` remain outside this preview workflow.

## Build and verify before distribution

Review the source files and original third-party notices before distribution.

Assembly needs a Git checkout, the Go toolchain from `go.mod` and Python 3.12+.
Build and verify a preview from its source commit:

```sh
make test
make smoke-release VERSION=0.1.0-alpha.0
```

The second command builds Linux/macOS archives for amd64/arm64, checks the native
archive's checksum, extracts it outside the checkout, and runs its self-check
with Go and Node absent from PATH. It exercises the actual packaged assets rather
than falling back to source. Other platform binaries are cross-compiled; their
execution still requires native runners. CI performs this check before uploading
build artifacts. This workflow does not publish a Release or Tag.
`make public-release VERSION=<version>` (also `make release`) separately checks
required tracked public assets, a non-placeholder module path and dependency
inventory status before assembly.

Public assets are selected with `git ls-files`: docs, examples, integrations, the
demo skill, third-party license inventory and explicit root/configuration/contract files. Untracked local notes,
credential files, caches and stale staging directories cannot be copied into
this asset set. Public archives require tracked LICENSE/NOTICE, community files and
the third-party inventory plus every original legal file it references. Missing
files or checksum changes stop public assembly. Adding a public asset requires
adding it to Git. Version/commit
metadata is injected into both binaries; implicit Go VCS stamping is disabled.

## Reproduce an archive

Archive assembly sorts entries and normalizes file ownership, permissions, tar
mtime and the gzip header. It uses the Git commit timestamp by default; to fix
that timestamp explicitly:

```sh
SOURCE_DATE_EPOCH=1700000000 make preview-release VERSION=0.1.0-alpha.0
```

With identical binary and public-asset bytes, VERSION, epoch, and Go/Python build
environment, repeated assembly produces identical archive bytes and SHA-256.
The packaging regression also checks reproducibility after source mtime/mode
changes, untracked-file exclusion and symlink rejection. Artifacts are assembled
in temporary paths and installed atomically after successful assembly.

The project uses Apache-2.0 and the public module path
`github.com/alibaba/open-guardrail`.

## Preview compatibility and upgrades

The `v1alpha1` label does not promise compatibility between every preview build.
Release notes must describe changes to API requests, CLI behavior, GuardrailSpec
semantics and storage before a new build is distributed. Use the server and CLI
from one archive by default. When mixing versions, inspect
`GET /api/v1alpha1/meta/capabilities` for the advertised CLI compatibility range;
the protocol range is independent of the server build version.

Schema 3 includes the initial schema, resource binding table (migration 2) and
optional Run selection/audit details (migration 3). In `auto` mode, a new database
is initialized and compatible version-1/2 databases from this source lineage
receive the bundled additions before the HTTP listener opens. Stored Policy
Revisions and existing Runs retain their identity and evidence; absent historical
selection evidence stays absent. The migration regression covers a version-1
SQLite database with an existing Policy, Revision and Run. PostgreSQL migration
and binding behavior also require the backend CI with a disposable database.

In `external` mode, storage must already have the exact bundled schema version.
A newer database is rejected by an older server in both modes. This is not a
promise to convert arbitrary historical development schemas. Switching drivers
or database URLs does not transfer data.

Before upgrading, stop writers and take a consistent backup of the database.
For SQLite, account for its WAL state; for PostgreSQL, use a consistent backup
and restore procedure. Upgrade one service instance and check readiness and
existing records before resuming use. To roll back, stop the new server and
restore the pre-upgrade backup with the matching old server. No automatic
downgrade command is provided. See
[database migration mode](configuration.md#schema-migration-mode).

Fixes target the default branch and subsequent preview builds. There are no
maintenance branches or backported security fixes during the preview. Security
reporting and support commitments are described in [SECURITY.md](../SECURITY.md).
