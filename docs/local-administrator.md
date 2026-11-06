# Local administrator guide

OpenGuardrail provides one local administrator account. For a complete walkthrough,
see [Getting started](getting-started.md).

## First use

1. Start `open-guardrail serve` and open <http://127.0.0.1:8080>.
2. Choose a username and password, then confirm the password. Passwords must contain
   at least 6 characters and no more than 128 UTF-8 bytes.
3. Complete setup to enter the management page automatically.

Keep the same data directory after restarting to retain the SQLite account. The
management page supports English and Simplified Chinese.

Token-free setup requires a direct loopback connection to a loopback-only
listener with no configured `public_url`. It is intended for a trusted local
machine. A reverse proxy or configured public origin uses the
[remote setup flow](#remote-access), including when its backend is loopback.

## Login and password changes

Use your administrator account to sign in. Sessions expire after eight hours.
Failed password attempts are audited and return the same unauthorized response.
Failed or unfinished attempts consume the shared temporary-backoff budget described
below. Use the local administrator only on a trusted machine, and do not expose its
login endpoint to untrusted clients.

Changing your password requires the current password and signs out all
administrator sessions. Sign in again with the new password.

### Forgotten username or password

Run this on the service host, using the same binary version and data directory as
the service:

```sh
open-guardrail admin reset --data-dir /path/to/persistent/data
```

Confirm the reset, choose a username (or press Enter to keep it), then enter the new
password twice. The old password is not required. For a configured deployment, use
`--config /path/to/config.yaml` and the service's database environment variables.

Reset signs out all administrator sessions but preserves Scenes, Specs and Project
keys. It requires an existing, initialized database; protect host and database access.

## Project machine keys

Create a machine key only when a service needs to call OpenGuardrail. The administrator
selects a role when issuing the key:

- `RUN_ORCHESTRATOR` (the default): its fixed effective scopes are `runs:create`,
  `runs:read` and `submission-credential:rotate`. It cannot submit results; that
  requires the Run credential. By default it can only use the Scene default active
  Revision. Trusted integration keys may additionally receive
  `runs:create-with-revision` when they must replay an already-frozen Revision from
  another system.
- `POLICY_PUBLISHER`: its fixed effective scopes are `policies:create` and
  `revisions:publish`. It cannot edit Drafts, activate Revisions, initialize Scenes
  or create Runs.

Roles are issuance templates: each key persists its complete effective scopes,
and runtime authorization checks those scopes rather than the role name. Both
roles are scoped to one Project. Neither grants administrator access or approves
a protected action.

Keys are displayed only once. Save them securely and do not commit them to a repository.
New keys expire after 90 days by default. Set `--expires-in` to whole seconds from
`1m` to `8760h` (365 days). Keys have stable non-secret IDs and secret-generation
versions; listing returns metadata, never plaintext keys or stored hashes.
Revocation is idempotent and immediately prevents subsequent authentication.
Already-authenticated requests are not canceled by revocation.

Rotation requires the current version, replaces the secret atomically and preserves
the ID, role and scopes. Omit `--expires-in` to preserve the expiry, or specify a new
bounded lifetime. Expired and revoked keys cannot be rotated or revived; issue a
new key instead. Concurrent rotations using the same version have one winner.
The old secret becomes invalid when rotation commits. For a gradual consumer
rollout, create a separate key, update consumers, then revoke the old key.

Do not automatically retry key creation if the response is lost: a key may already
have been created. After an uncertain rotation, inspect `auth list-keys` first.
If its version advanced but the secret was lost, rotate again using that new
version and a new output file, or revoke the key. Replaying the old version returns
`KEY_STATE_CONFLICT` and never issues another secret.

## Optional CLI usage

For local initialization without the browser:

```sh
guardrailctl bootstrap init --username admin
```

The password prompt is hidden. Initialization does not sign the CLI in automatically.
Use a private directory outside the checkout or extracted archive for sessions
and keys. These examples create a fresh directory and CLI session:

```sh
umask 077
guardrail_credentials_dir=$(mktemp -d)
export OPEN_GUARDRAIL_SESSION_FILE="$guardrail_credentials_dir/admin-session.json"
guardrailctl auth login --username admin
guardrailctl auth whoami
guardrailctl auth create-key \
  --result-file "$guardrail_credentials_dir/orchestrator-key.json"

# Optional short-lived key and a bounded metadata page.
guardrailctl auth create-key --expires-in 24h \
  --result-file "$guardrail_credentials_dir/short-lived-key.json"
guardrailctl auth list-keys --page-size 20
# Follow nextAfter with --after CURSOR if another page is present.

# Copy KEY_ID and KEY_VERSION from the metadata list. Use a new output path.
guardrailctl auth rotate-key --key-id "$KEY_ID" --expected-version "$KEY_VERSION" \
  --result-file "$guardrail_credentials_dir/replacement-key.json"
guardrailctl auth revoke-key --key-id "$KEY_ID"

# Issue a separate integration key that may select an explicit immutable Revision.
guardrailctl auth create-key --scope runs:create-with-revision \
  --result-file "$guardrail_credentials_dir/revision-orchestrator-key.json"

# Issue a separate Policy Publisher key for trusted automation.
guardrailctl auth create-key --role POLICY_PUBLISHER \
  --result-file "$guardrail_credentials_dir/policy-publisher-key.json"
guardrailctl auth logout
```

Run only the optional issuance or rotation examples you need. Keep the private
directory while any consumer uses its key files. Before discarding a key file,
revoke its key by ID while an administrator session is still active. Logging out
or deleting local files does not revoke Project keys. In another terminal, set
`guardrail_credentials_dir` to the existing private directory before reusing it.

The administrator session is stored in the operating system's user configuration
directory by default; the example above selects an isolated private file instead.
The default containing OpenGuardrail directory uses mode `0700` and the session
file uses mode `0600`. An expired default session is removed automatically before
the next login; a live session is never silently overwritten. If the default file
is invalid or belongs to another server, the CLI reports its exact path so the
operator can sign out against that server or move it aside deliberately. Protect
session and key files like passwords.

The HTTP equivalents are `GET/POST /api/v1alpha1/auth/api-keys`,
`DELETE /api/v1alpha1/auth/api-keys/{keyId}` and
`POST /api/v1alpha1/auth/api-keys/{keyId}/rotation`. Rotation accepts
`expectedVersion` and optional `expiresInSeconds`; creation accepts optional
`expiresInSeconds`. All require a live administrator session, and mutations retain
the management Origin/Host/CSRF checks. Machine keys cannot manage credentials.

Set `OPEN_GUARDRAIL_SESSION_FILE` or pass `--session-file PATH` to override the
default for CI, tests or isolated credentials. Explicit credential output files
are never overwritten.

For a different server address, set `OPEN_GUARDRAIL_URL` or add `--server URL`.
Automation can supply passwords through `--password-stdin`; never put passwords
in command arguments.

## Remote access

Remote access requires an HTTPS `public_url` (or `OPEN_GUARDRAIL_PUBLIC_URL`)
and a reverse proxy that terminates TLS. The proxy must preserve the external Host,
strip untrusted forwarding headers, prevent direct access to the HTTP backend, and
restrict the management interface to trusted operators. The local administrator's
shared temporary backoff does not replace source-based rate limiting at the trusted
reverse proxy. Do not expose default local mode through a tunnel, port forwarding,
or a proxy.

Remote initialization also requires the service host's `bootstrap.token` file;
the startup log shows its path. Enter the token on the setup page or pass
`--token-file PATH` when initializing through the CLI. Keep the file private (0600).

The token expires after one hour. If setup is still incomplete, restart the server
to renew an expired token. After initialization, the token no longer works.

For configuration options, see [configuration and storage](configuration.md).
For API details, see the [OpenAPI specification](../api/openapi/openapi.yaml).

## Temporary login backoff

The local administrator shares a persistent attempt budget across server instances
and attempted usernames. After five unsuccessful or unfinished attempts, further
attempts temporarily return HTTP 429 `RATE_LIMITED`. Backoff starts at one second,
doubles on admitted failed attempts and caps at 30 seconds. Requests rejected
during a cooldown do not extend it. The response includes conservative
`Retry-After: 30` guidance. A successful login or host administrator reset clears
the budget; 15 minutes without an admitted attempt resets the burst counter.
The account is never permanently locked.

Each server admits at most two password verifications at once. Hashing happens
outside the identity write transaction, and the password snapshot is rechecked
before a session is issued. A password reset therefore invalidates an in-flight
login that used the old hash. Cancellation may consume an attempt without issuing
a session; retry after the cooldown. Deployments should additionally apply
source-based rate limiting at their trusted reverse proxy; forwarded client IP
headers are not used to bypass the shared administrator budget.
