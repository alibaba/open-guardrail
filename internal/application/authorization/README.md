# Guardrail authorization use cases

Status: planned. Current management handlers still enforce the local administrator
role directly; this directory does not yet provide a replacement service.

Own authorization for guardrail resources and operations. Combine a verified actor
and external permission facts with authoritative Spec/Run ownership, scope and
participation. Business-state checks stay in the shared application.

An enterprise provider supplies organizational facts, not a replacement for Run
validity or evidence acceptance. Provider errors do not imply permission. Keep
authentication, external role lookup and core resource checks separate so an adopter
can replace identity without copying management handlers.
