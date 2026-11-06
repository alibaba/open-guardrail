# OpenGuardrail

**General-purpose, evidence-based guardrails for any scenario.**

OpenGuardrail is a standalone service for defining policy-based checks, recording
assessments and evidence, and returning an `ALLOW` or `DENY` decision.
Applications, services, scripts and agents integrate through HTTP or the
`guardrailctl` CLI.

**Status: development preview.** The API is `v1alpha1`. See
[current capabilities and limits](#current-capabilities-and-limits) for details.

[Source](https://github.com/alibaba/open-guardrail) · [Issues](https://github.com/alibaba/open-guardrail/issues) ·
[Get started](docs/getting-started.md) · [Documentation](docs/README.md) · [API and CLI](docs/api.md) ·
[Deployment](docs/configuration.md) · [Contributing](CONTRIBUTING.md) ·
[Apache-2.0](LICENSE)

## Why OpenGuardrail?

When a system needs a decision backed by explicit criteria and evidence, it needs
answers to concrete questions: Which policy applies? What is being assessed?
What evidence supports the assessment? Is the decision still valid?

OpenGuardrail records these inputs together so a caller can inspect the policy
revision, evaluation context, submitted evidence and final decision for one Run.
The caller defines its scenario and controls how the decision is used.

## Scenarios

Define a Scene and its policies around your own use case. Domain-specific
observations and assessment logic come from your evaluators and integrations.
The core policy, evidence and decision contracts are shared across scenarios.

Production delivery and recovery are the project's first reference scenarios.
The current demos use synthetic data to show a delivery system consuming a
decision. Other scenarios can use the same API and CLI with their own policies,
evaluation context and evidence.

## Target architecture

The target architecture below uses CD delivery as an example of the shared
guardrail model. An agent follows a Scene's checks and submits evidence;
OpenGuardrail validates the submission and records a decision; the calling system
verifies that decision before acting. Blue components belong to OpenGuardrail;
gray components represent the agent and external systems.

[![OpenGuardrail target architecture: an agent uses a Skill and guardrailctl to submit checks and evidence; the CLI and console share the OpenAPI service; policy, Run, evidence, decision and audit components share storage, while CD systems control deployment.](docs/images/target-architecture.png)](docs/images/target-architecture.svg)

*Open the diagram for the full-size SVG. Execution Claims and receipts,
replaceable providers, and some CD integrations remain planned. The server
validates evidence structure and completeness; the calling system verifies the
observations. See [current capabilities and limits](#current-capabilities-and-limits)
and [reference integrations](#reference-integrations) for what works today.*

## How it works

The [evaluation flow](docs/getting-started.md#evaluation-flow) in the getting-started
guide illustrates the current policy-to-decision lifecycle.

1. **Publish a policy.** Define checks and evidence requirements in a GuardrailSpec
   and publish an immutable revision, then activate it for the Scene.
2. **Create a Run.** A caller records the evaluation context and freezes the selected
   policy revision and evaluation deadline.
3. **Submit findings.** An evaluator submits complete check results and
   evidence. Intermediate snapshots can precede a final submission.
4. **Read the decision.** Query the authoritative Run for its stored decision and
   validity, then read its referenced Submission to inspect the evidence.
5. **Use the decision.** The calling system verifies the evidence and validity,
   then applies its own scenario-specific rules.

For a completed Run, the check results aggregate as follows:

| Result | Meaning | Decision |
| --- | --- | --- |
| `PASS` | All checks pass | `ALLOW` |
| `BLOCKED` | At least one check reports a confirmed failure | `DENY` |
| `UNKNOWN` | No check is blocked, but at least one cannot be assessed | `DENY` |

An unfinished Run can expire, and a completed decision has its own validity
window. Integrations must check the authoritative Run, its deadline and decision
validity. See [Runs and submissions](docs/runs.md).

## Quickstart

Get started with the [source repository](https://github.com/alibaba/open-guardrail).
For official binary releases, see [GitHub Releases](https://github.com/alibaba/open-guardrail/releases).

The included local trial uses the Recovery scene. For a complete example, install
Git, the Go version specified in `go.mod` and Python 3.12+, then run:

```sh
git clone https://github.com/alibaba/open-guardrail.git
cd open-guardrail
python3 examples/agent-demo/smoke.py
```

The script builds both commands, starts a temporary local service, publishes the
example policy, creates Runs and verifies their decisions. It uses synthetic data
and deterministic checks; no model, external service or deployment platform is
required.

Each case prints a real Run ID. The expected outcomes are:

| Case | Server decision | Delivery gate |
| --- | --- | --- |
| Healthy observation | `ALLOW` | `ALLOW` |
| Unhealthy observation | `DENY` | `DENY` |
| Probe timeout (`unknown`) | `DENY` | `DENY` |
| Submitted evidence disagrees with the gate's observation | `ALLOW` | `DENY` |

The last case demonstrates the integration's evidence check. A stored `ALLOW`
does not establish that submitted observations are true. The temporary service
and its data are removed when the trial finishes.

To keep a service running and inspect your own Runs in the browser, use Go and
Make from the checkout root:

```sh
make build
./bin/open-guardrail serve
```

Open <http://127.0.0.1:8080> and create the first administrator. There is no default
password. SQLite stores data in `./data`; no separate database service is needed.

Follow [Getting started](docs/getting-started.md) to publish a policy, submit a
synthetic assessment and inspect a decision in the UI. To let an agent assess
the sample observations, follow the [Recovery Agent demo](examples/agent-demo/README.md).

## Current capabilities and limits

The preview includes:

- A standalone Go service, an HTTP CLI and an embedded management UI.
- Local administrator setup, browser/CLI sessions and scoped machine credentials
  with bounded lifetimes, metadata listing, secret rotation and revocation.
- Scenes, editable policy drafts and immutable policy revisions.
- Runs with frozen policy/evaluation context, intermediate and final submissions,
  structured evidence, expiry and stored decisions.
- SQLite and PostgreSQL adapters, with one service instance per database.
- Native packaging for Linux and macOS on amd64 and arm64, plus optional Docker
  deployment behind HTTPS.

The current boundaries are:

- Evaluators run outside the service. Evaluator registration and attestation are
  not implemented; submitted evidence is not independently verified by the server.
- Applying a decision and controlling actions belong to the calling system.
  Execution Claims and ActionResults remain planned.
- Multi-instance operation and replaceable identity/provider APIs are not yet
  supported.
- Alpha APIs and storage contracts may change during the preview.

See the [roadmap](docs/development-roadmap.md), [runtime architecture](docs/architecture.md)
and [security policy](SECURITY.md) for details.

## Reference integrations

Use the versioned [HTTP/OpenAPI contract](api/openapi/openapi.yaml) or the
[shared CLI](cmd/guardrailctl/README.md) to connect your application, service,
script or agent. The examples below focus on the first production-delivery and
recovery scenarios.

| Integration | Current status |
| --- | --- |
| [Local Recovery Agent demo](examples/agent-demo/README.md) | Synthetic observations; agent-driven assessment and a deterministic self-check |
| [Jenkins](integrations/jenkins/README.md) | Synthetic pipeline demo with an outside agent and delivery-side evidence verification |
| [Argo Rollouts](integrations/argo/README.md) | Synthetic Analysis Job demo |
| [GitHub Actions](integrations/github-actions/README.md) | Planned adopter-facing workflow |
| [Argo CD](integrations/argo/README.md) | Sync hooks planned |

These examples demonstrate integration contracts. See the
[integration guide](integrations/README.md) for their scope and prerequisites.

## Documentation

Start with the [documentation index](docs/README.md) for reading paths, or read
[core concepts](docs/concepts.md) for the Scene, Policy, Run and Submission model.

| I want to… | Start here |
| --- | --- |
| Run a local example and inspect a decision | [Getting started](docs/getting-started.md) |
| Configure storage or a remote service | [Configuration and storage](docs/configuration.md), [Docker deployment](docs/docker.md) |
| Manage administrators and credentials | [Administrator guide](docs/local-administrator.md) |
| Author and publish a policy | [Scenes and Specs](docs/scenes-and-specs.md) |
| Create Runs and submit evidence | [Runs and submissions](docs/runs.md) |
| Call the API or automate with the CLI | [API overview](docs/api.md), [CLI guide](cmd/guardrailctl/README.md) |
| Understand the implementation or extension plans | [Architecture](docs/architecture.md), [extension boundaries](docs/extensions.md) |
| Build, contribute or prepare a release | [Development](docs/development.md), [release notes](docs/release-notes.md), [release verification](docs/releases.md) |

## Contributing and support

Bug reports, documentation improvements, integration feedback and focused code
changes are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for setup, issue
guidance and the pull request workflow.

Use [GitHub Issues](https://github.com/alibaba/open-guardrail/issues) for questions
and reproducible bug reports. Include your version and a minimal example with synthetic data.
Report vulnerabilities privately through [SECURITY.md](SECURITY.md). Community
participation follows our [Code of Conduct](CODE_OF_CONDUCT.md).

## License

OpenGuardrail is licensed under the [Apache License 2.0](LICENSE). Third-party
components retain their original terms; see the [notice inventory](third_party/README.md)
and [dependency review](docs/dependency-licenses.md).
