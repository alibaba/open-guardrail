#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT
trap 'exit 1' HUP INT TERM

mkdir -p "$temporary/scripts"
mkdir -p "$temporary/third_party"
cp "$root/scripts/check-public-release.sh" "$root/scripts/package_archive.py" "$temporary/scripts/"

cd "$temporary"
git init -q
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
mkdir -p deploy
printf 'config\n' > deploy/config.example.yaml
printf 'module open-guardrail\n' > go.mod
printf 'security\n' > SECURITY.md
printf 'conduct\n' > CODE_OF_CONDUCT.md
printf 'module|version|scope|license|status\nexample.com/dependency|v1.0.0|runtime|MIT|allowed\n' > third_party/go-modules.tsv
printf 'module example.com/open-guardrail\n' > go.mod
if VERSION=0.1.0 sh scripts/check-public-release.sh >output 2>&1; then
  echo "missing LICENSE passed the public release gate" >&2
  exit 1
fi
grep -q 'LICENSE is missing' output

printf 'license\n' > LICENSE
printf 'module open-guardrail\n' > go.mod
if VERSION=0.1.0 sh scripts/check-public-release.sh >output 2>&1; then
  echo "temporary module path passed the public release gate" >&2
  exit 1
fi
grep -q 'replace the temporary Go module path' output

printf 'module example.com/open-guardrail\n' > go.mod
git add .
VERSION=0.1.0 sh scripts/check-public-release.sh

for asset in LICENSE NOTICE SECURITY.md CODE_OF_CONDUCT.md third_party/go-modules.tsv third_party/notices.tsv third_party/README.md; do
  git rm --cached -q "$asset"
  if VERSION=0.1.0 sh scripts/check-public-release.sh >output 2>&1; then
    echo "untracked public asset passed: $asset" >&2
    exit 1
  fi
  grep -q 'must be tracked and nonempty' output
  git add "$asset"
done

asset=third_party/licenses/example.com/dependency@v1.0.0/LICENSE
git rm --cached -q "$asset"
if VERSION=0.1.0 sh scripts/check-public-release.sh >output 2>&1; then
  echo "notice inventory accepted an untracked original license" >&2
  exit 1
fi
grep -q 'public notice file must be tracked and nonempty' output
git add "$asset"
