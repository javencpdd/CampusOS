#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" \
  go test ./internal/modules/core/identity/... -count=1
./scripts/test-v12-identity-migration.sh

echo "identity email check passed: normalization, reserved identifiers, account state port, and migration preflight are covered"
