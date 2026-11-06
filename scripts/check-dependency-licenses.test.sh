#!/bin/sh
# A failed dependency query must never pass by emitting a matching partial graph.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT
trap 'exit 1' HUP INT TERM
mkdir -p "$temporary/third_party" "$temporary/bin"
cd "$temporary"
cat > third_party/go-modules.tsv <<'INVENTORY'
module|version|scope|license|status
example.com/runtime|v1.0.0|runtime|MIT|allowed
example.com/tool|v1.0.0|tool|MIT|allowed
INVENTORY
cat > bin/go <<'STUB'
#!/bin/sh
case "$*" in
  *oapi-codegen/v2/cmd*) target=tool; scope=tool ;;
  *) target="$GOOS/$GOARCH"; scope=runtime ;;
esac
printf 'example.com/%s|v1.0.0|%s\n' "$scope" "$scope"
if test "${FAIL_TARGET:-}" = "$target"; then exit 7; fi
STUB
chmod +x bin/go
PATH="$temporary/bin:$PATH" sh "$root/scripts/check-dependency-licenses.sh"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 tool; do
  if PATH="$temporary/bin:$PATH" FAIL_TARGET="$target" sh "$root/scripts/check-dependency-licenses.sh" >output 2>&1; then
    echo "failed dependency query was accepted: $target" >&2
    exit 1
  fi
  grep -q 'cannot inspect' output
done
