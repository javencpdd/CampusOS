#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test \
  ./internal/modules/core/identity/... \
  ./pkg/auth \
  ./pkg/middleware \
  ./internal/transport/httpapi \
  -count=1

grep -q 'refresh_token_digest' migrations/000030_v12_identity_sessions.up.sql
grep -q 'refresh_token = NULL' migrations/000030_v12_identity_sessions.up.sql
grep -q 'refresh_reuse_detected' internal/modules/core/identity/service/session_service.go
grep -q 'AUTH_REFRESH_BODY_COMPAT' pkg/config/config.go
grep -q 'HttpOnly' internal/modules/core/identity/handler/user_handler.go

echo "identity session checks passed"
