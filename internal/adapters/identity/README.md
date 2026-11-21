# Built-in external identity adapters

Status: planned. Standalone access is direct and has no local account or session.
This directory adds no identity provider.

Place generally reusable external identity implementations here as they are
introduced. They implement public identity contracts and map provider responses
to verified actors. Provider-specific credential transports and login callbacks
must be wired explicitly through server composition.

Corporate SDK integrations live in the adopter's module. Shared resource
authorization, Spec logic and Run state do not belong in an identity adapter.
