#!/usr/bin/env bash
# 用法：在 Git Bash、WSL2 或 Linux 中执行 ./scripts/test-v14-g0-baseline-migration.sh。
# 本脚本只创建并删除名称带 v14_g0 的隔离数据库；不会执行开发数据库的 down/reset。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -f "$repo_root/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$repo_root/.env"
  set +a
fi

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
if ! docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER" && \
  docker ps --format '{{.Names}}' | grep -qx 'campusos-dev-postgres-1'; then
  POSTGRES_CONTAINER='campusos-dev-postgres-1'
fi
DB_USER="${DB_USER:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
drill_db="campusos_v14_g0_$(date -u +%Y%m%d%H%M%S)_$$"
migration_log="$(mktemp)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -f "$migration_log"
}
trap cleanup EXIT

if ! docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER"; then
  echo "PostgreSQL container '$POSTGRES_CONTAINER' is not running." >&2
  exit 1
fi

run_file() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 <"$1"
}

scalar() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "$1"
}

require_equals() {
  local label="$1"
  local actual="$2"
  local expected="$3"
  if [[ "$actual" != "$expected" ]]; then
    echo "$label expected '$expected', got '$actual'." >&2
    exit 1
  fi
}

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  createdb -U "$DB_USER" "$drill_db"

if ! (
  cd "$repo_root"
  PSQL_MODE=docker POSTGRES_CONTAINER="$POSTGRES_CONTAINER" DB_NAME="$drill_db" ./scripts/migrate.sh up
) >"$migration_log" 2>&1; then
  echo "Empty-database migration failed; recent output follows:" >&2
  tail -n 80 "$migration_log" >&2
  exit 1
fi

require_equals "migration count" "$(scalar "SELECT count(*) FROM schema_migrations")" "43"
require_equals "posts.parent_floor_number after up" "$(scalar "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='posts' AND column_name='parent_floor_number')")" "t"

run_file "$repo_root/migrations/000043_v13_post_parent_floor.down.sql" >/dev/null
require_equals "posts.parent_floor_number after down" "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='posts' AND column_name='parent_floor_number'")" "0"

run_file "$repo_root/migrations/000043_v13_post_parent_floor.up.sql" >/dev/null
require_equals "posts.parent_floor_number after re-up" "$(scalar "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='posts' AND column_name='parent_floor_number')")" "t"

if ! (
  cd "$repo_root"
  POSTGRES_CONTAINER="$POSTGRES_CONTAINER" DB_NAME="$drill_db" ./scripts/database-check.sh all
) >"$migration_log" 2>&1; then
  echo "Database contract check failed; recent output follows:" >&2
  tail -n 80 "$migration_log" >&2
  exit 1
fi

echo "v14 G0 empty migration and 000043 down/up drill passed (PostgreSQL container: $POSTGRES_CONTAINER)"
