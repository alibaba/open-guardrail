# Plan your first integration

Start with one action, a small set of explicit checks, and a tool that can verify
the decision before it acts. The same assessment contract connects your policy,
observations and execution workflow.

## Pick an operation you can observe

| Starting point | Checks to define | Where to start |
| --- | --- | --- |
| File cleanup | Exact target files, temporary-file scope and batch size | [Optional file cleanup](getting-started.md#optional-file-cleanup-without-a-model) |
| An application delivery step | The target application and environment, required test or health observations | [First-batch check](getting-started.md#run-the-first-batch-check) and [delivery integrations](../integrations/README.md) |
| A configuration or data operation | Intended resource scope and observations required by your business rules | The [GuardrailSpec reference](guardrailspec-reference.md) and [HTTP API](api.md) |

For your first integration, choose checks whose observations your tools can
collect and whose evidence your execution tool can verify.

## Connect three responsibilities

1. **Policy owner:** publish the GuardrailSpec and select the Scene's active policy.
2. **Agent or evaluator:** read the requirements, perform the checks, and submit
   complete results and evidence with the Run's submission credential.
3. **Execution tool:** read the authoritative Run, confirm its operation context,
   decision validity, accepted evidence and current targets, then apply its own
   permissions before acting.

OpenGuardrail validates the assessment against the policy and records the
decision. Your observation and execution integrations provide the connection to
the real system. See [Runs and submissions](runs.md) for the read and retry flow.

## Choose a deployment

| Use | Storage and setup |
| --- | --- |
| Explore locally | Start the service with SQLite using a native binary or a source build, or use the disposable cleanup demo. |
| Retain shared assessment records | Configure PostgreSQL and run one service instance per database. |
| Provide remote access | Put the service behind a trusted HTTPS proxy and configure administrator and machine credentials. |

See [configuration and storage](configuration.md), [Docker deployment](docker.md)
and [administrator setup](local-administrator.md) for the required settings.

## Try the complete loop

Run the example, inspect both the denied and allowed assessments, and read the
accepted evidence in the console. Then replace the example policy and observation
sources with the requirements of your own operation.

Current release: development preview, with a `v1alpha1` HTTP API. Use the documented
single-instance deployment and check the API contract when updating versions.

Continue with [Getting started](getting-started.md).
