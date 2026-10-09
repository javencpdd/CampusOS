#!/usr/bin/env bash
# V12-01a full data-foundation exit drill on an owned PostgreSQL 16 tmpfs container.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
for executable in docker go curl python3 make; do
  command -v "$executable" >/dev/null || { echo "missing $executable" >&2; exit 127; }
done
if (( $# != 0 && $# != 2 )); then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi
if (( $# == 2 )) && [[ "$1" != "--report" ]]; then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi

mkdir -p .cache/v12-01a/tmp .cache/v12-01a/go-cache
export TMPDIR="$repo_root/.cache/v12-01a/tmp"
export GOCACHE="$repo_root/.cache/v12-01a/go-cache"
report="$repo_root/.cache/v12-01a/exit-drill.json"
if (( $# == 2 )); then report="$2"; fi
work_dir="$(mktemp -d "$repo_root/.cache/v12-01a/exit.XXXXXX")"
container="campusos-v12-01a-exit-$$"
database="campusos_v12_01a_exit"
restored_database="campusos_v12_01a_exit_restored"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
bootstrap_secret="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
jwt_secret="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
challenge_secret="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
mfa_secret="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
api_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
plugin_ui_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
container_started=false
api_pid=""

cleanup() {
  local status=$?
  if [[ -n "$api_pid" ]]; then
    kill "$api_pid" 2>/dev/null || true
    wait "$api_pid" 2>/dev/null || true
  fi
  if [[ "$container_started" == true ]]; then
    if (( status != 0 )); then
      docker logs "$container" >"$repo_root/.cache/v12-01a/exit-postgres-last-failure.log" 2>&1 || true
      echo "isolated PostgreSQL failure log: .cache/v12-01a/exit-postgres-last-failure.log" >&2
    fi
    docker stop "$container" >/dev/null 2>&1 || true
  fi
  if (( status != 0 )) && [[ -f "$work_dir/api.log" ]]; then
    python3 - "$work_dir/api.log" <<'PYERROR'
import re
import sys
from pathlib import Path
for line in Path(sys.argv[1]).read_text(errors='replace').splitlines():
    match = re.search(r'\(SQLSTATE ([0-9A-Z]{5})\)', line)
    if match:
        print('isolated API SQLSTATE ' + match.group(1), file=sys.stderr)
PYERROR
  fi
  rm -rf -- "$work_dir"
}
trap cleanup EXIT

psql_scalar_db() {
  local target_database="$1"
  shift
  docker exec -e PGPASSWORD="$pg_password" "$container" \
    psql -U campusos -d "$target_database" -qAtv ON_ERROR_STOP=1 -v VERBOSITY=verbose -c "$1"
}
psql_scalar() { psql_scalar_db "$database" "$1"; }
run_migrate_db() {
  local target_database="$1" migration_dir="$2"
  shift 2
  CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
    DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$target_database" \
    MIGRATIONS_DIR="$migration_dir" bash scripts/migrate.sh "$@"
}
run_migrate() { run_migrate_db "$database" "$@"; }
require_equals() {
  local label="$1" actual="$2" expected="$3"
  if [[ "$actual" != "$expected" ]]; then
    echo "$label: expected '$expected', got '$actual'" >&2
    exit 1
  fi
}
reject_sql_db() {
  local target_database="$1" label="$2" sqlstate="$3" statement="$4"
  if psql_scalar_db "$target_database" "$statement" >"$work_dir/rejected-$label.log" 2>&1; then
    echo "$label: invalid mutation was accepted" >&2
    exit 1
  fi
  if ! grep -Eq "ERROR: +$sqlstate:" "$work_dir/rejected-$label.log"; then
    echo "$label: rejection did not have SQLSTATE $sqlstate" >&2
    cat "$work_dir/rejected-$label.log" >&2
    exit 1
  fi
}
reject_sql() { reject_sql_db "$database" "$@"; }
require_passed_test() {
  local log_path="$1" test_name="$2"
  if ! grep -Eq "^--- PASS: $test_name( |$)" "$log_path"; then
    echo "$test_name did not execute and pass" >&2
    tail -n 100 "$log_path" >&2
    exit 1
  fi
}

docker run --rm -d --name "$container" \
  --tmpfs /var/lib/postgresql/data:rw,size=512m \
  -e POSTGRES_DB="$database" -e POSTGRES_USER=campusos \
  -e POSTGRES_PASSWORD="$pg_password" \
  -p 127.0.0.1::5432 postgres:16-alpine >/dev/null
container_started=true
for attempt in $(seq 1 80); do
  if docker exec "$container" pg_isready -U campusos -d "$database" >/dev/null 2>&1; then break; fi
  if (( attempt == 80 )); then echo "isolated PostgreSQL did not become ready" >&2; exit 1; fi
  sleep 0.25
done
pg_version="$(psql_scalar 'SHOW server_version_num;')"
if (( pg_version < 160000 || pg_version >= 170000 )); then
  echo "expected PostgreSQL 16, got server_version_num=$pg_version" >&2
  exit 1
fi
tmpfs_options="$(docker inspect --format '{{index .HostConfig.Tmpfs "/var/lib/postgresql/data"}}' "$container")"
[[ "$tmpfs_options" == *size=512m* ]] || { echo "PostgreSQL data directory is not tmpfs" >&2; exit 1; }
pg_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
[[ "$pg_port" =~ ^[0-9]+$ ]] || { echo "PostgreSQL port is not loopback-bound" >&2; exit 1; }
database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$database?sslmode=disable"

# Start at the frozen v1.1 schema so the 000004 historical actor conversion is
# exercised instead of only checking the final structure of a clean database.
baseline_dir="$work_dir/v1.1-migrations"
mkdir -p "$baseline_dir"
cp migrations/00000[1-3]_v1_1_*.sql "$baseline_dir/"
run_migrate "$baseline_dir" up >"$work_dir/migrations.log"
run_migrate "$baseline_dir" check >>"$work_dir/migrations.log"
require_equals "v1.1 migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "3"
psql_scalar "INSERT INTO authorization_audits(id,actor_id,outcome) VALUES (992001,77,'allow'),(992002,NULL,'deny');" >/dev/null

run_migrate migrations up >>"$work_dir/migrations.log"
run_migrate migrations check >>"$work_dir/migrations.log"
require_equals "v1.2 migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "6"
require_equals "historical user actor" "$(psql_scalar "SELECT actor_kind || ':' || actor_id FROM authorization_audits WHERE id=992001;")" "user:77"
require_equals "historical unknown actor" "$(psql_scalar "SELECT actor_kind FROM authorization_audits WHERE id=992002 AND actor_id IS NULL;")" "legacy_unknown"
psql_scalar "INSERT INTO authorization_audits(id,actor_kind,actor_id,outcome) VALUES (992003,'user','77','allow'),(992004,'admin','77','deny');" >/dev/null
require_equals "same ID remains two domains" "$(psql_scalar "SELECT count(DISTINCT actor_kind) FROM authorization_audits WHERE actor_id='77' AND id IN (992003,992004);")" "2"
reject_sql "audit-admin-without-id" "23514" "INSERT INTO authorization_audits(id,actor_kind,actor_id,outcome) VALUES (992005,'admin',NULL,'allow');"
require_equals "version identity trigger" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname='trg_plugin_version_identity_immutable' AND NOT tgisinternal;")" "1"
require_equals "declaration identity trigger" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname='trg_plugin_declaration_immutable' AND NOT tgisinternal;")" "1"
require_equals "publication guard trigger" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname='trg_plugin_version_publication_guard' AND NOT tgisinternal;")" "1"
require_equals "declaration membership trigger" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname='trg_plugin_declaration_membership_guard' AND NOT tgisinternal;")" "1"
require_equals "retirement revocation trigger" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname='trg_plugin_version_revoke_delegations' AND NOT tgisinternal;")" "1"

psql_scalar "INSERT INTO plugins(id,name,display_name,version,runtime) VALUES (992010,'v12-01a-exit-fixture','01a Exit Fixture','1.0.0','grpc');
INSERT INTO users(id,username,nickname,email) VALUES (992011,'v12_01a_exit','01a Exit','v12-01a-exit@example.invalid');
INSERT INTO plugin_versions(id,plugin_id,version,package_digest,manifest_api_version,host_api_version,permission_fingerprint,manifest,lifecycle_status)
VALUES (992012,992010,'1.0.0',repeat('a',64),'v3','v3',repeat('b',64),'{\"name\":\"v12-01a-exit-fixture\",\"version\":\"1.0.0\"}'::jsonb,'staged');
INSERT INTO plugin_capability_declarations(id,plugin_version_id,capability_code,purpose,resource_scope)
VALUES (992013,992012,'schedule.self.read','exit fixture permission','{}'::jsonb);
UPDATE plugin_versions SET lifecycle_status='active',activated_at=NOW() WHERE id=992012;
INSERT INTO plugin_admin_grants(id,plugin_version_id,capability_code,status)
VALUES (992014,992012,'schedule.self.read','revoked');
INSERT INTO plugin_user_consents(id,user_id,plugin_version_id,capability_code,status,purpose_hash,revoked_at)
VALUES (992015,992011,992012,'schedule.self.read','revoked',repeat('c',64),NOW());
INSERT INTO plugin_delegations(id,plugin_version_id,subject_user_id,token_digest,status,expires_at)
VALUES (992016,992012,992011,repeat('d',64),'active',NOW()+INTERVAL '1 hour');" >/dev/null
reject_sql "published-declaration-insert" "23514" "INSERT INTO plugin_capability_declarations(id,plugin_version_id,capability_code,purpose) VALUES (992017,992012,'schedule.self.write','unexpected');"
reject_sql "published-declaration-delete" "23514" "DELETE FROM plugin_capability_declarations WHERE id=992013;"
reject_sql "published-declaration-update" "23514" "UPDATE plugin_capability_declarations SET purpose='changed' WHERE id=992013;"
reject_sql "published-version-update" "23514" "UPDATE plugin_versions SET package_digest=repeat('e',64) WHERE id=992012;"

psql_scalar "INSERT INTO plugin_versions(id,plugin_id,version,package_digest,manifest_api_version,host_api_version,permission_fingerprint,manifest,lifecycle_status)
VALUES (992020,992010,'2.0.0',repeat('e',64),'v3','v3',repeat('f',64),'{\"name\":\"v12-01a-exit-fixture\",\"version\":\"2.0.0\"}'::jsonb,'staged');
INSERT INTO plugin_capability_declarations(id,plugin_version_id,capability_code,purpose,resource_scope)
VALUES (992021,992020,'schedule.self.read','exit fixture permission','{}'::jsonb);
UPDATE plugin_versions SET lifecycle_status='retired',retired_at=NOW() WHERE id=992012;
UPDATE plugin_versions SET lifecycle_status='active',activated_at=NOW() WHERE id=992020;" >/dev/null
require_equals "retirement revokes old delegation" "$(psql_scalar 'SELECT status FROM plugin_delegations WHERE id=992016;')" "revoked"
reject_sql "retired-declaration-insert" "23514" "INSERT INTO plugin_capability_declarations(id,plugin_version_id,capability_code,purpose) VALUES (992022,992012,'schedule.self.write','unexpected');"
reject_sql "retired-declaration-delete" "23514" "DELETE FROM plugin_capability_declarations WHERE id=992013;"
reject_sql "second-active-version" "23505" "UPDATE plugin_versions SET lifecycle_status='active' WHERE id=992012;"
psql_scalar "UPDATE plugin_versions SET lifecycle_status='retired',retired_at=NOW() WHERE id=992020;
UPDATE plugin_versions SET lifecycle_status='active',retired_at=NULL WHERE id=992012;" >/dev/null
require_equals "A-B-A returns original version" "$(psql_scalar "SELECT version FROM plugin_versions WHERE plugin_id=992010 AND lifecycle_status='active';")" "1.0.0"
require_equals "A-B-A original declaration retained" "$(psql_scalar 'SELECT id FROM plugin_capability_declarations WHERE plugin_version_id=992012;')" "992013"
require_equals "A-B-A revoked facts retained" "$(psql_scalar "SELECT ag.status || ':' || uc.status || ':' || d.status FROM plugin_admin_grants ag JOIN plugin_user_consents uc USING(plugin_version_id,capability_code) JOIN plugin_delegations d ON d.plugin_version_id=ag.plugin_version_id WHERE ag.id=992014 AND uc.id=992015 AND d.id=992016;")" "revoked:revoked:revoked"

run_migrate migrations down >>"$work_dir/migrations.log"
require_equals "000006 down migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "5"
require_equals "000006 down removes membership guard" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname='trg_plugin_declaration_membership_guard' AND NOT tgisinternal;")" "0"
require_equals "000006 down retains revocation" "$(psql_scalar 'SELECT status FROM plugin_delegations WHERE id=992016;')" "revoked"
run_migrate migrations check >>"$work_dir/migrations.log"
run_migrate migrations up >>"$work_dir/migrations.log"
run_migrate migrations check >>"$work_dir/migrations.log"
require_equals "000006 down/up migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "6"
reject_sql "reapplied-declaration-guard" "23514" "DELETE FROM plugin_capability_declarations WHERE id=992013;"

drift_dir="$work_dir/drift-migrations"
mkdir -p "$drift_dir"
cp migrations/00000[1-6]_*.sql "$drift_dir/"
printf '\n-- intentional checksum drift\n' >>"$drift_dir/000006_v1_2_plugin_publication_seal.up.sql"
if run_migrate "$drift_dir" check >"$work_dir/checksum-drift.log" 2>&1; then
  echo "000006 checksum drift was not rejected" >&2
  exit 1
fi
if ! grep -qi checksum "$work_dir/checksum-drift.log"; then
  echo "000006 drift rejection did not identify a checksum mismatch" >&2
  exit 1
fi
run_migrate migrations check >>"$work_dir/migrations.log"

CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
  DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database" \
  CAMPUSOS_V1_DRILL_DB=campusos_v1_history_01a_exit_isolated \
  make database-check >"$work_dir/database-check.log" 2>&1 || {
    tail -n 100 "$work_dir/database-check.log" >&2
    exit 1
  }

CAMPUSOS_IDENTITY_TEST_DATABASE_URL="$database_url" \
  go test ./internal/modules/core/identity/repository -run '^TestAuthorizationAuditActorPostgreSQL$' -count=1 -v \
  >"$work_dir/audit-test.log" 2>&1 || { tail -n 100 "$work_dir/audit-test.log" >&2; exit 1; }
require_passed_test "$work_dir/audit-test.log" TestAuthorizationAuditActorPostgreSQL
if grep -q -- '--- SKIP:' "$work_dir/audit-test.log"; then
  echo "required PostgreSQL audit test skipped" >&2
  exit 1
fi

plugin_tests='^(TestPostgresVersionIdentityAndConcurrentActivation|TestPostgresAuthorizationRequiresActiveVersion|TestAuthorizationVersionedHandlersRequireMatchingActiveVersion|TestPostgresPublicationSealAndAuthorizationWrites)$'
CAMPUSOS_PG_INTEGRATION_DSN="$database_url" \
  go test ./internal/plugin -run "$plugin_tests" -count=1 -v \
  >"$work_dir/plugin-tests.log" 2>&1 || { tail -n 140 "$work_dir/plugin-tests.log" >&2; exit 1; }
for required_test in \
  TestPostgresVersionIdentityAndConcurrentActivation \
  TestPostgresAuthorizationRequiresActiveVersion \
  TestAuthorizationVersionedHandlersRequireMatchingActiveVersion \
  TestPostgresPublicationSealAndAuthorizationWrites; do
  require_passed_test "$work_dir/plugin-tests.log" "$required_test"
done
if grep -q -- '--- SKIP:' "$work_dir/plugin-tests.log"; then
  echo "required PostgreSQL plugin or Gin HTTP test skipped" >&2
  exit 1
fi
require_equals "no plugin has multiple active versions" "$(psql_scalar "SELECT count(*) FROM (SELECT plugin_id FROM plugin_versions WHERE lifecycle_status='active' GROUP BY plugin_id HAVING count(*) > 1) duplicate_active;")" "0"

go build -o "$work_dir/campusos-server" ./cmd/server
mkdir -p "$work_dir/app/modules" "$work_dir/app/data/resources" "$work_dir/app/data/plugins" "$work_dir/app/plugins"
cp -a modules/. "$work_dir/app/modules/"
cp -a data/resources/. "$work_dir/app/data/resources/"
(
  cd "$work_dir/app"
  exec env -i PATH="$PATH" \
    CAMPUSOS_ENV=test CAMPUSOS_INSTANCE_MODE=single GOMAXPROCS=8 \
    SERVER_HOST=127.0.0.1 SERVER_PORT="$api_port" DATABASE_DSN="$database_url" \
    REDIS_ENABLED=false HOST_API_ENABLED=false AI_ENABLED=false EMAIL_PROVIDER=fake \
    AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN=false AUTH_BOOTSTRAP_ADMIN_SECRET="$bootstrap_secret" \
    JWT_SECRET="$jwt_secret" AUTH_CHALLENGE_ACTIVE_KEY_ID=v12 \
    AUTH_CHALLENGE_HMAC_KEYS="v12:$challenge_secret" \
    AUTH_CHALLENGE_IP_HASH_SECRET="$challenge_secret" \
    AUTH_SESSION_IP_HASH_SECRET="$challenge_secret" \
    AUTH_MFA_ACTIVE_KEY_ID=v12 AUTH_MFA_ENCRYPTION_KEYS="v12:$mfa_secret" \
    PLUGINS_DIR="$work_dir/app/data/plugins" PLUGIN_DATA_DIR="$work_dir/app/data/plugin_data" \
    CAMPUSOS_PLUGIN_V4_DIR="$work_dir/app/plugins" CAMPUSOS_PLUGIN_V4_DEV_SOURCE=false \
    CAMPUSOS_PLUGIN_UI_ORIGIN="http://127.0.0.1:$plugin_ui_port" \
    MODULE_DATA_DIR="$work_dir/app/data/module_data" RESOURCE_DIR="$work_dir/app/data/resources" \
    "$work_dir/campusos-server"
) >"$work_dir/api.log" 2>&1 &
api_pid="$!"
for attempt in $(seq 1 80); do
  if curl -fsS "http://127.0.0.1:$api_port/api/v1/health" >/dev/null 2>&1; then break; fi
  if ! kill -0 "$api_pid" 2>/dev/null; then
    echo "isolated API exited before health check" >&2
    exit 1
  fi
  if (( attempt == 80 )); then echo "isolated API did not become healthy" >&2; exit 1; fi
  sleep 0.25
done
curl -fsS "http://127.0.0.1:$api_port/api/v1/threads?page=1&page_size=1" >/dev/null
require_equals "post-start migration checksum" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "6"
kill "$api_pid" 2>/dev/null || true
wait "$api_pid" 2>/dev/null || true
api_pid=""

# Restore to a second database inside the same disposable container. This
# proves the 01a data/schema snapshot can be reopened; credential generation
# rotation after a whole-system restore remains a later stage.
docker exec -e PGPASSWORD="$pg_password" "$container" \
  pg_dump -U campusos -d "$database" --format=custom --no-owner --no-acl >"$work_dir/exit-snapshot.dump"
[[ -s "$work_dir/exit-snapshot.dump" ]] || { echo "isolated pg_dump produced no snapshot" >&2; exit 1; }
docker exec -e PGPASSWORD="$pg_password" "$container" \
  createdb -U campusos --template=template0 "$restored_database"
docker exec -i -e PGPASSWORD="$pg_password" "$container" \
  pg_restore -U campusos -d "$restored_database" --exit-on-error --no-owner --no-privileges \
  <"$work_dir/exit-snapshot.dump" >"$work_dir/restore.log" 2>&1 || {
    tail -n 100 "$work_dir/restore.log" >&2
    exit 1
  }
run_migrate_db "$restored_database" migrations check >"$work_dir/restored-migration-check.log"
require_equals "restored migration count" "$(psql_scalar_db "$restored_database" 'SELECT count(*) FROM schema_migrations;')" "6"
require_equals "restored audit actor domains" "$(psql_scalar_db "$restored_database" "SELECT string_agg(actor_kind,',' ORDER BY actor_kind) FROM authorization_audits WHERE id IN (992003,992004);")" "admin,user"
require_equals "restored historical unknown actor" "$(psql_scalar_db "$restored_database" "SELECT actor_kind FROM authorization_audits WHERE id=992002;")" "legacy_unknown"
require_equals "restored current version" "$(psql_scalar_db "$restored_database" "SELECT version FROM plugin_versions WHERE plugin_id=992010 AND lifecycle_status='active';")" "1.0.0"
require_equals "restored declaration identity" "$(psql_scalar_db "$restored_database" 'SELECT id FROM plugin_capability_declarations WHERE plugin_version_id=992012;')" "992013"
require_equals "restored revoked grant consent and token" "$(psql_scalar_db "$restored_database" "SELECT ag.status || ':' || uc.status || ':' || d.status FROM plugin_admin_grants ag JOIN plugin_user_consents uc USING(plugin_version_id,capability_code) JOIN plugin_delegations d ON d.plugin_version_id=ag.plugin_version_id WHERE ag.id=992014 AND uc.id=992015 AND d.id=992016;")" "revoked:revoked:revoked"
require_equals "restored publication guard" "$(psql_scalar_db "$restored_database" "SELECT count(*) FROM pg_trigger WHERE tgname='trg_plugin_declaration_membership_guard' AND NOT tgisinternal;")" "1"
reject_sql_db "$restored_database" "restored-declaration-guard" "23514" "DELETE FROM plugin_capability_declarations WHERE id=992013;"
reject_sql_db "$restored_database" "restored-audit-actor-guard" "23514" "INSERT INTO authorization_audits(id,actor_kind,actor_id,outcome) VALUES (992099,'admin',NULL,'allow');"

V12_01A_REPORT_PATH="$report" V12_01A_PG_VERSION="$pg_version" \
V12_01A_DUMP_PATH="$work_dir/exit-snapshot.dump" python3 - <<'PYREPORT'
import hashlib
import json
import os
import platform
from datetime import datetime, timezone
from pathlib import Path

report = Path(os.environ['V12_01A_REPORT_PATH'])
report.parent.mkdir(parents=True, exist_ok=True)
sources = [
    Path('migrations/000004_v1_2_authorization_audit_actor.up.sql'),
    Path('migrations/000004_v1_2_authorization_audit_actor.down.sql'),
    Path('migrations/000005_v1_2_plugin_version_identity.up.sql'),
    Path('migrations/000005_v1_2_plugin_version_identity.down.sql'),
    Path('migrations/000006_v1_2_plugin_publication_seal.up.sql'),
    Path('migrations/000006_v1_2_plugin_publication_seal.down.sql'),
    Path('internal/modules/core/identity/repository/pg_role_repository.go'),
    Path('internal/modules/core/identity/repository/authorization_audit_actor_test.go'),
    Path('internal/plugin/authorization.go'),
    Path('internal/plugin/authorization_pg.go'),
    Path('internal/plugin/authorization_handler.go'),
    Path('internal/plugin/authorization_version_pg_integration_test.go'),
    Path('internal/plugin/authorization_active_version_pg_integration_test.go'),
    Path('internal/plugin/authorization_version_handler_test.go'),
    Path('internal/plugin/authorization_publication_pg_integration_test.go'),
    Path('scripts/schema-contract.sql'),
    Path('scripts/v12-01a-exit-drill.sh'),
]
missing = [str(path) for path in sources if not path.is_file()]
if missing:
    raise SystemExit('missing required acceptance source files: ' + ', '.join(missing))
data = {
    'schema': 'campusos.v12-01a-exit-drill/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-01a',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'environment': {
        'os': platform.system().lower(),
        'architecture': platform.machine(),
        'postgres_server_version_num': int(os.environ['V12_01A_PG_VERSION']),
        'postgres_image': 'postgres:16-alpine',
        'database_storage': 'temporary container tmpfs',
        'postgres_bind': '127.0.0.1',
        'api_bind': '127.0.0.1',
        'restore_database': 'second isolated database in the same disposable container',
    },
    'checks': {
        'v11_baseline_to_000006_up_and_checksum': 'passed',
        'authorization_audit_actor_backfill_and_constraints': 'passed',
        'plugin_publication_and_declaration_set_guards': 'passed',
        'version_reactivation_preserves_identity_and_revokes_old_delegation': 'passed',
        '000006_down_up_and_checksum_drift_rejection': 'passed',
        'current_database_and_historical_v11_gate': 'passed',
        'postgres_audit_repository_test': 'passed',
        'postgres_version_concurrency_test': 'passed',
        'postgres_active_authorization_test': 'passed',
        'postgres_publication_write_race_test': 'passed',
        'gin_http_versioned_handler_test': 'passed',
        'built_api_health_and_community_list': 'passed',
        'isolated_pg_dump_restore_and_reopened_constraints': 'passed',
    },
    'http_coverage': 'Versioned mutation responses were exercised by Gin HTTP handlers backed by PostgreSQL; the separately booted API was checked for health and community list.',
    'restore_scope': 'Database snapshot, schema, authorization facts and constraints only; whole-system credential generation rotation is reserved for V12-08b.',
    'snapshot_sha256': hashlib.sha256(Path(os.environ['V12_01A_DUMP_PATH']).read_bytes()).hexdigest(),
    'source_sha256': {str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in sources},
}
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print(f"V12-01a isolated PostgreSQL 16 exit drill passed; evidence: {report}")
PYREPORT
