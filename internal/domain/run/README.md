# Run evaluation rules

This package implements the preview's scenario-independent evaluation rules.
It validates and deterministically normalizes arbitrary JSON-object ChangeSets
without interpreting business fields. Size, depth and duplicate-key limits are
transport and consistency boundaries.

The Run references an immutable Policy Revision and its Spec digest. A complete
Submission Snapshot supplies every Check verdict and applicable structured
evidence, for either an intermediate or final assessment. Domain validation
checks evidence requirements and limits, rejects credential-shaped content,
verifies the declared aggregate verdict and maps PASS to ALLOW and
BLOCKED/UNKNOWN to DENY. It does not independently attest to observations.

Application and storage code own locks, clocks and persistence. They advance an
overdue OPEN Run to EXPIRED with UNKNOWN/DENY and atomically accept final
Submissions. Intermediate Snapshots advance the baseline; a final Snapshot
creates the terminal Result and Decision. Execution Claims remain planned.

Keep these rules shared across integrations. External adapters supply facts;
they do not replace evidence acceptance or mutate the Run independently. See
[core concepts](../../../docs/concepts.md) and
[Runs and submissions](../../../docs/runs.md) for the public model.
