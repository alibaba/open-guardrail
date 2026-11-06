# GuardrailSpec document rules

This package implements the policy document schema, strict YAML validation,
canonical JSON encoding, SHA-256 digest and document size limit. Validation tests
live beside the implementation.

Drafts can remain incomplete. Publication validates the exact reviewed Draft or a
complete directly published document. Draft editing uses last-write-wins; it
does not have a Draft version counter. The publication transaction compares the
reviewed YAML with the stored Draft before creating a Revision.

The application prepares validated publication commands. Storage owns locking,
idempotency, immutable Revision creation, activation compare-and-swap and required
audit writes. This domain package has no Scene, HTTP, authentication, SQL or
platform-provider dependency. See
[Scenes and Specs](../../../docs/scenes-and-specs.md) for the public workflow.
