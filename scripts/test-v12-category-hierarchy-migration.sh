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
drill_db="campusos_v12_category_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..31}_*.sql; do
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
INSERT INTO categories (id, name, slug, description, parent_id) VALUES
  (9932001, 'Root', 'v12-category-root', '', NULL),
  (9932002, 'Child', 'v12-category-child', '', 9932001),
  (9932003, 'Grandchild', 'v12-category-grandchild', '', 9932002);
SQL

set +e
preflight_output="$(run_file "$repo_root/migrations/000032_v12_category_hierarchy.up.sql" 2>&1)"
preflight_status=$?
set -e
if [[ $preflight_status -eq 0 || "$preflight_output" != *"hierarchy exceeds two levels"* ]]; then
  echo "category migration accepted a third hierarchy level: $preflight_output" >&2
  exit 1
fi
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='categories' AND column_name='node_kind'")" = "0"

run_sql >/dev/null <<'SQL'
DELETE FROM categories WHERE id=9932003;
SQL
run_file "$repo_root/migrations/000032_v12_category_hierarchy.up.sql" >/dev/null
test "$(scalar "SELECT node_kind || ':' || lifecycle_status || ':' || version FROM categories WHERE id=9932001")" = "group:active:1"
test "$(scalar "SELECT node_kind || ':' || lifecycle_status || ':' || version FROM categories WHERE id=9932002")" = "board:active:1"
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code IN ('community.category.create','community.category.update','community.category.move','community.category.archive','community.category.restore')")" = "5"

set +e
parent_output="$(run_sql 2>&1 <<'SQL'
INSERT INTO categories (id, name, slug, description, parent_id, node_kind, lifecycle_status, version, color)
VALUES (9932004, 'Illegal', 'v12-category-illegal', '', 9932002, 'board', 'active', 1, '');
SQL
)"
parent_status=$?
set -e
if [[ $parent_status -eq 0 || "$parent_output" != *"category parent must be an active group"* ]]; then
  echo "category migration accepted a board parent: $parent_output" >&2
  exit 1
fi

set +e
archive_output="$(run_sql 2>&1 <<'SQL'
UPDATE categories SET lifecycle_status='archived' WHERE id=9932001;
SQL
)"
archive_status=$?
set -e
if [[ $archive_status -eq 0 || "$archive_output" != *"archive or move active child boards"* ]]; then
  echo "category migration accepted archiving a group with active boards: $archive_output" >&2
  exit 1
fi

set +e
color_output="$(run_sql 2>&1 <<'SQL'
UPDATE categories SET color='blue' WHERE id=9932002;
SQL
)"
color_status=$?
set -e
if [[ $color_status -eq 0 || "$color_output" != *"chk_categories_color"* ]]; then
  echo "category migration accepted an invalid color: $color_output" >&2
  exit 1
fi

run_file "$repo_root/migrations/000032_v12_category_hierarchy.down.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='categories' AND column_name='node_kind'")" = "0"

run_file "$repo_root/migrations/000032_v12_category_hierarchy.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.idx_categories_parent_active') IS NOT NULL")" = "t"
test "$(scalar "SELECT count(*) FROM role_permissions rp JOIN permission_definitions pd ON pd.id=rp.permission_id JOIN roles r ON r.id=rp.role_id WHERE r.name='admin' AND rp.deleted_at IS NULL AND pd.code='community.category.move'")" = "1"

echo "v0.12 category hierarchy migration preflight and up/down/up drill passed"
