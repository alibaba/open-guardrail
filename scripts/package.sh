#!/bin/sh
set -eu
# Split the allowlisted platform list on whitespace without pathname expansion.
set -f
export GOWORK=off

# Invoke from the repository root. Packages stay local; this script never uploads.
script_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
python_command=${PYTHON:-python3}
go_command=${GO:-go}
version=${VERSION:-dev}
package_kind=${PACKAGE_KIND:-public}
case "$version" in
  *[!a-zA-Z0-9._-]*|'') echo "Invalid VERSION" >&2; exit 1 ;;
esac
case "$package_kind" in
  public) test -f LICENSE || { echo "Public package requires LICENSE" >&2; exit 1; } ;;
  preview) ;;
  *) echo "Invalid PACKAGE_KIND" >&2; exit 1 ;;
esac
platforms=${PACKAGE_PLATFORMS:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64}
for platform in $platforms; do
  case "$platform" in
    linux/amd64|linux/arm64|darwin/amd64|darwin/arm64) ;;
    *) echo "Unsupported package platform: $platform" >&2; exit 1 ;;
  esac
done
public_flag=
if test "$package_kind" = public; then
  "$python_command" "$script_root/scripts/package_archive.py" --check-public-assets
  public_flag=--public
fi
commit=$(git rev-parse HEAD 2>/dev/null || echo unknown)
epoch=${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || echo 0)}
mkdir -p dist
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT
trap 'exit 1' HUP INT TERM
for platform in $platforms; do
  target_os=${platform%/*}
  target_arch=${platform#*/}
  name="open-guardrail-${version}-${target_os}-${target_arch}"
  target="$staging/$name"
  mkdir -p "$target"
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -buildvcs=false \
    -ldflags "-X github.com/alibaba/open-guardrail/internal/buildinfo.Version=$version -X github.com/alibaba/open-guardrail/internal/buildinfo.Commit=$commit" \
    -o "$target/open-guardrail" ./cmd/open-guardrail
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -buildvcs=false \
    -ldflags "-X github.com/alibaba/open-guardrail/internal/buildinfo.Version=$version -X github.com/alibaba/open-guardrail/internal/buildinfo.Commit=$commit" \
    -o "$target/guardrailctl" ./cmd/guardrailctl
  "$python_command" "$script_root/scripts/package_archive.py" --binaries "$target" \
    --output "dist/$name.tar.gz" --epoch "$epoch" $public_flag
  (
    cd dist
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$name.tar.gz" > "$name.tar.gz.sha256"
    else
      shasum -a 256 "$name.tar.gz" > "$name.tar.gz.sha256"
    fi
  )
done
