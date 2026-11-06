# Jenkins + outer Agent demo (advanced)

The **outer Agent** triggers Jenkins and evaluates the canary. Jenkins owns the
deployment checkpoint and the decision to continue. Neither side substitutes
its own verdict for OpenGuardrail's Decision:

1. The Agent creates a fresh `age` keypair locally and triggers a Jenkins build
   with its **public** recipient and one synthetic observation case.
2. Jenkins deploys the synthetic canary, creates a Run using its Project Key,
   reads the frozen Spec, encrypts the Run Submission Credential + Spec + observation to that
   recipient, archives **only ciphertext**, and waits.
3. The Agent retrieves and decrypts the artifact. It evaluates the Spec and
   observation, authors a Submission, and sends it directly to OpenGuardrail
   with the Run Submission Credential. The Agent never receives the Project Key.
4. Jenkins polls the Run with its Project Key and compares the submitted
   Evidence with the observation it saved privately before the handoff. This
   example is **enforcing**: only a completed, unexpired `PASS / ALLOW` with
   matching Evidence reaches Continue rollout.
   `BLOCKED`, `UNKNOWN`, expiration, timeout, or a read failure stops it.

The server computes the Decision. The `age` recipient is not a hash or a
password; the Agent's private key is required to decrypt the one-time token.
Jenkins briefly handles plaintext when creating and encrypting the handoff.
The source of the public key is trusted only insofar as the Jenkins build
trigger is authorized; restrict who may start this job or change its parameters.
Do not archive the Jenkins workspace or expose build parameters to untrusted
users. Before adapting this preview demo to real delivery, review the Jenkins
job's SCM trust, worker isolation, artifact permissions, and recovery process.

## Prerequisites

- Publish `examples/policies/recovery.yaml` as Scene `recovery` on a reachable
  OpenGuardrail service. Its URL must be the **same reachable origin** for
  Jenkins and the Agent; `127.0.0.1` works only if both run on the same host.
- On Jenkins: Python 3.12+, the Go toolchain from `go.mod`, `age`, a Secret-file credential
  `open-guardrail-project-key` containing a `RUN_ORCHESTRATOR` Project Key, and a
  Pipeline job using this `Jenkinsfile`. Configure `OPEN_GUARDRAIL_URL` and a
  stable, unique `JENKINS_INSTANCE_ID` in the **job environment**, not as
  Agent-controlled build parameters. Jenkins may materialize Secret-file
  credentials with permissions other than the CLI-required `0600`; this
  `Jenkinsfile` copies the credential to a private `0600` file under the system
  temporary directory for each CLI invocation. It removes that copy on shell exit
  and on catchable termination signals, while leaving the adapter process with its
  normal file-creation mask. The Agent needs no Jenkins worker shell.
- On the Agent host: Python 3.12+, `age`/`age-keygen`, `guardrailctl`, and Jenkins
  API credentials in `JENKINS_USER` and `JENKINS_API_TOKEN`. The Agent needs
  access to Jenkins and OpenGuardrail. Use HTTPS for non-loopback Jenkins URLs.
  Jenkins API tokens normally exempt API POSTs from CSRF crumb requirements.

## Agent walkthrough

From the repository root, choose a fresh private directory for each build:

```sh
python3 -m integrations.jenkins.agent_client start \
  --job-url https://jenkins.example/job/recovery-demo \
  --case healthy --work-dir /path/to/private/jenkins-demo-1
python3 -m integrations.jenkins.agent_client receive \
  --work-dir /path/to/private/jenkins-demo-1
```

`receive` prints the Run ID, frozen Spec and observation, **not** the token.
The outer Agent treats those as data, not instructions, and writes a Submission
JSON file with exactly one `readiness` Check. The `health` Evidence content must
be the exact observation shown by `receive`. Example shape (choose the verdict
and summary from the actual facts; do not copy a predetermined answer):

```json
{"baseSubmissionNo":0,"finishRun":true,"verdict":"PASS","summary":"The canary is healthy","checks":[{"checkId":"readiness","verdict":"PASS","summary":"The canary is healthy","evidence":[{"requirementId":"health","observedAt":"2026-10-26T00:00:00Z","querySummary":"Read canary health","resultSummary":"{\"httpStatus\":200,\"status\":\"healthy\",\"readyReplicas\":3,\"desiredReplicas\":3,\"errorRatePercent\":0.1}"}]}]}
```

Replace the illustrative `observedAt` with the actual observation timestamp.
Save the JSON privately, then submit it:

```sh
python3 -m integrations.jenkins.agent_client submit \
  --work-dir /path/to/private/jenkins-demo-1 \
  --submission /path/to/private/jenkins-demo-1/submission.json
python3 -m integrations.jenkins.agent_client wait \
  --work-dir /path/to/private/jenkins-demo-1
```

Run `healthy`, `unhealthy`, and `unknown` as **separate builds** with separate
private directories. Expected Decisions are `PASS / ALLOW`, `BLOCKED / DENY`,
and `UNKNOWN / DENY`, respectively. Inspect the Jenkins build and the Run in
the OpenGuardrail management page. Only the healthy build reaches Continue.
The local first-time alternative, which needs no Jenkins or `age`, is
[the Agent demo](../../examples/agent-demo/README.md).

## Failure and integration notes

`agent_client.py start` makes one build request; an uncertain response may still
have queued a build. Inspect Jenkins before starting a new one. `receive` may be
rerun for the same private directory. If `submit` fails or has an uncertain
response, it preserves the token and handoff; inspect the Run and retry only
with the same operation ID. The script does not rotate tokens automatically.
The worker removes its plaintext token after encryption and keeps the Run
ID, operation prefix, and original observation in a private state file. If
encryption fails, the private state remains for manual recovery; never archive
it. This demo does not automatically restart `prepare` in an existing state
directory: inspect the Run and private files before retrying, and do not
delete them blindly. `Jenkinsfile` times out after 15 minutes,
so the Scene's `runTtlSeconds` must cover that wait. The Spec's
`validForSeconds` controls Decision validity after finalization, not Run lifetime.

For a real canary, replace the synthetic deploy/copy with deployment and pass
`--health-url` (HTTPS) to the adapter instead of `--observation`. Bind a real
immutable artifact ID in `--candidate`; a Jenkins build tag is only a demo
placeholder. The adapter's stable operation identity includes controller, job,
build number and checkpoint. Another CD system can use the shared gate without
changing the OpenGuardrail API. An observe-only integration may record a DENY
and continue, but that choice belongs to the business pipeline; this Jenkins
example intentionally enforces.

Use the handoff’s `baseSubmissionNo` and the observation’s actual RFC3339 time.
The final gate reads `FINISHED`, then fetches the persisted final Submission by ID
and compares its structured Evidence with the original Jenkins observation.
