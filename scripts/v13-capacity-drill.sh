#!/usr/bin/env bash
set -euo pipefail

# Run capacity comparisons against a disposable database and API process. This
# keeps historical dead-letter records and active developer sessions out of
# release evidence while exercising the same migrations and HTTP contracts.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-${POSTGRES_PORT:-5432}}"
DB_USER="${DB_USER:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
API_PORT="${V13_CAPACITY_API_PORT:-18081}"
# The reliability summary is deliberately limited to 120 reads per actor each
# minute. The drill performs three warm-up reads plus two captures, so keeping
# each capture at 50 samples or fewer exercises the real limit without
# weakening it for release evidence.
SAMPLES="${V13_CAPACITY_SAMPLES:-40}"
DRILL_EMAIL="${V13_CAPACITY_ADMIN_EMAIL:-admin@campusos.local}"
DRILL_PASSWORD="${V13_CAPACITY_ADMIN_PASSWORD:-Admin@123456}"
drill_db="campusos_v13_capacity_$(date -u +%Y%m%d%H%M%S)_$$"
work_dir="$(mktemp -d)"
api_binary="$work_dir/campusos-api"
api_log="$work_dir/api.log"
authorization_file="$work_dir/authorization"
api_pid=""

if ! [[ "$SAMPLES" =~ ^[0-9]+$ ]] || (( SAMPLES < 20 || SAMPLES > 50 )); then
  echo "V13_CAPACITY_SAMPLES must be an integer between 20 and 50 for the isolated drill" >&2
  exit 2
fi

cleanup() {
  stop_api
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$work_dir"
}
trap cleanup EXIT

stop_api() {
  if [[ -z "$api_pid" ]]; then
    return
  fi
  kill "$api_pid" 2>/dev/null || true
  wait "$api_pid" 2>/dev/null || true
  api_pid=""
}

start_api() {
  local phase="$1"
  api_log="$work_dir/api-$phase.log"
  : >"$api_log"

  DATABASE_DSN="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${drill_db}?sslmode=disable" \
    SERVER_HOST=127.0.0.1 \
    SERVER_PORT="$API_PORT" \
    CAMPUSOS_ENV=development \
    AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN=true \
    AUTH_BOOTSTRAP_ADMIN_SECRET= \
    AUTH_CHALLENGE_ACTIVE_KEY_ID=capacity-v1 \
    AUTH_CHALLENGE_HMAC_KEYS=capacity-v1:campusos-capacity-challenge-key \
    AUTH_CHALLENGE_IP_HASH_SECRET=campusos-capacity-challenge-ip-hash-key \
    AUTH_SESSION_IP_HASH_SECRET=campusos-capacity-session-ip-hash-key \
    AUTH_MFA_ACTIVE_KEY_ID=capacity-v1 \
    AUTH_MFA_ENCRYPTION_KEYS=capacity-v1:campusos-capacity-mfa-envelope-key \
    AUTH_MFA_ISSUER='CampusOS Capacity Drill' \
    JWT_SECRET=campusos-capacity-drill-jwt-secret \
    EMAIL_PROVIDER=fake \
    HOST_API_ENABLED=false \
    OBSERVABILITY_PROMETHEUS_ENABLED=false \
    REDIS_ENABLED=false \
    PLUGIN_DATA_DIR="$work_dir/plugin_data" \
    "$api_binary" >"$api_log" 2>&1 &
  api_pid="$!"

  for attempt in $(seq 1 80); do
    if curl -fsS "http://127.0.0.1:$API_PORT/api/v1/health" >/dev/null 2>&1; then
      return
    fi
    if ! kill -0 "$api_pid" 2>/dev/null; then
      sed -n '1,220p' "$api_log" >&2
      exit 1
    fi
    if (( attempt == 80 )); then
      echo "isolated CampusOS API did not become ready during $phase" >&2
      sed -n '1,220p' "$api_log" >&2
      exit 1
    fi
    sleep 0.25
  done
}

capture_phase() {
  local phase="$1"
  local output="$work_dir/$phase.json"

  echo "==> starting fresh isolated API for $phase"
  start_api "$phase"
  GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go run ./cmd/campusos-capacity capture \
    --base-url "http://127.0.0.1:$API_PORT" \
    --samples 3 \
    --include-admin-observability \
    --authorization-file "$authorization_file" \
    --output "$work_dir/$phase-warmup.json"
  GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go run ./cmd/campusos-capacity capture \
    --base-url "http://127.0.0.1:$API_PORT" \
    --samples "$SAMPLES" \
    --include-admin-observability \
    --authorization-file "$authorization_file" \
    --output "$output"
  stop_api
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "v0.13 capacity drill requires $1" >&2
    exit 127
  }
}

require_command docker
require_command curl
require_command go
require_command node

if ! docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER"; then
  echo "PostgreSQL container '$POSTGRES_CONTAINER' is not running" >&2
  exit 127
fi

echo "==> creating isolated v0.13 capacity database"
docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
PSQL_MODE=docker DB_NAME="$drill_db" DB_PASSWORD="$DB_PASSWORD" POSTGRES_CONTAINER="$POSTGRES_CONTAINER" \
  ./scripts/migrate.sh up >"$work_dir/migrations.log"

echo "==> building isolated CampusOS API"
GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go build -o "$api_binary" ./cmd/server/main.go

echo "==> issuing short-lived local capacity authorization"
start_api authorization
CAPACITY_API_URL="http://127.0.0.1:$API_PORT/api/v1" \
  CAPACITY_AUTH_FILE="$authorization_file" \
  CAPACITY_ADMIN_EMAIL="$DRILL_EMAIL" \
  CAPACITY_ADMIN_PASSWORD="$DRILL_PASSWORD" \
  node --input-type=module <<'NODE'
import { chmodSync, writeFileSync } from 'node:fs'

const response = await fetch(`${process.env.CAPACITY_API_URL}/auth/admin/login`, {
  method: 'POST',
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify({ email: process.env.CAPACITY_ADMIN_EMAIL, password: process.env.CAPACITY_ADMIN_PASSWORD }),
})
const payload = await response.json()
const token = payload?.data?.access_token
if (!response.ok || !token || payload?.data?.mfa_required) {
  throw new Error('isolated capacity administrator login did not return a direct access token')
}
writeFileSync(process.env.CAPACITY_AUTH_FILE, `Bearer ${token}\n`, { mode: 0o600 })
chmodSync(process.env.CAPACITY_AUTH_FILE, 0o600)
NODE
stop_api

echo "==> capturing baseline and candidate in independent fresh API processes"
capture_phase baseline
capture_phase candidate
GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go run ./cmd/campusos-capacity compare \
  --baseline "$work_dir/baseline.json" \
  --candidate "$work_dir/candidate.json" \
  --budget docs/项目计划v0.13/evidence/v0.13-capacity-drill-budget.json

echo "v0.13 isolated capacity drill passed: clean database, fixed public/admin reads, runtime and reliability budgets"
