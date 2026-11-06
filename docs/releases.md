# Release verification

A preview archive contains the native `open-guardrail` server and `guardrailctl`,
configuration and user guides, the OpenAPI document, policy/observation examples,
CD integration recipes and `.agents/skills/open-guardrail-demo`. Archives also retain
the Apache-2.0 LICENSE, project NOTICE and original third-party legal files and their
version/checksum inventory. The service and
CLI require no Go, Node.js or Python runtime. The optional self-check below needs
Python 3.12+.

See [release notes](release-notes.md) for the capabilities, compatibility and
limitations of a release. For official binary releases, see
[GitHub Releases](https://github.com/alibaba/open-guardrail/releases).

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

For an isolated technical trial from the extraction root:

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

The test uses the packaged binaries and fixtures. It does not invoke a model,
Jenkins Controller or age encryption. The Agent demo skill is available for a
separate reasoning-based trial; see [the demo guide](../examples/agent-demo/README.md).

## Public artifact verification policy

The package script produces archives and SHA-256 checksum sidecars. It does not
generate SBOMs or attestations; the module-license inventory is not an artifact
SBOM. Check the release's published assets and instructions for any additional
provenance or signature verification.

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

The current schema initializes a new database. It has no historical schema
upgrade path or automatic conversion of development databases. A different
schema version is rejected. Switching database configuration does not transfer
data. See [database initialization](configuration.md#schema-migration-mode).

Fixes target the default branch and subsequent preview builds. There are no
maintenance branches or backported security fixes during the preview. Security
reporting and support commitments are described in [SECURITY.md](../SECURITY.md).
