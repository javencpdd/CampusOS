#!/usr/bin/env bash
# V12-02a PermissionService path replacement acceptance on an owned
# PostgreSQL 16 tmpfs container. The developer database is never touched.
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

mkdir -p .cache/v12-02a-permission-path/tmp
export TMPDIR="$repo_root/.cache/v12-02a-permission-path/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
report="${2:-docs/项目计划书v1/项目计划v1.2/evidence/v12-02a-permission-path-linux.json}"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02a-permission-path/path.XXXXXX")"
container="campusos-v12-02a-path-$$"
database="campusos_v12_02a_path"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
container_started=false

cleanup() {
  local status=$?
  if [[ "$container_started" == true ]]; then
    if (( status != 0 )); then
      docker logs "$container" >"$repo_root/.cache/v12-02a-permission-path/postgres-last-failure.log" 2>&1 || true
      echo "isolated PostgreSQL failure log: .cache/v12-02a-permission-path/postgres-last-failure.log" >&2
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
  echo "V12-02a permission path: $name"
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
  CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
    DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database" \
    MIGRATIONS_DIR="migrations" bash scripts/migrate.sh "$@"
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
for attempt in $(seq 1 80); do
  if psql_scalar 'SELECT 1;' >/dev/null 2>&1; then break; fi
  if (( attempt == 80 )); then echo "isolated PostgreSQL did not stabilize" >&2; exit 1; fi
  sleep 0.25
done
pg_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
[[ "$pg_port" =~ ^[0-9]+$ ]] || { echo "PostgreSQL port is not loopback-bound" >&2; exit 1; }
database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$database?sslmode=disable"

run_check migrations run_migrate up

# The two suites share cleanup namespaces, so each gets its own database in
# the same owned container; parallel go test processes never cross-contaminate.
kernel_database="campusos_v12_02a_path_kernel"
psql_scalar_db() {
  docker exec -e PGPASSWORD="$pg_password" "$container"     psql -U campusos -d "$1" -qAtv ON_ERROR_STOP=1 -c "$2"
}
psql_scalar_db postgres "CREATE DATABASE $kernel_database;" >/dev/null
CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$container" \
  DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$kernel_database" \
  MIGRATIONS_DIR="migrations" bash scripts/migrate.sh up >>"$work_dir/migrations.log" 2>&1
kernel_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$kernel_database?sslmode=disable"

# Service-level path replacement on real PostgreSQL, plus the delegation and
# policy kernels with their env-gated PostgreSQL profiles.
run_check path-tests env CAMPUSOS_IDENTITY_TEST_DATABASE_URL="$database_url" \
  go test -race -count=1 -v ./internal/modules/core/identity/service
run_check kernel-tests env CAMPUSOS_IDENTITY_TEST_DATABASE_URL="$kernel_url" \
  go test -race -count=1 -v \
  ./internal/modules/core/identity/delegation \
  ./internal/modules/core/identity/repository \
  ./internal/modules/core/identity/policy \
  ./internal/modules/core/identity/port

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
text = (work / 'path-tests.log').read_text() + '\n' + (work / 'kernel-tests.log').read_text()
required_tests = [
    'TestPostgreSQLModeratorScopeReplaceCommitsDelegationGrants',
    'TestPostgreSQLModeratorScopeReplaceRevokesRemovedBoards',
    'TestPostgreSQLModeratorScopeReplaceFailsClosedWithoutProof',
    'TestPermissionCatalogUsesStableCodesAndPreventsPrivilegeEscalation',
    'TestPostgreSQLGrantCommitAuditAndOutbox',
    'TestPostgreSQLConcurrentGrantRevokeRace',
]
for test in required_tests:
    assert re.search(r'^--- PASS: ' + test + r'(?: |$)', text, re.M), test
assert not re.search(r'^--- FAIL: ', text, re.M), 'unexpected failure'
allowed_skips = {'TestBoardDelegationG1WireCorpus', 'TestResourcePolicyG1WireCorpus', 'TestScopeDelegationG1WireCorpus'}
for skipped in re.findall(r'^--- SKIP: (\w+)', text, re.M):
    assert skipped in allowed_skips, 'unexpected skip: ' + skipped
sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
sources = [
    'internal/modules/core/identity/service/permission_service.go',
    'internal/modules/core/identity/service/permission_service_delegation_test.go',
    'internal/modules/core/identity/service/session_service.go',
    'internal/modules/core/identity/delegation/service.go',
    'internal/modules/core/identity/delegation/context.go',
    'internal/modules/core/identity/repository/pg_delegation_repository.go',
    'internal/modules/core/identity/port/port.go',
    'internal/modules/core/identity/port/board_delegation_policy.go',
    'internal/modules/core/identity/portadapt/adapter.go',
    'internal/modules/core/identity/portadapt/moderation_adapter.go',
    'internal/modules/core/identity/module.go',
    'internal/modules/core/identity/profile.go',
    'internal/modules/core/moderation/service.go',
    'internal/modules/core/moderation/handler.go',
    'internal/modules/core/moderation/module.go',
    'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md',
    'scripts/v12-02a-permission-path-drill.sh',
]
logs = {
    name: {'path': str((work / f'{name}.log').relative_to(root)), 'sha256': sha(work / f'{name}.log')}
    for name in ['migrations', 'path-tests', 'kernel-tests']
}
dependencies = {}
for name in ['v12-g0-exit.json', 'v12-g1-exit-linux.json', 'v12-01a-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-02a-board-delegation-linux.json', 'v12-02a-delegation-store-linux.json', 'v12-02a-delegation-service-linux.json']:
    path = Path('docs/项目计划书v1/项目计划v1.2/evidence') / name
    dependencies[name] = {
        'report': str(path),
        'sha256': sha(path),
        'role': 'accepted predecessor evidence; historical source snapshots are retained unchanged',
    }
data = {
    'schema': 'campusos.v12-02a-permission-path-acceptance/v1',
    'generated_at': datetime.now(timezone.utc).isoformat(),
    'stage': 'V12-02a',
    'stage_status': 'partial',
    'slice_status': 'completed',
    'implementation': 'passed',
    'automation': 'passed',
    'target_environment': 'passed',
    'scope': 'PermissionService governance read/grant path replacement: the two board governance codes authorize via current delegation grants unioned with global catalog grants; moderator scope replacement commits role scopes and delegation grants in one command transaction',
    'environment': {
        'os': platform.system(),
        'architecture': platform.machine(),
        'go': subprocess.check_output(['go', 'version'], text=True).strip(),
        'postgres': 'PostgreSQL 16 (owned tmpfs container, loopback-only port)',
    },
    'checks': {test: 'passed' for test in required_tests},
    'coverage': {
        'path_tests': 6,
        'kernels': ['delegation service PG suite', 'repository PG suite', 'policy/port codecs'],
        'security_assertions': ['role scope without grant denies', 'outside board denies', 'missing actor proof fails closed', 'global admin catalog grant preserved', 'revoked board denied immediately'],
    },
    'source_sha256': {path: sha(path) for path in sources},
    'dependencies': dependencies,
    'target_logs': logs,
    'common_gates_status': 'pending',
    'common_gates': {},
    'boundaries': [
        'Only community.thread.take_down and community.post.delete moved to delegation grants; the moderator role keeps its five low-risk codes and global role management keeps the existing catalog checks pending a future contract.',
        'Read-path caller strength uses the entry actor proof when present; MFA-required grants fail closed until V12-02b wires entry principals everywhere.',
        'Admin identity domain separation remains V12-02d; community fact loading/ABA versioning and HTTP wiring of the resource policy remain V12-02b.',
        'The G1 old-mechanism replacement entries policy.user_only_check and policy.grant_by_execution are covered for the governance category scope only.',
    ],
}
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print('Native Linux target acceptance passed; common gates pending')
PY

run_check go-all go test ./... -count=1
run_check contracts make contracts-check
run_check architecture make architecture-check
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
print('V12-02a permission path slice passed; 7 common gates passed; stage remains partial')
PY
