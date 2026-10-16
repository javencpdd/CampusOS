#!/usr/bin/env bash
# Explicit author build, never executed by package validation or installation.
set -euo pipefail
cd "$(dirname "$0")/.."
export GOCACHE="$PWD/.cache/v12-g0/go-cache"
pnpm --dir plugins/campusos.pdf-viewer/frontend build
mkdir -p plugins/campusos.chat-echo/dist
GOOS=wasip1 GOARCH=wasm go build -trimpath -buildvcs=false -o plugins/campusos.chat-echo/dist/provider.wasm ./plugins/campusos.chat-echo
node scripts/update-v5-example-digests.mjs
