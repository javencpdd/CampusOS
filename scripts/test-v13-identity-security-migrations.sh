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
drill_db="campusos_v13_identity_security_$(date -u +%Y%m%d%H%M%S)_$$"
migration_dir="$(mktemp -d)"

cleanup() {
  docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" dropdb -U "$DB_USER" --if-exists --force "$drill_db" >/dev/null 2>&1 || true
  rm -rf "$migration_dir"
}
trap cleanup EXIT

docker exec -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" createdb -U "$DB_USER" "$drill_db"
for migration in "$repo_root"/migrations/0000{01..38}_*.sql; do
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

run_file "$repo_root/migrations/000039_v13_admin_admission_operations.up.sql" >/dev/null
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code LIKE 'identity.admin_account.%'")" = "4"
test "$(scalar "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='identity_admin_accounts' AND column_name='status_reason')")" = "t"

docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
INSERT INTO users (id, username, nickname, email, status, auth_version)
VALUES (9913001, 'v13_security_admin', 'V13 Security Admin', 'v13-security@example.test', 'active', 1);
INSERT INTO accounts (
  id, user_id, type, identifier, identifier_normalized, credential, verified,
  verification_state, verified_at, verification_source, password_changed_at, credential_version
) VALUES (
  9913002, 9913001, 'email', 'v13-security@example.test', 'v13-security@example.test',
  'hash', TRUE, 'verified', NOW(), 'migration_drill', NOW(), 1
);
INSERT INTO user_roles (id, user_id, role_id, scope_type, scope_id)
VALUES (9913003, 9913001, 1, 'global', NULL);
UPDATE identity_admin_accounts
SET status='suspended', status_reason='migration_drill', status_changed_at=NOW(), version=version+1
WHERE user_id=9913001;
UPDATE user_roles SET scope_type='global' WHERE id=9913003;
SQL
test "$(scalar "SELECT status || ':' || status_reason FROM identity_admin_accounts WHERE user_id=9913001")" = "suspended:migration_drill"

run_file "$repo_root/migrations/000040_v13_identity_mfa.up.sql" >/dev/null
docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
INSERT INTO sessions (
  id, user_id, refresh_token, refresh_token_digest, token_family_id,
  expires_at, authentication_strength
) VALUES (
  9913004, 9913001, NULL,
  'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  'v13-mfa-drill-family', NOW() + INTERVAL '1 hour', 'password'
);
SQL
test "$(scalar "SELECT to_regclass('public.identity_mfa_totp_methods') IS NOT NULL")" = "t"
test "$(scalar "SELECT mode || ':' || version FROM identity_mfa_policies WHERE id='admin'")" = "off:1"
test "$(scalar "SELECT column_default LIKE '%password%' FROM information_schema.columns WHERE table_name='sessions' AND column_name='authentication_strength'")" = "t"
test "$(scalar "SELECT count(*) FROM permission_definitions WHERE code IN ('identity.mfa_policy.read','identity.mfa_policy.update','identity.mfa.local_recovery')")" = "3"
test "$(scalar "SELECT string_agg(id::text || ':' || code, ',' ORDER BY id) FROM permission_definitions WHERE code IN ('identity.mfa_policy.read','identity.mfa_policy.update','identity.mfa.local_recovery')")" = "900000000000001040:identity.mfa_policy.read,900000000000001041:identity.mfa_policy.update,900000000000001042:identity.mfa.local_recovery"
test "$(scalar "SELECT risk_level FROM permission_definitions WHERE code='identity.mfa.local_recovery'")" = "high"

set +e
invalid_output="$(docker exec -i -e PGPASSWORD="$DB_PASSWORD" "$POSTGRES_CONTAINER" \
  psql -U "$DB_USER" -d "$drill_db" -v ON_ERROR_STOP=1 2>&1 <<'SQL'
UPDATE sessions SET authentication_strength='untrusted' WHERE id=9913004;
SQL
)"
invalid_status=$?
set -e
if [[ $invalid_status -eq 0 || "$invalid_output" != *"chk_sessions_authentication_strength"* ]]; then
  echo "MFA migration accepted an invalid session strength: $invalid_output" >&2
  exit 1
fi

run_file "$repo_root/migrations/000040_v13_identity_mfa.down.sql" >/dev/null
run_file "$repo_root/migrations/000039_v13_admin_admission_operations.down.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_mfa_totp_methods') IS NULL")" = "t"
test "$(scalar "SELECT count(*) FROM information_schema.columns WHERE table_name='identity_admin_accounts' AND column_name='status_reason'")" = "0"

run_file "$repo_root/migrations/000039_v13_admin_admission_operations.up.sql" >/dev/null
run_file "$repo_root/migrations/000040_v13_identity_mfa.up.sql" >/dev/null
test "$(scalar "SELECT to_regclass('public.identity_mfa_tickets') IS NOT NULL")" = "t"
test "$(scalar "SELECT status FROM identity_admin_accounts WHERE user_id=9913001")" = "suspended"

echo "v0.13 administrator admission and MFA migration up/down/up drill passed"
