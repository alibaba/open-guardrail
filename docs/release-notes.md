# Release notes

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
- A synthetic local Recovery Agent example, a deterministic self-check and
  reference Jenkins and Argo Rollouts demos.

### Distribution scope

The native packaging matrix contains both `open-guardrail` and `guardrailctl`:

| Operating system | Architectures | Archive platform name |
| --- | --- | --- |
| Linux | amd64, arm64 | `linux-amd64`, `linux-arm64` |
| macOS | amd64, arm64 | `darwin-amd64`, `darwin-arm64` |

These binaries require no Go, Node.js or Python runtime. The optional synthetic
self-check requires Python 3.12+. The repository also includes a Docker Compose
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

The current schema initializes a new database. Historical schema conversion and
data transfer are not supported.

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
