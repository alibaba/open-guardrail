# External permission facts

Status: planned. This directory does not yet define a Go interface.

Expose the enterprise role or permission facts required for a verified actor,
resource, operation and scope. The contract must use public types and define
denial, provider unavailability and cancellation behavior.

The application combines those facts with authoritative guardrail state, such as
resource ownership, the selected Spec and Run participation. Enterprise role
lookup must not replace these checks, change a verdict or write business state.

Keep local credentials and external directory objects out of the contract.
Validate the first interface with both the built-in authorization path and a
synthetic provider. See [core authorization](../../internal/application/authorization/README.md).
