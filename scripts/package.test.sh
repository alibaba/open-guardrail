#!/bin/sh
# Check release assembly with a stub compiler; cross-compilation is a CI job.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT
trap 'exit 1' HUP INT TERM
mkdir -p "$temporary/third_party"
mkdir -p "$temporary/bin" "$temporary/docs" "$temporary/cmd/guardrailctl" "$temporary/deploy"
mkdir -p "$temporary/examples/policies" "$temporary/integrations/jenkins" "$temporary/.agents/skills/open-guardrail-demo"
cd "$temporary"
printf 'readme\n' > README.md
printf 'contributing\n' > CONTRIBUTING.md
printf 'notice\n' > NOTICE
printf 'third-party guide\n' > third_party/README.md
mkdir -p third_party/licenses/example.com/dependency@v1.0.0
printf 'upstream license\n' > third_party/licenses/example.com/dependency@v1.0.0/LICENSE
"${PYTHON:-python3}" - <<'PYNOTICES'
from pathlib import Path
import hashlib
path = Path('third_party/licenses/example.com/dependency@v1.0.0/LICENSE')
Path('third_party/notices.tsv').write_text('path|sha256\n' + str(path) + '|' + hashlib.sha256(path.read_bytes()).hexdigest() + '\n')
PYNOTICES
printf 'conduct\n' > CODE_OF_CONDUCT.md
printf 'security\n' > SECURITY.md
printf 'module|version|scope|license|status\n' > third_party/go-modules.tsv
printf 'architecture\n' > docs/architecture.md
printf 'cli\n' > cmd/guardrailctl/README.md
printf 'config\n' > deploy/config.example.yaml
printf 'policy\n' > examples/policies/recovery.yaml
printf 'adapter\n' > integrations/jenkins/adapter.py
printf 'skill\n' > .agents/skills/open-guardrail-demo/SKILL.md
printf 'license fixture\n' > LICENSE
git init -q
git add README.md CONTRIBUTING.md CODE_OF_CONDUCT.md SECURITY.md LICENSE NOTICE third_party docs cmd deploy examples integrations .agents
# Developer state and credentials must not enter a public release.
printf 'private credential\n' > integrations/jenkins/handoff.json
printf 'private notes\n' > docs/local-notes.md
cat > bin/go <<'EOF'
#!/bin/sh
set -eu
# Both binaries must receive the release version or CLI negotiation becomes dev-only.
case "$*" in
  *github.com/alibaba/open-guardrail/internal/buildinfo.Version=0.1.0-test*) ;;
  *) echo 'Missing release version' >&2; exit 1 ;;
esac
if test -n "${BUILD_CALLS:-}"; then
  printf '%s/%s\n' "$GOOS" "$GOARCH" >> "$BUILD_CALLS"
fi
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then
    shift
    printf 'test binary\n' > "$1"
    exit 0
  fi
  shift
done
exit 1
EOF
chmod +x bin/go

# Public packaging must retain the main branch's missing-license guard.
rm LICENSE
if PATH="$temporary/bin:$PATH" VERSION=0.1.0-test sh "$root/scripts/package.sh" >output 2>&1; then
  echo "public package without LICENSE succeeded" >&2
  exit 1
fi
grep -q 'Public package requires LICENSE' output
# The non-public preview still works before a license is approved.
git rm --cached -q LICENSE
PATH="$temporary/bin:$PATH" PACKAGE_KIND=preview SOURCE_DATE_EPOCH=1700000000 VERSION=0.1.0-test sh "$root/scripts/package.sh"
printf 'license fixture\n' > LICENSE
if PATH="$temporary/bin:$PATH" VERSION=0.1.0-test sh "$root/scripts/package.sh" >output 2>&1; then
  echo "public package accepted an untracked LICENSE" >&2
  exit 1
fi
grep -q 'must be tracked and nonempty: LICENSE' output
git add LICENSE

# A pre-existing staging directory must never contaminate a new archive.
mkdir -p dist/open-guardrail-0.1.0-test-linux-amd64
printf 'stale\n' > dist/open-guardrail-0.1.0-test-linux-amd64/stale
PATH="$temporary/bin:$PATH" SOURCE_DATE_EPOCH=1700000000 VERSION=0.1.0-test sh "$root/scripts/package.sh"
for platform in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
  archive="dist/open-guardrail-0.1.0-test-$platform.tar.gz"
  tar -tzf "$archive" > contents
  "${PYTHON:-python3}" - "$archive" <<'PYTEST'
import sys, tarfile
with tarfile.open(sys.argv[1], "r:gz") as archive:
    for member in archive:
        assert member.uid == member.gid == 0
        assert member.uname == member.gname == ""
        assert member.mtime == 1700000000
        expected_mode = 0o755 if member.isdir() or member.name.removeprefix("./") in ("open-guardrail", "guardrailctl") else 0o644
        assert member.mode == expected_mode, member.name
PYTEST

  for file in open-guardrail guardrailctl docs/architecture.md cmd/guardrailctl/README.md CONTRIBUTING.md CODE_OF_CONDUCT.md SECURITY.md LICENSE NOTICE third_party/go-modules.tsv third_party/notices.tsv third_party/README.md third_party/licenses/example.com/dependency@v1.0.0/LICENSE deploy/config.example.yaml examples/policies/recovery.yaml integrations/jenkins/adapter.py .agents/skills/open-guardrail-demo/SKILL.md; do
    grep -Fqx "./$file" contents
  done
  if grep -E -q "stale|handoff.json|local-notes.md" contents; then exit 1; fi
  (
    cd dist
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum -c "$(basename "$archive").sha256"
    else
      shasum -a 256 -c "$(basename "$archive").sha256"
    fi
  )
done

# Repeated assembly is byte-identical even when source modes/mtimes differ.
cp -R dist expected-dist
chmod 600 README.md
touch README.md docs/architecture.md
PATH="$temporary/bin:$PATH" SOURCE_DATE_EPOCH=1700000000 VERSION=0.1.0-test sh "$root/scripts/package.sh"
for platform in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
  cmp "expected-dist/open-guardrail-0.1.0-test-$platform.tar.gz" "dist/open-guardrail-0.1.0-test-$platform.tar.gz"
done

# A native job compiles exactly its server and CLI, with no other platform outputs.
mv dist all-platform-dist
PATH="$temporary/bin:$PATH" SOURCE_DATE_EPOCH=1700000000 VERSION=0.1.0-test \
  PACKAGE_PLATFORMS=linux/arm64 BUILD_CALLS="$temporary/single-build-calls" sh "$root/scripts/package.sh"
test "$(wc -l < single-build-calls)" -eq 2
test "$(sort -u single-build-calls)" = linux/arm64
"${PYTHON:-python3}" - <<'PYSINGLE'
from pathlib import Path
assert {p.name for p in Path('dist').iterdir()} == {
    'open-guardrail-0.1.0-test-linux-arm64.tar.gz',
    'open-guardrail-0.1.0-test-linux-arm64.tar.gz.sha256'}
PYSINGLE
# Validate the complete list first: a later invalid target must not compile or
# replace even the first valid target's pre-existing archive.
cp dist/open-guardrail-0.1.0-test-linux-arm64.tar.gz preserved-single.tar.gz
if PATH="$temporary/bin:$PATH" VERSION=0.1.0-test PACKAGE_PLATFORMS='linux/arm64 windows/amd64' \
  BUILD_CALLS="$temporary/invalid-build-calls" sh "$root/scripts/package.sh" >invalid.log 2>&1; then
  echo 'unsupported platform was accepted' >&2
  exit 1
fi
grep -q 'Unsupported package platform' invalid.log
test ! -e invalid-build-calls
cmp preserved-single.tar.gz dist/open-guardrail-0.1.0-test-linux-arm64.tar.gz
mv dist single-platform-dist
mv all-platform-dist dist

# A tracked symlink must not import a local file outside the public assets.
ln -s "$temporary/integrations/jenkins/handoff.json" docs/unsafe-link.md
git add docs/unsafe-link.md
if PATH="$temporary/bin:$PATH" SOURCE_DATE_EPOCH=1700000000 VERSION=0.1.0-test sh "$root/scripts/package.sh" >failure.log 2>&1; then
  echo 'release accepted a symlink' >&2
  exit 1
fi
grep -q 'release asset must be a regular file' failure.log
