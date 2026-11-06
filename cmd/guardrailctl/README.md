# guardrailctl

This is the HTTP CLI for an existing OpenGuardrail service. For a complete first
Run, follow [Getting started](../../docs/getting-started.md). Run the examples
from the checkout root with `guardrailctl` on `PATH`, or use
`./bin/guardrailctl` after `make build`. In a native archive, use `./guardrailctl`.

## Commands and credentials

Current commands cover `status`, `capabilities`,
`version`, `bootstrap init`, `auth login`, `logout`, `whoami`, `create-key`,
`list-keys`, `rotate-key` and `revoke-key`,
and `scene apply`.
It also implements `run create`, `run get`, `run revision`, `run submission`,
`run list`, `run rotate-credential` and `run submit`. `run get` reports
frozen deadlines, lazy expiry and Decision validity. Waiting and Claims remain
planned; there is no checkpoint model.

| Command group | Credential |
| --- | --- |
| `status`, `version`, `capabilities` | Public diagnostics; no credential required |
| `bootstrap init` | First-administrator setup; a private bootstrap token is required for remote mode |
| `auth` and `scene apply` | Administrator session (`auth login` creates it) |
| `run create/get/list/revision/submission/rotate-credential` | Project key with the endpoint's effective scopes |
| `run submit` | The Submission Credential bound to that Run |

## Scenes and administrator sessions

Keep credential files outside the checkout or extracted archive. For an isolated
session, create a private directory and sign in:

```sh
umask 077
guardrail_credentials_dir=$(mktemp -d)
export OPEN_GUARDRAIL_SESSION_FILE="$guardrail_credentials_dir/admin-session.json"
guardrailctl auth login --username admin
guardrailctl auth create-key \
  --result-file "$guardrail_credentials_dir/orchestrator-key.json"
```

If you already have a Project key, use its existing private directory instead of
issuing another. The Run examples below assume it contains `orchestrator-key.json`.

Create a Scene, save its Spec and publish it with an administrator CLI session:

```sh
guardrailctl scene apply --key recovery --name Recovery \
  --spec ./examples/policies/recovery.yaml --publish
```

The command is safe to repeat. It reuses a matching Scene, skips an unchanged
draft, and unchanged publication returns the existing active revision. A different
name or activation conflict fails instead of overwriting another
administrator's work. A new Scene requires `--publish`; on an existing Scene, omit it to save an incomplete Draft for later editing.
If draft saving succeeds but publication fails, the error states that partial
success so the operator can inspect state before retrying. Project keys cannot
create, save or publish Scenes.
Administrator commands use the session saved by `auth login`. Its default
location is the operating system's user configuration directory; the example
above selects an isolated private file. Set `OPEN_GUARDRAIL_SESSION_FILE` or pass
`--session-file PATH` to override that location for CI or isolated credentials.
Set `OPEN_GUARDRAIL_URL` or pass `--server URL` to select the service origin.

Machine keys default to a 90-day lifetime; `auth create-key --expires-in 24h`
creates a shorter-lived credential. `auth list-keys` returns a bounded metadata
page with IDs, versions, expiry and revocation timestamps, never secrets. Use
`--page-size` and the returned `nextAfter` as `--after` to page through keys.
`auth rotate-key --key-id ID --expected-version VERSION --result-file NEW_FILE`
atomically replaces an active secret, preserving its grants and its expiry unless
`--expires-in` is supplied. It invalidates the old secret and never overwrites an
existing output file. A stale version fails without rotating again.
`auth revoke-key --key-id ID` is safe to repeat. See the
[administrator guide](../../docs/local-administrator.md#project-machine-keys)
for credential lifecycle details and recovery after an uncertain response.

## Runs and submissions

Create and query a Run with the default `RUN_ORCHESTRATOR` Project key file
produced by `auth create-key`. It receives the fixed `runs:create`, `runs:read`
and `submission-credential:rotate` scopes. Use
`auth create-key --role POLICY_PUBLISHER` for Policy publication automation; that
key receives `policies:create` and `revisions:publish` and cannot operate Runs.
An HTTP integration that must select an already-frozen immutable Revision uses a
separate key created with `--scope runs:create-with-revision`; ordinary orchestrator
keys cannot supply `policyRevisionRef`. The current `run create` CLI selects the
Scene default active Revision and has no explicit-Revision flag.
Creation saves a one-time Run Submission Credential in a new private file:

```sh
guardrailctl run create --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --scene recovery --change-set ./change-set.json \
  --operation-id pipeline-123 \
  --submission-credential-file "$guardrail_credentials_dir/run-credential.json"
guardrailctl run get --key-file "$guardrail_credentials_dir/orchestrator-key.json" run_EXAMPLE
guardrailctl run revision --key-file "$guardrail_credentials_dir/orchestrator-key.json" run_EXAMPLE
guardrailctl run submission --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --submission-id sub_EXAMPLE run_EXAMPLE
guardrailctl run list --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --scene recovery --status OPEN --limit 20
guardrailctl run submit --submission-credential-file "$guardrail_credentials_dir/run-credential.json" \
  --submission ./submission.json --operation-id submit-123 run_EXAMPLE
```

Only the Run Submission Credential can submit to its bound Run. If it is lost,
use the Project key to replace it while the Run is `OPEN`:

```sh
guardrailctl run rotate-credential --key-file "$guardrail_credentials_dir/orchestrator-key.json" \
  --submission-credential-file "$guardrail_credentials_dir/replacement-run-credential.json" run_EXAMPLE
```

The CLI creates credential files with mode `0600` and never prints the raw token.
`run list` returns bounded, lightweight summaries in descending creation order.
Summaries include `changeSetDigest` but omit the potentially large `changeSet`;
use `run get` when the full immutable input is needed. Pass the response
`nextCursor` back through `--cursor` to request the next page; cursors are opaque
and must not be constructed by callers.

`--operation-id` becomes the HTTP `Idempotency-Key`. Automation should supply a
stable value for retries. Interactive use may omit it; the CLI generates one, and
includes it in uncertain transport errors. A `run create` retry can recover the
existing Run but cannot recover its one-time plaintext credential. Inspect an existing
credential file first; if the credential was not saved, use the Project key and
`run rotate-credential` with a new output path.

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

See [administrator setup and CLI sessions](../../docs/local-administrator.md)
for the currently supported initialization and identity commands.
