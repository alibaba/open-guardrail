# Contributing to OpenGuardrail

Thank you for helping improve OpenGuardrail. Bug reports, documentation fixes,
policy examples, integration feedback and code changes are welcome. You do not
need to begin with a code contribution.

Participation follows our [Code of Conduct](CODE_OF_CONDUCT.md). Report suspected
vulnerabilities privately using [SECURITY.md](SECURITY.md), rather than a public
issue or pull request.

## Ask a question or report a problem

Use [GitHub Issues](https://github.com/alibaba/open-guardrail/issues). For a bug
report, include:

- What you expected and what happened.
- The server/CLI version, operating system and storage configuration.
- Minimal reproduction steps using synthetic data.
- A stable error code or trace ID, when available.

Remove credentials, session files, private hostnames, personal data, database
contents and production records. Review diagnostic output before attaching it.

For a feature proposal, describe the use case and problem first, including any
existing workaround. Discuss changes to public APIs, policy semantics, storage
contracts or extension boundaries before starting a large implementation.

## Make your first change

1. Try [Getting started](docs/getting-started.md) and choose a focused problem.
2. Create a branch from the default branch. Fork
   [alibaba/open-guardrail](https://github.com/alibaba/open-guardrail) first if you
   do not have write access.
3. Make the smallest change that addresses the problem. Update affected examples
   and documentation alongside behavior changes.
4. Run the checks appropriate to the change and describe what you verified.
5. Open a [pull request](https://github.com/alibaba/open-guardrail/pulls) with the
   problem, resulting behavior and verification.
   Link a related issue when one exists.

Documentation improvements and reproduction cases are useful contributions.
For a documentation-only change, check relative links, command examples and
current capability claims. Do not report a runtime check as passed unless you
ran it.

## Development setup and checks

Use the toolchains specified in the [development guide](docs/development.md).
The full checks require the Go version from `go.mod`, Node.js 22+ and Python 3.12+.

```sh
make test
make build
make smoke
```

If you change the HTTP contract, edit `api/openapi/openapi.yaml` first and run
`make generate`. Commit the generated code; do not edit
`api/openapi/generated.gen.go` by hand.

PostgreSQL changes also need the backend suite against a disposable empty test
database. The [development guide](docs/development.md) lists separate test targets
and database precautions. Include any skipped checks and the reason in the pull
request.

## Implementation conventions

- Use English identifiers, comments and public documentation.
- Follow the [package and extension boundaries](docs/extensions.md#dependency-boundaries).
  Keep domain rules independent of HTTP, storage and platform SDKs; keep use-case
  ports with their consumers.
- Connect behavior changes to focused executable tests.
- Keep public extension signatures usable from an external Go module. Do not
  expose internal services, repository types or private SDKs through them.
- Introduce provider APIs with default implementations and an external-module
  example. Keep planned APIs explicitly marked until they work.
- Add forward migrations for schema changes; do not edit shipped migrations.
  Keep official drivers' migration versions and observable behavior aligned.
- An official storage driver must pass lifecycle, identity and business
  conformance checks; a connection-only implementation is insufficient.
- Use synthetic fixtures and reserved example domains. Never commit credentials,
  database files, private endpoints or logs.

## Documentation and diagrams

Write maintained documentation, diagram labels, captions and accessibility text
in English. Use the [documentation index](docs/README.md) to choose an existing
guide before adding another page, and keep terminology aligned with
[core concepts](docs/concepts.md).

The README presents the product and target architecture. Put the implemented
evaluation-flow diagram and runnable first-use steps in Getting started. Keep
current behavior, synthetic demos and planned APIs explicitly distinguished.
Examples should run from the checkout or archive root; document prerequisites,
credential roles, placeholders and expected outcomes.

Keep editable diagram sources under `docs/images/`. When a diagram has an SVG
source and PNG preview, update both together, use English for the SVG title and
description, and link the preview to the full-size SVG. Check labels at README
width and on a narrow screen. Preserve original third-party legal notices in
their upstream form.

Before submitting documentation changes, check local links and heading anchors,
image references, fenced examples and capability claims against the current
OpenAPI contract and implementation. Record the checks actually performed.

Update [release notes](docs/release-notes.md) for
user-visible capabilities, compatibility changes, migration requirements and
security-relevant behavior. Keep unpublished work distinct from a dated release.

## Pull request review

Keep pull requests focused enough to review independently. Explain what changes
for users, include a small before/after example when useful, and list the checks
actually performed. Maintainers may ask for changes to preserve API semantics,
cross-driver behavior or the documented preview boundaries.

## Contribution terms

Before a contribution can be merged, contributors must sign the applicable
Alibaba Open Source Contributor License Agreement (CLA). The
[Alibaba CLA](https://gist.github.com/alibaba-oss/65c6f733da03f449603d)
includes individual and corporate agreements; review the applicable agreement
before signing. Follow the CLA assistant link in the pull request to sign; if no
prompt appears, contact the maintainers.

OpenGuardrail uses the [Apache License 2.0](LICENSE). Contributions are submitted
under the project's Apache-2.0 terms unless explicitly agreed otherwise.
Third-party components retain their original terms; see the
[notice inventory](third_party/README.md).
