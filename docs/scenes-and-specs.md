# Scenes, Policies and GuardrailSpecs

A Scene has a stable key, a configurable Run TTL and an optional default Policy.
A Scene can contain multiple Policies. Each Policy has at most one mutable Draft
Revision, immutable published Revisions and one active Revision. Revisions are identified across
services by `policyRevisionId`; the number is only for display. See
[core concepts](concepts.md) for the shared terminology.

## Initialize a Scene

Use the management page or an administrator CLI session. Run commands from the
checkout or archive root; see [Getting started](getting-started.md) for login and
credential setup.

```sh
guardrailctl scene apply --key recovery --name Recovery \
  --spec ./examples/policies/recovery.yaml --publish
```

The first application validates the YAML and atomically creates the Scene,
default Policy, Revision 1 and activation. A new Scene requires `--publish`.
A repeat with the same content reuses the active Revision. An existing Scene can
have its default Policy Draft saved without `--publish`.

Administrators can also call `POST /api/v1alpha1/scenes/{key}:initialize` with
`name`, `defaultPolicyName`, `yaml` and `Idempotency-Key`. The same key and request
return the original resources; a changed request or a separately initialized
Scene returns a conflict.

## Edit and publish

For the default Policy, the management page saves Draft YAML with last-write-wins
semantics. Publishing requires the exact YAML the caller reviewed. The application
first validates and canonicalizes that YAML before acquiring database write locks.
The storage transaction then compares the reviewed YAML with the current Draft
under the Policy lock, assigns the next number and marks that same Draft Revision
as `PUBLISHED` with its audit record. Repeated saves update one `DRAFT` row; drafts
have no revision number or publication metadata. After publication, saving again
creates a new Draft with a new business ID. Published content is immutable.

Invalid reviewed YAML fails validation with `422 SPEC_VALIDATION_FAILED` before
the Draft-state comparison. For a valid new publication, a Draft replaced by
another session returns `409 RESOURCE_STATE_CONFLICT` without creating or
activating a Revision. Reload and review the new content before publishing again.
An exact idempotent retry returns its original Revision as described below.

Activation remains a separate request. The management page offers publication and
activation in one flow, while the API keeps them independent. Direct publication
with `POST /api/v1alpha1/policies/{policyId}/revisions` is for a trusted producer
and does not modify the human Draft. It creates a published Revision directly.

Policy responses contain metadata and the active Revision pointer. Read the
editable Revision with `GET /api/v1alpha1/policies/{policyId}/draft`; no draft
returns `404 NOT_FOUND`. Saving returns that `DRAFT` Revision, including its
identity, YAML and edit metadata. Publication-only fields are absent until it is
published. Policy responses do not contain rule content or draft fields.
Revision history and Run selection include only published Revisions; a direct
management lookup by Revision ID can read either state. See the
[persistence model](adr/0002-persistence-model.md) for the schema.

- `GET /api/v1alpha1/policies/{policyId}/draft`: read the current Draft Revision.
- `PUT /api/v1alpha1/policies/{policyId}/draft`: save YAML, including an incomplete Draft, and return the Draft Revision.
- `POST /api/v1alpha1/policies/{policyId}/draft:publish`: send `{"yaml":"<reviewed Draft YAML>"}` with `Idempotency-Key` to validate and publish that exact Draft. An exact retry returns its original Revision even if the Draft has since changed; reusing the key with different YAML conflicts.
- `POST /api/v1alpha1/policies/{policyId}/revisions`: publish complete YAML directly with `Idempotency-Key`.
- `PUT /api/v1alpha1/policies/{policyId}/active-revision`: activate a Revision using `expectedActiveRevisionId` for compare-and-swap.
- `PUT /api/v1alpha1/scenes/{key}/default-policy`: select an active Policy with `expectedDefaultPolicyId`.
- `PATCH /api/v1alpha1/scenes/{key}`: change `runTtlSeconds` for future Runs only.

A Policy can be archived and restored, but the default Policy must be changed
before archiving it. Published Revisions and existing Runs remain intact.
Archived Policies cannot be edited, published, activated or used to create Runs.
An existing Draft remains readable while its Policy is archived. Archiving does
not delete or publish the Draft; restoring the Policy allows editing and
publication of that same Draft again.

GuardrailSpec YAML uses `apiVersion: guardrail.open/v1alpha1` and
`kind: GuardrailSpec`. The parser rejects duplicate keys, anchors, aliases, unknown
fields and invalid constraints. The file limit is 64 KiB.
`spec.validForSeconds` controls the final Decision's validity; the Scene's
`runTtlSeconds` controls the Run deadline. See the
[OpenAPI contract](../api/openapi/openapi.yaml) and
[Run lifecycle](runs.md#run-state-and-decision-validity).

## Read published history

`GET /api/v1alpha1/policies/{policyId}/revisions?pageSize=20` returns
`{"items": [...], "nextBefore": 123}`. Items contain revision identity, number,
digest and publication metadata, without YAML. The default page size is 20 and
the maximum is 100. For the next page, pass `before=<nextBefore>`; this exclusive
revision-number cursor keeps older pages stable when new revisions are published.
When `nextBefore` is absent, there are no more older items. The console offers
newer/older page controls and fetches YAML only for the selected version using
`GET /api/v1alpha1/policy-revisions/{revisionId}`.

## Validity range and canonical identity

`spec.validForSeconds` accepts a base-10 integer from 1 through 9007199254740991
(`2^53 - 1`), inclusive. Omitting the field still supplies 3600 before computing
the canonical JSON and SHA-256 digest. This is an interoperability bound, not a
configurable business-duration cap: existing values such as 7200 remain valid.

This fixed schema has only one numeric field. Restricting it to IEEE 754 safe
integers preserves the same exact number and decimal JSON spelling in Go and
ECMAScript implementations of RFC 8785. Larger integers are rejected rather than
rounded; strings, fractions, exponents and non-decimal YAML spellings are rejected
as well. Canonical bytes and digests for already supported in-range Specs do not
change. The same bound applies when resolving validity from a stored canonical
Spec. An existing out-of-range Revision is not rewritten; publish a corrected
Revision before creating new Runs from it.

Reference: [RFC 8785 Sections 3.1 and 3.2.2.3](https://www.rfc-editor.org/rfc/rfc8785.html#section-3.1).
