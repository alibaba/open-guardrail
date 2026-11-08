# GuardrailSpec YAML reference

[简体中文](i18n/guardrailspec-reference.zh-CN.md) | English

A GuardrailSpec describes the checks and evidence required for one kind of
operation. It is a YAML policy document, not an executable rule language. An
external Agent or integration observes the real system, submits a verdict and
evidence for every Check, and the OpenGuardrail service validates the submission
against the frozen Spec. The service does not independently attest that an
observation is true. The execution tool must verify the authoritative decision,
accepted evidence and current operation before acting. See [core
concepts](concepts.md) and [Runs and submissions](runs.md).

This reference describes `guardrail.open/v1alpha1`. The [OpenAPI
contract](../api/openapi/openapi.yaml) and [Spec validator](../internal/domain/spec/document.go)
define the accepted fields and constraints for this checkout.

## A complete example

This example is valid as written and can be copied into `file-cleanup.yaml`:

```yaml
apiVersion: guardrail.open/v1alpha1
kind: GuardrailSpec
metadata:
  name: file-cleanup
spec:
  description: Check a proposed cleanup of temporary files
  instructions: Inspect the exact files and submit current observations for every check.
  validForSeconds: 60
  checks:
    - id: temporary-only
      displayName: Temporary files only
      description: Protect business files from deletion
      instructions: Confirm that every target is an existing temporary file in the intended workspace.
      criteria:
        pass: Every target is an existing temporary file in the intended workspace
        block: At least one target is a protected business file
        unknown: Target identity or current file state cannot be verified
      evidenceRequirements:
        - id: files
          description: Workspace identity, exact targets and observed file metadata
          appliesToVerdicts: [PASS, BLOCKED, UNKNOWN]
```

The policy text alone does not inspect files or delete them. The [file cleanup
demo](../examples/cleanup-demo/README.md) supplies those integrations and a
two-Check Spec. The smaller [Recovery Spec](../examples/policies/recovery.yaml)
is another complete example.

## Fields

All fields below are case-sensitive. Fields marked required must be present.
Text fields must contain a non-whitespace character; lengths count Unicode
characters. Unknown fields are rejected.

| Path | Required | Meaning and constraints |
| --- | --- | --- |
| `apiVersion` | Yes | Exactly `guardrail.open/v1alpha1`. |
| `kind` | Yes | Exactly `GuardrailSpec`. |
| `metadata.name` | Yes | Spec identifier. Use 1–63 lowercase letters, digits or hyphens; start with a letter and end with a letter or digit. |
| `spec.description` | Yes | Purpose of the Spec; at most 1,024 characters. |
| `spec.instructions` | Yes | Instructions for the evaluator; at most 8,000 characters. |
| `spec.validForSeconds` | No | Lifetime in seconds of a terminal Decision. It starts when the Run finishes (`FINISHED`) or at the logical deadline if the Run expires (`EXPIRED`). Positive base-10 integer from `1` to `9007199254740991`; default `3600`. Write it without quotes or leading zeros. |
| `spec.checks` | Yes | Array of 1–20 Checks. Check IDs must be unique within the Spec. |
| `spec.checks[].id` | Yes | Check identifier, following the same format as `metadata.name`. |
| `spec.checks[].displayName` | Yes | Human-readable Check name; at most 128 characters. |
| `spec.checks[].description` | Yes | What the Check covers; at most 1,024 characters. |
| `spec.checks[].instructions` | Yes | How the evaluator should investigate this Check; at most 8,000 characters. |
| `spec.checks[].criteria.pass` | Yes | When this Check can report `PASS`; at most 2,048 characters. |
| `spec.checks[].criteria.block` | Yes | When this Check must report `BLOCKED`; at most 2,048 characters. |
| `spec.checks[].criteria.unknown` | Yes | When this Check cannot be verified and must report `UNKNOWN`; at most 2,048 characters. |
| `spec.checks[].evidenceRequirements` | Yes | Array of 1–20 requirements. Requirement IDs must be unique within their Check. |
| `spec.checks[].evidenceRequirements[].id` | Yes | Requirement identifier, following the same format as `metadata.name`. |
| `spec.checks[].evidenceRequirements[].description` | Yes | What observation this requirement expects; at most 1,024 characters. |
| `spec.checks[].evidenceRequirements[].appliesToVerdicts` | Yes | Nonempty, unique subset of `[PASS, BLOCKED, UNKNOWN]`. Evidence for this requirement is accepted only for the listed Check verdicts and required when one of those verdicts is submitted. |

The identifier pattern is `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`.
`criteria` describes how an evaluator should classify observations; it is not
code evaluated by the service. A final Submission includes every Check and its
applicable evidence. The service aggregates Check verdicts: all `PASS` means
`PASS / ALLOW`; any `BLOCKED` means `BLOCKED / DENY`; otherwise an `UNKNOWN`
means `UNKNOWN / DENY`. See [Run evaluation](runs.md) for the submission format.

## YAML and Scene boundaries

- The file must contain one UTF-8 YAML document of at most 64 KiB, 4,096 nodes
  and nesting depth 16. Duplicate keys, anchors, aliases, custom tags, unknown
  fields and implicit scalar coercions are rejected. Except for
  `spec.validForSeconds`, scalar values must be YAML strings. Quote values that
  YAML could interpret as a boolean or number.
- `sceneKey`, `runTtlSeconds`, Project keys and the operation's `changeSet` are
  **not** GuardrailSpec fields. A Scene has its own stable key and Run TTL;
  `spec.validForSeconds` instead controls how long a terminal Decision remains
  valid. The caller supplies `changeSet` when creating each Run.
- A published Revision is immutable. Publication validates and canonicalizes
  the YAML, assigning a content digest. A Run freezes the selected Revision and
  digest. Comments and formatting are not policy checks.

## Publish and use it

With a running service and an administrator CLI session, publish the file from
the checkout root:

```sh
guardrailctl scene apply --key file-cleanup --name 'File cleanup' \
  --spec ./file-cleanup.yaml --publish
```

The first application creates the Scene, its default Policy and an active
Revision. Repeating an unchanged publication reuses that Revision. Publishing
changes service state; use an isolated service while experimenting. An
orchestrator Project key can create and read Runs but cannot publish the Spec.
See [Scenes and Specs](scenes-and-specs.md) for Draft review, activation and
credentials, and [Getting started](getting-started.md) for a complete local flow.

## Common mistakes

| Input | Why it fails or misleads | Correct approach |
| --- | --- | --- |
| `sceneKey` or `runTtlSeconds` inside the YAML | They are Scene settings, not Spec fields. | Select the Scene with `scene apply --key`; configure its Run TTL separately. |
| `requirement: SQL must have WHERE` as a Check field | `requirement` is not in the schema. | Supply `description`, `instructions`, all three `criteria` values and `evidenceRequirements`. |
| `criteria` says `PASS` without evidence | Criteria text does not run checks or prove observations. | Collect evidence in an external integration and submit it for the applicable verdict. |
| `validForSeconds: "300"` or `validForSeconds: 0300` | The only numeric field must be an unquoted positive base-10 integer without leading zeros. | Use `validForSeconds: 300`. |
| Treating `ALLOW` as permission to execute any later action | Decisions are tied to a Run, frozen context and validity window. | Re-read the authoritative Run and accepted Submission; verify identity, evidence and current state at the execution boundary. |
