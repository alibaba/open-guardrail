# Creating, submitting and querying Runs

A Run freezes its Scene, Policy Revision ID, Spec digest, Run TTL and the
caller-defined JSON context in `changeSet`. See [core concepts](concepts.md) and
[Getting started](getting-started.md) for a complete example.

## Create and query a Run

A machine key needs `runs:create` to create a Run, `runs:read` to list or read it,
and `submission-credential:rotate` to replace its Submission Credential. Create a
Project key after [administrator login](local-administrator.md#project-machine-keys).
Run the examples from the checkout or archive root with `guardrailctl` on `PATH`.

Set `guardrail_credentials_dir` to the private directory containing your
`orchestrator-key.json`, outside the checkout or extracted archive. If you used
the administrator guide in the same terminal, the variable is already set.
Otherwise, replace this placeholder with the private directory path:

```sh
guardrail_credentials_dir=/absolute/path/to/private/guardrail-credentials
```

New Run credentials stay in this directory. See Getting started for example
`change-set.json` and `submission.json` inputs.

The Scene default Policy's active Revision is selected at creation. A trusted HTTP
caller with `runs:create-with-revision` can explicitly select an immutable Revision
through `policyRevisionRef`; ordinary orchestrator keys cannot. The current CLI
has no explicit-Revision flag. Machine callers can inspect their effective scopes
through `GET /runtime/v1alpha1/identity` before enabling an integration.

```sh
guardrailctl run create --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --scene recovery --change-set ./change-set.json \
  --operation-id create-123 \
  --submission-credential-file "$guardrail_credentials_dir/run-credential.json"
guardrailctl run get --key-file "$guardrail_credentials_dir/orchestrator-key.json" run_EXAMPLE
guardrailctl run list --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --scene recovery --status FINISHED --limit 20
```

Run lists contain bounded summaries with `changeSetDigest`, not the potentially
large `changeSet`. Use `run get` or `GET /runtime/v1alpha1/runs/{runId}` to read
the full immutable ChangeSet for one Run.

## Run retry and credential recovery

`POST /runtime/v1alpha1/runs` returns a Run and a plaintext Submission Credential
only on first creation. The CLI writes the credential to a new private file.
An idempotent replay returns the Run without reissuing the credential.

Keep the same operation ID and input when retrying an uncertain creation or
Submission. Inspect the returned Run and any saved credential before retrying.
If the credential was not saved, rotate it while the Run is `OPEN`. Rotation
invalidates the old credential and does not extend the deadline:

```sh
guardrailctl run rotate-credential --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --submission-credential-file "$guardrail_credentials_dir/replacement-run-credential.json" run_EXAMPLE
```

The API path is `POST /runtime/v1alpha1/runs/{runId}/submission-credential:rotate`.
Only the matching Run credential can submit; the CLI never overwrites an existing
credential output file.

Rotation and Submission share the Run lock, and Submission rechecks its credential
under that lock. A final Submission prevents rotation. After rotation succeeds,
the old credential cannot create or replay a Submission; exact retries remain
available with the current credential after the Run is terminal.

The server checks time after acquiring the Run lock, so waiting for a connection
or lock cannot extend the deadline. After the deadline, a new Submission or
rotation expires the Run and returns `RUN_EXPIRED`; exact retries of accepted
Submissions still return their original records.

## Complete Snapshot Submissions

Each Submission contains all Check verdicts and applicable structured evidence.
It is a complete Snapshot, including for an intermediate submission.
`baseSubmissionNo` must match the Run's latest number. The server verifies the
aggregate verdict: `BLOCKED` overrides `UNKNOWN`, and `UNKNOWN` overrides `PASS`.

The top-level `summary` field is required and may be `""`. Nonempty summaries
must contain non-whitespace text and stay within 2048 characters. Check summaries
and Evidence query/result summaries must contain non-whitespace text.

The JSON below is an illustrative shape. Use the actual observation time and
findings, and include every Check required by your Run's frozen Spec.

```json
{
  "baseSubmissionNo": 0,
  "finishRun": true,
  "verdict": "PASS",
  "summary": "Recovery checks passed",
  "checks": [{
    "checkId": "readiness",
    "verdict": "PASS",
    "summary": "Service is healthy",
    "evidence": [{
      "requirementId": "health",
      "observedAt": "2026-10-26T00:00:00Z",
      "querySummary": "Read current health metrics",
      "resultSummary": "All replicas ready"
    }]
  }]
}
```

Save this as `submission.json`, then run:

```sh
guardrailctl run submit --submission-credential-file "$guardrail_credentials_dir/run-credential.json" \
  --submission ./submission.json --operation-id submit-123 run_EXAMPLE
```

`finishRun: false` stores an intermediate Snapshot, increments
`latestSubmissionNo`, and leaves the Run `OPEN`. `finishRun: true` stores the
final Snapshot and atomically creates one Result and Decision, moving the Run to
`FINISHED`. An exact retry with the same idempotency key returns the original
Submission; a changed request conflicts.

## Run state and decision validity

| Constraint | Frozen source | Behavior |
| --- | --- | --- |
| `deadlineAt` | Scene `runTtlSeconds` at Run creation | Bounds the time for accepting a new Submission; intermediate Snapshots and rotation do not extend it |
| `decisionExpiresAt` | Selected Spec `spec.validForSeconds` | Bounds the lifetime of a terminal Decision; expiry does not rewrite historical Result or Run state |

An unfinished Run observed at its deadline becomes `EXPIRED` with `UNKNOWN / DENY`.
A `FINISHED` Run stays `FINISHED`. Expiry and final Submission use the same Run
lock, so only one terminal outcome can win. An expired Run's Decision starts at
its logical deadline, even if the lazy expiry is persisted later.

`PASS` produces `ALLOW`; `BLOCKED` and `UNKNOWN` produce `DENY`. The calling
system must check the authoritative Run, decision validity, evaluation identity
and accepted evidence before acting. Execution Claims and ActionResults remain
planned; the server does not perform a deployment or independently attest to
submitted observations.

## Read the immutable inputs and accepted evidence

Project orchestrators can read a Run's Revision and Submission without Policy
management permissions:

```sh
guardrailctl run revision --key-file "$guardrail_credentials_dir/orchestrator-key.json" "$RUN_ID"
guardrailctl run submission --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --submission-id "$SUBMISSION_ID" "$RUN_ID"
```

These map to `GET /runtime/v1alpha1/runs/{runId}/policy-revision` and
`GET /runtime/v1alpha1/runs/{runId}/submissions/{submissionId}`. The latter returns
Submission metadata plus its persisted `snapshot`. Both readers check scope and
Run membership; a Run Submission Credential cannot use them. For a finished Run,
use its `finalSubmissionId` as `SUBMISSION_ID`. Run metadata does not include the
full evidence Snapshot.

The management page shows the latest accepted Submission and its evidence by default.
An administrator can select earlier submissions from paginated metadata at
`GET /api/v1alpha1/runs/{runId}/submissions?limit=20`, then read one snapshot at
`GET /api/v1alpha1/runs/{runId}/submissions/{submissionId}`. Use `nextBefore` as
the next page's `before` parameter. Selecting history does not change the Run's
final result or decision. Project-key runtime readers remain unchanged.

## Evidence limits

Evidence is limited to 16 KiB per entry, 100 entries per Submission, and 1 MiB per
request. Text limits are counted in Unicode code points. Credential-shaped content
is rejected, including known credential fields in JSON summaries, nested objects,
arrays and JSON-encoded strings. Duplicate JSON keys and excessive JSON structure
are rejected too.

Screening preserves accepted text and its retry identity. Callers must still
remove secrets before submission because free-form text screening is not a
general data-loss prevention system. The management UI lists and inspects Runs
without exposing credentials or accepting Submissions. See the
[OpenAPI contract](../api/openapi/openapi.yaml) for fields and error codes.
