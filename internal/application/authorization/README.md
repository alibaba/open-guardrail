# Guardrail authorization use cases

Status: planned. Standalone management uses direct access. Enterprise adapters
retain their identity, authorization and per-Policy maintainer checks.

Own authorization for guardrail resources and operations. Combine a verified actor
and external permission facts with authoritative Spec/Run ownership, scope and
participation. Business-state checks stay in the shared application.

An enterprise provider supplies organizational facts, not a replacement for Run
validity or evidence acceptance. Provider errors do not imply permission. Keep
authentication, external role lookup and core resource checks separate so an adopter
can replace identity without copying management handlers.
