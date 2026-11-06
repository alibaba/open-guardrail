# Public extension contracts

Status: planned. These directories contain design only; no provider interface is
implemented or supported yet.

Contracts describe the external facts needed by the shared application. Organize
them by identity, authorization and delivery capability. A provider supplies facts;
the core applies guardrail rules and commits business state.

Every public signature must be usable from another Go module. Types crossing the
boundary belong in the relevant public contract package, or are mapped through an
explicit boundary DTO. Do not expose internal services, repositories, SQL handles,
local password records or provider SDK objects.

Introduce each interface with a built-in implementation, a synthetic external
implementation and defined cancellation, failure and compatibility behavior.
Repository ports stay with internal application use cases. Use standard logging
and telemetry interfaces rather than adding a generic hook or plugin registry.

Providers are selected during server composition and compiled into the binary.
External evaluators remain separate processes; a provider adapter is an in-process
client of its fact source. See the
[provider execution clarification](../docs/adr/0001-product-baseline.md#provider-execution-clarification).
The core must remain replaceable by an upstream dependency upgrade without copying
handlers or business code. See the [repository layout](../docs/extensions.md).
