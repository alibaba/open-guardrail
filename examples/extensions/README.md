# External-module integration example

Status: planned. This directory contains no executable example yet.

The first example must be built as a separate Go module and consume only the public
server API and extension contracts. Use a synthetic identity/permission provider
with reserved example domains and no corporate dependency.

The consumer should select its providers, use the standard storage/HTTP/UI and run
the same guardrail behavior without importing `internal` packages, copying handlers
or maintaining core patches. Build it in compatibility checks to expose public type
leaks, forced local bootstrap and incomplete provider wiring.

Create the example alongside the first implemented provider API. It serves as the
minimal template for an adopter's small `main`, adapters and pinned dependency.
