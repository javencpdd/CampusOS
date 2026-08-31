#!/usr/bin/env bash
# 用法：在 Git Bash、WSL2 或 Linux 中执行 ./scripts/test-v14-storage-objects-migration.sh。
# 脚本只创建并删除 campusos_v14_storage_objects_* 隔离数据库，不会回滚开发数据库。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-campusos-postgres}"
if ! docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER" && docker ps --format '{{.Names}}' | grep -qx 'campusos-dev-postgres-1'; then POSTGRES_CONTAINER='campusos-dev-postgres-1'; fi
DB_USER="${DB_USER:-campusos}"
DB_PASSWORD="${DB_PASSWORD:-${POSTGRES_PASSWORD:-campusos_dev}}"
drill_db="campusos_v14_storage_objects_$(date -u +%Y%m%d%H%M%S)_$$"
migration_log="$(mktemp)"
cleanup() { docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true; rm -f "$migration_log"; }
trap cleanup EXIT
if ! docker ps --format '{{.Names}}' | grep -qx "$POSTGRES_CONTAINER"; then echo "PostgreSQL container '$POSTGRES_CONTAINER' is not running." >&2; exit 1; fi
scalar() { docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "$1"; }
run_file() { docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 <"$1"; }
require_equals() { [[ "$2" == "$3" ]] || { echo "$1 expected '$3', got '$2'." >&2; exit 1; }; }

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
if ! (cd "$repo_root" && PSQL_MODE=docker POSTGRES_CONTAINER="$POSTGRES_CONTAINER" DB_NAME="$drill_db" ./scripts/migrate.sh up) >"$migration_log" 2>&1; then tail -n 80 "$migration_log" >&2; exit 1; fi
require_equals "migration count" "$(scalar "SELECT count(*) FROM schema_migrations")" "49"
require_equals "storage objects table" "$(scalar "SELECT to_regclass('public.storage_objects') IS NOT NULL")" "t"
user_id="$(scalar "SELECT id FROM users ORDER BY id LIMIT 1")"
scalar "INSERT INTO user_storage_accounts (user_id, used_bytes, reserved_bytes, version) VALUES ($user_id, 0, 0, 1); INSERT INTO storage_objects (id, owner_user_id, namespace, purpose, provider, storage_key, original_name, mime_type, size_bytes, sha256, status, version) VALUES (9145001, $user_id, 'documents', 'document.source', 'local', '9145001.bin', 'term.txt', 'text/plain', 1, repeat('a', 64), 'ready', 1); INSERT INTO user_storage_reservations (id, user_id, object_id, reserved_bytes, status, expires_at) VALUES (9145001, $user_id, 9145001, 1, 'committed', NOW() + INTERVAL '1 minute'); SELECT count(*) FROM storage_objects" >/dev/null
set +e
duplicate_output="$(scalar "INSERT INTO storage_objects (id, owner_user_id, namespace, purpose, provider, storage_key, original_name, mime_type, size_bytes, sha256, status, version) VALUES (9145002, $user_id, 'documents', 'document.source', 'local', '9145001.bin', 'copy.txt', 'text/plain', 1, repeat('b', 64), 'ready', 1)" 2>&1)"; duplicate_status=$?
set -e
if [[ $duplicate_status -eq 0 || "$duplicate_output" != *"uk_storage_objects_provider_key"* ]]; then echo "provider/storage key uniqueness was not enforced: $duplicate_output" >&2; exit 1; fi
run_file "$repo_root/migrations/000049_v14_schedule_object_bindings.down.sql" >/dev/null
run_file "$repo_root/migrations/000047_v14_personal_documents.down.sql" >/dev/null
run_file "$repo_root/migrations/000045_v14_storage_objects.down.sql" >/dev/null
require_equals "storage objects after down" "$(scalar "SELECT to_regclass('public.storage_objects') IS NULL")" "t"
run_file "$repo_root/migrations/000045_v14_storage_objects.up.sql" >/dev/null
require_equals "storage objects after re-up" "$(scalar "SELECT to_regclass('public.storage_objects') IS NOT NULL")" "t"
run_file "$repo_root/migrations/000047_v14_personal_documents.up.sql" >/dev/null
run_file "$repo_root/migrations/000049_v14_schedule_object_bindings.up.sql" >/dev/null
if ! (cd "$repo_root" && POSTGRES_CONTAINER="$POSTGRES_CONTAINER" DB_NAME="$drill_db" ./scripts/database-check.sh all) >"$migration_log" 2>&1; then tail -n 80 "$migration_log" >&2; exit 1; fi
echo "v14 storage-object empty migration and 000045 down/up drill passed (PostgreSQL container: $POSTGRES_CONTAINER)"
