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
drill_db="campusos_v12_challenge_policy_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..35}_*.sql; do
  ln -s "$migration" "$migration_dir/$(basename "$migration")"
done
(
  cd "$repo_root"
  PSQL_MODE=docker DB_NAME="$drill_db" MIGRATIONS_DIR="$migration_dir" ./scripts/migrate.sh up >/dev/null
)

run_file() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 <"$1"
}

scalar() {
  docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
    psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 -Atc "$1"
}

# A customized installation may already use gaps in the older Category grant
# range. The policy migration must use its own stable ID range.
docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
INSERT INTO permission_definitions (
  id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level
) VALUES
  (900000000009999001, 'test.migration.placeholder_one', 'test', 'migration', 'placeholder_one', 'migration collision fixture', 'low', '["global"]'::jsonb, 'standard'),
  (900000000009999002, 'test.migration.placeholder_two', 'test', 'migration', 'placeholder_two', 'migration collision fixture', 'low', '["global"]'::jsonb, 'standard');
INSERT INTO role_permissions (id, role_id, permission_id, created_by)
VALUES
  (900000000000204001, 4, 900000000009999001, 'migration-collision-fixture'),
  (900000000000204002, 4, 900000000009999002, 'migration-collision-fixture');
SQL

run_file "$repo_root/migrations/000036_v12_identity_challenge_policy.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_challenge_policies') IS NOT NULL")" = "t"
test "$(scalar "SELECT email_window_minutes || ':' || email_max_requests FROM identity_challenge_policies WHERE id='email_verification'")" = "10:5"
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code IN ('identity.challenge_policy.read','identity.challenge_policy.update')")" = "2"
test "$(scalar "SELECT count(*) FROM role_permissions rp INNER JOIN permission_definitions pd ON pd.id=rp.permission_id WHERE rp.role_id=1 AND rp.deleted_at IS NULL AND pd.code IN ('identity.challenge_policy.read','identity.challenge_policy.update')")" = "2"

set +e
invalid_output="$(docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 2>&1 <<'SQL'
UPDATE identity_challenge_policies SET email_max_requests=0 WHERE id='email_verification';
SQL
)"
invalid_status=$?
set -e
if [[ $invalid_status -eq 0 || "$invalid_output" != *"chk_identity_challenge_policy_email_limit"* ]]; then
  echo "challenge policy migration accepted a disabled email limit: $invalid_output" >&2
  exit 1
fi

run_file "$repo_root/migrations/000036_v12_identity_challenge_policy.down.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_challenge_policies') IS NULL")" = "t"

run_file "$repo_root/migrations/000036_v12_identity_challenge_policy.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_challenge_policies') IS NOT NULL")" = "t"
test "$(scalar "SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='identity_challenge_policies'::regclass AND conname='chk_identity_challenge_policy_email_limit')")" = "t"

echo "v0.12 identity challenge policy migration up/down/up drill passed"
