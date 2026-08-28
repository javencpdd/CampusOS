#!/usr/bin/env bash
# 用法：在 Git Bash、WSL2 或 Linux 中运行；仅创建和销毁隔离数据库，验证 v14 课表对象绑定 migration。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
if ! docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER" && docker ps --format '{{.Names}}' | grep -qx 'campusos-dev-postgres-1'; then
  POSTGRES_CONTAINER='campusos-dev-postgres-1'
fi
DB_USER="${DB_USER:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
drill_db="campusos_v14_schedule_bindings_$(date -u +%Y%m%d%H%M%S)_$$"
log_file="$(mktemp)"
cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -f "$log_file"
}
trap cleanup EXIT
docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER" || { echo "PostgreSQL container '$POSTGRES_CONTAINER' is not running." >&2; exit 1; }
docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
if ! (cd "$repo_root" && PSQL_MODE=docker POSTGRES_CONTAINER="$POSTGRES_CONTAINER" DB_NAME="$drill_db" ./scripts/migrate.sh up) >"$log_file" 2>&1; then
  tail -n 80 "$log_file" >&2; exit 1
fi
scalar() { docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "$1"; }
run_file() { docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 <"$1"; }
[[ "$(scalar "SELECT count(*) FROM schema_migrations")" == "49" ]] || { echo "migration count is not 49" >&2; exit 1; }
[[ "$(scalar "SELECT to_regclass('public.user_schedule_preferences') IS NOT NULL")" == "t" ]] || { echo "schedule preference table is missing" >&2; exit 1; }
[[ "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='user_schedule_terms' AND column_name IN ('current_object_id','first_week_start','version')")" == "3" ]] || { echo "schedule object binding columns are missing" >&2; exit 1; }
run_file "$repo_root/migrations/000049_v14_schedule_object_bindings.down.sql" >/dev/null
[[ "$(scalar "SELECT to_regclass('public.user_schedule_preferences') IS NULL")" == "t" ]] || { echo "schedule preferences remained after down" >&2; exit 1; }
run_file "$repo_root/migrations/000049_v14_schedule_object_bindings.up.sql" >/dev/null
[[ "$(scalar "SELECT to_regclass('public.user_schedule_preferences') IS NOT NULL")" == "t" ]] || { echo "schedule preferences missing after re-up" >&2; exit 1; }
echo "v14 schedule-object-binding empty migration and 000049 down/up drill passed (PostgreSQL container: $POSTGRES_CONTAINER)"
