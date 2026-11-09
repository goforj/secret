#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export GOWORK=off
snapshot="$(mktemp -d "${TMPDIR:-/tmp}/secret-generated.XXXXXX")"
trap 'rm -rf "$snapshot"' EXIT
cp README.md "$snapshot/README.md"
cp -R docs/assets/badges "$snapshot/badges"
go -C docs run ./readme
go -C docs run ./readme
diff -u "$snapshot/README.md" README.md
diff -ru "$snapshot/badges" docs/assets/badges
