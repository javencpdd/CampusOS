#!/usr/bin/env bash
# 用法：POSTGRES_CONTAINER=campusos-dev-postgres-1 ./scripts/test-v1-database-baseline.sh
# 在专用临时数据库中验证 v1 零建库、参考数据、逐步回滚、重置和 checksum 漂移门禁。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
DB_USER="${DB_USER:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-campusos_dev}"
drill_db="${CAMPUSOS_V1_DRILL_DB:-campusos_v1_database_baseline_drill}"
temp_migrations=""

cleanup() {
  if [[ -n "$temp_migrations" && -d "$temp_migrations" ]]; then
    rm -rf -- "$temp_migrations"
  fi
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d postgres -q -c "DROP DATABASE IF EXISTS $drill_db WITH (FORCE);" >/dev/null 2>&1 || true
}
trap cleanup EXIT

psql_scalar() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -qAtc "$1"
}

require_equals() {
  local label="$1" actual="$2" expected="$3"
  if [[ "$actual" != "$expected" ]]; then
    echo "$label: expected '$expected', got '$actual'" >&2
    exit 1
  fi
}

run_migrate() {
  CAMPUSOS_SKIP_DOTENV=true \
  PSQL_MODE=docker \
  POSTGRES_CONTAINER="$POSTGRES_CONTAINER" \
  DB_USER="$DB_USER" \
  DB_PASSWORD="$DB_PASSWORD" \
  DB_NAME="$drill_db" \
  CAMPUSOS_ENV=test \
  CAMPUSOS_RESET_CONFIRM="$drill_db" \
  ./scripts/migrate.sh "$@"
}

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d postgres -v ON_ERROR_STOP=1 \
  -c "DROP DATABASE IF EXISTS $drill_db WITH (FORCE);" \
  -c "CREATE DATABASE $drill_db;" >/dev/null

run_migrate reset >/dev/null
run_migrate check >/dev/null

require_equals "migration count" "$(psql_scalar "SELECT count(*) FROM schema_migrations;")" "3"
require_equals "public table count" "$(psql_scalar "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';")" "86"
require_equals "legacy permission table removed" "$(psql_scalar "SELECT to_regclass('public.permissions') IS NULL;")" "t"
require_equals "raw session secret columns removed" "$(psql_scalar "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='sessions' AND column_name IN ('refresh_token','ip_address');")" "0"
require_equals "refresh digest required" "$(psql_scalar "SELECT is_nullable FROM information_schema.columns WHERE table_schema='public' AND table_name='sessions' AND column_name='refresh_token_digest';")" "NO"
require_equals "user rows are not seeded" "$(psql_scalar "SELECT count(*) FROM users;")" "0"
require_equals "account rows are not seeded" "$(psql_scalar "SELECT count(*) FROM accounts;")" "0"
require_equals "admin admission rows are not seeded" "$(psql_scalar "SELECT count(*) FROM identity_admin_accounts;")" "0"
require_equals "system role count" "$(psql_scalar "SELECT count(*) FROM roles WHERE is_system;")" "4"
require_equals "permission definition count" "$(psql_scalar "SELECT count(*) FROM permission_definitions;")" "76"
require_equals "admin permission count" "$(psql_scalar "SELECT count(*) FROM role_permissions WHERE role_id=1 AND deleted_at IS NULL;")" "76"
require_equals "timestamp normalization" "$(psql_scalar "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND data_type='timestamp without time zone';")" "0"
require_equals "v1 plugin foundation" "$(psql_scalar "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('plugin_publishers','plugin_versions','plugin_capability_declarations','plugin_admin_grants','plugin_user_consents','plugin_delegations','plugin_secret_values','plugin_authorization_decisions');")" "8"
require_equals "foreign-key leading index coverage" "$(psql_scalar "SELECT count(*) FROM pg_constraint fk WHERE fk.contype='f' AND fk.connamespace='public'::regnamespace AND NOT EXISTS (SELECT 1 FROM pg_index idx WHERE idx.indrelid=fk.conrelid AND idx.indisvalid AND (idx.indkey::smallint[])[0:cardinality(fk.conkey)-1] @> fk.conkey);")" "0"

POSTGRES_CONTAINER="$POSTGRES_CONTAINER" DB_USER="$DB_USER" DB_PASSWORD="$DB_PASSWORD" DB_NAME="$drill_db" \
  ./scripts/database-check.sh all >/dev/null

run_migrate down >/dev/null
require_equals "reference-data rollback migration count" "$(psql_scalar "SELECT count(*) FROM schema_migrations;")" "2"
require_equals "reference-data rollback permission count" "$(psql_scalar "SELECT count(*) FROM permission_definitions;")" "0"
run_migrate up >/dev/null

temp_migrations="$(mktemp -d)"
cp migrations/*.sql "$temp_migrations/"
printf '\n-- intentional checksum drift\n' >>"$temp_migrations/000003_v1_reference_data.up.sql"
if CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$POSTGRES_CONTAINER" \
   DB_USER="$DB_USER" DB_PASSWORD="$DB_PASSWORD" DB_NAME="$drill_db" MIGRATIONS_DIR="$temp_migrations" \
   ./scripts/migrate.sh check >/dev/null 2>&1; then
  echo "checksum drift was not rejected" >&2
  exit 1
fi
rm -rf -- "$temp_migrations"
temp_migrations=""

run_migrate down >/dev/null
run_migrate down >/dev/null
run_migrate down >/dev/null
require_equals "full rollback migration count" "$(psql_scalar "SELECT count(*) FROM schema_migrations;")" "0"
require_equals "full rollback keeps only migration metadata" "$(psql_scalar "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';")" "2"

run_migrate up >/dev/null
require_equals "up/down/up migration count" "$(psql_scalar "SELECT count(*) FROM schema_migrations;")" "3"
require_equals "up/down/up public table count" "$(psql_scalar "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';")" "86"

echo "v1 clean database baseline reset/checksum/up-down-up drill passed (PostgreSQL container: $POSTGRES_CONTAINER)"
