#!/usr/bin/env bash
# V12-02d auto-admission removal acceptance on an owned PostgreSQL 16 tmpfs
# container. The developer database is never touched. Client-container psql is
# used because host /tmp is unreliable for runc exec state files.
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

mkdir -p .cache/v12-02d-auto-admission/tmp
export TMPDIR="$repo_root/.cache/v12-02d-auto-admission/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02d-auto-admission-linux.json}"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02d-auto-admission/auto.XXXXXX")"
container="campusos-v12-02d-auto-$$"
database="campusos_v12_02d_auto"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
container_started=false

cleanup() {
  local status=$?
  if [[ "$container_started" == true ]]; then
    if (( status != 0 )); then
      docker logs "$container" >"$repo_root/.cache/v12-02d-auto-admission/postgres-last-failure.log" 2>&1 || true
      echo "isolated PostgreSQL failure log: .cache/v12-02d-auto-admission/postgres-last-failure.log" >&2
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
  echo "V12-02d auto admission: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then
    return
  fi
  tail -n 120 "$work_dir/$name.log" >&2
  return 1
}

psql_scalar() {
  docker run --rm -i --network "container:$container" -e PGPASSWORD="$pg_password" \
    postgres:16-alpine psql -h 127.0.0.1 -U campusos -d "$database" -qAtv ON_ERROR_STOP=1 -v VERBOSITY=verbose -c "$1"
}
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

stage_dir="$work_dir/migrations-pre"
mkdir -p "$stage_dir"
cp migrations/00000[1-9]_*.sql migrations/00001[01]_*.sql "$stage_dir/"
run_check migrations-to-11 run_migrate_dir "$stage_dir" up
require_equals "migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "11"

# Fixture: user with email account, no admission row yet.
psql_scalar "
INSERT INTO users (id, username, nickname, email) VALUES (88001, 'auto_drill', 'Auto Drill', 'auto@drill.test');
INSERT INTO accounts (id, user_id, type, identifier, credential, identifier_normalized, verification_state)
  VALUES (88001, 88001, 'email', 'auto@drill.test', 'fixture', 'auto@drill.test', 'verified');
" >/dev/null

# Baseline behavior before 000012: the trigger provisions admission.
psql_scalar "INSERT INTO user_roles (id, user_id, role_id, scope_type) VALUES (880010, 88001, 1, 'global');" >/dev/null
require_equals "trigger provisions admission" "$(psql_scalar "SELECT count(*) FROM identity_admin_accounts WHERE user_id = 88001 AND status = 'active';")" "1"
psql_scalar "DELETE FROM user_roles WHERE id = 880010; DELETE FROM identity_admin_accounts WHERE user_id = 88001;" >/dev/null

total_migrations="$(ls migrations/*.up.sql | wc -l)"
run_check apply-000012 run_migrate up
require_equals "migration count after full chain" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "$total_migrations"
require_equals "trigger removed" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname = 'trg_sync_identity_admin_account_from_role';")" "0"
require_equals "helper removed" "$(psql_scalar "SELECT count(*) FROM pg_proc WHERE proname IN ('sync_identity_admin_account_from_role','sync_identity_admin_account_for_user');")" "0"

# After 000012 the same role assignment provisions nothing.
psql_scalar "INSERT INTO user_roles (id, user_id, role_id, scope_type) VALUES (880011, 88001, 1, 'global');" >/dev/null
require_equals "no auto admission" "$(psql_scalar "SELECT count(*) FROM identity_admin_accounts WHERE user_id = 88001;")" "0"
# Existing admissions are preserved (legacy rows untouched).
psql_scalar "INSERT INTO identity_admin_accounts (id, user_id, credential_account_id) VALUES (880012, 88001, 88001);" >/dev/null
require_equals "legacy admission preserved" "$(psql_scalar "SELECT count(*) FROM identity_admin_accounts WHERE user_id = 88001 AND status = 'active';")" "1"
psql_scalar "DELETE FROM user_roles WHERE id = 880011; DELETE FROM identity_admin_accounts WHERE user_id = 88001;" >/dev/null

# Application-level decoupling against real PostgreSQL.
pg_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$database?sslmode=disable"
run_check service-tests env CAMPUSOS_IDENTITY_TEST_DATABASE_URL="$database_url" \
  go test -race -count=1 -v ./internal/modules/core/identity/service -run '^(TestAdminAccountAdmission|TestPostgreSQLAdminDomain)'

# Rollback restores the trigger byte-for-byte semantics. The staged chain is
# capped at 000012 so "down" rolls back exactly the auto-admission removal.
final_dir="$work_dir/migrations-final"
mkdir -p "$final_dir"
cp migrations/*.sql "$final_dir/"
# migrate.sh down rolls back exactly one version per invocation; roll back
# 000014, 000013 and 000012 so the auto-admission removal itself is undone.
down_three() {
  run_migrate_dir "$final_dir" down && run_migrate_dir "$final_dir" down && run_migrate_dir "$final_dir" down
}
run_check down down_three
require_equals "trigger restored" "$(psql_scalar "SELECT count(*) FROM pg_trigger WHERE tgname = 'trg_sync_identity_admin_account_from_role';")" "1"
psql_scalar "INSERT INTO user_roles (id, user_id, role_id, scope_type) VALUES (880013, 88001, 1, 'global');" >/dev/null
require_equals "trigger provisions after rollback" "$(psql_scalar "SELECT count(*) FROM identity_admin_accounts WHERE user_id = 88001 AND status = 'active';")" "1"
psql_scalar "DELETE FROM user_roles WHERE id = 880013; DELETE FROM identity_admin_accounts WHERE user_id = 88001; DELETE FROM accounts WHERE user_id = 88001; DELETE FROM users WHERE id = 88001;" >/dev/null

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
text = (work / 'service-tests.log').read_text()
required_tests = ['TestAdminAccountAdmissionIsIndependentFromRoleAssignment', 'TestPostgreSQLAdminDomainLifecycleAndRace']
for test in required_tests:
    assert re.search(r'^--- PASS: ' + test + r'(?: |$)', text, re.M), test
assert not re.search(r'^--- (?:FAIL|SKIP): ', text, re.M), 'unexpected failure or skip'
sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
sources = [
    'migrations/000012_v1_2_remove_admin_role_auto_admission.up.sql',
    'migrations/000012_v1_2_remove_admin_role_auto_admission.down.sql',
    'internal/modules/core/identity/service/permission_service.go',
    'internal/modules/core/identity/service/admin_access_service_test.go',
    'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md',
    'scripts/v12-02d-auto-admission-drill.sh',
]
logs = {
    name: {'path': str((work / f'{name}.log').relative_to(root)), 'sha256': sha(work / f'{name}.log')}
    for name in ['migrations-to-11', 'apply-000012', 'service-tests', 'down']
}
dependencies = {}
for name in ['v12-g0-exit.json', 'v12-g1-exit-linux.json', 'v12-01a-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-02a-exit-linux.json', 'v12-02d-admin-service-linux.json']:
    path = Path('docs/项目计划书v1/项目计划v1.2/evidence') / name
    dependencies[name] = {
        'report': str(path),
        'sha256': sha(path),
        'role': 'accepted predecessor evidence; historical source snapshots are retained unchanged',
    }
data = {
    'schema': 'campusos.v12-02d-auto-admission-acceptance/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-02d',
    'stage_status': 'partial',
    'slice_status': 'completed',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'scope': 'migration 000012 removes the user-role auto-admission SQL trigger and helper; PermissionService stops syncing admission from role assignment or revocation; existing admission rows are preserved',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go': subprocess.check_output(['go', 'version'], text=True).strip(),
        'postgres': 'PostgreSQL 16 (owned tmpfs container, loopback-only port; client-container psql)',
    },
    'checks': {test: 'passed' for test in required_tests},
    'coverage': {
        'trigger_before_after': 'baseline provisions, post-000012 silent, rollback restores',
        'legacy_rows_preserved': True,
        'service_decoupling': 'role assignment/revocation no longer touches admission',
    },
    'source_sha256': {path: sha(path) for path in sources},
    'dependencies': dependencies,
    'target_logs': logs,
    'common_gates_status': 'pending',
    'common_gates': {},
    'boundaries': [
        'Legacy admission rows keep serving the current Admin UI; the admin login/session HTTP switch slice replaces the entry path.',
        'AdminDomainService has no HTTP caller yet; explicit administrator creation arrives with the login routes slice.',
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
print('V12-02d auto-admission slice passed; 9 common gates passed; stage remains partial')
PY
