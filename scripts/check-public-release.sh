#!/bin/sh
set -eu

status=0
fail() {
  echo "public release blocked: $1" >&2
  status=1
}

test -f LICENSE || fail "LICENSE is missing"
script_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
"${PYTHON:-python3}" "$script_root/scripts/package_archive.py" --check-public-assets || fail "public assets are incomplete or not tracked"
test -f SECURITY.md || fail "SECURITY.md is missing"
test -f CODE_OF_CONDUCT.md || fail "CODE_OF_CONDUCT.md is missing"
test -f third_party/go-modules.tsv || fail "dependency license inventory is missing"

if test -f third_party/go-modules.tsv && grep -q '|review-required$' third_party/go-modules.tsv; then
  fail "dependency license approval is incomplete"
fi

module=$(sed -n 's/^module[[:space:]][[:space:]]*//p' go.mod)
case "$module" in
  ""|open-guardrail) fail "replace the temporary Go module path" ;;
esac

version=${VERSION:-}
case "$version" in
  ""|dev) fail "set VERSION to the public release version" ;;
  *[!a-zA-Z0-9._-]*) fail "VERSION contains unsupported characters" ;;
esac

exit "$status"
