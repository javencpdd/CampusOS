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
drill_db="campusos_v12_admin_accounts_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..37}_*.sql; do
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

run_file "$repo_root/migrations/000038_v12_admin_accounts.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_admin_accounts') IS NOT NULL")" = "t"
test "$(scalar "SELECT status || ':' || activation_source FROM identity_admin_accounts WHERE user_id=1000000000000000001")" = "active:v12_role_backfill"
test "$(scalar "SELECT credential_account_id FROM identity_admin_accounts WHERE user_id=1000000000000000001")" = "1000000000000000002"

docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
INSERT INTO users (id, username, nickname, email, status, auth_version)
VALUES (9938001, 'v12_admin_candidate', 'Admin Candidate', 'v12-admin@example.test', 'active', 1);
INSERT INTO accounts (
  id, user_id, type, identifier, identifier_normalized, credential, verified,
  verification_state, verified_at, verification_source, password_changed_at, credential_version
) VALUES (
  9938002, 9938001, 'email', 'v12-admin@example.test', 'v12-admin@example.test',
  'hash', TRUE, 'verified', NOW(), 'migration_drill', NOW(), 1
);
INSERT INTO user_roles (id, user_id, role_id, scope_type, scope_id)
VALUES (9938003, 9938001, 1, 'global', NULL);
SQL
test "$(scalar "SELECT status || ':' || credential_account_id FROM identity_admin_accounts WHERE user_id=9938001")" = "active:9938002"

# A management-plane suspension is independent from the RBAC grant. Refreshing
# the same role assignment must not silently reactivate the account.
docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
UPDATE identity_admin_accounts
SET status='suspended', revoked_at=NULL, updated_at=NOW(), version=version+1
WHERE user_id=9938001;
UPDATE user_roles SET scope_type='global' WHERE id=9938003;
SQL
test "$(scalar "SELECT status FROM identity_admin_accounts WHERE user_id=9938001")" = "suspended"

docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
UPDATE user_roles SET deleted_at=NOW() WHERE id=9938003;
SQL
test "$(scalar "SELECT status || ':' || (revoked_at IS NOT NULL)::text FROM identity_admin_accounts WHERE user_id=9938001")" = "revoked:true"

docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
INSERT INTO user_roles (id, user_id, role_id, scope_type, scope_id)
VALUES (9938004, 9938001, 1, 'global', NULL);
SQL
test "$(scalar "SELECT status || ':' || (revoked_at IS NULL)::text FROM identity_admin_accounts WHERE user_id=9938001")" = "active:true"

set +e
invalid_output="$(docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 2>&1 <<'SQL'
UPDATE identity_admin_accounts SET status='invalid' WHERE user_id=9938001;
SQL
)"
invalid_status=$?
set -e
if [[ $invalid_status -eq 0 || "$invalid_output" != *"chk_identity_admin_account_status"* ]]; then
  echo "admin account migration accepted an invalid status: $invalid_output" >&2
  exit 1
fi

run_file "$repo_root/migrations/000038_v12_admin_accounts.down.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_admin_accounts') IS NULL")" = "t"
test "$(scalar "SELECT count(*) FROM user_roles WHERE user_id=9938001 AND role_id=1 AND deleted_at IS NULL")" = "1"

run_file "$repo_root/migrations/000038_v12_admin_accounts.up.sql" >/dev/null
test "$(scalar "SELECT status FROM identity_admin_accounts WHERE user_id=9938001")" = "active"

echo "v0.12 independent administrator account migration up/down/up drill passed"
