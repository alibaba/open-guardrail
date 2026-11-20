# Release-order correlation

Associate each release evaluation attempt with one immutable Run. Administrators
configure a Scene and publish its policy; callers create a Run with a business ID,
submit observations, then find its decision and frozen evidence from the release
order's attempt ID. No release or deployment is executed by this example.

```sh
make build
python3 -B examples/business-id/smoke.py
```

The example starts a real disposable SQLite service and uses `guardrailctl` for
creation, lookup, submission and report export. It verifies exact UTF-8 lookup,
credential preservation, content conflict, Scene isolation, eight concurrent
callers, completed and expired lookup, and report linkage. All state and synthetic
credentials are removed on exit. Database transaction semantics are additionally
covered by shared backend tests in the full quality gate.

For a real release order, choose an attempt ID such as
`release-42/batch-1/attempt-1`, keep it stable across retries, and supply the release
context in `changeSet`. Find the matching Run with `run list --scene SCENE
--business-id ID`, or filter by business ID on the console's Runs page. Inspect
its frozen policy, final evidence and decision before taking any external action.
A new evaluation after completion or expiry requires a new attempt ID.

Creation replay returns exit 5 and the existing Run ID without a new credential.
Inspect the original credential file; only rotate a lost credential for an OPEN
Run. Rotation invalidates the old credential. Business IDs are included in Run
responses and diagnostic reports, so avoid secrets and review reports before sharing.
See the [CLI guide](../../cmd/guardrailctl/README.md#associate-a-release-order-with-a-run).
