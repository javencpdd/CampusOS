#!/usr/bin/env bash
# Public list, count and pagination acceptance on an owned PostgreSQL 16.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-list/tmp
export TMPDIR="$repo_root/.cache/v12-02b-list/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-list/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-list/drill.XXXXXX")"
container="campusos-v12-02b-list-$$"
database="campusos_v12_02b_list"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-public-list-linux.json"
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
  echo "V12-02b public list: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then return; fi
  tail -n 80 "$work_dir/$name.log" >&2
  return 1
}
psql_scalar() {
  docker run --rm -i --network "container:$container" -e PGPASSWORD="$pg_password" \
    postgres:16-alpine psql -h 127.0.0.1 -U campusos -d "$database" -qAtv ON_ERROR_STOP=1 -c "$1"
}
mkdir -p "$work_dir/bin"
cat >"$work_dir/bin/psql" <<'SHIM'
#!/usr/bin/env bash
exec docker run --rm -i --network "container:$DRILL_DB_CONTAINER" -v "$DRILL_REPO:$DRILL_REPO:ro" -w "$DRILL_REPO" -e PGPASSWORD postgres:16-alpine psql "$@"
SHIM
chmod +x "$work_dir/bin/psql"
export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host PATH="$work_dir/bin:$PATH"
export DRILL_DB_CONTAINER="$container" DRILL_REPO="$repo_root"
export DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database"
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
run_check migrations-up bash scripts/migrate.sh up
run_check migrations-check bash scripts/migrate.sh check
expected_migrations="$(python3 -c 'from pathlib import Path; print(len(list(Path("migrations").glob("*.up.sql"))))')"
[[ "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" == "$expected_migrations" ]] || { echo "expected all current migrations" >&2; exit 1; }
run_check list-tests env CAMPUSOS_COMMUNITY_TEST_DATABASE_URL="$database_url" \
  go test -race -count=1 -v -run '^Test(PublicThreadList|PublicThreadQuery|PublicList|MemoryThreadListSnapshot|PublicThreadDetail(Policy|Memory|Retry|Fails|HTTP))' \
  ./internal/modules/core/community ./internal/modules/core/community/repository ./internal/modules/core/community/service ./internal/modules/core/community/handler
run_check database bash scripts/database-check.sh all
run_check go-all go test ./... -count=1
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check
python3 - "$work_dir" "$report" "$pg_version" <<'PY'
import hashlib, json, platform, re, subprocess, sys
from datetime import datetime, timezone
from pathlib import Path
work, report = map(Path, sys.argv[1:3])
tests = (work / 'list-tests.log').read_text()
required = [
 'TestMemoryThreadListSnapshotPaginationAndRevocation', 'TestMemoryThreadListSnapshotPaginationBoundaries',
 'TestMemoryThreadListSnapshotConcurrentCountAndRows', 'TestPublicThreadListStateMatrixAndPagination',
 'TestPublicThreadListRetryWholeSnapshot', 'TestPublicThreadListFailsClosed',
 'TestPublicThreadListSustainedConflictAndCancellation', 'TestPublicThreadListDispatchAndBoundaries',
 'TestPublicThreadListPostgreSQLStateMatrixAndPagination',
 'TestPublicThreadListPostgreSQLSnapshotRetry', 'TestPublicThreadListPostgreSQLSustainedConflict',
 'TestPublicThreadListPostgreSQLRawFactsFailsClosed', 'TestPublicThreadListPostgreSQLRepositorySnapshotConcurrency',
 'TestPublicThreadListForcesCanonicalVisibility', 'TestPublicThreadListHTTPErrorBoundary',
 'TestPublicThreadListHTTPEmptyPageKeepsPublicCount', 'TestPublicThreadQueryUsesCanonicalList',
 'TestPublicThreadQueryRevocationChangesWholePage', 'TestPublicThreadQueryPreservesAuthorList',
 'TestPublicThreadQueryUnavailable', 'TestPublicListRechecksPublicationAfterAnotherServiceChangesIt',
 'TestPublicThreadDetailPolicyStateMatrix', 'TestPublicThreadDetailMemoryRevocation',
 'TestPublicThreadDetailRetryReloadsFactsAndDecision', 'TestPublicThreadDetailFailsClosed',
 'TestPublicThreadDetailHTTPErrorBoundary',
]
for name in required:
    assert re.search(r'^--- PASS: '+name+r'(?: |$)', tests, re.M), name
assert not re.search(r'^\s*--- (?:FAIL|SKIP): ', tests, re.M), 'target tests skipped or failed'
sha = lambda path: hashlib.sha256(Path(path).read_bytes()).hexdigest()
paths = [
 'internal/modules/core/community/repository/thread_repository.go',
 'internal/modules/core/community/repository/pg_thread_repository.go',
 'internal/modules/core/community/repository/thread_list_snapshot_test.go',
 'internal/modules/core/community/service/thread_service.go',
 'internal/modules/core/community/service/thread_public_read.go',
 'internal/modules/core/community/service/thread_public_read_test.go',
 'internal/modules/core/community/service/thread_list_visibility_test.go',
 'internal/modules/core/community/service/thread_public_list.go',
 'internal/modules/core/community/service/thread_public_list_test.go',
 'internal/modules/core/community/service/thread_public_list_postgres_test.go',
 'internal/modules/core/community/handler/thread_handler.go',
 'internal/modules/core/community/handler/thread_handler_test.go',
 'internal/modules/core/community/handler/thread_public_list_test.go',
 'internal/modules/core/community/moderation_gateway.go',
 'internal/modules/core/community/public_list_query_test.go',
 'migrations/000016_v1_2_thread_facts_version.up.sql',
 'scripts/v12-02b-public-list-drill.sh',
]
dependency = 'docs/项目计划书v1/项目计划v1.2/evidence/'
predecessors = ['v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-public-detail-linux.json']
data = {
 'schema':'campusos.v12-02b-public-list-acceptance/v1', 'generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02b', 'stage_status':'partial', 'slice_status':'completed',
 'implementation':'passed', 'automation':'passed', 'target_environment':'passed',
 'scope':'Canonical public thread list, count and pagination: strict raw predicate, policy decisions per row and final whole-page snapshot recheck.',
 'environment':{'os':platform.system(),'architecture':platform.machine(),
   'go':subprocess.check_output(['go','version'],text=True).strip(),
   'postgres_version_num':int(sys.argv[3]),'database':'owned disposable PostgreSQL 16 container; loopback only'},
 'checks':{name:'passed' for name in required},
 'coverage':{'state_combinations_per_adapter':36,'postgres_commit_schedules':9,
   'postgres_snapshot_concurrency':{'writer_transactions':100,'repository_reads':200},
   'adapters':['memory','postgres'],'http':'canonical filters, revocation, 409, safe 500, empty page count',
   'consistency':'one SQL statement / one Memory read lock for count and rows; final whole-page recheck including empty pages'},
 'source_sha256':{path:sha(path) for path in paths},
 'dependencies':{dependency+name:sha(dependency+name) for name in predecessors},
 'common_gates_status':'passed',
 'logs':{path.stem:{'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())),
   'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'boundaries':[
   'V12-02b remains partial: private author reads, moderation/MFA Principal, batch authorization and effective-permission preview await subsequent slices.',
   'Admin/author and legacy noncanonical ListThreads filters retain their authorization path; shared repository snapshot consistency does not add new Policy acceptance to them.',
   'Plugin/integration Scope, Grant/Consent and lifecycle checks remain ingress responsibilities.',
   'Public results linearize at the final whole-page repository snapshot; later writes cannot retract returned bytes.',
   'No browser, Windows/WSL, capacity benchmark or full-stage acceptance is claimed.',
   'No new migration; existing 000016 trigger supplies ABA-safe versions for content, visibility and ordering changes.',
 ],
}
report.parent.mkdir(parents=True, exist_ok=True)
report.write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
print('V12-02b public list slice passed; stage remains partial')
PY
