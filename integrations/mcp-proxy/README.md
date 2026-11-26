# MCP guardrail proxy

`guardrailctl mcp-proxy` wraps a local stdio MCP server. Ordinary tools pass
through without contacting OpenGuardrail. Matching tools require a frozen Run
with `FINISHED`, `PASS`, `ALLOW` and an unexpired `decisionExpiresAt`.

## Quick start

Build or install `guardrailctl` using the [CLI guide](../../cmd/guardrailctl/README.md).
Start your OpenGuardrail service, then publish the example database Scene:

```sh
guardrailctl scene apply --server http://127.0.0.1:8080 \
  --key db-write-guard --name db-write-guard \
  --spec integrations/mcp-proxy/db-write-guard.yaml --publish

guardrailctl mcp-proxy \
  --config integrations/mcp-proxy/example-policy.yaml \
  --server http://127.0.0.1:8080 -- /absolute/path/to/your-mcp-server arg1
```

The downstream command must speak newline-delimited JSON-RPC 2.0 on stdio.
Frames up to 16 MiB are supported. Ambiguous duplicate JSON keys and invalid
UTF-8 in client frames are rejected. Downstream stderr and proxy diagnostics go to
stderr. Choose a trusted downstream command and route the client's tool access
through the proxy.

Blank or whitespace-only lines are ignored in either direction; nonempty invalid
JSON-RPC frames still terminate the session. Client stdin EOF starts session
shutdown: downstream stdin is closed and the proxy waits one second for final
responses before proceeding with child shutdown. Responses beyond that drain
window are not guaranteed to be complete. Keep stdin open until all outstanding
tool results have been consumed; newlines delimit requests, while EOF ends the
session.

## Client configuration

For Cursor, add the following entry to `.cursor/mcp.json`. Claude Code uses the
same `mcpServers` object in `.mcp.json`; Cline accepts this entry in its MCP
Servers configuration. Replace all absolute paths and downstream arguments:

```json
{
  "mcpServers": {
    "guarded-database": {
      "command": "/absolute/path/to/guardrailctl",
      "args": [
        "mcp-proxy",
        "--config", "/absolute/path/to/example-policy.yaml",
        "--server", "http://127.0.0.1:8080",
        "--", "/absolute/path/to/your-mcp-server", "arg1"
      ]
    }
  }
}
```

The downstream inherits the proxy's environment, so configure its database
connection using the downstream server's usual environment variables. The
OpenGuardrail server must also be reachable by the Agent's `guardrailctl`.

## Assessment and retry

A blocked call returns an MCP tool result with `isError: true`, the Run ID,
private credential-file path and exact CLI commands:

```sh
guardrailctl run revision --server http://127.0.0.1:8080 run_EXAMPLE
guardrailctl run submit --server http://127.0.0.1:8080 \
  --submission-credential-file /private/path/op_EXAMPLE.json \
  --submission ./submission.json run_EXAMPLE
```

Read the frozen Revision and collect evidence for every required check. Submit
the complete Snapshot using the [Run submission contract](../../docs/concepts.md).
Then retry the original tool with its exact arguments plus the reserved field:

```json
{
  "name": "execute_sql",
  "arguments": {
    "query": "DELETE FROM sessions WHERE id = 42",
    "_guardrail_run_id": "run_EXAMPLE"
  }
}
```

The proxy removes `_guardrail_run_id` before forwarding. It verifies the Run
identity, Scene, frozen tool and argument digest, final verdict, decision and
expiry. Only Runs issued by this proxy process for the matching rule and exact
arguments are accepted. Restarting the proxy requires a new Run. Unavailable
OpenGuardrail, malformed responses, unfinished Runs and expired decisions block
execution. The proxy does not automatically retry an uncertain Run creation.
Creation errors retain the operation ID for reconciliation. Inspect server state
before retrying the original tool call without `_guardrail_run_id`; recreating a
Run using a separate `guardrailctl run create` cannot recover this proxy session.

`reuseValidRun: true` caches a verified ALLOW until its expiry, keyed by rule,
Scene and SHA-256 of the tool name and recursively key-sorted JSON arguments.
Changing any argument requires a new assessment. This permits repeated execution
of the same approved call during that window; set `reuseValidRun: false` to
require a Run lookup on every retry. This is decision reuse, not exactly-once
execution or protection from database state changes after assessment.

Credentials are mode `0600`. The default mode `0700` temporary directory is
removed on exit. `--run-dir /private/directory` retains credentials for inspection
and must be private; existing files are never overwritten. Raw tokens are never
printed. Use the paths printed by the proxy, especially when your MCP client runs
in a container or another host namespace.

This preview has no count limit for issued Run bindings, cached decisions or
credential files. They accumulate during long sessions; an expired cache entry
is removed when its exact key is revisited. Rotate the proxy after outstanding
assessments and tool results finish. The default temporary credentials are
removed on exit. With `--run-dir`, use a fresh private directory per session and
archive or remove old credentials only after that proxy has stopped. Credentials
needed by an assessment must remain available until submission completes.

## Policy and custom Specs

`--config` is required. Server precedence is `--server`, policy `server`,
`OPEN_GUARDRAIL_URL`, then `http://127.0.0.1:8080`. Policies require `version: 1`
and reject unknown fields, invalid patterns, duplicate rule names and multiple
YAML documents. Rules are checked in order; the first matching rule wins.

```yaml
version: 1
defaultAction: deny
destructiveHintPolicy:
  scene: file-cleanup
  reuseValidRun: false
rules:
  - name: workspace-delete
    tools: [delete_file, remove_file, 'rm_*']
    scene: file-cleanup
    reuseValidRun: true
```

Tool patterns use Go `path.Match`. Each rule requires `name`, `tools` and `scene`.
An optional `when` requires `arg` and exactly one of `equals`, `notEquals`,
`matchesRegex`, `notMatchesRegex`, or `notReadOnlySql: true`. Missing or nonstring
arguments match negative predicates, so incomplete SQL calls cannot bypass the
gate. `reuseValidRun` defaults to false for explicit rules and true for the
destructive-hint policy.

The proxy observes client-requested `tools/list` responses, including paginated
results, and remembers tools declaring `annotations.destructiveHint: true`.
Explicit matching rules take precedence. Otherwise destructive tools use
`destructiveHintPolicy.scene`, or are denied when that Scene is omitted.
Unmatched ordinary tools use `defaultAction` (`pass_through` by default).
Annotations are supplied by the downstream and learned only after `tools/list`;
configure explicit rules or `defaultAction: deny` when discovery cannot be trusted.

`notReadOnlySql` passes only a conservative subset of a single `SELECT`, `WITH`,
`EXPLAIN`, `SHOW`, `DESCRIBE`, `DESC` or `VALUES` statement. It rejects write and
locking keywords anywhere outside quoted text, `SELECT INTO`, multiple statements,
executable comments and unterminated quotes/comments. Ambiguous dialect features
(backslash escapes, `#`, nested comments, nonportable `--` comments), `ANALYZE`
and arbitrary function calls also require assessment (only unqualified standard
read-only aggregates and null helpers `COUNT`, `SUM`, `AVG`, `MIN`, `MAX`,
`COALESCE` and `NULLIF` pass through).
Derived tables (`FROM (SELECT ...)`), join column lists (`USING (...)`),
`EXPLAIN (FORMAT JSON) SELECT ...` and `SHOW CREATE TABLE ...` are recognized as
read-only grammar. Recognizing parentheses after `OVER` does not make unknown
window functions safe; function calls and writes inside nested queries remain
gated.
This small recognizer deliberately routes uncertain SQL to the gate. Align
`when.arg` with the actual SQL parameter exposed by your server (`query` in the
example). Configure different tools and argument names explicitly.

Publish your own [GuardrailSpec](../../docs/guardrailspec-reference.md), then point
`scene` at its Scene key. Use separate Scenes for database writes and filesystem
cleanup; the database example Spec cannot establish filesystem safety. The
example's destructive-hint fallback shares the database Scene only as a starting
configuration. Required evidence IDs use lowercase letters, digits and hyphens.
Arguments are frozen in `changeSet`; if they exceed 256 KiB, display arguments are
omitted with `argumentsTruncated: true`, while the complete hash remains bound.

The proxy and policy files are trusted integration components. Direct management
access follows the service's [access model](../../docs/access.md). Keep the proxy,
its local credentials and OpenGuardrail management access within that trust
boundary; the stdio wrapper cannot restrict separate clients connecting directly
to a downstream server or independently changing service policy.
