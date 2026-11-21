# Access and Run credentials

OpenGuardrail starts ready to use. Open the console or call the API/CLI directly;
there is no administrator account, password, login, session file or Project API Key.
The service automatically prepares the default workspace and project.

The console, Scene/Spec editing, bindings, Run creation and inspection, audit queries
and credential rotation are available to callers that can reach the service.
The default listener is `127.0.0.1:8080`. Use a trusted network or an authenticating
reverse proxy when sharing a deployment. Remote deployments still require the
configured HTTPS origin, exact Host/Origin checks and a trusted TLS proxy.
Browser mutations require `X-Guardrail-CSRF: 1`; the console and CLI send it automatically.

## Run Submission credentials

Creating a Run returns a credential bound to that Run. Only this credential can
submit its intermediate or final assessment. Management access does not bypass
the Run's credential, expiry, finalization, evidence or verdict checks.

```sh
umask 077
guardrail_credentials_dir=$(mktemp -d)
guardrailctl scene apply --key recovery --name Recovery   --spec ./examples/policies/recovery.yaml --publish
guardrailctl run create --scene recovery --change-set ./change-set.json   --submission-credential-file "$guardrail_credentials_dir/run.json"
guardrailctl run get run_EXAMPLE
guardrailctl run submit run_EXAMPLE --submission ./submission.json   --submission-credential-file "$guardrail_credentials_dir/run.json"
```

Credential files must be private regular files with mode `0600`. Store them outside
the checkout. A Run credential is accepted only by its Run's Submission endpoint.
For retries and rotation, see [Runs](runs.md#run-retry-and-credential-recovery).

## Existing installations

Existing Scenes, Specs, Runs and audit records remain in place. Stored local
accounts, sessions, bootstrap tokens and Project keys are no longer read or
accepted. An old Policy's maintainer list does not prevent standalone editing.
Historical schema migrations remain compatible with existing databases.

## Enterprise integrations

`trusted_operator_namespace` retains API-only operator handling and per-Policy
maintainer checks. Enterprise identity and authorization adapters continue to
enforce their own access controls.
