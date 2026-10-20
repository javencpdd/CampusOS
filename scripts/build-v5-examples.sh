#!/usr/bin/env bash
# Explicit PDF author build, never executed by validation or installation.
set -euo pipefail
cd "$(dirname "$0")/.."
export GOCACHE="$PWD/.cache/v12-g0/go-cache"
pnpm --dir plugins/campusos.pdf-viewer/frontend build
node scripts/update-v5-example-digests.mjs
printf '%s\n' 'PDF UI built. Echo now uses the Container HTTP stdio SDK; build it explicitly with:'
printf '%s\n' '  bash scripts/build-v5-chat-echo.sh --out <new-exclusive-directory>'
printf '%s\n' 'The Echo build emits an unpublished image-digest template; image build, signing and installation are separate explicit steps.'
