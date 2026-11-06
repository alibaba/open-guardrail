# Server composition API

Status: planned. This directory contains design only; there is no public Go server
API yet. The current executable calls `internal/bootstrap.Serve`.

This package will let a separate Go module compose an enterprise distribution.
Its public configuration and provider options must assemble the same application,
storage, HTTP handlers, migrations and UI as the standard distribution.

The API owns validated startup, provider selection, readiness, cancellation and
shutdown. Configuration passed from an enterprise configuration center must receive
the same validation as file-based configuration. Explicit enterprise login routes
and callbacks must coexist with the standard management API.

Selecting enterprise identity must replace the local login/bootstrap requirement.
Local identity remains the default for standalone use. Wrapping the current
`bootstrap.Serve` without changing its fixed identity wiring does not meet this goal.

Public signatures use public configuration and extension types. Keep database
connections, application services and corporate SDK clients behind the facade.
Start with the first working provider and an external-module consumer test before
committing to API signatures. See the [repository layout](../docs/extensions.md).
