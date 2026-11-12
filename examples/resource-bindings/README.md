# Resource binding walkthrough

[简体中文](README.zh-CN.md)

Use an administrator CLI session to bind a resource to a Policy. A trusted
orchestrator then creates a Run with a Project key and the original resource
reference. The service selects and freezes the Policy Revision.

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

Start with [administrator sign-in and Scene publication](../../cmd/guardrailctl/README.md#scenes-and-administrator-sessions).
Use the Policy ID returned by `scene apply` or an existing Policy in the same
Scene. Keep credentials and input files in a private working directory.

```sh
export OPEN_GUARDRAIL_URL=https://guardrail.example
export OPEN_GUARDRAIL_SESSION_FILE=/absolute/private/admin-session.json
policy_id=REPLACE_WITH_POLICY_ID
revision_id=REPLACE_WITH_PUBLISHED_REVISION_ID
key_file=/absolute/private/orchestrator-key.json

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

guardrailctl run create --key-file "$key_file" --scene recovery \
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

- Management uses administrator sessions. Project keys keep their existing Run scopes.
- `--resources` accepts a JSON array of 1–100 distinct `{kind,id}` references,
  bounded to 64 KiB. IDs are original strings, including slashes and Unicode.
- Omitted resources and entirely unbound sets select the Scene default.
  Partially bound or conflicting sets fail with 409. Bound unpublished/archived
  Policies also fail instead of falling back.
- Resource references are supplied by the trusted integration. The integration
  must derive them from the intended operation and enforce execution against the
  same resources. See the [binding contract](../../docs/extensions.md#resource-bindings).
- A binding write timeout can have an uncertain outcome. List the bindings to
  inspect the actual state before repeating a write. Pagination returns one page
  and the server's opaque `nextAfter` value.
