# Third-party licenses and notices

OpenGuardrail uses Apache-2.0 for its own code. Dependencies retain their own terms.
The original, unmodified legal files for each pinned module are preserved in
`licenses/`. `notices.tsv` records their versions, runtime/tool scopes, public source
archives, repository paths and SHA-256 checksums. The Go standard-library legal files
are included for the `go.mod` toolchain version.

The bundle covers root-level LICENSE/LICENCE, NOTICE, COPYING, COPYRIGHT, AUTHORS
and PATENTS files, including supplemental files such as `LICENSE-3RD-PARTY.md`,
`LICENSE-SQLITE`, `LICENSE-SQLITE_VEC` and `LICENSE-MMAP-GO`. Keep upstream copyright
and attribution intact. Their public author names are part of the original notices.
The bundle does not include dependency source code or relicense those components.

Verify the bundle without a network connection or module cache:

```sh
make check-dependency-notices
```

After changing pinned dependencies, download the corresponding modules and update
the bundle from their unmodified source directories:

```sh
python3 scripts/dependency-notices.py --update
make check-dependency-notices
```

`--module-cache PATH` and `--go-root PATH` can select existing pinned sources.
Collection refuses a different Go version or a missing upstream license and
replaces only the generated `third_party/licenses` directory after collecting all
inputs. Commit the complete updated bundle and inventory together. The verifier
rejects changed checksums, stale versions/scopes, symlinks and unindexed files.

Module-level approval is recorded separately in `go-modules.tsv`. This bundle
covers only the public runtime and generation tools. Exact source archives provide
traceability but do not replace review of embedded code, per-file terms or the
final artifact's distribution obligations. See [the dependency review](../docs/dependency-licenses.md).
