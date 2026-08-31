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
drill_db="campusos_v12_identity_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..27}_*.sql; do
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
INSERT INTO users (id, username, nickname, email, status) VALUES
  (9928001, 'v12_case_one', 'Case One', 'Case.One@Example.Test', 'active'),
  (9928002, 'v12_case_two', 'Case Two', 'case.one@example.test', 'active');
INSERT INTO accounts (id, user_id, type, identifier, credential, verified) VALUES
  (9928003, 9928001, 'email', 'Case.One@Example.Test', 'hash', FALSE),
  (9928004, 9928002, 'email', 'case.one@example.test', 'hash', FALSE);
SQL

set +e
preflight_output="$(run_file "$repo_root/migrations/000028_v12_identity_account_state.up.sql" 2>&1)"
preflight_status=$?
set -e
if [[ $preflight_status -eq 0 ]]; then
  echo "identity migration accepted a normalized-email collision" >&2
  exit 1
fi
if [[ "$preflight_output" != *"casefold email collision"* ]]; then
  echo "identity migration preflight error did not identify the collision: $preflight_output" >&2
  exit 1
fi
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='accounts' AND column_name='identifier_normalized'")" = "0"

run_sql >/dev/null <<'SQL'
DELETE FROM accounts WHERE id IN (9928003, 9928004);
DELETE FROM users WHERE id IN (9928001, 9928002);
INSERT INTO users (id, username, nickname, email, status) VALUES
  (9928005, 'v12_legacy_user', 'Legacy User', 'Legacy.User@Example.Test', 'active');
INSERT INTO accounts (id, user_id, type, identifier, credential, verified) VALUES
  (9928006, 9928005, 'email', 'Legacy.User@Example.Test', 'hash', TRUE);
SQL

run_file "$repo_root/migrations/000028_v12_identity_account_state.up.sql" >/dev/null
test "$(scalar "SELECT identifier || ':' || identifier_normalized || ':' || verification_state || ':' || verified::text FROM accounts WHERE id=9928006")" = "legacy.user@example.test:legacy.user@example.test:legacy_accepted:false"
test "$(scalar "SELECT email || ':' || auth_version || ':' || must_change_password::text FROM users WHERE id=9928005")" = "legacy.user@example.test:1:false"
test "$(scalar "SELECT count(*) FROM identity_reserved_identifiers WHERE identifier_type='email' AND identifier_normalized='1904650862@qq.com'")" = "1"
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='accounts' AND column_name IN ('identifier_normalized','verification_state','credential_version')")" = "3"

run_file "$repo_root/migrations/000028_v12_identity_account_state.down.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_reserved_identifiers') IS NULL")" = "t"
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='accounts' AND column_name='identifier_normalized'")" = "0"

run_file "$repo_root/migrations/000028_v12_identity_account_state.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.uk_accounts_email_normalized') IS NOT NULL")" = "t"
test "$(scalar "SELECT verification_state FROM accounts WHERE id=9928006")" = "legacy_accepted"

echo "v0.12 identity account migration preflight and up/down/up drill passed"
