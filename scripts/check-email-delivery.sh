#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" \
  go test ./internal/modules/core/emaildelivery ./internal/modules/core/identity/... ./internal/server -count=1

if rg -n --glob '*.go' 'log\.(Print|Printf|Println)|fmt\.(Print|Printf|Println)' internal/modules/core/emaildelivery; then
  echo "email delivery check failed: the email Core module must not print transient delivery data" >&2
  exit 1
fi

echo "email delivery check passed: opaque Outbox consumer, stale replay skip, generic provider failures, and local-only Fake provider are covered"
