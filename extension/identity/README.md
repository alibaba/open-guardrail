# Identity provider boundary

Status: planned. This directory does not yet define a Go interface.

Represent a verified human or machine actor and its authenticated attributes.
Separate the actor from local password users, session storage and administrator
bootstrap. Enterprise identity must not require a local `identity.User` record.

Define who verifies credentials, how delegated identity is represented and which
attributes are trusted. HTTP credential extraction and provider-specific login
callbacks belong to adapters; the core receives a verified actor.

Scope and roles are checked by authorization. Authentication failure, unavailable
providers and request cancellation need distinct, explicit behavior. Introduce the
contract with the local/default mapping and a synthetic external provider through
the public server entrypoint.
