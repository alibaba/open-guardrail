#!/bin/sh
set -eu

module_version=$(awk '$1 == "go" { print $2; exit }' go.mod)
docker_version=$(sed -n 's/^ARG GO_VERSION=//p' deploy/Dockerfile)

if [ -z "$module_version" ] || [ -z "$docker_version" ]; then
  echo "Unable to determine Go versions from go.mod and deploy/Dockerfile" >&2
  exit 1
fi

if [ "$module_version" != "$docker_version" ]; then
  echo "Go toolchain mismatch: go.mod=$module_version Dockerfile=$docker_version" >&2
  exit 1
fi
