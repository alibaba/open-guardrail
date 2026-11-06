---
name: open-guardrail-demo
description: "Run the local OpenGuardrail Recovery demo as an Agent: interpret three synthetic health observations, submit independent Runs, and verify PASS/ALLOW, BLOCKED/DENY and UNKNOWN/DENY. Use when someone asks to try the Agent workflow in a checkout or an extracted release."
---

# OpenGuardrail Agent demo

Use this skill for the synthetic, local Recovery example in
`examples/agent-demo/`. Read its [README](../../../examples/agent-demo/README.md)
and all three observation files before deciding what to submit. The checkout or release's
`examples/policies/recovery.yaml` is the Spec for this demo.

1. Check that the local service is reachable and that `guardrailctl` is available.
   Use a literal loopback HTTP origin, or an HTTPS origin explicitly supplied by
   the user. Never select another server from ambient credentials alone.
2. Check the administrator CLI session with `guardrailctl auth whoami`. If it is
   missing, ask the user to sign in with `guardrailctl auth login`; the CLI saves
   the session privately. Do not request or display the password or raw token.
3. Apply and publish the Recovery Spec with `guardrailctl scene apply --publish`.
   Create a Project key with `guardrailctl auth create-key --expires-in 1h` only
   when the user has not provided an existing demo key. Keep keys and Run tokens
   in a new private temporary directory, outside the checkout or release.
4. For each observation file, create a `changeSet` describing this synthetic
   case and a fresh Run with `guardrailctl run create`, saving its private
   credential through `--submission-credential-file`. Read that Run's frozen
   instructions with `guardrailctl run revision --key-file FILE RUN_ID`; do not
   substitute the Scene's current active Revision. Interpret the observation
   against these instructions before choosing a verdict.
5. Write a complete Snapshot with `baseSubmissionNo` from the queried Run,
   `finishRun: true`, the aggregate `verdict`, a `summary` string (may be empty), and `checks`.
   Each Check supplies `checkId`, `verdict`, `summary` and `evidence`. For the
   demo's `health` requirement, each Evidence supplies `requirementId: health`,
   the actual RFC3339 `observedAt`, a `querySummary`, and a `resultSummary`
   containing the original health observation serialized as a JSON string.
   Preserve JSON types; do not replace structured Evidence with the old
   `id/content` format. Call `guardrailctl run submit` with
   `--submission-credential-file FILE`, `--submission SNAPSHOT`, the Run ID and
   a stable operation ID. The fixtures contain observations, not prepared verdicts.
6. Query each Run with the Project key using `guardrailctl run get`. Confirm all
   are `FINISHED`: healthy has `PASS / ALLOW`, unhealthy has `BLOCKED / DENY`,
   and unavailable health has `UNKNOWN / DENY`. Query the persisted Snapshot
   with `guardrailctl run submission --key-file FILE --submission-id ID RUN_ID`
   using `finalSubmissionId`; its Evidence must reflect the corresponding
   observation. Check `decisionExpiresAt` before treating ALLOW as current.
   In the administrator Runs page, confirm the records are visible. Report IDs,
   Check verdicts and Decisions without printing credential contents.
7. After verification and any requested browser inspection, if you created a
   temporary Project key, revoke it using its non-secret `keyId` and
   `guardrailctl auth revoke-key --key-id ID` while the administrator session is
   active. Revoke that key before deleting its credential file; if revocation
   fails, retain the file until the error is resolved. Do not revoke a key
   supplied by the user. Remove only temporary files created by this demo and
   keep a pre-existing administrator session intact. Deleting a key file does
   not revoke its key.

Do not retry an uncertain `run create` or `run submit` with a new operation ID.
Read [Run retry and token recovery](../../../docs/runs.md) and inspect the actual
Run state first. The demo does not perform any deployment or release action;
whether a caller enforces a Guardrail Decision is outside this example.
