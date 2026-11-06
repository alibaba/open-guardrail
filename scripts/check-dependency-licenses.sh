#!/bin/sh
set -eu
export GOWORK=off

inventory=third_party/go-modules.tsv
test -f "$inventory" || {
  echo "dependency license inventory is missing: $inventory" >&2
  exit 1
}

temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT
trap 'exit 1' HUP INT TERM

if test "$(sed -n '1p' "$inventory")" != 'module|version|scope|license|status'; then
  echo "dependency license inventory has an invalid header" >&2
  exit 1
fi

if ! awk -F '|' '
  NR == 1 { next }
  NF != 5 || $1 == "" || $2 == "" { print "invalid dependency inventory row " NR ": " $0; bad=1; next }
  $3 != "runtime" && $3 != "tool" { print "invalid dependency scope on row " NR ": " $3; bad=1 }
  $4 != "MIT" && $4 != "BSD-3-Clause" && $4 != "Apache-2.0" &&
    $4 != "MIT AND Apache-2.0" && $4 != "MPL-2.0" {
      print "invalid dependency license on row " NR ": " $4; bad=1
    }
  $5 != "allowed" && $5 != "review-required" && $5 != "forbidden" {
    print "invalid dependency status on row " NR ": " $5; bad=1
  }
  { key=$1 "|" $2 "|" $3; if (seen[key]++) { print "duplicate dependency inventory row: " key; bad=1 } }
  END { exit bad ? 1 : 0 }
' "$inventory" >&2; then
  exit 1
fi

: > "$temporary/raw"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  target_os=${target%/*}
  target_arch=${target#*/}
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
    go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}|{{.Version}}|runtime{{end}}{{end}}' \
      ./cmd/open-guardrail ./cmd/guardrailctl >> "$temporary/raw" || {
        echo "cannot inspect runtime dependencies for $target" >&2
        exit 1
      }
done

go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}|{{.Version}}|tool{{end}}{{end}}' \
  github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
  >> "$temporary/raw" || {
    echo "cannot inspect tool dependencies" >&2
    exit 1
  }
sed '/^[[:space:]]*$/d' "$temporary/raw" > "$temporary/nonempty"
sort -u "$temporary/nonempty" > "$temporary/actual"

awk -F '|' 'NR > 1 { print $1 "|" $2 "|" $3 }' "$inventory" > "$temporary/inventory-keys"
sort -u "$temporary/inventory-keys" > "$temporary/expected"
if ! diff -u "$temporary/expected" "$temporary/actual"; then
  echo "dependency graph differs from the reviewed license inventory" >&2
  exit 1
fi

if grep -Eiq '(^|[./-])(alibaba-inc\.com|antfin\.com|alipay\.com|taobao\.net)([./-]|$)' "$temporary/actual"; then
  echo "private module path found in release dependency graph" >&2
  exit 1
fi

if awk -F '|' 'NR > 1 && $5 == "forbidden" { print; bad=1 } END { exit bad ? 0 : 1 }' "$inventory"; then
  echo "forbidden dependency license found" >&2
  exit 1
fi

if awk -F '|' 'NR > 1 && $5 == "review-required" { print $1 " " $2 " (" $4 ")" }' "$inventory" | grep . > "$temporary/review"; then
  echo "dependency licenses awaiting approval:" >&2
  sed 's/^/  - /' "$temporary/review" >&2
fi
