# ADR 0002: Numeric storage keys and revision-owned drafts

Status: accepted.

## Identifiers and relationships

All model tables use generated integer `id` primary keys. The singleton
`identity_state.id` remains fixed at 1. SQLite uses `INTEGER ... AUTOINCREMENT`;
PostgreSQL uses `BIGINT ... AS IDENTITY`. Goose records the initialization version.

Public identifiers remain stable unique business columns:
`workspace.workspace_id`, `project.project_id`, `local_user.user_id`,
`project_api_key.api_key_id`, `policy.policy_id`,
`policy_revision.policy_revision_id`, `guardrail_run.run_id` and
`submission.submission_id`. HTTP and CLI identifiers remain strings. SQL
repositories resolve those identifiers at the storage boundary and project them
back into responses; numeric keys are never substituted into public IDs.

Record relationships use integer keys. Scope columns reference Workspace and
Project integer keys. Scene keys, check IDs and requirement IDs remain natural
identifiers. Polymorphic audit/idempotency `resource_id` values remain public
business identifiers. Credential hashes stay unique, and no extra business ID is
introduced for Scene, Evidence, Session, Audit or Run credentials.

Draft-save audit events identify the edited Revision: `resource_type=revision`
and `resource_id` is its string business ID. The audit row's integer `id` is a
separate storage key.

## Policy and Revision

Policy stores identity, ownership, name, lifecycle and the active Revision pointer.
Rule content belongs to `policy_revision`:

| Field | DRAFT | PUBLISHED |
|---|---|---|
| `revision_number` | NULL | Positive, increasing per Policy |
| `yaml` | Editable; may be incomplete | Validated and immutable |
| `canonical_json`, `digest` | NULL | Computed at publication, immutable |
| `updated_by`, `updated_at` | Updated on save | Final publication actor/time |
| `published_by`, `published_at` | NULL | Publication actor/time |

A partial unique index permits at most one DRAFT per Policy. Saving edits that
same row. Publication locks the parent, checks the exact reviewed YAML, assigns
one number and changes the row to PUBLISHED without changing its identity.
The next edit creates a new DRAFT. Published rows cannot be updated or deleted.
Only published Revisions may be active or selected by a Run.

Trusted direct publication creates a PUBLISHED row without modifying an existing
human draft. Publication and activation remain separate operations. Policy
responses expose metadata only. Draft read/save endpoints return a Revision in
state DRAFT; published history excludes drafts. Publication metadata
is absent from Draft responses and required when publishing.

## Run finalization

`guardrail_run` owns `result_verdict`, `decision_verdict` and
`decision_expires_at`. There are no separate `run_result` or `decision` tables.
All three fields are NULL while OPEN and required in terminal states. Normal
finalization stores the final Submission pointer and result atomically; expiry
stores UNKNOWN/DENY. Decision validity is computed from the frozen
`decision_ttl_seconds` and `finished_at` or `expired_at`. Terminal Runs remain
immutable. Submission and Evidence retain immutable history, while the separate
Run credential may be rotated only while the Run is OPEN and before its deadline.

The idempotency table records committed resource mappings in the same transaction
as the resource. Its redundant single-value `state` column is removed. Run and
Submission idempotency remains enforced in their own tables.

## Initialization

Each supported database has one `00001_initial.sql` that creates the final 15
model tables, including the singleton identity state. Goose records initialization
version 1. There is no historical schema conversion or data backfill.

With automatic initialization the service installs this schema before opening
its listener. External mode requires the same schema and version metadata to be
prepared in advance. Neither mode downgrades an unexpected schema.
