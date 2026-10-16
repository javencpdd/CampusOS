#!/usr/bin/env bash
# Public detail Policy wiring on an owned disposable PostgreSQL 16 database.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-public/tmp
export TMPDIR="$repo_root/.cache/v12-02b-public/tmp"
export GOTMPDIR="$TMPDIR"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-public/drill.XXXXXX")"
container="campusos-v12-02b-public-$$"
database="campusos_v12_02b_public"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-public-detail-linux.json"
started=false
cleanup() {
  local status=$?
  if [[ "$started" == true ]]; then
    if (( status != 0 )); then docker logs "$container" >"$work_dir/postgres-failure.log" 2>&1 || true; fi
    docker stop "$container" >/dev/null 2>&1 || true
  fi
  if (( status != 0 )); then echo "failed drill logs retained: $work_dir" >&2; fi
}
trap cleanup EXIT
run_check() {
  local name="$1"
  shift
  echo "V12-02b public detail: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then return; fi
  tail -n 80 "$work_dir/$name.log" >&2
  return 1
}
psql_scalar() {
  docker run --rm -i --network "container:$container" -e PGPASSWORD="$pg_password" \
    postgres:16-alpine psql -h 127.0.0.1 -U campusos -d "$database" -qAtv ON_ERROR_STOP=1 -c "$1"
}
mkdir -p "$work_dir/bin" "$work_dir/migrations-pre"
cat >"$work_dir/bin/psql" <<'SHIM'
#!/usr/bin/env bash
exec docker run --rm -i --network "container:$DRILL_DB_CONTAINER" -v "$DRILL_REPO:$DRILL_REPO:ro" -w "$DRILL_REPO" -e PGPASSWORD postgres:16-alpine psql "$@"
SHIM
chmod +x "$work_dir/bin/psql"
export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host
export PATH="$work_dir/bin:$PATH"
export DRILL_DB_CONTAINER="$container" DRILL_REPO="$repo_root"
export DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database"
run_migrate() { MIGRATIONS_DIR="${drill_migration_dir:-migrations}" bash scripts/migrate.sh "$@"; }

docker run --rm -d --name "$container" --tmpfs /var/lib/postgresql/data:rw,size=512m \
  -e POSTGRES_DB="$database" -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$pg_password" \
  -p 127.0.0.1::5432 postgres:16-alpine >/dev/null
started=true
for attempt in $(seq 1 80); do
  if psql_scalar 'SELECT 1;' >/dev/null 2>&1; then break; fi
  if (( attempt == 80 )); then echo "PostgreSQL did not become ready" >&2; exit 1; fi
  sleep 0.5
done
pg_version="$(psql_scalar 'SHOW server_version_num;')"
(( pg_version >= 160000 && pg_version < 170000 )) || { echo "PostgreSQL 16 required" >&2; exit 1; }
pg_port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/ {print $NF; exit}')"
[[ "$pg_port" =~ ^[0-9]+$ ]] || { echo "loopback PostgreSQL port required" >&2; exit 1; }
database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/$database?sslmode=disable"

# Test backfill against a real pre-existing thread, not just an empty schema.
for migration in migrations/*.sql; do
  if [[ "$(basename "$migration")" < "000016" ]]; then cp "$migration" "$work_dir/migrations-pre/"; fi
done
drill_migration_dir="$work_dir/migrations-pre"
run_check migrations-pre run_migrate up
run_check seed psql_scalar "
INSERT INTO users(id,username,nickname,email) VALUES (97301,'backfill-owner','Backfill','backfill@test.local');
INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES (97310,'Backfill group','backfill-group','group','active');
INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES (97311,'Backfill board','backfill-board',97310,'board','active');
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES (97311,'discussion',TRUE);
INSERT INTO threads(id,title,content,author_id,author_name,category_id,status,publication_status,moderation_status,deletion_status)
VALUES(97320,'backfill title','preserved body',97301,'Backfill',97311,'published','published','clear','active');"
drill_migration_dir=migrations
run_check migrations-up run_migrate up
before="$(psql_scalar 'SELECT facts_version FROM threads WHERE id=97320;')"
[[ "$before" =~ ^[1-9][0-9]*$ ]] || { echo "backfill version missing" >&2; exit 1; }
while :; do
  latest_applied="$(psql_scalar 'SELECT max(version) FROM schema_migrations;')"
  [[ "$latest_applied" > "000016" ]] || break
  run_check "migrations-down-before-thread-$latest_applied" run_migrate down
done
run_check migrations-down run_migrate down
[[ "$(psql_scalar 'SELECT content FROM threads WHERE id=97320;')" == 'preserved body' ]] || { echo "down lost content" >&2; exit 1; }
[[ "$(psql_scalar "SELECT to_regclass('public.thread_facts_version_seq') IS NOT NULL;")" == t ]] || { echo "down lost high-water sequence" >&2; exit 1; }
run_check migrations-reup run_migrate up
after="$(psql_scalar 'SELECT facts_version FROM threads WHERE id=97320;')"
(( after > before )) || { echo "down/up reused facts version" >&2; exit 1; }
run_check migrations-check run_migrate check
expected_migrations="$(python3 -c 'from pathlib import Path; print(len(list(Path("migrations").glob("*.up.sql"))))')"
[[ "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" == "$expected_migrations" ]] || { echo "expected all current migrations" >&2; exit 1; }
run_check detail-tests env CAMPUSOS_COMMUNITY_TEST_DATABASE_URL="$database_url" \
  go test -race -count=1 -v -run '^Test(PublicThreadDetail|MemoryPublicThread|MemoryThreadFactsVersion)' \
  ./internal/modules/core/community/repository ./internal/modules/core/community/service ./internal/modules/core/community/handler
run_check database bash scripts/database-check.sh all
check_disabled_guard() {
  psql_scalar 'ALTER TABLE threads DISABLE TRIGGER trg_threads_facts_version;' >/dev/null
  if bash scripts/database-check.sh schema >"$work_dir/disabled-schema.out" 2>&1; then
    echo "schema contract accepted a disabled facts version guard" >&2
    return 1
  fi
  rg -q 'thread facts version column, allocation sequence or write guard is missing or disabled' "$work_dir/disabled-schema.out"
  psql_scalar 'ALTER TABLE threads ENABLE TRIGGER trg_threads_facts_version;' >/dev/null
  bash scripts/database-check.sh schema
}
run_check database-guard check_disabled_guard
run_check go-all go test ./... -count=1
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check admin-build make admin-build
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check

python3 - "$work_dir" "$report" "$pg_version" <<'PY'
import hashlib, json, platform, re, subprocess, sys
from datetime import datetime, timezone
from pathlib import Path
work, report = map(Path, sys.argv[1:3])
tests = (work / 'detail-tests.log').read_text()
required = [
 'TestPublicThreadDetailPolicyStateMatrix', 'TestPublicThreadDetailMemoryRevocation',
 'TestPublicThreadDetailRetryReloadsFactsAndDecision', 'TestPublicThreadDetailFailsClosed',
 'TestPublicThreadDetailPostgreSQLRevocation', 'TestPublicThreadDetailPostgreSQLVersion',
 'TestPublicThreadDetailPostgreSQLCommitRace', 'TestPublicThreadDetailHTTPErrorBoundary',
 'TestMemoryPublicThreadSnapshotRejectsABAAndReplacement',
 'TestMemoryThreadFactsVersionRollbackPreservesHiddenFactsAndAllocation',
 'TestMemoryPublicThreadSnapshotCountersAndMissingState',
 'TestMemoryPublicThreadSnapshotConcurrentRevocation',
]
for name in required:
    assert re.search(r'^--- PASS: ' + name + r'(?: |$)', tests, re.M), name
assert not re.search(r'^--- (?:FAIL|SKIP): ', tests, re.M), 'required tests skipped or failed'
sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
paths = [
 'internal/modules/core/community/domain/thread.go',
 'internal/modules/core/community/repository/thread_repository.go',
 'internal/modules/core/community/repository/pg_thread_repository.go',
 'internal/modules/core/community/repository/thread_public_snapshot_test.go',
 'internal/modules/core/community/service/thread_service.go',
 'internal/modules/core/community/service/thread_public_read.go',
 'internal/modules/core/community/service/thread_public_read_test.go',
 'internal/modules/core/community/service/thread_public_read_postgres_test.go',
 'internal/modules/core/community/handler/thread_handler.go',
 'internal/modules/core/community/handler/thread_public_read_test.go',
 'migrations/000016_v1_2_thread_facts_version.up.sql',
 'migrations/000016_v1_2_thread_facts_version.down.sql',
 'admin/src/modules/architecture/pages/SystemArchitectureView.vue',
 'scripts/v12-02b-public-detail-drill.sh', 'scripts/schema-contract.sql',
]
data = {
 'schema': 'campusos.v12-02b-public-detail-acceptance/v1',
 'generated_at': datetime.now(timezone.utc).isoformat(),
 'stage': 'V12-02b', 'stage_status': 'partial', 'slice_status': 'completed',
 'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed',
 'scope': 'Canonical public thread detail: raw facts, pure Policy, atomic version recheck and optional view increment; 000016 ABA-safe facts epochs.',
 'environment': {'os': platform.system(), 'architecture': platform.machine(),
   'go': subprocess.check_output(['go','version'], text=True).strip(),
   'postgres_version_num': int(sys.argv[3]), 'database': 'owned disposable PostgreSQL 16 container; loopback only'},
 'checks': {name: 'passed' for name in required},
 'coverage': {'state_combinations': 36, 'revocation_schedules_per_adapter': 9,
   'adapters': ['memory','postgres'], 'migration': '000001-000015 seed + 000016 up/down/up; content preserved, version high-water retained',
   'commit_race': 'view UPDATE waits for concurrent writer commit then rejects private row'},
 'source_sha256': {path: sha(path) for path in paths},
 'common_gates_status': 'passed',
 'logs': {path.stem: {'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())), 'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'boundaries': [
   'Lists, counts and pagination still use existing repository filters; no new Policy list consistency acceptance.',
   'Private author reads, moderation commands, MFA ingress Principal, batch authorization and effective-permission preview remain later V12-02b slices.',
   'Plugin/integration Scope, Grant/Consent and lifecycle checks remain ingress responsibilities; public predicate cannot grant them.',
   'Returned bytes linearize at the final repository snapshot; later revocation cannot retract bytes already returned.',
   'Rollback requires stopping/reverting version-aware readers; allocation sequence intentionally survives down/up.',
   'No browser or Windows/WSL target acceptance is claimed for this backend slice.',
 ],
}
report.parent.mkdir(parents=True, exist_ok=True)
report.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print('V12-02b public detail slice passed; stage remains partial')
PY
