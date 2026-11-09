#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export GOWORK=off
mode="${1:-test}"
while IFS= read -r module; do
 case "$mode" in
 test) go -C "$module" test ./... ;;
 race) go -C "$module" test -race ./... ;;
 vet) go -C "$module" vet ./... ;;
 tidy) go -C "$module" mod tidy ;;
 *) echo "unknown module operation" >&2; exit 1 ;;
 esac
done < scripts/module-manifest.txt
