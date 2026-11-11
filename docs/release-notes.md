# Release notes

## Unreleased

- Prepared the first preview's versioned notes and draft-release workflow.
  See [v0.1.0-alpha.0](releases/v0.1.0-alpha.0.md). The presence of these notes
  does not mean a Release has been published or its native jobs have passed.
- Resource-to-Policy bindings select a fixed published Revision or follow the
  active Revision. Run detail records optional frozen binding selection evidence;
  idempotent binding writes retain before/after audit records. Resource references
  are trusted caller inputs, not independently verified ownership facts.
- One Agent plugin package serves pi and DeepSeek Harness, sharing the
  `scenarios/file-cleanup/` policy, tools, runtime and Skill. Versioned native/plugin
  bundles support checksum-verified installation and deterministic clean trials.
- Go 1.27.4 is the build baseline. The native server and CLI need no Go runtime.
- The first-use demo now runs a file cleanup task against a real local service
  and a controlled file tool. Its browser page shows actual Runs, accepted
  evidence and file effects. The predefined runner needs no model account.
  Requests and batch limits can be changed and rerun; the original Recovery
  contract self-check remains available separately.
- The optional Recovery demo can keep its temporary service and four synthetic
  Runs available for browser inspection with `--keep-running`. Choose the
  temporary administrator password through hidden prompts or `--password-stdin`;
  Ctrl-C stops the service and removes its temporary data and credentials.
- First-use instructions now connect demo results to the Runs page, and the demo
  reports Python/Go requirements, build progress and startup exit codes.
- The optional local demo supports Python 3.9+, including the default Python on
  older macOS installations. Maintainer release scripts still need Python 3.12+.

## Development preview

This page describes the current preview. For versioned releases, see
[GitHub Releases](https://github.com/alibaba/open-guardrail/releases).

OpenGuardrail is a development preview with a `v1alpha1` HTTP API.

The canonical source repository is
[alibaba/open-guardrail](https://github.com/alibaba/open-guardrail), and the Go
module path is `github.com/alibaba/open-guardrail`. Source consumers must use
this path instead of the earlier temporary `open-guardrail` module name.

### Included capabilities

- A standalone Go service, the `guardrailctl` HTTP client and an embedded
  management UI.
- First-administrator setup, browser and CLI sessions, and scoped Project keys
  with expiry, metadata listing, secret rotation and revocation.
- Scenes, editable Draft Revisions, immutable published Revisions and separate
  activation of a published Revision.
- Runs that freeze the evaluation context, selected policy and deadline;
  complete intermediate and final Submissions; structured evidence; stored
  decisions and expiry.
- Per-Run Submission Credentials that are separate from Project keys and can be
  replaced while the Run is open.
- SQLite and PostgreSQL storage, with one service instance per database.
- Exact resource bindings and frozen binding selection evidence; the public
  distribution uses caller-supplied references without an external resolver.
- A matched pi/DeepSeek Harness plugin and a reusable file-cleanup scenario.
- A synthetic local Recovery Agent example, a deterministic self-check and
  reference Jenkins and Argo Rollouts demos.

### Distribution scope

The native packaging matrix contains both `open-guardrail` and `guardrailctl`:

| Operating system | Architectures | Archive platform name |
| --- | --- | --- |
| Linux | amd64, arm64 | `linux-amd64`, `linux-arm64` |
| macOS | amd64, arm64 | `darwin-amd64`, `darwin-arm64` |

These binaries require no Go, Node.js or Python runtime. The optional synthetic
self-check requires Python 3.9+. The repository also includes a Docker Compose
example behind HTTPS; an official container registry is not defined yet.
There is no Windows archive in the current packaging matrix.

Builds for another platform are cross-compiled. Each target still needs its
native archive verification before being published; a successful cross-build
does not establish that verification. See [release verification](releases.md).

### Compatibility and security maintenance

The HTTP API is `v1alpha1`. API, CLI, Spec and storage contracts can change during
the preview. Use the server and CLI from the same archive by default, and check
the server's advertised CLI compatibility when mixing versions. No stable public
Go provider or server-composition API is included.

Run Decisions and delivery gate outcomes use `ALLOW` and `DENY`.
`PASS` produces `ALLOW`; `BLOCKED` and `UNKNOWN` produce `DENY`. Delivery gates
also deny execution when their independent evidence validation fails.

Fixes target the default branch and subsequent preview builds. There are no
maintenance branches, backported security fixes or fixed security-response times.
Report vulnerabilities using [SECURITY.md](../SECURITY.md).

Schema version 3 adds resource bindings and optional Run/audit details. `auto`
applies the bundled migrations to a new database or the compatible version-1/2
schema from this source lineage. Existing Runs retain their frozen Revision and
their stored selection evidence or its absence. Arbitrary historical development
schemas and data transfer between databases are unsupported. Back up before an
upgrade; see [upgrade and rollback](releases.md#preview-compatibility-and-upgrades).

### Known limitations

- The server validates evidence structure and completeness, but does not prove
  that submitted observations are true. Integrations verify observations and
  control their own actions.
- Evaluator registration and attestation, Execution Claims, ActionResults,
  multi-instance operation and replaceable identity/provider APIs remain planned.
- Delivery examples use synthetic observations. The GitHub Actions deployment
  recipe and Argo CD sync hooks are not implemented.
- Remote deployments require HTTPS through a trusted reverse proxy and the
  documented deployment protections.
