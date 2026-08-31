#!/usr/bin/env bash
# 用法：在 Git Bash、WSL2 或 Linux 中执行 ./scripts/test-v14-academic-terms-migration.sh。
# 该脚本只创建并删除 campusos_v14_academic_terms_* 隔离数据库，不会回滚开发数据库。
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
drill_db="campusos_v14_academic_terms_$(date -u +%Y%m%d%H%M%S)_$$"
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

scalar() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "$1"
}

run_sql() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -c "$1"
}

run_file() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 <"$1"
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

require_equals "migration count" "$(scalar "SELECT count(*) FROM schema_migrations")" "49"
require_equals "academic_terms exists" "$(scalar "SELECT to_regclass('public.academic_terms') IS NOT NULL")" "t"

run_sql "INSERT INTO academic_terms (id, year, semester, first_week_start, status, is_default, version) VALUES (9144001, 2026, 'fall', '2026-08-31', 'open', TRUE, 1)" >/dev/null
set +e
second_default_output="$(run_sql "INSERT INTO academic_terms (id, year, semester, first_week_start, status, is_default, version) VALUES (9144002, 2027, 'spring', '2027-03-01', 'open', TRUE, 1)" 2>&1)"
second_default_status=$?
set -e
if [[ $second_default_status -eq 0 || "$second_default_output" != *"uk_academic_terms_open_default"* ]]; then
  echo "partial unique default-term index did not reject a second default: $second_default_output" >&2
  exit 1
fi

set +e
weekday_output="$(run_sql "INSERT INTO academic_terms (id, year, semester, first_week_start, status, is_default, version) VALUES (9144003, 2027, 'fall', '2027-09-01', 'open', FALSE, 1)" 2>&1)"
weekday_status=$?
set -e
if [[ $weekday_status -eq 0 || "$weekday_output" != *"chk_academic_terms_first_week_monday"* ]]; then
  echo "Monday check did not reject a non-Monday start: $weekday_output" >&2
  exit 1
fi

# Later v14 tables carry foreign keys to AcademicTerm. Isolated down/up must
# unwind dependent migrations first; production never follows this path.
run_file "$repo_root/migrations/000049_v14_schedule_object_bindings.down.sql" >/dev/null
run_file "$repo_root/migrations/000047_v14_personal_documents.down.sql" >/dev/null
run_file "$repo_root/migrations/000046_v14_schedule_term_references.down.sql" >/dev/null
run_file "$repo_root/migrations/000045_v14_storage_objects.down.sql" >/dev/null
run_file "$repo_root/migrations/000044_v14_academic_terms.down.sql" >/dev/null
require_equals "academic_terms after down" "$(scalar "SELECT to_regclass('public.academic_terms') IS NULL")" "t"
run_file "$repo_root/migrations/000044_v14_academic_terms.up.sql" >/dev/null
require_equals "academic_terms after re-up" "$(scalar "SELECT to_regclass('public.academic_terms') IS NOT NULL")" "t"
run_file "$repo_root/migrations/000045_v14_storage_objects.up.sql" >/dev/null
run_file "$repo_root/migrations/000046_v14_schedule_term_references.up.sql" >/dev/null
run_file "$repo_root/migrations/000047_v14_personal_documents.up.sql" >/dev/null
run_file "$repo_root/migrations/000049_v14_schedule_object_bindings.up.sql" >/dev/null

if ! (
  cd "$repo_root"
  POSTGRES_CONTAINER="$POSTGRES_CONTAINER" DB_NAME="$drill_db" ./scripts/database-check.sh all
) >"$migration_log" 2>&1; then
  echo "Database contract check failed; recent output follows:" >&2
  tail -n 80 "$migration_log" >&2
  exit 1
fi

echo "v0.14 academic-term empty migration and 000044 down/up drill passed (PostgreSQL container: $POSTGRES_CONTAINER)"
