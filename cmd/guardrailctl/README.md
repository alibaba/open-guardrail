# guardrailctl

The console, CLI and management API use direct access. Only assessment submission requires a Run credential. See [Access](../../docs/access.md).

| Command | Credential |
| --- | --- |
| `status`, `version`, `capabilities` | None |
| `scene apply`, `binding list/put/delete`, `audit list/export` | None |
| `run create/get/wait/list/report/revision/submission/rotate-credential` | None |
| `run submit` | Credential bound to the selected Run |
| `mcp-proxy` | Creates and privately saves a credential for each guarded Run |

```sh
guardrailctl scene apply --key recovery --name Recovery   --spec ./examples/policies/recovery.yaml --publish
umask 077
guardrail_credentials_dir=$(mktemp -d)
```

Scene application reuses a matching Scene, saves changed drafts and optionally
publishes them. A new Scene requires `--publish`; an existing Scene may save an
incomplete draft. Conflicts fail without overwriting another edit.
Run credential files must be regular files with mode `0600`.
See [Access](../../docs/access.md).

## MCP stdio proxy

```sh
guardrailctl mcp-proxy --config ./policy.yaml [--server URL] [--run-dir DIR] -- downstream-command args
```

The proxy passes ordinary tools through without HTTP calls and gates matching
or destructive tools using an immutable Run bound to the exact arguments.
See [MCP proxy setup, client configuration and custom policies](../../integrations/mcp-proxy/README.md)
for a runnable database-write Scene and the assessment/retry flow.

## Runs and submissions

```sh
guardrailctl run create \
  --scene recovery --change-set ./change-set.json \
  --operation-id pipeline-123 \
  --submission-credential-file "$guardrail_credentials_dir/run-credential.json"
guardrailctl run get run_EXAMPLE
guardrailctl run wait run_EXAMPLE
guardrailctl run revision run_EXAMPLE
guardrailctl run submission \
  --submission-id sub_EXAMPLE run_EXAMPLE
guardrailctl run list \
  --scene recovery --status OPEN --limit 20
guardrailctl run submit --submission-credential-file "$guardrail_credentials_dir/run-credential.json" \
  --submission ./submission.json --operation-id submit-123 run_EXAMPLE
```

`run wait` polls the authoritative Run until it is `FINISHED` or `EXPIRED`, then
prints the final Run document. By default it waits until the Run's own deadline;
`--timeout` sets an earlier limit and `--interval` sets the polling period.
Exit code 0 means `FINISHED`; an `EXPIRED` Run prints its state and exits nonzero
because no final Submission arrived before the deadline. Transport errors, `5xx`
and `429` are retried until the deadline, while authentication and other client
errors return immediately. Until the Run deadline is known, connection retries are
capped at 30 seconds; a `429` `Retry-After` value longer than `--interval` is
honored, bounded by the deadline.
Waiting only observes the Run. The caller still verifies the Decision, its
validity and the evidence before performing the guarded operation.

```sh
guardrailctl run rotate-credential \
  --submission-credential-file "$guardrail_credentials_dir/replacement-run-credential.json" run_EXAMPLE
```

The CLI creates credential files with mode `0600` and never prints the raw token.
`run list` returns bounded, lightweight summaries in descending creation order.
Summaries include `changeSetDigest` but omit the potentially large `changeSet`;
use `run get` when the full immutable input is needed. Pass the response
`nextCursor` back through `--cursor` to request the next page; cursors are opaque
and must not be constructed by callers.

## Associate a release order with a Run

Use an optional exact business identifier on creation and lookup:

```sh
guardrailctl run create \
  --scene release-check --business-id 'release-42/batch-1/attempt-1' \
  --change-set change-set.json --operation-id release-42-create \
  --submission-credential-file "$guardrail_credentials_dir/release-42.json"
guardrailctl run list \
  --scene release-check --business-id 'release-42/batch-1/attempt-1'
```

Identifiers contain 1–128 UTF-8 bytes, with no surrounding whitespace or ASCII
control characters. Matching is exact: quote shell arguments; do not trim or
normalize identifiers. The identifier is unique within a project and Scene.
Omitting the flag preserves existing behavior; an explicitly empty value is invalid.
Listing without `--scene` can return matching identifiers from multiple Scenes.

The same identifier and immutable input replay the original Run, even with a
new operation ID. A different input returns `BUSINESS_ID_CONFLICT` (exit 5).
Replay also exits 5 because the one-time Submission Credential is not reissued;
stderr identifies the existing Run and recovery options. An existing credential
file is never overwritten. For an OPEN Run, inspect the saved credential first;
rotate only when it is lost, as rotation invalidates the old credential.
Completed and expired Runs retain the identifier permanently. Use a new business
identifier for each new evaluation attempt, and keep the release order in `changeSet`.
A business identifier is a correlation key, not execution authorization.

The console's Runs page supports exact business-ID filtering, shareable filter
URLs and display in both the list and detail. Select a matching Run to inspect
its frozen policy and evidence, or export its diagnostic report. Reports include
the business identifier; review it before sharing outside the intended audience.
See the runnable [release-order example](../../examples/business-id/README.md).

## Transport and compatibility

HTTP commands use the Go client generated from `api/openapi/openapi.yaml`.
The CLI never opens the service database or embeds a separate decision engine.
Human users, agents and CD integrations use the
same command semantics. Keep credential-file handling and transport behavior here;
keep policy validation, evidence acceptance and state transitions in the server.

Management commands negotiate `v1alpha1` and the server-advertised CLI version
window before sending credentials or changing state. Diagnostic commands remain
available when versions are incompatible so operators can still inspect `status`,
`version`, and `capabilities`.

## Automation exit codes

The process exit codes are stable automation contracts:

| Code | Meaning |
| ---: | --- |
| 0 | Success |
| 1 | Local or unexpected failure |
| 2 | Invalid command, flags, URL, or unsafe transport |
| 3 | Authentication or authorization rejected |
| 4 | Server connection or response transport failed |
| 5 | Server rejected the request |
| 6 | API or CLI version is incompatible |

The generated client is wrapped by one bounded transport that refuses redirects,
limits response bodies, and attaches a credential only to the intended origin.
Server error messages are not echoed; failures expose only the stable error code
and trace identifier needed for support.

Jenkins, GitHub Actions and Argo integration recipes live in
[integrations](../../integrations/README.md). Platform-specific scripts or workflow
configuration belong there. Add shared business commands here alongside the matching
server APIs, rather than creating a CLI implementation for each CD platform.

See [access and Run credentials](../../docs/access.md)
for direct access and Run credential handling.

## Audit and diagnostic exports

Use the default project directly to inspect audit events and export a Run report:

```sh
guardrailctl audit list --limit 20
guardrailctl run report --output ./run-report.json run_EXAMPLE
```

Exports are bounded and omit business text. See the [audit and report guide](../../docs/audit-and-reports.md) for filters, pagination and privacy boundaries.
