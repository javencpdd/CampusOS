#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" \
  go test ./internal/modules/core/identity/... ./internal/transport/httpapi ./internal/projectaudit -count=1

if ! rg -q 'identity.verification_required' pkg/response/response.go; then
  echo "identity registration check failed: stable legacy registration error is missing" >&2
  exit 1
fi
if ! rg -q 'CreateVerifiedAccount' internal/modules/core/identity/service/user_service.go; then
  echo "identity registration check failed: verified account creation boundary is missing" >&2
  exit 1
fi

echo "identity registration check passed: verified registration, one-time Ticket rollback, legacy rejection, and API contract tests are covered"
