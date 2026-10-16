#!/usr/bin/env bash
# V12-02d admin identity domain store acceptance on an owned PostgreSQL 16
# tmpfs container. The developer database is never touched.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
for executable in docker go python3; do
  command -v "$executable" >/dev/null || { echo "missing $executable" >&2; exit 127; }
done
if (( $# != 0 && $# != 2 )) || { (( $# == 2 )) && [[ "$1" != "--report" ]]; }; then
  echo "usage: $0 [--report evidence.json]" >&2
  exit 2
fi

mkdir -p .cache/v12-02d-mfa-security/tmp
export TMPDIR="$repo_root/.cache/v12-02d-mfa-security/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02d-mfa-security-linux.json}"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02d-mfa-security/store.XXXXXX")"
container="campusos-v12-02d-mfa-$$"
database="campusos_v12_02d_mfa"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
container_started=false

cleanup() {
  local status=$?
  if [[ "$container_started" == true ]]; then
    if (( status != 0 )); then
      docker logs "$container" >"$repo_root/.cache/v12-02d-mfa-security/postgres-last-failure.log" 2>&1 || true
      echo "isolated PostgreSQL failure log: .cache/v12-02d-mfa-security/postgres-last-failure.log" >&2
    fi
    docker stop "$container" >/dev/null 2>&1 || true
  fi
  if (( status != 0 )); then
    echo "work directory retained for diagnosis: $work_dir" >&2
  fi
}
trap cleanup EXIT

run_check() {
  local name="$1"
  shift
  echo "V12-02d mfa security: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then
    return
  fi
  tail -n 120 "$work_dir/$name.log" >&2
  return 1
}

# The host /tmp is full and unreliable for runc exec state files, so every
# PostgreSQL interaction runs in a throwaway client container that shares the
# database container's network namespace; docker run never touches /tmp.
psql_scalar() {
  docker run --rm -i --network "container:$container" -e PGPASSWORD="$pg_password" \
    postgres:16-alpine psql -h 127.0.0.1 -U campusos -d "$database" -qAtv ON_ERROR_STOP=1 -v VERBOSITY=verbose -c "$1"
}
# migrate.sh runs in host mode against a psql shim that forwards into a client
# container on the database network namespace.
shim_dir="$work_dir/bin"
mkdir -p "$shim_dir"
cat >"$shim_dir/psql" <<'SHIM'
#!/usr/bin/env bash
exec docker run --rm -i --network "container:$DRILL_DB_CONTAINER" -v "$DRILL_TMP:$DRILL_TMP:ro" -e PGPASSWORD postgres:16-alpine psql "$@"
SHIM
chmod +x "$shim_dir/psql"
run_migrate_dir() {
  local migration_dir="$1"
  shift
  CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host PATH="$shim_dir:$PATH" \
    DRILL_DB_CONTAINER="$container" DRILL_TMP="$TMPDIR" \
    DB_HOST=127.0.0.1 DB_PORT=5432 \
    DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database" \
    MIGRATIONS_DIR="$migration_dir" bash scripts/migrate.sh "$@"
}
run_migrate() { run_migrate_dir "migrations" "$@"; }
require_equals() {
  local label="$1" actual="$2" expected="$3"
  if [[ "$actual" != "$expected" ]]; then
    echo "$label: expected '$expected', got '$actual'" >&2
    exit 1
  fi
}
reject_sql() {
  local label="$1" sqlstate="$2" statement="$3"
  if psql_scalar "$statement" >"$work_dir/rejected-$label.log" 2>&1; then
    echo "$label: invalid mutation was accepted" >&2
    exit 1
  fi
  if ! grep -Eq "ERROR: +$sqlstate:" "$work_dir/rejected-$label.log"; then
    echo "$label: rejection did not have SQLSTATE $sqlstate" >&2
    cat "$work_dir/rejected-$label.log" >&2
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
  if psql_scalar 'SELECT 1;' >/dev/null 2>&1; then break; fi
  if (( attempt == 80 )); then echo "isolated PostgreSQL did not stabilize" >&2; exit 1; fi
  sleep 0.5
done
pg_version="$(psql_scalar 'SHOW server_version_num;')"
if (( pg_version < 160000 || pg_version >= 170000 )); then
  echo "expected PostgreSQL 16, got server_version_num=$pg_version" >&2
  exit 1
fi
pg_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
[[ "$pg_port" =~ ^[0-9]+$ ]] || { echo "PostgreSQL port is not loopback-bound" >&2; exit 1; }
database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$database?sslmode=disable"

# This slice validates the 000011 baseline; later chain members are excluded
# so the rollback semantics under test remain exactly migration 000011.
stage_dir="$work_dir/migrations-baseline"
mkdir -p "$stage_dir"
cp migrations/00000[1-9]_*.sql migrations/00001[01]_*.sql "$stage_dir/"
run_check migrations run_migrate_dir "$stage_dir" up
require_equals "migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "11"

require_equals "no seeded admin TOTP" "$(psql_scalar 'SELECT count(*) FROM admin_mfa_totp;')" "0"
require_equals "no seeded admin tickets" "$(psql_scalar 'SELECT count(*) FROM admin_mfa_tickets;')" "0"

# Direct SQL negatives hit the CHECK constraints, not application code.
reject_sql "totp-active-without-confirmation" "23514" "INSERT INTO admin_accounts (id,username,email) VALUES ('sql-1','sqltotp1','a@b.test'); INSERT INTO admin_mfa_totp (account_id,status,key_id,nonce,ciphertext,enrollment_expires_at) VALUES ('sql-1','active','k','nonce-123456','ciphertext-0123456789',NOW()+INTERVAL '1 hour');"
reject_sql "totp-short-envelope" "23514" "INSERT INTO admin_accounts (id,username,email) VALUES ('sql-2','sqltotp2','b@b.test'); INSERT INTO admin_mfa_totp (account_id,status,key_id,nonce,ciphertext,enrollment_expires_at) VALUES ('sql-2','pending','k','short','c',NOW()+INTERVAL '1 hour');"
reject_sql "ticket-bad-audience" "23514" "INSERT INTO admin_accounts (id,username,email) VALUES ('sql-3','sqlticket1','c@b.test'); INSERT INTO admin_mfa_tickets (id,account_id,audience,purpose,ticket_digest,expires_at) VALUES ('sql-3-t','sql-3','web','login','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',NOW()+INTERVAL '10 minutes');"
reject_sql "ticket-bad-purpose" "23514" "INSERT INTO admin_accounts (id,username,email) VALUES ('sql-4','sqlticket2','d@b.test'); INSERT INTO admin_mfa_tickets (id,account_id,audience,purpose,ticket_digest,expires_at) VALUES ('sql-4-t','sql-4','admin','anything','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',NOW()+INTERVAL '10 minutes');"
reject_sql "ticket-bad-attempts" "23514" "INSERT INTO admin_accounts (id,username,email) VALUES ('sql-5','sqlticket3','e@b.test'); INSERT INTO admin_mfa_tickets (id,account_id,audience,purpose,ticket_digest,expires_at,max_attempts) VALUES ('sql-5-t','sql-5','admin','login','cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',NOW()+INTERVAL '10 minutes',99);"
reject_sql "ticket-missing-account" "23503" "INSERT INTO admin_mfa_tickets (id,account_id,audience,purpose,ticket_digest,expires_at) VALUES ('sql-6-t','sql-missing','admin','login','dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',NOW()+INTERVAL '10 minutes');"
reject_sql "recovery-bad-purpose" "23514" "INSERT INTO admin_accounts (id,username,email) VALUES ('sql-7','sqlrec1','f@b.test'); INSERT INTO admin_recovery_tickets (id,account_id,purpose,ticket_digest,expires_at) VALUES ('sql-7-t','sql-7','other','eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',NOW()+INTERVAL '10 minutes');"

# Go repository against the real database: lifecycle, CAS, security count, race.
run_check go-repository env CAMPUSOS_IDENTITY_TEST_DATABASE_URL="$database_url" \
  go test -race ./internal/modules/core/identity/repository -run '^(TestAdminDomain|TestAdminSecurity|TestAdminRecovery)' -count=1 -v

# Rollback refuses live admin MFA/recovery data, then removes the stores when empty.
psql_scalar "INSERT INTO admin_accounts (id,username,email) VALUES ('rollback-guard','rollbackguard','g@b.test'); INSERT INTO admin_mfa_totp (account_id,status,key_id,nonce,ciphertext,enrollment_expires_at) VALUES ('rollback-guard','pending','k','nonce-123456','ciphertext-0123456789',NOW()+INTERVAL '1 hour');" >/dev/null
if run_migrate down >"$work_dir/down-refused.log" 2>&1; then
  echo "down succeeded despite live admin MFA data" >&2
  exit 1
fi
grep -q 'admin MFA/recovery data must be exported' "$work_dir/down-refused.log" || { echo "down refusal lacks the guard message" >&2; cat "$work_dir/down-refused.log" >&2; exit 1; }
psql_scalar "DELETE FROM admin_recovery_tickets; DELETE FROM admin_mfa_tickets; DELETE FROM admin_mfa_totp; DELETE FROM admin_role_bindings WHERE granted_by = 'repo-test'; DELETE FROM admin_credentials WHERE account_id LIKE 'repo-test-%'; DELETE FROM admin_sessions; DELETE FROM admin_accounts;" >/dev/null
run_migrate_dir "$stage_dir" down >"$work_dir/down.log" 2>&1
require_equals "tables dropped" "$(psql_scalar "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('admin_mfa_totp','admin_mfa_tickets','admin_recovery_tickets');")" "0"
require_equals "migration count after down" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "10"
run_migrate_dir "$stage_dir" up >>"$work_dir/down.log" 2>&1
require_equals "migration count after re-up" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "11"

python3 - "$work_dir" "$report" <<'PY'
import hashlib
import json
import platform
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

work, report = map(Path, sys.argv[1:])
root = Path.cwd()
text = (work / 'go-repository.log').read_text()
required_tests = [
    'TestAdminDomainAccountLifecycle',
    'TestAdminDomainCredentialCAS',
    'TestAdminDomainRoleBindingsAndSecurityCount',
    'TestAdminDomainConcurrentStatusCAS',
    'TestAdminSecurityTOTPLifecycle',
    'TestAdminSecurityTicketSingleUse',
    'TestAdminRecoveryTicketSingleUse',
    'TestAdminSecurityTicketDigestShape',
    'TestAdminSecurityConcurrentConsume',
]
for test in required_tests:
    assert re.search(r'^--- PASS: ' + test + r'(?: |$)', text, re.M), test
assert not re.search(r'^--- (?:FAIL|SKIP): ', text, re.M), 'unexpected failure or skip'
sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
sources = [
    'migrations/000011_v1_2_admin_mfa_recovery.up.sql',
    'migrations/000011_v1_2_admin_mfa_recovery.down.sql',
    'internal/modules/core/identity/repository/pg_admin_domain_repository.go',
    'internal/modules/core/identity/repository/pg_admin_security_repository_test.go',
    'admin/src/modules/architecture/pages/SystemArchitectureView.vue',
    'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md',
    'scripts/v12-02d-mfa-security-drill.sh',
]
logs = {
    name: {'path': str((work / f'{name}.log').relative_to(root)), 'sha256': sha(work / f'{name}.log')}
    for name in ['migrations', 'go-repository', 'down-refused', 'down']
    if (work / f'{name}.log').exists()
}
dependencies = {}
for name in ['v12-g0-exit.json', 'v12-g1-exit-linux.json', 'v12-01a-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-02a-exit-linux.json', 'v12-02d-identity-domains-linux.json']:
    path = Path('docs/项目计划书v1/项目计划v1.2/evidence') / name
    dependencies[name] = {
        'report': str(path),
        'sha256': sha(path),
        'role': 'accepted predecessor evidence; historical source snapshots are retained unchanged',
    }
data = {
    'schema': 'campusos.v12-02d-mfa-security-acceptance/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-02d',
    'stage_status': 'partial',
    'slice_status': 'completed',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'scope': 'migration 000011 admin MFA/recovery stores (TOTP envelope, MFA tickets, recovery tickets) with PostgreSQL/Memory repositories and isolated PostgreSQL 16 acceptance; no service or HTTP wiring',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go': subprocess.check_output(['go', 'version'], text=True).strip(),
        'postgres': 'PostgreSQL 16 (owned tmpfs container, loopback-only port)',
    },
    'checks': {test: 'passed' for test in required_tests},
    'coverage': {
        'migration_chain': '000001-000011 up + check',
        'seed_assertions': 2,
        'sql_constraint_negatives': 7,
        'down_up_cycle': '000011 down refused with live data, re-up clean',
        'go_repository_tests': 9,
        'adapters': ['postgres', 'memory'],
    },
    'source_sha256': {path: sha(path) for path in sources},
    'dependencies': dependencies,
    'target_logs': logs,
    'common_gates_status': 'pending',
    'common_gates': {},
    'boundaries': [
        'No service, HTTP route, JWT issuer or middleware consumes these tables yet; the runtime domain switch is a later V12-02d slice.',
        'The legacy admission table identity_admin_accounts, the auto-admission trigger and role_assignment activation remain until their dedicated replacement slices; the G1 replacement-map checker still requires pending for them.',
        'Admin sessions, MFA, recovery stores and admin-domain role permission bindings arrive in later slices; no default administrator is seeded.',
    ],
}
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print('Native Linux target acceptance passed; common gates pending')
PY

run_check go-all go test ./... -count=1
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check database-er python3 migrations/tools/generate_er.py --check
run_check architecture-sync python3 skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
run_check admin-build make admin-build
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check

python3 - "$work_dir" "$report" <<'PY'
import hashlib
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

work, report = map(Path, sys.argv[1:])
data = json.loads(report.read_text())
commands = {
    'go-all': 'go test ./... -count=1',
    'contracts': 'make contracts-check',
    'architecture': 'make architecture-check',
    'database-er': 'python3 migrations/tools/generate_er.py --check',
    'architecture-sync': 'check_architecture_sync.py --root .',
    'admin-build': 'make admin-build',
    'docs-build': 'make docs-build',
    'docs-links': 'make docs-links',
    'line-endings': 'python3 scripts/check-line-endings.py --include-untracked',
    'diff': 'git diff --check',
}
for name, command in commands.items():
    path = work / f'{name}.log'
    data['common_gates'][name] = {
        'command': command,
        'status': 'passed',
        'exit_code': 0,
        'local_log': str(path.relative_to(Path.cwd())),
        'log_sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
    }
for path, digest in data['source_sha256'].items():
    assert hashlib.sha256(Path(path).read_bytes()).hexdigest() == digest, 'source changed during acceptance: ' + path
data['common_gates_status'] = 'passed'
data['generated_at'] = datetime.now(timezone.utc).isoformat()
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print('V12-02d mfa security slice passed; 10 common gates passed; stage remains partial')
PY
