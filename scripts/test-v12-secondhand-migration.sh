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
drill_db="campusos_v12_secondhand_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..34}_*.sql; do
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

run_file "$repo_root/migrations/000035_v12_secondhand.up.sql" >/dev/null

run_sql >/dev/null <<'SQL'
INSERT INTO users (id, username, nickname, email, status)
VALUES (9935999, 'v12_secondhand_other', 'v12 secondhand other', 'v12-secondhand-other@example.invalid', 'active');
INSERT INTO threads (id, thread_type, title, content, content_format, author_id, author_name, category_id, status)
SELECT 9935001, 'secondhand', 'Desk lamp', 'detail guard fixture', 'markdown', u.id, 'v12', c.id, 'published'
FROM users u CROSS JOIN categories c
WHERE u.deleted_at IS NULL AND c.deleted_at IS NULL AND c.node_kind = 'board'
LIMIT 1;
INSERT INTO threads (id, thread_type, title, content, content_format, author_id, author_name, category_id, status)
SELECT 9935002, 'discussion', 'Ordinary discussion', 'detail guard fixture', 'markdown', u.id, 'v12', c.id, 'published'
FROM users u CROSS JOIN categories c
WHERE u.deleted_at IS NULL AND c.deleted_at IS NULL AND c.node_kind = 'board'
LIMIT 1;
INSERT INTO threads (id, thread_type, title, content, content_format, author_id, author_name, category_id, status)
SELECT 9935003, 'secondhand', 'Invalid price fixture', 'detail guard fixture', 'markdown', u.id, 'v12', c.id, 'published'
FROM users u CROSS JOIN categories c
WHERE u.deleted_at IS NULL AND c.deleted_at IS NULL AND c.node_kind = 'board'
LIMIT 1;
INSERT INTO threads (id, thread_type, title, content, content_format, author_id, author_name, category_id, status)
SELECT 9935004, 'secondhand', 'Invalid currency fixture', 'detail guard fixture', 'markdown', u.id, 'v12', c.id, 'published'
FROM users u CROSS JOIN categories c
WHERE u.deleted_at IS NULL AND c.deleted_at IS NULL AND c.node_kind = 'board'
LIMIT 1;
INSERT INTO threads (id, thread_type, title, content, content_format, author_id, author_name, category_id, status)
SELECT 9935005, 'secondhand', 'Invalid author fixture', 'detail guard fixture', 'markdown', u.id, 'v12', c.id, 'published'
FROM users u CROSS JOIN categories c
WHERE u.deleted_at IS NULL AND u.id <> 9935999 AND c.deleted_at IS NULL AND c.node_kind = 'board'
LIMIT 1;
INSERT INTO secondhand_details (thread_id, price_minor, currency, item_condition, trade_method, trade_status, location_scope, version, created_by)
SELECT 9935001, 2500, 'CNY', 'good', 'in_person', 'available', 'Main library', 1, u.id
FROM users u WHERE u.deleted_at IS NULL LIMIT 1;
SQL

test "$(scalar "SELECT price_minor FROM secondhand_details WHERE thread_id=9935001")" = "2500"
test "$(scalar "SELECT to_regclass('public.idx_secondhand_details_status_updated') IS NOT NULL")" = "t"

set +e
wrong_type_output="$(run_sql 2>&1 <<'SQL'
INSERT INTO secondhand_details (thread_id, price_minor, currency, item_condition, trade_method, trade_status, location_scope, version, created_by)
SELECT 9935002, 2500, 'CNY', 'good', 'in_person', 'available', '', 1, u.id
FROM users u WHERE u.deleted_at IS NULL LIMIT 1;
SQL
)"
wrong_type_status=$?
set -e
if [[ $wrong_type_status -eq 0 || "$wrong_type_output" != *"requires an existing secondhand thread"* ]]; then
  echo "secondhand detail guard accepted a non-secondhand thread: $wrong_type_output" >&2
  exit 1
fi

set +e
bad_price_output="$(run_sql 2>&1 <<'SQL'
INSERT INTO secondhand_details (thread_id, price_minor, currency, item_condition, trade_method, trade_status, location_scope, version, created_by)
SELECT 9935003, -1, 'CNY', 'good', 'in_person', 'available', '', 1, u.id
FROM users u WHERE u.deleted_at IS NULL LIMIT 1;
SQL
)"
bad_price_status=$?
set -e
if [[ $bad_price_status -eq 0 || "$bad_price_output" != *"chk_secondhand_details_price_minor"* ]]; then
  echo "secondhand migration accepted a negative price: $bad_price_output" >&2
  exit 1
fi

set +e
bad_currency_output="$(run_sql 2>&1 <<'SQL'
INSERT INTO secondhand_details (thread_id, price_minor, currency, item_condition, trade_method, trade_status, location_scope, version, created_by)
SELECT 9935004, 2500, 'USD', 'good', 'in_person', 'available', '', 1, u.id
FROM users u WHERE u.deleted_at IS NULL LIMIT 1;
SQL
)"
bad_currency_status=$?
set -e
if [[ $bad_currency_status -eq 0 || "$bad_currency_output" != *"chk_secondhand_details_currency"* ]]; then
  echo "secondhand migration accepted a non-CNY currency: $bad_currency_output" >&2
  exit 1
fi

set +e
wrong_author_output="$(run_sql 2>&1 <<'SQL'
INSERT INTO secondhand_details (thread_id, price_minor, currency, item_condition, trade_method, trade_status, location_scope, version, created_by)
VALUES (9935005, 2500, 'CNY', 'good', 'in_person', 'available', '', 1, 9935999);
SQL
)"
wrong_author_status=$?
set -e
if [[ $wrong_author_status -eq 0 || "$wrong_author_output" != *"created_by must match the thread author"* ]]; then
  echo "secondhand detail guard accepted a mismatched creator: $wrong_author_output" >&2
  exit 1
fi

run_file "$repo_root/migrations/000035_v12_secondhand.down.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.secondhand_details') IS NULL")" = "t"

run_file "$repo_root/migrations/000035_v12_secondhand.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.secondhand_details') IS NOT NULL")" = "t"
test "$(scalar "SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='secondhand_details'::regclass AND tgname='trg_secondhand_detail_guard' AND NOT tgisinternal)")" = "t"
test "$(scalar "SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='secondhand_details'::regclass AND conname='chk_secondhand_details_currency')")" = "t"

echo "v12 secondhand migration up/down/up drill passed"
