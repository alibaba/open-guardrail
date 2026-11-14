# Meet OpenGuardrail

An open-source action guardrail for AI agents: define the checks, collect the
evidence, and verify the decision before a tool acts.

## Why use it?

Bring the policy, check results and evidence for an operation into one traceable
assessment. Bind policies to resources so new Runs select the appropriate revision.
Your tool can verify the record before acting; your team can inspect it afterward.

## How it works

1. **Define the requirements.** Publish a policy with checks and evidence requirements.
2. **Submit an assessment.** Your agent or integration performs the checks and submits its observations.
3. **Verify, then act.** OpenGuardrail records `ALLOW` or `DENY`. The tool verifies the evidence, validity and business permissions before execution.

## Try it

Start with a [native download](native-install.md) and run the cleanup demo,
or try the [Agent plugin](plugin-trial.md) in pi or DeepSeek Harness.
The downloaded cleanup demo opens locally in your browser and uses a predefined
runner; the plugin uses your host's model.

Ready to connect your tool? Read the [integration guide](../integrations/README.md).
