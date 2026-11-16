# Audit queries and Run diagnostic reports

Administrators can inspect project-scoped audit records in **Audit events** in
the console or through `guardrailctl audit list`. A Run's detail page also offers
**Export diagnostic report**. Reports are diagnostic records, not execution
permissions or proof that an external action took place.

## Audit query and export

Use an administrator CLI session created by `guardrailctl auth login`:

```sh
guardrailctl audit list --server http://127.0.0.1:8080 \
  --session-file ./session.json --resource-type policy-binding --limit 20

guardrailctl audit export --server http://127.0.0.1:8080 \
  --session-file ./session.json --resource-type policy-binding \
  --from 2026-11-01T00:00:00Z --to 2026-12-01T00:00:00Z \
  --output ./audit-page.json
```

`--event-type`, `--actor-id`, `--resource-type` and `--resource-id` are exact-match
filters. `--resource-id` means the identifier stored in the audit event, rather
than a resource binding's original reference. Binding events use the SHA-256 of
`sceneKey + "\n" + kind + "\n" + resourceId` as their stored resource ID. Their
administrator-only details include the Scene and before/after binding values.
The console time inputs use the browser's local timezone; CLI timestamps require
an explicit RFC3339 timezone. The start is inclusive, the end exclusive.

Each response contains `items`, optional `nextCursor`, and `redacted`. Results
are ordered by descending event ID, with a limit of 1–100 (default 20). Pass
`--cursor` to fetch the next page with the same filters. The cursor is bound to
the authenticated project and filters; it is not an authorization credential.
Newly inserted events do not shift older pages. Exports contain **one page**,
not the entire audit log. The console re-queries the selected page at export
time, so new events may appear on the first page. Use the returned cursor for
subsequent pages and keep each page in a separate file.

Export replaces actor and resource IDs with SHA-256 references and removes
stored details. Event IDs, types, resource types, times and available details
fingerprints remain. Hashes permit correlation; they do not anonymize values
with a small guessable domain. Administrator queries are not redacted and must
be handled as private data. Review retained identifiers before sharing exports.
Missing historical details remain absent. Malformed or oversized details are
omitted with `detailsUnavailable: true`; valid details are bounded to 4096 bytes in both the database reader and the application. Unscoped historic
records are not attributed to the current project and are excluded. This reader
only exposes events already recorded by the service; it does not add Run
execution receipts or guarantee an event for every read.

Audit endpoints require an administrator browser/CLI session. Project keys and
Run Submission Credentials cannot query audit records. Existing audit storage
is unchanged; this feature does not delete or retain data on a new schedule.

## Run report

```sh
guardrailctl run report --server http://127.0.0.1:8080 \
  --key-file ./project-key.json --output ./run-report.json run_REPLACE_ME
```

Place flags before the Run ID. The Project key needs `runs:read` and can only
read its own project. A Submission Credential cannot read reports. Administrators
can use the console or management HTTP endpoint instead.

`openguardrail.run-report.v1` contains:

- Run identifiers, frozen Revision metadata/digest, context digest, status,
  result, decision, deadlines and decision validity.
- Structured reasons such as `RUN_OPEN`, `RUN_EXPIRED`, `DECISION_EXPIRED`,
  `CHECK_BLOCKED` and `CHECK_UNKNOWN`. Expired decisions do not rewrite the
  historical result.
- At most 20 recent Submission summaries, bounded by the captured Run's latest
  submission number. `nextBefore` indicates older records available through the
  existing Submission list API. Concurrent later submissions require a refresh.
- The accepted final Submission, including check verdicts, evidence requirement
  IDs, observation times, snapshot hash and SHA-256 hashes of collection text,
  result text and optional references.

The report excludes policy YAML, ChangeSet contents, binding selection contents,
free-text summaries and raw evidence. It retains Scene/check/requirement IDs for
troubleshooting, so it is not an anonymized report. Review those identifiers
before sharing. OPEN/EXPIRED Runs without a final Submission have no final
evidence block. To investigate intermediate observations, use the authorized
Submission readers. Evidence hashes identify stored content; they do not prove
that the observation is true. Neither report generation nor an ALLOW decision
performs the business action.

Both export commands write to stdout unless `--output` is provided. File output
uses a new private `0600` file, refuses an existing file or symlink, and publishes
only after writing the complete JSON. Keep session/key files private and never
attach them to an issue.

## HTTP endpoints and runnable acceptance

| Endpoint | Authentication |
| --- | --- |
| `GET /api/v1alpha1/audit-events` | Administrator session |
| `GET /api/v1alpha1/runs/{runId}/report` | Administrator session |
| `GET /runtime/v1alpha1/runs/{runId}/report` | Project key with `runs:read` |

Audit HTTP `from`/`to` parameters are Unix seconds. `redacted=true` selects the
export representation. See [OpenAPI](../api/openapi/openapi.yaml) for schemas.

Run `make smoke-bindings` to launch an isolated real service, exercise binding
creation/update/deletion, paginate and export audit records, finish a denied Run,
and verify its report against stored evidence. The example removes its temporary
service and files afterward; it does not use production credentials or resources.

[简体中文](i18n/audit-and-reports.zh-CN.md)

The audit pagination index is introduced by schema migration 4 on SQLite,
PostgreSQL and MySQL. Automatic migration applies it at startup. Externally
managed databases must apply the bundled migration before starting this binary.
The index preserves existing events and supports descending project pagination.
Follow the [database migration configuration](configuration.md) for deployment
and backup requirements.

File publication requires hard-link support in the output directory. An existing
path is a usage error (exit 2); permissions or unsupported-filesystem errors are
reported as an export failure (exit 1). Export to stdout or select a suitable
filesystem when hard links are unavailable. An absent actor remains absent in
redacted records, without an actor fingerprint.
