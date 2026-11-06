# Third-party dependency license review

This review covers the union of module closures linked into both public commands
for every release target, plus the complete transitive closure of the pinned
OpenAPI generator. The public build excludes all private modules.

## Result

- Inventory updated on 2026-11-06: 26 runtime modules and 17 tool modules.
- The runtime and tool closure contains only the reviewed MIT, BSD-3-Clause and
  Apache-2.0 module-level labels; no private dependency is recorded.
- These are module-level findings, not an exhaustive per-file legal audit.
- The public module and commands do not depend on the MySQL driver. Dependency
  checks explicitly disable local Go workspaces and compare the actual graph
  against `third_party/go-modules.tsv`.

Original root-level license, NOTICE, copyright, author and patent files are retained
under `third_party/licenses`; `third_party/notices.tsv` records exact versions,
public source archives and SHA-256 hashes. Supplemental modernc notices and the
Go standard-library license and patent notice are included. Preserve these files
byte-for-byte when refreshing dependencies. See [the notice guide](../third_party/README.md).

The inventory and copied notices do not replace a per-file review. Entries marked
`review-required` block public packaging until their conditions are recorded.

Primary reference: [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0).
