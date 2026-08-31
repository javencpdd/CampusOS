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
drill_db="campusos_v12_mutual_aid_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..33}_*.sql; do
  ln -s "$migration" "$migration_dir/$(basename "$migration")"
done
(
  cd "$repo_root"
  PSQL_MODE=docker DB_NAME="$drill_db" MIGRATIONS_DIR="$migration_dir" ./scripts/migrate.sh up >/dev/null
)

run_sql() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 "$@"
}

run_file() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 <"$1"
}

scalar() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "$1"
}

run_file "$repo_root/migrations/000034_v12_mutual_aid.up.sql" >/dev/null

run_sql >/dev/null <<'SQL'
INSERT INTO threads (id, thread_type, title, content, content_format, author_id, author_name, category_id, status)
SELECT 9934001, 'mutual_aid', 'Need campus help', 'detail guard fixture', 'markdown', u.id, 'v12', c.id, 'published'
FROM users u CROSS JOIN categories c
WHERE u.deleted_at IS NULL AND c.deleted_at IS NULL AND c.node_kind = 'board'
LIMIT 1;
INSERT INTO threads (id, thread_type, title, content, content_format, author_id, author_name, category_id, status)
SELECT 9934002, 'discussion', 'Ordinary discussion', 'detail guard fixture', 'markdown', u.id, 'v12', c.id, 'published'
FROM users u CROSS JOIN categories c
WHERE u.deleted_at IS NULL AND c.deleted_at IS NULL AND c.node_kind = 'board'
LIMIT 1;
INSERT INTO threads (id, thread_type, title, content, content_format, author_id, author_name, category_id, status)
SELECT 9934003, 'mutual_aid', 'Invalid state fixture', 'detail guard fixture', 'markdown', u.id, 'v12', c.id, 'published'
FROM users u CROSS JOIN categories c
WHERE u.deleted_at IS NULL AND c.deleted_at IS NULL AND c.node_kind = 'board'
LIMIT 1;
INSERT INTO mutual_aid_details (thread_id, aid_type, aid_status, location_scope, contact_mode, version, created_by)
SELECT 9934001, 'request', 'open', 'Main library', 'comment', 1, u.id
FROM users u WHERE u.deleted_at IS NULL LIMIT 1;
SQL

test "$(scalar "SELECT aid_status FROM mutual_aid_details WHERE thread_id=9934001")" = "open"
test "$(scalar "SELECT to_regclass('public.idx_mutual_aid_details_status_updated') IS NOT NULL")" = "t"

set +e
wrong_type_output="$(run_sql 2>&1 <<'SQL'
INSERT INTO mutual_aid_details (thread_id, aid_type, aid_status, location_scope, contact_mode, version, created_by)
SELECT 9934002, 'request', 'open', '', 'comment', 1, u.id
FROM users u WHERE u.deleted_at IS NULL LIMIT 1;
SQL
)"
wrong_type_status=$?
set -e
if [[ $wrong_type_status -eq 0 || "$wrong_type_output" != *"requires an existing mutual_aid thread"* ]]; then
  echo "mutual aid detail guard accepted a non-mutual-aid thread: $wrong_type_output" >&2
  exit 1
fi

set +e
bad_status_output="$(run_sql 2>&1 <<'SQL'
INSERT INTO mutual_aid_details (thread_id, aid_type, aid_status, location_scope, contact_mode, version, created_by)
SELECT 9934003, 'request', 'invalid', '', 'comment', 1, u.id
FROM users u WHERE u.deleted_at IS NULL LIMIT 1;
SQL
)"
bad_status_result=$?
set -e
if [[ $bad_status_result -eq 0 || "$bad_status_output" != *"chk_mutual_aid_details_status"* ]]; then
  echo "mutual aid migration accepted an invalid status: $bad_status_output" >&2
  exit 1
fi

run_file "$repo_root/migrations/000034_v12_mutual_aid.down.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.mutual_aid_details') IS NULL")" = "t"

run_file "$repo_root/migrations/000034_v12_mutual_aid.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.mutual_aid_details') IS NOT NULL")" = "t"

echo "v0.12 mutual aid migration up/down/up drill passed"
