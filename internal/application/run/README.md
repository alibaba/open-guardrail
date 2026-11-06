# Run use cases

This package owns preview Run orchestration and the repository port it consumes:
creation and query, Project-scoped idempotency, bounded listing, scoped credential
rotation, immutable Revision/Submission readers and complete Snapshot submission.
Domain rules normalize the ChangeSet and validate assessment content.

Creation freezes the Scene's Run TTL, selected Policy Revision reference, Spec
digest and evaluation context. The repository atomically creates the Run and its
audit record. The selected Spec determines terminal decision validity.
Intermediate Submissions advance the baseline; one final Submission atomically
persists evidence and produces the Result and Decision.

Submission deduplication, expected-baseline checks, credential revalidation, Run
advancement and required audit records form one transaction. The clock is sampled
after acquiring the Run lock for Submission and credential rotation. Queries and
writes lazily expire overdue OPEN Runs; lock ordering permits only one terminal
outcome. An uncertain response is resolved with the same operation identity and
an authoritative read.

Keep repository and transaction ports beside their use cases. Consume domain
rules without importing HTTP adapters, SQL implementations or private platform
SDKs. Claims and provider composition remain planned. See
[Runs and submissions](../../../docs/runs.md) for supported caller behavior.
