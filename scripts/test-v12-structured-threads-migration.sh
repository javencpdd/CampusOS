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
drill_db="campusos_v12_structured_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..32}_*.sql; do
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

run_sql >/dev/null <<'SQL'
INSERT INTO threads (id, title, content, content_format, author_id, author_name, category_id, status)
SELECT 9933001, 'Historical article', 'body', 'richtext_article', u.id, 'v12', c.id, 'draft'
FROM users u CROSS JOIN categories c
WHERE u.deleted_at IS NULL AND c.deleted_at IS NULL AND c.node_kind = 'board'
LIMIT 1;
INSERT INTO threads (id, title, content, content_format, author_id, author_name, category_id, status)
SELECT 9933002, 'Historical discussion', 'body', 'markdown', u.id, 'v12', c.id, 'published'
FROM users u CROSS JOIN categories c
WHERE u.deleted_at IS NULL AND c.deleted_at IS NULL AND c.node_kind = 'board'
LIMIT 1;
INSERT INTO richtext_article_contents (id, thread_id, title, created_by)
SELECT 9933003, 9933001, 'Historical article', u.id
FROM users u WHERE u.deleted_at IS NULL LIMIT 1;
SQL

run_file "$repo_root/migrations/000033_v12_structured_threads.up.sql" >/dev/null
test "$(scalar "SELECT thread_type FROM threads WHERE id=9933001")" = "article"
test "$(scalar "SELECT thread_type FROM threads WHERE id=9933002")" = "discussion"
test "$(scalar "SELECT count(*) FROM category_thread_type_policies WHERE thread_type IN ('discussion','article') AND enabled")" -ge 2
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code='community.category.configure_thread_types'")" = "1"

run_sql >/dev/null <<'SQL'
INSERT INTO category_thread_type_policies (category_id, thread_type, enabled)
SELECT id, 'mutual_aid', TRUE FROM categories
WHERE deleted_at IS NULL AND node_kind='board'
LIMIT 1;
SQL

run_sql >/dev/null <<'SQL'
INSERT INTO categories (id, name, slug, description, node_kind, lifecycle_status, version, color)
VALUES (9933004, 'v12 structured group', 'v12-structured-group', '', 'group', 'active', 1, '');
SQL
set +e
group_output="$(run_sql 2>&1 <<'SQL'
INSERT INTO category_thread_type_policies (category_id, thread_type, enabled)
VALUES (9933004, 'discussion', TRUE);
SQL
)"
group_status=$?
set -e
if [[ $group_status -eq 0 || "$group_output" != *"thread type policy requires an existing board category"* ]]; then
  echo "structured thread migration accepted a group policy: $group_output" >&2
  exit 1
fi

run_file "$repo_root/migrations/000033_v12_structured_threads.down.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='threads' AND column_name='thread_type'")" = "0"
test "$(scalar "SELECT to_regclass('public.category_thread_type_policies') IS NULL")" = "t"

run_file "$repo_root/migrations/000033_v12_structured_threads.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.idx_threads_thread_type_created') IS NOT NULL")" = "t"
test "$(scalar "SELECT count(*) FROM role_permissions rp JOIN permission_definitions pd ON pd.id=rp.permission_id JOIN roles r ON r.id=rp.role_id WHERE r.name='admin' AND rp.deleted_at IS NULL AND pd.code='community.category.configure_thread_types'")" = "1"

echo "v0.12 structured thread migration preflight and up/down/up drill passed"
