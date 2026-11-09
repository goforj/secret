#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export GOWORK=off
mkdir -p .artifacts
if ! go -C integration test -tags=integration -count=1 -json ./... > .artifacts/integration-all.json; then
 cat .artifacts/integration-all.json; exit 1
fi
go -C docs run ./report -mode integration
