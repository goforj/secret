#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export GOWORK=off
mkdir -p .artifacts
for module in . secrettest driver/awssecret driver/azuresecret driver/filesecret driver/gcpsecret driver/vaultsecret; do
 name="${module//\//-}"
 if ! go -C "$module" test -count=1 -json -covermode=atomic -coverprofile="$PWD/.artifacts/unit-$name.cover" ./... > ".artifacts/unit-$name.json"; then
  cat ".artifacts/unit-$name.json"; exit 1
 fi
done
go -C docs run ./report -mode unit
