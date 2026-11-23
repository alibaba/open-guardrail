# GitHub Actions release gate

Start with a runnable CD release scenario, then replace its Spec and assessment command with
your own business rules and evidence sources. The Action reuses `guardrailctl`;
no changes to the server, CLI, or core Spec schema are needed.

## Try the complete sample locally

Use a source checkout containing this directory. Install Make, the Go version
declared in `go.mod`, Python 3.9+, Bash and jq. Run all commands below from the
checkout root (the directory containing `go.mod` and `Makefile`):

```sh
make build
python3 integrations/github-actions/demo.py
```

The demo starts a temporary loopback service with SQLite, runs the actual Action
script, and exercises all three observations without an account, model API key,
external data source or production deployment:

| Observation | Submitted result | Server decision | Deployment simulation |
| --- | --- | --- | --- |
| `example/pass.json`: no freeze/incident, compatible bounded change, healthy canary, ready rollback | PASS | ALLOW | Continues after independent verification |
| `example/blocked.json`: active freeze window | BLOCKED | DENY | Blocked |
| `example/unknown.json`: canary signals unavailable | UNKNOWN | DENY | Blocked |

`release-gate.yaml` contains three production-release reference checks:
`freeze-window`, `compatibility-and-blast-radius` and `rollback-readiness`.
The bundled evaluator applies those rules to synthetic structured release signals,
so the console shows a complete CD assessment without needing access to real
incident, CI, monitoring or artifact systems. The sample performs no real deployment.

All three expected outcomes make the demo command succeed; the individual gate
invocations for BLOCKED and UNKNOWN still fail. The demo also reads each persisted
final Submission, so it checks the real service/CLI flow rather than canned Run
responses. Temporary service data and credentials are removed on exit.

To inspect Runs and evidence in the console:

```sh
python3 integrations/github-actions/demo.py --case pass --keep-running
```

Open the printed console address. Press Ctrl-C to stop and clean up.
Use `--binaries-dir /path/to/binaries` to reuse existing server and CLI binaries.
This local path does not require a GitHub repository, Actions configuration or a
previously running service. Source builds may download Go dependencies on first use.

## Try it in GitHub Actions

1. Use a GitHub fork or copy of this source repository with Actions enabled.
   Make a test OpenGuardrail service reachable from both jobs and create the
   repository variable `OPEN_GUARDRAIL_URL` with its HTTPS origin. A temporary
   `127.0.0.1` service on your laptop cannot be reached by GitHub-hosted runners.
   See [Access](../../docs/access.md) for shared-service setup.
2. Copy the complete workflow:
   ```sh
   mkdir -p .github/workflows
   cp integrations/github-actions/release-gate-example.yml .github/workflows/release-gate-example.yml
   ```
   Commit it to your GitHub repository's default branch. GitHub requires the
   workflow to exist there before manual dispatch is available; see
   [Manually running a workflow](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow).
3. In Actions, select **OpenGuardrail sample release gate**, then **Run workflow**.
   Choose `pass`, `blocked` or `unknown`. The pass run should succeed;
   blocked/unknown intentionally fail the assessment job and skip deployment.

The workflow builds the CLI from the checked-out source and publishes the sample
Spec into the dedicated `github-actions-demo` Scene. The first job creates,
assesses, submits and verifies a Run. A separate job independently verifies the
same release and simulates deployment only for ALLOW. The default pass case
needs no handwritten evidence file or placeholder assessment step.

The example is a manually dispatched workflow, not a `workflow_call` entrypoint.
In another repository, vendor this Action directory from a reviewed revision and
install a compatible CLI using the [CLI guide](../../cmd/guardrailctl/README.md)
instead of the example's source build steps.

## Configure your own scenario

### 1. Create a working starting point

Copy the matched Spec and evaluator instead of creating an empty configuration:

```sh
mkdir -p .guardrail build
cp integrations/github-actions/release-gate.yaml .guardrail/spec.yaml
cp integrations/github-actions/example/assess.py .guardrail/assess.py
cp integrations/github-actions/example/pass.json build/release-signals.json
export RELEASE_SIGNALS_FILE="$PWD/build/release-signals.json"
python3 integrations/github-actions/demo.py \
  --spec-file .guardrail/spec.yaml \
  --assessment-command 'python3 .guardrail/assess.py --signals "$RELEASE_SIGNALS_FILE"'
```

This uses your files, a fresh temporary service and the same submission and
verification flow. Expect `custom: ... PASS/ALLOW`. The command runs from the
current directory, so relative paths refer to your project, not the Action folder.

### 2. Replace the evidence source and rules

For the preset release rules, have your collector write the following structured
signals to `$RELEASE_SIGNALS_FILE`; no Action changes are needed:

| Object | Fields used by the evaluator | PASS / BLOCKED / UNKNOWN |
| --- | --- | --- |
| `freezeWindow` | Boolean `active`, `blockingIncident` | Both false / either true / otherwise unknown |
| `changeAssessment` | Boolean `backwardCompatible`, `blastRadiusBounded`, `canaryHealthy` | All true / any false / otherwise unknown |
| `rollback` | Boolean `ready`, string `target` | Ready with a nonempty target / explicitly not ready / otherwise unknown |

Each example object also records its `source` for evidence inspection. Replace
sample source names and findings with your actual collector's observations.
Every assessment includes all three checks and their required evidence. BLOCKED
overrides UNKNOWN, and UNKNOWN overrides PASS. Missing files or unverifiable
signals produce UNKNOWN, rather than fabricated passing evidence.

To test a non-passing result,
append `--expect-result BLOCKED` or `--expect-result UNKNOWN` to the local command.
The demo succeeds when the observed result matches your expectation, while the
underlying deployment gate still blocks both outcomes.

For a different business rule, change `.guardrail/spec.yaml` and the matching
evaluation logic in `.guardrail/assess.py`. Update the evaluator's `checkId`,
`requirementId`, field lookup and the Spec's criteria together, then rerun the
local command. Additional checks must all appear in the
final Submission. The copied evaluator already handles Run numbering, timestamps
and Snapshot output; replace its collection and judgment logic as needed.

For API/MCP/Agent-based assessment, replace `--assessment-command` with your own
entrypoint and pass source configuration through its arguments or environment.
It must follow the three-file contract below; it need not use Python or the
sample observation shape.

### 3. Use the same configuration in your workflow

After local verification, commit `.guardrail/spec.yaml` and your evaluator.
Generate actual signal files in a preceding workflow step, then configure:

```yaml
- name: Evaluate release
  id: gate
  uses: ./integrations/github-actions
  env:
    RELEASE_SIGNALS_FILE: ./build/release-signals.json
  with:
    mode: evaluate
    server: ${{ vars.OPEN_GUARDRAIL_URL }}
    scene: my-release
    spec-file: .guardrail/spec.yaml
    assessment-command: python3 .guardrail/assess.py --signals "$RELEASE_SIGNALS_FILE"
    deployment-environment: staging
    business-id: gha-${{ github.run_id }}-${{ github.run_attempt }}
    operation-id: gha-${{ github.run_id }}-${{ github.run_attempt }}-create
```

In a different repository, vendor the Action directory before using the local
`uses` path and install `guardrailctl` and jq on **each** job's runner. The example's
`go build ./cmd/guardrailctl` assumes an OpenGuardrail source checkout; it is not an
installation command for your application repository. Keep the independent
verification job from the complete workflow, using the same Scene and target
environment. Only the Run ID needs to be passed between jobs.

| Setting | What the user controls |
| --- | --- |
| `spec-file` | Check IDs, natural-language criteria, evidence requirements and decision validity |
| `assessment-command` | A script or Agent that evaluates the frozen Spec and writes a standard Submission |
| Command arguments / `env` | Evidence sources: a build output, monitoring query, API client, MCP-enabled Agent, or another trusted tool |
| `scene` | Scenario identity; no release-specific checks are built into the Action |

Providing `spec-file` explicitly applies and publishes it before Run creation,
using the Scene key as its display name. Choose a fresh Scene for the first try.
For a Scene managed separately, omit `spec-file`: the Action uses its configured
policy. Verification never publishes or changes a Spec.

The bundled `release-gate.yaml` and `example/assess.py` are a matched CD reference
scenario with three release checks. The evaluator reads the structured release
signals above and deterministically evaluates them. It is not a general interpreter
for arbitrary natural-language policies. When changing the checks, use an
assessment implementation that understands those checks, or an Agent that reads
the frozen instructions and has tools for your actual evidence sources. The Action
has no fixed check IDs, evidence schema or data-source integration.

### Assessment command contract

The command runs in the workflow workspace with the step's environment and receives:

| Environment variable | Purpose |
| --- | --- |
| `OG_RUN_FILE` | JSON metadata of the created Run, including ChangeSet and `latestSubmissionNo` |
| `OG_SPEC_FILE` | YAML of the Run's frozen policy Revision, not a later mutable draft |
| `OG_SUBMISSION_FILE` | Private output path where the command must write a complete final Snapshot |

Write every check required by that Spec, set `baseSubmissionNo` from the Run and
`finishRun: true`, and include the required evidence. See the
[Submission format](../../docs/runs.md#complete-snapshot-submissions).
Exit 0 for a completed assessment, including BLOCKED or UNKNOWN; the Action
submits those outcomes and then blocks on DENY. Exit nonzero for an execution
failure. A missing/invalid Snapshot, rejected submission, failed read or expired
decision stops the Action. No uncertain write is retried automatically.

The command does not receive the Run credential as an input. The Action owns
submission and removes its temporary files on success and failure. The command
still runs under the same OS identity; this is not a sandbox for untrusted code.
Keep command text in reviewed workflow configuration and pass dynamic data through
arguments/files/environment rather than interpolating it into shell source.

### Independent deployment verification

In the deployment job, call the same Action with `mode: verify`, the first
job's `run-id`, and independently configured `server`, `scene`,
`deployment-environment` and `business-id`. Do not pass `spec-file` or
`assessment-command` to verification. See the complete example workflow.

All verification requires FINISHED / PASS / ALLOW and an unexpired
`decisionExpiresAt`. It also compares Run ID, Scene, exact expected ChangeSet and
optional business ID. The default ChangeSet includes repository, workflow run,
attempt, commit, ref and target environment. To bind an immutable artifact digest,
pass the same trusted `change-set-json` at creation and independent verification.
Rerun both jobs for a new workflow attempt; an old attempt's Run will not match.

The second job checks service state and release identity. Evidence truth and
assessment quality depend on your evaluator and sources; server validation checks
Snapshot structure, required evidence and verdict aggregation. Protect workflow
source and service access, and configure your actual deployment/environment gates.

## Advanced modes and troubleshooting

- `evaluate`: optional Spec publication, Run creation, assessment command,
  submission and immediate verification. Use this for the complete synchronous flow.
- `create`: return a Run ID and private `submission-credential-file` path for
  manual or external assessment. Delete the credential after use and never upload
  it as an artifact; an external evaluator needs a trusted handoff.
- `gate` (default): wait for an existing `run-id`, then verify. Start the
  assessment and hand off credentials before waiting.
- `verify`: read an existing Run immediately and independently verify it.

`wait-timeout` in gate mode uses Go durations such as `300s`, `5m` (default),
or `1h30m`; bare numbers are invalid. Wait failures retain the CLI exit code and
include a GitHub error summary. Operation IDs are for exact retries of the same
request; inspect the Run before retrying or rotating a credential. See
[Runs and recovery](../../docs/runs.md#run-retry-and-credential-recovery).

| Symptom | Next step |
| --- | --- |
| Missing jq, CLI or Spec file | Install the named command, set `guardrailctl-path`, or create the starter files above; the Action checks before creating a Run. |
| No Run workflow button | Commit the workflow to the GitHub default branch and confirm Actions is enabled. |
| Service connection fails in Actions | Use a service reachable from the runners; the local demo's loopback URL is only for your machine. |
| New Spec rejects a Submission | Match every check/evidence ID to the frozen Spec and provide all required checks; editing only the YAML does not change a deterministic evaluator's logic. |
| Local service exits with -9 / SIGKILL | The OS terminated the process before readiness; inspect local system/resource diagnostics, then retry the local walkthrough or use a Linux environment. |

The Action, evaluator, demo and Python tests need no third-party Python packages.
Lightweight asset checks and real Bash execution tests always run without skips.
The live demo exercises actual Spec YAML parsing by the server. `make test-python`
runs these checks and the CD demo on both Aone and GitHub CI without invoking pip.
The source and native archive walkthroughs for the core service remain in
[Getting started](../../docs/getting-started.md).
