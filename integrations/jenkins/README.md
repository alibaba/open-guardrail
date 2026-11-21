# Jenkins + outer Agent demo (advanced)

The console, CLI and management API use direct access. Only assessment submission requires a Run credential. See [Access](../../docs/access.md).

The **outer Agent** triggers Jenkins and evaluates the canary. Jenkins owns the
deployment checkpoint and the decision to continue. Neither side substitutes
its own verdict for OpenGuardrail's Decision:

The server computes the Decision. The `age` recipient is not a hash or a
password; the Agent's private key is required to decrypt the one-time token.
Jenkins briefly handles plaintext when creating and encrypting the handoff.
The source of the public key is trusted only insofar as the Jenkins build
trigger is authorized; restrict who may start this job or change its parameters.
Do not archive the Jenkins workspace or expose build parameters to untrusted
users. Before adapting this preview demo to real delivery, review the Jenkins
job's SCM trust, worker isolation, artifact permissions, and recovery process.

## Prerequisites

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
