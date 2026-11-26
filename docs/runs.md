# Creating, submitting and querying Runs

The console, CLI and management API use direct access. Only assessment submission requires a Run credential. See [Access](access.md).

A Run freezes its Scene, Policy Revision ID, Spec digest, Run TTL and the
caller-defined JSON context in `changeSet`. See [core concepts](concepts.md) and
[Getting started](getting-started.md) for a complete example.

## Create and query a Run

```sh
guardrail_credentials_dir=/absolute/path/to/private/guardrail-credentials
```

New Run credentials stay in this directory. See Getting started for example
`change-set.json` and `submission.json` inputs.

Resource bindings select the Policy/Revision at creation; entirely unbound resources
use the Scene default active Revision. See [bindings](extensions.md#resource-bindings).
An HTTP caller may explicitly select an immutable Revision through
`policyRevisionRef`. The current CLI has no explicit-Revision flag.

```sh
guardrailctl run create \
  --scene recovery --change-set ./change-set.json \
  --operation-id create-123 \
  --submission-credential-file "$guardrail_credentials_dir/run-credential.json"
guardrailctl run get run_EXAMPLE
guardrailctl run wait run_EXAMPLE
guardrailctl run list \
  --scene recovery --status FINISHED --limit 20
```

Run lists contain bounded summaries with `changeSetDigest`, not the potentially
large `changeSet`. Use `run get` or `GET /runtime/v1alpha1/runs/{runId}` to read
the full immutable ChangeSet for one Run.

`run wait` polls `GET /runtime/v1alpha1/runs/{runId}` until the Run is terminal and
prints the final document. It exits 0 only for `FINISHED`; an `EXPIRED` Run exits
nonzero because no final Submission arrived before the deadline, and a timeout is
also a nonzero exit.
Transient transport, `5xx` and `429` failures are retried until the deadline, while
authentication and other client errors fail immediately. Until the Run deadline is
known, connection retries are capped at 30 seconds. A `429` `Retry-After` value
longer than `--interval` is honored within the remaining wait. Waiting does not
replace the calling system's verification of the Decision, its validity and its
evidence.

## Run retry and credential recovery

`POST /runtime/v1alpha1/runs` returns a Run and a plaintext Submission Credential
only on first creation. The CLI writes the credential to a new private file.
An idempotent replay returns the Run without reissuing the credential.

Keep the same operation ID and input when retrying an uncertain creation or
Submission. Inspect the returned Run and any saved credential before retrying.
If the credential was not saved, rotate it while the Run is `OPEN`. Rotation
invalidates the old credential and does not extend the deadline:

```sh
guardrailctl run rotate-credential \
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

The top-level `summary` field is required and may be `""`. Nonempty top-level
and Check summaries must contain non-whitespace text and stay within 8192 UTF-16
code units. Evidence query/result summaries must contain non-whitespace text and
stay within 4096 UTF-16 code units each; an Evidence entry is also limited to
16 KiB after JSON serialization. Optional Evidence references remain limited to
2048 Unicode code points.

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
guardrailctl run revision "$RUN_ID"
guardrailctl run submission \
  --submission-id "$SUBMISSION_ID" "$RUN_ID"
```

These map to `GET /runtime/v1alpha1/runs/{runId}/policy-revision` and
`GET /runtime/v1alpha1/runs/{runId}/submissions/{submissionId}`. The latter returns
Submission metadata plus its persisted `snapshot`. Both readers check scope and
Run membership; a Run Submission Credential cannot use them. For a finished Run,
use its `finalSubmissionId` as `SUBMISSION_ID`. Run metadata does not include the
full evidence Snapshot.

## Evidence limits

Evidence is limited to 16 KiB per entry, 100 entries per Submission, and 1 MiB per
request. Summary limits are counted in UTF-16 code units; identifier and reference
limits are counted in Unicode code points. Credential-shaped content
is rejected, including known credential fields in JSON summaries, nested objects,
arrays and JSON-encoded strings. Duplicate JSON keys and excessive JSON structure
are rejected too.

Screening preserves accepted text and its retry identity. Callers must still
remove secrets before submission because free-form text screening is not a
general data-loss prevention system. The management UI lists and inspects Runs
without exposing credentials or accepting Submissions. See the
[OpenAPI contract](../api/openapi/openapi.yaml) for fields and error codes.

Use `guardrailctl run create|list --business-id ID` or the console Runs filter to find a business attempt. See the [release-order correlation example](../examples/business-id/README.md) for replay, credential and terminal-state handling.
