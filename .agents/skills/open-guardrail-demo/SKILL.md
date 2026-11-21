---
name: open-guardrail-demo
description: "Inspect a paused first-batch Run using metrics, logs and alerts, or run the three original synthetic Recovery observations when requested."
---

# OpenGuardrail Agent demo

## First-batch recovery

When asked to reopen a completed trial, open the saved `recordPath` from the
conversation, or look up `data/first-batch-reports/<runId>.json` in this checkout.
It contains the service report and the accepted synthetic observations.

If the user supplies a Run directory, reuse it and leave their launcher running.
Otherwise, from this checkout start `python3 examples/agent-demo/first_batch.py start`
in a persistent terminal session you control. Keep it alive while you investigate.
When the user asks to try healthy data or asks what happens if logs are unavailable,
start a fresh Run with `--case clean` or `--case logs-unavailable`, respectively.
Use the new Run directory for all subsequent queries and the assessment.
Wait for the Run ID and take the exact Run directory from the printed instruction.
Set `WORK` to that directory. Show the scope and findings of each query in chat.
Only add `--console` if the user asks to inspect the Run in a browser.

1. Read the frozen Spec and Run scope:
   `python3 examples/agent-demo/first_batch.py spec --work-dir "$WORK"`.
2. Query each required source separately:
   `python3 examples/agent-demo/first_batch.py query --work-dir "$WORK" metrics`.
   Repeat for `logs` and `alerts`. Inspect the returned values. An unavailable
   source differs from a completed query with empty results.
3. When a write error warrants investigation, query `changes` and then
   `records --record-id ID` with the ID from that batch's log. Connect the
   changed handler and observed record state before judging business impact.
4. Decide using the frozen criteria. Submit your own verdict and a short
   explanation with `python3 examples/agent-demo/first_batch.py submit
   --work-dir "$WORK" --verdict VERDICT --summary "YOUR FINDINGS"` (one line).
   The tool attaches the actual query results and reads back the final Run and
   accepted evidence. Link the returned `recordPath` with the Run ID, decision and
   findings. Explain that stopping the temporary service removes its database
   and credentials while the linked record stays available offline.
   If submission or export fails, keep the launcher running and retry the same
   submission after addressing the error. A finished Run can be exported this
   way without a second submission. Stop your own launcher after successful export.
   For browser inspection, start with `--console` and leave that session running
   until the user finishes. Leave user-owned launchers under their control.

## Original three health observations

Use this skill for the synthetic, local Recovery example in
`examples/agent-demo/`. Read its [README](../../../examples/agent-demo/README.md)
and all three observation files before deciding what to submit. The checkout or release's
`examples/policies/recovery.yaml` is the Spec for this demo.

1. Check that the local service is reachable and that `guardrailctl` is available.
   Use a literal loopback HTTP origin, or an HTTPS origin explicitly supplied by
   the user. Never select another server from ambient credentials alone.
2. Management and Run creation use direct service access. No administrator
   session or Project Key is needed.
3. Apply and publish the Recovery Spec with guardrailctl scene apply --publish.
   Save each Run's Submission credential in a new private temporary directory
   outside the checkout or release.
4. For each observation file, create a `changeSet` describing this synthetic
   case and a fresh Run with `guardrailctl run create`, saving its private
   credential through `--submission-credential-file`. Read that Run's frozen
   instructions with `guardrailctl run revision RUN_ID`; do not
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
6. Query each Run directly using `guardrailctl run get`. Confirm all
   are `FINISHED`: healthy has `PASS / ALLOW`, unhealthy has `BLOCKED / DENY`,
   and unavailable health has `UNKNOWN / DENY`. Query the persisted Snapshot
   with `guardrailctl run submission --submission-id ID RUN_ID`
   using `finalSubmissionId`; its Evidence must reflect the corresponding
   observation. Check `decisionExpiresAt` before treating ALLOW as current.
   In the Runs page, confirm the records are visible. Report IDs,
   Check verdicts and Decisions without printing credential contents.
7. After verification and any requested browser inspection, remove only temporary
   files created by this demo. Keep Run Submission credentials private until
   the trial is finished. No account session or Project Key cleanup is needed.

Do not retry an uncertain `run create` or `run submit` with a new operation ID.
Read [Run retry and token recovery](../../../docs/runs.md) and inspect the actual
Run state first. The demo does not perform any deployment or release action;
whether a caller enforces a Guardrail Decision is outside this example.
