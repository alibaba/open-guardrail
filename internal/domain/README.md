# Domain rules

`spec` implements policy document validation, canonicalization and digest identity.
`run` implements ChangeSet normalization, complete Snapshot validation, evidence
requirements and verdict-to-decision aggregation. See its
[implementation notes](run/README.md).

Domain packages own reusable business rules and value types. They do not import
HTTP handlers, SQL adapters, deployment configuration or enterprise SDKs. Document
format parsing belongs with the document rules; external IO belongs in adapters.

Keep publication workflows and transaction ports in application packages. Move
types into a public contract only when an external provider actually needs them;
public interfaces cannot depend on this `internal` directory.
