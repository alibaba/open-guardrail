# Delivery context provider boundary

Status: planned. This directory does not yet define a Go interface.

Map an external delivery system's change identity, target, checkpoint and action
correlation into inputs used by guardrail commands. Freeze decision-relevant facts
with the Run rather than consulting changing context midway through acceptance.

The CD platform owns deployment, batches and traffic. The core owns check validity,
evidence acceptance and the decision. A provider must not implement a separate
release state machine or silently resume a blocked action.

Define retry identity, stale-context handling, provider failures and cancellation
when the first real context use case is implemented. A CD platform that already
calls the HTTP API or CLI may need only an integration recipe; it does not require
a compiled Go provider. See [CD-side recipes](../../integrations/README.md).
