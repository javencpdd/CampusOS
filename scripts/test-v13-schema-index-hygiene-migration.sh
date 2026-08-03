#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -f "$repo_root/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$repo_root/.env"
  set +a
fi

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
DB_USER="${DB_USER:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
drill_db="campusos_v13_index_hygiene_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..40}_*.sql; do
  ln -s "$migration" "$migration_dir/$(basename "$migration")"
done
(
  cd "$repo_root"
  PSQL_MODE=docker DB_NAME="$drill_db" MIGRATIONS_DIR="$migration_dir" \
    ./scripts/migrate.sh up >/dev/null
)

run_file() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 <"$1"
}

scalar() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "$1"
}

redundant_indexes="'idx_notifications_user_id',
  'idx_plugin_permissions_plugin',
  'idx_plugin_records_search',
  'idx_posts_thread_id',
  'idx_role_permissions_role',
  'idx_sessions_user_id',
  'idx_threads_author_id',
  'idx_user_roles_user_id',
  'idx_webhook_deliveries_status'"

covering_indexes="'idx_notifications_user_read',
  'uk_plugin_permissions',
  'idx_plugin_records_owner_collection_updated',
  'idx_posts_thread_floor',
  'uk_role_permissions_active',
  'idx_sessions_user_active',
  'idx_threads_author_visibility_v10',
  'idx_user_roles_scope_lookup',
  'idx_webhook_deliveries_status_created'"

test "$(scalar "SELECT count(*) FROM pg_class WHERE relkind='i' AND relname IN ($redundant_indexes)")" = "9"
test "$(scalar "SELECT count(*) FROM pg_class WHERE relkind='i' AND relname IN ($covering_indexes)")" = "9"

set +e
pre_migration_hygiene="$(run_file "$repo_root/scripts/migration-hygiene.sql" 2>&1)"
pre_migration_status=$?
set -e
if [[ $pre_migration_status -eq 0 || "$pre_migration_hygiene" != *"redundant_btree_coverage"* ]]; then
  echo "schema hygiene checker did not reject the pre-000041 redundant indexes" >&2
  exit 1
fi

run_file "$repo_root/migrations/000041_v13_schema_index_hygiene.up.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM pg_class WHERE relkind='i' AND relname IN ($redundant_indexes)")" = "0"
test "$(scalar "SELECT count(*) FROM pg_class WHERE relkind='i' AND relname IN ($covering_indexes)")" = "9"
run_file "$repo_root/scripts/migration-hygiene.sql" >/dev/null
run_file "$repo_root/scripts/database-audit.sql" >/dev/null
run_file "$repo_root/scripts/schema-contract.sql" >/dev/null

docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
CREATE INDEX test_duplicate_notification_user_read
    ON notifications(user_id, is_read) WHERE deleted_at IS NULL;
ALTER TABLE users
    ADD CONSTRAINT test_duplicate_users_status
    CHECK (status IN ('active', 'suspended', 'deactivated'));
SQL
set +e
duplicate_hygiene="$(run_file "$repo_root/scripts/migration-hygiene.sql" 2>&1)"
duplicate_hygiene_status=$?
set -e
if [[ $duplicate_hygiene_status -eq 0 ||
      "$duplicate_hygiene" != *"duplicate_index"* ||
      "$duplicate_hygiene" != *"duplicate_constraint"* ]]; then
  echo "schema hygiene checker did not reject duplicate index and constraint fixtures" >&2
  exit 1
fi
docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
DROP INDEX test_duplicate_notification_user_read;
ALTER TABLE users DROP CONSTRAINT test_duplicate_users_status;
SQL
run_file "$repo_root/scripts/migration-hygiene.sql" >/dev/null

run_file "$repo_root/migrations/000041_v13_schema_index_hygiene.down.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM pg_class WHERE relkind='i' AND relname IN ($redundant_indexes)")" = "9"

run_file "$repo_root/migrations/000041_v13_schema_index_hygiene.up.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM pg_class WHERE relkind='i' AND relname IN ($redundant_indexes)")" = "0"
run_file "$repo_root/scripts/migration-hygiene.sql" >/dev/null

echo "v13 schema index hygiene migration and up/down/up drill passed"
