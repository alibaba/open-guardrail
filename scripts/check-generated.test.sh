#!/bin/sh
# Exercise the check in isolation; no network, toolchain or repository writes.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT
trap 'exit 1' HUP INT TERM
mkdir -p "$temporary/api/openapi"
cd "$temporary"
generated=api/openapi/generated.gen.go
printf 'original\n' > "$generated"
cp "$generated" expected

cat > generator <<'EOF'
#!/bin/sh
case "$GENERATOR_MODE" in
  drift) printf 'changed\n' > api/openapi/generated.gen.go ;;
  fail) printf 'partial\n' > api/openapi/generated.gen.go; exit 1 ;;
  unchanged) ;;
  *) exit 2 ;;
esac
EOF
chmod +x generator
for mode in unchanged drift fail; do
  if GENERATOR_MODE="$mode" GO="$temporary/generator" sh "$root/scripts/check-generated.sh" > output 2>&1; then
    test "$mode" = unchanged
  else
    test "$mode" != unchanged
  fi
  cmp expected "$generated"
done
