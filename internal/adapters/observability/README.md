# Operational observability adapters

Status: planned. The current server already accepts a standard `slog.Logger`;
this directory reserves additional metrics and tracing integration code.

Use standard logging and telemetry APIs. Reusable instrumentation belongs upstream;
an adopter supplies its backend/exporter through composition. Keep private collectors
and corporate SDKs in the adopter's module.

Operational logs, metrics and traces are separate from required decision audit
records. Core commands and their storage transactions retain ownership of those
records. Failure evidence collected by an Agent belongs to the Run decision model,
not to the telemetry exporter.
