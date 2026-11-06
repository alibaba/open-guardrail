# Built-in external identity adapters

Status: planned. Existing local credentials remain in `application/identity`,
`adapters/security` and the HTTP adapter; this directory adds no new provider yet.

Place generally reusable external identity implementations here as they are
introduced. They implement public identity contracts and map provider responses
to verified actors. Provider-specific credential transports and login callbacks
must be wired explicitly through server composition.

Corporate SDK integrations live in the adopter's module. Shared resource
authorization, Spec logic and Run state do not belong in an identity adapter.
