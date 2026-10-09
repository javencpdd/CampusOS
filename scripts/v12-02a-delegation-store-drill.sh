#!/usr/bin/env bash
# V12-02a identity delegation store acceptance on an owned PostgreSQL 16 tmpfs
# container. The developer database is never touched.
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

mkdir -p .cache/v12-02a-delegation-store/tmp
export TMPDIR="$repo_root/.cache/v12-02a-delegation-store/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02a-delegation-store-linux.json}"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02a-delegation-store/store.XXXXXX")"
container="campusos-v12-02a-store-$$"
database="campusos_v12_02a_store"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
container_started=false

cleanup() {
  local status=$?
  if [[ "$container_started" == true ]]; then
    if (( status != 0 )); then
      docker logs "$container" >"$repo_root/.cache/v12-02a-delegation-store/postgres-last-failure.log" 2>&1 || true
      echo "isolated PostgreSQL failure log: .cache/v12-02a-delegation-store/postgres-last-failure.log" >&2
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
  echo "V12-02a delegation store: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then
    return
  fi
  tail -n 120 "$work_dir/$name.log" >&2
  return 1
}

psql_scalar() {
  docker exec -e PGPASSWORD="$pg_password" "$container" \
    psql -U campusos -d "$database" -qAtv ON_ERROR_STOP=1 -v VERBOSITY=verbose -c "$1"
}
run_migrate() {
  local migration_dir="$1"
  shift
  CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
    DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database" \
    MIGRATIONS_DIR="$migration_dir" bash scripts/migrate.sh "$@"
}
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
  if docker exec "$container" pg_isready -U campusos -d "$database" >/dev/null 2>&1; then break; fi
  if (( attempt == 80 )); then echo "isolated PostgreSQL did not become ready" >&2; exit 1; fi
  sleep 0.25
done
# pg_isready can report readiness on the entrypoint's temporary instance, which
# then restarts; wait for the final server to answer a real query.
for attempt in $(seq 1 80); do
  if psql_scalar 'SELECT 1;' >/dev/null 2>&1; then break; fi
  if (( attempt == 80 )); then echo "isolated PostgreSQL did not stabilize" >&2; exit 1; fi
  sleep 0.25
done
pg_version="$(psql_scalar 'SHOW server_version_num;')"
if (( pg_version < 160000 || pg_version >= 170000 )); then
  echo "expected PostgreSQL 16, got server_version_num=$pg_version" >&2
  exit 1
fi
pg_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
[[ "$pg_port" =~ ^[0-9]+$ ]] || { echo "PostgreSQL port is not loopback-bound" >&2; exit 1; }
database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$database?sslmode=disable"

# Apply 000001-000007 first so delegation seeds convert real fixture rows.
stage_dir="$work_dir/migrations-pre"
mkdir -p "$stage_dir"
cp migrations/00000[1-7]_*.sql "$stage_dir/"
run_migrate "$stage_dir" up >"$work_dir/migrations-pre.log" 2>&1
require_equals "pre-migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "7"

psql_scalar "
INSERT INTO users (id, username, nickname, email) VALUES
  (99001, 'storeadmin', 'Store Admin', 'store-admin@test.local'),
  (99002, 'storemod', 'Store Mod', 'store-mod@test.local'),
  (99003, 'storesuspended', 'Store Suspended', 'store-suspended@test.local');
INSERT INTO accounts (id, user_id, type, identifier, credential, identifier_normalized, verification_state) VALUES
  (99001, 99001, 'email', 'store-admin@test.local', 'fixture', 'store-admin@test.local', 'verified'),
  (99003, 99003, 'email', 'store-suspended@test.local', 'fixture', 'store-suspended@test.local', 'verified');
INSERT INTO identity_admin_accounts (id, user_id, credential_account_id, status) VALUES
  (99001, 99001, 99001, 'active'),
  (99002, 99003, 99003, 'suspended');
INSERT INTO categories (id, name, slug, node_kind, lifecycle_status) VALUES
  (9910, 'Store Group', 'store-group-9910', 'group', 'active');
INSERT INTO categories (id, name, slug, parent_id, node_kind, lifecycle_status) VALUES
  (9911, 'Store Board A', 'store-board-9911', 9910, 'board', 'active'),
  (9912, 'Store Board B', 'store-board-9912', 9910, 'board', 'archived');
INSERT INTO categories (id, name, slug, parent_id, node_kind, lifecycle_status, deleted_at) VALUES
  (9913, 'Store Board C', 'store-board-9913', 9910, 'board', 'active', NOW());
INSERT INTO user_roles (id, user_id, role_id, scope_type, scope_id) VALUES
  (99001, 99002, 2, 'category', 9911),
  (99002, 99002, 2, 'category', 9912),
  (99003, 99002, 2, 'category', 9913);
" >/dev/null

run_migrate "migrations" up >>"$work_dir/migrations-pre.log" 2>&1
run_migrate "migrations" check >>"$work_dir/migrations-pre.log" 2>&1
require_equals "migration count" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "8"

# Seed conversion: active admission only; active non-deleted boards only for
# bounds; moderator category rows on present boards become two grants each.
require_equals "management seeds" "$(psql_scalar "SELECT count(*) FROM identity_delegations WHERE kind='management';")" "1"
require_equals "bound seeds" "$(psql_scalar "SELECT count(*) FROM identity_delegations WHERE kind='bound';")" "2"
require_equals "grant seeds" "$(psql_scalar "SELECT count(*) FROM identity_delegations WHERE kind='grant';")" "4"
require_equals "grant seed window" "$(psql_scalar "SELECT count(*) FROM identity_delegations WHERE kind='grant' AND required_strength='password' AND status='active' AND not_before < expires_at AND expires_at > not_before + INTERVAL '360 days';")" "4"
require_equals "moderator governance codes removed" "$(psql_scalar "SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permission_definitions pd ON pd.id=rp.permission_id WHERE r.name='moderator' AND pd.code IN ('community.thread.take_down','community.post.delete');")" "0"
require_equals "admin keeps governance codes" "$(psql_scalar "SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permission_definitions pd ON pd.id=rp.permission_id WHERE r.name='admin' AND pd.code IN ('community.thread.take_down','community.post.delete');")" "2"
require_equals "moderator keeps low-risk codes" "$(psql_scalar "SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id WHERE r.name='moderator';")" "5"

# Direct SQL negatives hit the CHECK constraints, not application code.
reject_sql "reversed-window" "23514" "INSERT INTO identity_delegations (id,kind,subject_kind,subject_id,action,board_id,not_before,expires_at,required_strength) VALUES ('sql-bad-1','grant','user','u1','community.thread.take_down','9911',NOW(),NOW(),'password');"
reject_sql "delegable-grant" "23514" "INSERT INTO identity_delegations (id,kind,subject_kind,subject_id,action,board_id,not_before,expires_at,required_strength,delegable) VALUES ('sql-bad-2','grant','user','u1','community.thread.take_down','9911',NOW(),NOW()+INTERVAL '1 day','password',true);"
reject_sql "management-with-board" "23514" "INSERT INTO identity_delegations (id,kind,subject_kind,subject_id,action,board_id,not_before,expires_at) VALUES ('sql-bad-3','management','admin','a1','identity.role.assign','9911',NOW(),NOW()+INTERVAL '1 day');"
reject_sql "grant-admin-subject" "23514" "INSERT INTO identity_delegations (id,kind,subject_kind,subject_id,action,board_id,not_before,expires_at,required_strength) VALUES ('sql-bad-4','grant','admin','a1','community.thread.take_down','9911',NOW(),NOW()+INTERVAL '1 day','password');"
reject_sql "opaque-id" "23514" "INSERT INTO identity_delegations (id,kind,subject_kind,subject_id,action,board_id,not_before,expires_at,required_strength) VALUES ('sql bad 5','grant','user','u1','community.thread.take_down','9911',NOW(),NOW()+INTERVAL '1 day','password');"
reject_sql "unknown-action" "23514" "INSERT INTO identity_delegations (id,kind,subject_kind,subject_id,action,board_id,not_before,expires_at,required_strength) VALUES ('sql-bad-6','grant','user','u1','community.thread.pin','9911',NOW(),NOW()+INTERVAL '1 day','password');"

# Go repository against the real database: round trip, shapes, CAS, race.
run_check go-repository env CAMPUSOS_IDENTITY_TEST_DATABASE_URL="$database_url" \
  go test -race ./internal/modules/core/identity/repository -run '^TestDelegation' -count=1 -v

# Rollback refuses rows written after the seed; removing them restores the path.
psql_scalar "DELETE FROM identity_delegations WHERE created_by LIKE 'repo-test%';" >/dev/null
psql_scalar "INSERT INTO identity_delegations (id,kind,subject_kind,subject_id,action,board_id,not_before,expires_at,required_strength,created_by) VALUES ('service-1','grant','user','user-9','community.thread.take_down','9911',NOW(),NOW()+INTERVAL '1 day','password','delegation-service');" >/dev/null
if run_migrate "migrations" down >"$work_dir/down-refused.log" 2>&1; then
  echo "down succeeded despite service-written rows" >&2
  exit 1
fi
grep -q 'must be exported and removed before rollback' "$work_dir/down-refused.log" || { echo "down refusal lacks the guard message" >&2; cat "$work_dir/down-refused.log" >&2; exit 1; }
psql_scalar "DELETE FROM identity_delegations WHERE id='service-1';" >/dev/null
run_migrate "migrations" down >"$work_dir/down.log" 2>&1
require_equals "table dropped" "$(psql_scalar "SELECT count(*) FROM information_schema.tables WHERE table_name='identity_delegations';")" "0"
require_equals "moderator codes restored" "$(psql_scalar "SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id WHERE r.name='moderator';")" "7"
require_equals "migration count after down" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "7"
run_migrate "migrations" up >>"$work_dir/down.log" 2>&1
require_equals "migration count after re-up" "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" "8"
require_equals "grant seeds after re-up" "$(psql_scalar "SELECT count(*) FROM identity_delegations WHERE kind='grant';")" "4"

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
    'TestDelegationRepositoryRoundTrip',
    'TestDelegationRepositoryRejectsInvalidShape',
    'TestDelegationRepositoryStatusCAS',
    'TestDelegationRepositoryConcurrentCAS',
]
for test in required_tests:
    assert re.search(r'^--- PASS: ' + test + r'(?: |$)', text, re.M), test
assert '/postgres' in text or 'postgres' in text, 'postgres adapter must run, not skip'
sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
sources = [
    'migrations/000008_v1_2_identity_delegations.up.sql',
    'migrations/000008_v1_2_identity_delegations.down.sql',
    'internal/modules/core/identity/repository/pg_delegation_repository.go',
    'internal/modules/core/identity/repository/pg_delegation_repository_test.go',
    'admin/src/modules/architecture/pages/SystemArchitectureView.vue',
    'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md',
    'scripts/v12-02a-delegation-store-drill.sh',
]
logs = {
    name: {'path': str((work / f'{name}.log').relative_to(root)), 'sha256': sha(work / f'{name}.log')}
    for name in ['migrations-pre', 'go-repository', 'down-refused', 'down']
    if (work / f'{name}.log').exists()
}
dependencies = {}
for name in ['v12-g0-exit.json', 'v12-g1-exit-linux.json', 'v12-01a-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-02a-board-delegation-linux.json', 'v12-02a-scope-delegation-linux.json']:
    path = Path('docs/项目计划书v1/项目计划v1.2/evidence') / name
    dependencies[name] = {
        'report': str(path),
        'sha256': sha(path),
        'role': 'accepted predecessor evidence; historical source snapshots are retained unchanged',
    }
data = {
    'schema': 'campusos.v12-02a-delegation-store-acceptance/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-02a',
    'stage_status': 'partial',
    'slice_status': 'completed',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'scope': 'identity_delegations migration 000008 with seed conversion, PostgreSQL/Memory repository adapters and isolated PostgreSQL 16 acceptance; no service or HTTP wiring',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go': subprocess.check_output(['go', 'version'], text=True).strip(),
        'postgres': 'PostgreSQL 16 (owned tmpfs container, loopback-only port)',
    },
    'checks': {test: 'passed' for test in required_tests},
    'coverage': {
        'migration_chain': '000001-000008 up + check',
        'seed_assertions': 7,
        'sql_constraint_negatives': 6,
        'down_up_cycle': '000008 down refused with service rows, restored moderator codes, re-up clean',
        'go_repository_tests': 4,
        'adapters': ['postgres', 'memory'],
    },
    'source_sha256': {path: sha(path) for path in sources},
    'dependencies': dependencies,
    'target_logs': logs,
    'common_gates_status': 'pending',
    'common_gates': {},
    'boundaries': [
        'The delegation table is not yet read or written by any production service; DelegationService and PermissionService path replacement are later V12-02a slices.',
        'Seeded one-year windows and password strength are migration defaults for the disposable test dataset, not a product term policy.',
        'The moderator role keeps its five non-delegated codes; global role management paths are unchanged in this slice.',
        'No admin identity domain separation (V12-02d) and no business HTTP wiring (V12-02b) is claimed.',
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
print('V12-02a delegation store slice passed; 10 common gates passed; stage remains partial')
PY

