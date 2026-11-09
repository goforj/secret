#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export GOWORK=off
while IFS= read -r module; do
 (cd "$module" && govulncheck ./...)
done < scripts/module-manifest.txt
