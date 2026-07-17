#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test \
  ./cmd/campusosctl \
  ./internal/modules/core/identity/... \
  ./internal/modules/core/emaildelivery/... \
  ./internal/projectaudit \
  ./internal/transport/httpapi \
  -count=1

rg -q 'identity_account_recovery_cases' migrations/000031_v12_identity_recovery_cases.up.sql
rg -q 'BumpAuthVersion' internal/modules/core/identity/service/recovery_service.go
rg -q 'RevokeByUser' internal/modules/core/identity/service/recovery_service.go
rg -q 'interactive terminal input is required' cmd/campusosctl/identity_commands.go
rg -q 'identity.account.recovery.override' internal/transport/httpapi/router.go

echo "identity recovery checks passed"
