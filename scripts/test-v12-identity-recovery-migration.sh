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
drill_db="campusos_v12_recovery_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..30}_*.sql; do
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
  (9931001, 'v12_recovery_user', 'Recovery User', 'v12-recovery@example.test', 'active');
INSERT INTO accounts (
  id, user_id, type, identifier, credential, verified, identifier_normalized,
  verification_state, credential_version, verification_source
) VALUES (
  9931002, 9931001, 'email', 'v12-recovery@example.test', 'hash', FALSE,
  'v12-recovery@example.test', 'legacy_accepted', 1, 'v12-test'
);
INSERT INTO identity_email_challenges (
  id, public_id, purpose, email_normalized, account_id, key_id, nonce,
  expires_at, requested_ip_hash
) VALUES (
  9931003, 'v12-recovery-challenge', 'password_reset', 'v12-recovery@example.test',
  9931002, 'v1', 'nonce', NOW() + interval '10 minutes', 'digest'
);
SQL

run_file "$repo_root/migrations/000031_v12_identity_recovery_cases.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_account_recovery_cases') IS NOT NULL")" = "t"
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='identity_account_recovery_cases' AND column_name IN ('public_id','user_id','account_id','challenge_id','target_email_normalized','proof_reference','status')")" = "7"
test "$(scalar "SELECT to_regclass('public.uk_identity_recovery_case_challenge') IS NOT NULL")" = "t"
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code IN ('identity.account.recovery.override','identity.session.read','identity.session.revoke','platform.email_delivery.read')")" = "4"

run_sql >/dev/null <<'SQL'
INSERT INTO identity_account_recovery_cases (
  id, public_id, user_id, account_id, target_email_normalized, challenge_id,
  proof_reference, status, expires_at
) VALUES (
  9931004, 'v12-recovery-case', 9931001, 9931002, 'new-address@example.test',
  9931003, 'offline-case-9931004', 'pending', NOW() + interval '15 minutes'
);
SQL

set +e
duplicate_output="$(run_sql 2>&1 <<'SQL'
INSERT INTO identity_account_recovery_cases (
  id, public_id, user_id, account_id, target_email_normalized, challenge_id,
  proof_reference, status, expires_at
) VALUES (
  9931005, 'v12-recovery-case-duplicate', 9931001, 9931002, 'other@example.test',
  9931003, 'offline-case-9931005', 'pending', NOW() + interval '15 minutes'
);
SQL
)"
duplicate_status=$?
set -e
if [[ $duplicate_status -eq 0 || "$duplicate_output" != *"uk_identity_recovery_case_challenge"* ]]; then
  echo "recovery migration accepted a second case for one challenge: $duplicate_output" >&2
  exit 1
fi

set +e
invalid_output="$(run_sql 2>&1 <<'SQL'
INSERT INTO identity_account_recovery_cases (
  id, public_id, user_id, account_id, target_email_normalized, challenge_id,
  proof_reference, status, expires_at
) VALUES (
  9931006, 'v12-recovery-case-invalid', 9931001, 9931002, 'invalid@example.test',
  9931003, 'offline-case-9931006', 'invalid', NOW() + interval '15 minutes'
);
SQL
)"
invalid_status=$?
set -e
if [[ $invalid_status -eq 0 || "$invalid_output" != *"chk_identity_recovery_case_status"* ]]; then
  echo "recovery migration did not enforce case state: $invalid_output" >&2
  exit 1
fi

run_file "$repo_root/migrations/000031_v12_identity_recovery_cases.down.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_account_recovery_cases') IS NULL")" = "t"
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code IN ('identity.account.recovery.override','identity.session.read','identity.session.revoke','platform.email_delivery.read')")" = "0"

run_file "$repo_root/migrations/000031_v12_identity_recovery_cases.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.uk_identity_recovery_case_public_id') IS NOT NULL")" = "t"
test "$(scalar "SELECT count(*) FROM role_permissions rp JOIN permission_definitions pd ON pd.id=rp.permission_id JOIN roles r ON r.id=rp.role_id WHERE r.name='admin' AND rp.deleted_at IS NULL AND pd.code='identity.account.recovery.override'")" = "1"

echo "v0.12 identity recovery migration up/down/up drill passed"
