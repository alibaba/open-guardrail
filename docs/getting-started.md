# Getting started

Try a local Recovery example using synthetic data. Run the self-check for a quick
result, or follow the walkthrough to create a Run and inspect it in the browser.
Neither option requires an AI model.

## Evaluation flow

[![OpenGuardrail evaluation flow: publish and activate a policy, create a Run, submit a complete assessment, read the Run and final Submission, and apply the decision in the calling system.](images/overview.png)](images/overview.svg)

Open the diagram for the full-size SVG. See [core concepts](concepts.md) for the
policy, assessment and decision model.

## Prerequisites

For a source checkout:

- The Go toolchain specified in `go.mod`.
- Make for `make build`.
- Python 3.12+ only if you run the isolated self-check below.

Run commands from the checkout root. Building the server and CLI does not require
Node.js or Docker.

If you received a preview binary archive, first follow
[archive verification](releases.md#check-and-try-a-downloaded-archive).
Run commands from its extraction root. Use `./open-guardrail` and
`./guardrailctl` in place of the source build's `./bin/` paths; running the
binaries needs no Go, Node.js or Python.

## Option 1: try an isolated decision loop

With Go and Python available, run:

```sh
python3 examples/agent-demo/smoke.py
```

The script builds the commands from source or uses the binaries in an archive.
It starts a temporary local service, publishes the Recovery policy and checks
decisions from synthetic assessments.

The output has this shape; Run IDs vary:

```text
healthy: run_... server=ALLOW gate=ALLOW
unhealthy: run_... server=DENY gate=DENY
unknown: run_... server=DENY gate=DENY
forged: run_... server=ALLOW gate=DENY
```

The last case shows why a caller must verify evidence even when the stored
decision is `ALLOW`. The script removes its temporary service and data; follow
the walkthrough below to keep and inspect a Run.

## Option 2: create and inspect your first Run

### 1. Start the service

From a source checkout:

```sh
make build
./bin/open-guardrail serve
```

Keep that terminal open. The listener defaults to `127.0.0.1:8080`, with SQLite
data in `./data/guardrail.db` relative to the startup directory.

Open <http://127.0.0.1:8080> and create an administrator named `admin` with a
password you choose. Setup signs you into the browser automatically. There is no
default password.

Local setup without a bootstrap token requires a direct loopback connection.
For remote or container deployment, follow [Docker deployment](docker.md) and
[administrator setup](local-administrator.md).

### 2. Sign in to the CLI

Open a second terminal in the same checkout root:

```sh
guardrail_cli=./bin/guardrailctl
export OPEN_GUARDRAIL_URL=http://127.0.0.1:8080
umask 077
guardrail_demo_dir=$(mktemp -d)
export OPEN_GUARDRAIL_SESSION_FILE="$guardrail_demo_dir/session.json"
"$guardrail_cli" status
"$guardrail_cli" auth login --username admin
```

For an extracted binary archive, set `guardrail_cli=./guardrailctl` instead.
Enter your administrator password at the hidden prompt. Browser and CLI sessions
are separate. This example stores its CLI session and machine credentials in a
private temporary directory outside the source tree.

### 3. Publish the example policy

```sh
"$guardrail_cli" scene apply --key recovery --name Recovery \
  --spec ./examples/policies/recovery.yaml --publish
"$guardrail_cli" auth create-key --expires-in 1h \
  --result-file "$guardrail_demo_dir/project-key.json"
```

The policy requires a `readiness` check with `health` evidence. The command
publishes and activates the revision; the temporary Project key lasts one hour.

See [Scenes and Specs](scenes-and-specs.md) for policy editing and publication.

### 4. Create a Run

Create a synthetic change description, then create one evaluation:

```sh
cat > "$guardrail_demo_dir/change-set.json" <<'JSON'
{
  "application": "example-service",
  "environment": "local",
  "operation": "recovery"
}
JSON
guardrail_demo_operation="quickstart-$(date +%s)-$$"
"$guardrail_cli" run create \
  --key-file "$guardrail_demo_dir/project-key.json" \
  --scene recovery \
  --change-set "$guardrail_demo_dir/change-set.json" \
  --operation-id "$guardrail_demo_operation-create" \
  --submission-credential-file "$guardrail_demo_dir/run-credential.json"
```

The command prints a Run object with `status: "OPEN"`. Copy its top-level `id`
into this variable, replacing the example value:

```sh
guardrail_demo_run_id=run_REPLACE_WITH_RETURNED_ID
```

The Run freezes the policy revision, caller-defined `changeSet` and deadline.
The submission credential is saved privately; the CLI does not print its token.

Complete the next step before the Run deadline. For an uncertain create result,
retain the same operation ID and inspect state before retrying. The initial
credential is returned only once; see [credential recovery](runs.md).

### 5. Submit a synthetic passing assessment

Submit a synthetic passing assessment; the timestamp is generated at execution time:

```sh
cat > "$guardrail_demo_dir/submission.json" <<JSON
{
  "baseSubmissionNo": 0,
  "finishRun": true,
  "verdict": "PASS",
  "summary": "Synthetic local recovery assessment",
  "checks": [
    {
      "checkId": "readiness",
      "verdict": "PASS",
      "summary": "Synthetic health observation is healthy",
      "evidence": [
        {
          "requirementId": "health",
          "observedAt": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
          "querySummary": "Read a synthetic local health observation",
          "resultSummary": "Example service is healthy; no real service was queried"
        }
      ]
    }
  ]
}
JSON
"$guardrail_cli" run submit \
  --submission-credential-file "$guardrail_demo_dir/run-credential.json" \
  --submission "$guardrail_demo_dir/submission.json" \
  --operation-id "$guardrail_demo_operation-submit" \
  "$guardrail_demo_run_id"
"$guardrail_cli" run get \
  --key-file "$guardrail_demo_dir/project-key.json" \
  "$guardrail_demo_run_id"
```

The returned Run should have `status: "FINISHED"`, `result: "PASS"` and
`decision: "ALLOW"`, plus its final submission ID and decision expiry.

### 6. Inspect the record

In the browser, open **Runs**, select the returned Run ID and inspect its frozen
policy reference, evaluation context, submitted evidence and decision.

A real integration must verify the authoritative Run, decision validity and
observations it relies on. See [Runs and submissions](runs.md) and the
[integration recipes](../integrations/README.md).

## Clean up or keep exploring

The service data persists in `./data`. Stop the server with Ctrl-C and restart
from the same directory to keep your administrator, policy and Run.
Use `--data-dir /path/to/persistent/data` when you want an explicit storage location.

First revoke only the Project key created for this walkthrough. While the
administrator CLI session is still active, inspect the secret-free metadata:

```sh
"$guardrail_cli" auth list-keys --page-size 20
```

Identify the walkthrough's key by its issuance time, role and scopes. Its private
credential file also records the same non-secret `keyId`; compare only that field
locally if needed, without displaying or sharing the token. Follow `nextAfter`
with `--after CURSOR` if the key is on another page. Do not revoke a key used by
another integration.

Replace the example ID with the selected key's `id`, then revoke it:

```sh
guardrail_demo_key_id=key_REPLACE_WITH_DEMO_KEY_ID
"$guardrail_cli" auth revoke-key --key-id "$guardrail_demo_key_id"
```

After revocation succeeds, remove the temporary CLI session and files in the
second terminal while its variables are still set. If revocation fails, retain
the session and credential file until you have resolved the error.

```sh
"$guardrail_cli" auth logout
rm -f "$guardrail_demo_dir/session.json" \
  "$guardrail_demo_dir/project-key.json" \
  "$guardrail_demo_dir/run-credential.json" \
  "$guardrail_demo_dir/change-set.json" \
  "$guardrail_demo_dir/submission.json"
rmdir "$guardrail_demo_dir"
unset OPEN_GUARDRAIL_SESSION_FILE
```

Removing a machine key file does not revoke that key. Revocation and secret
rotation are implemented; revocation immediately prevents subsequent key
authentication without canceling already-authenticated requests. See
[Project machine keys](local-administrator.md#project-machine-keys) for lifetime,
rotation and recovery rules. Use this walkthrough with a local evaluation service.

Next, try the [agent-driven demo](../examples/agent-demo/README.md), configure
[PostgreSQL or remote access](configuration.md), or read the
[contribution guide](../CONTRIBUTING.md).
