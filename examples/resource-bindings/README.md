# Resource binding walkthrough

The console, CLI and management API use direct access. Only assessment submission requires a Run credential. See [Access](../../docs/access.md).

[简体中文](README.zh-CN.md)

## Disposable live check

From the checkout root, run `make smoke-bindings`. With extracted native
binaries, run `python3 examples/resource-bindings/smoke.py --binaries .`.
The CLI must include `binding` and `run create --resources`; earlier Alpha
artifacts do not include these commands.

The script starts a real loopback service with a temporary SQLite database,
creates private credentials, and uses the CLI for binding management and Run
creation. Existing HTTP APIs prepare a non-default Policy and publish its
Revisions. It verifies resource encoding, pagination, active/pinned selection,
frozen Runs, idempotent replay, partial/conflicting binding rejection, default
fallback and repeated deletion. Success prints `result: PASS` with check names
and public IDs. Shutdown removes the temporary service, database and credentials.
No model is required for this policy-selection walkthrough.

## Use an existing service

Start with [Scene publication through the CLI](../../cmd/guardrailctl/README.md); no sign-in is required.
Use the Policy ID returned by `scene apply` or an existing Policy in the same
Scene. Keep credentials and input files in a private working directory.

```sh
export OPEN_GUARDRAIL_URL=https://guardrail.example
policy_id=REPLACE_WITH_POLICY_ID
revision_id=REPLACE_WITH_PUBLISHED_REVISION_ID

# Follow the Policy's active Revision. Pass the unencoded resource ID.
guardrailctl binding put --scene recovery --kind repository \
  --resource-id 'team/repository' --policy-id "$policy_id"

guardrailctl binding list --scene recovery --limit 20
# If nextAfter is present, pass its exact value to --after on the next call.

# Pin a published Revision belonging to the same Policy.
guardrailctl binding put --scene recovery --kind repository \
  --resource-id 'team/repository' --policy-id "$policy_id" \
  --revision-id "$revision_id"

umask 077
cat > resources.json <<'JSON'
[{"kind":"repository","id":"team/repository"}]
JSON
cat > change-set.json <<'JSON'
{"changeSetRef":"example-operation-1"}
JSON

guardrailctl run create --scene recovery \
  --change-set change-set.json --resources resources.json \
  --operation-id example-operation-1 \
  --submission-credential-file /absolute/private/new-run-credential.json
```

The Run output contains `policyId`, `policyRevisionId` and, for a matched binding,
`selection` with active/pinned mode. Read it again with `run get` and fetch the
frozen content with `run revision`. Changing a binding or activating a different
Revision affects new Runs. Existing Runs and exact idempotent replays retain their
original Policy/Revision. Preserve the saved Submission Credential. A replay
cannot disclose its plaintext again into a new file; inspect the Run and use the
existing [credential recovery flow](../../cmd/guardrailctl/README.md#runs-and-submissions).

Repeat PUT without `--revision-id` to return to active selection. Remove a binding
with `guardrailctl binding delete --scene recovery --kind repository --resource-id 'team/repository'`.
Repeated deletion succeeds without changing the Scene default.

## Selection and permissions

The smoke also checks audit mutation history, redacted export and a denied Run diagnostic report. See [audit and reports](../../docs/audit-and-reports.md).
