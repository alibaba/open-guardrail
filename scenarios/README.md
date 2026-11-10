# Scenario packages

Each directory owns one concrete operation: tool definitions, evidence
collection and the execution gate. Host adapters in
`integrations/agent-plugins/` register scenario tools and manage session
lifecycle. A scenario must be explicitly listed in the adapter registry and
package script before it is shipped; merely adding a directory does not enable
it in pi or DSH.

| Scenario | Status | Entry |
| --- | --- | --- |
| [File cleanup](file-cleanup/README.md) | Bundled internal preview | `file-cleanup/scenario.mjs` |

The file cleanup scenario operates only on four disposable sample files. It
does not provide a general filesystem guard.
