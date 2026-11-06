#!/bin/sh
set -eu

generated=api/openapi/generated.gen.go
go_command=${GO:-go}

if [ ! -f "$generated" ]; then
  echo "$generated is missing; run 'make generate'" >&2
  exit 1
fi

snapshot=$(mktemp)
cp "$generated" "$snapshot"
cleanup() {
  cp "$snapshot" "$generated"
  rm -f "$snapshot"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

"$go_command" generate ./api/openapi
if ! cmp -s "$snapshot" "$generated"; then
  echo "$generated is stale; run 'make generate' and commit the result" >&2
  exit 1
fi
