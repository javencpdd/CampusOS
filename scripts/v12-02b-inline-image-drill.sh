#!/usr/bin/env bash
# Authenticated richtext reads acceptance on an owned PostgreSQL 16.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-inline-image/tmp
export TMPDIR="$repo_root/.cache/v12-02b-inline-image/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-inline-image/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-inline-image/drill.XXXXXX")"
container="campusos-v12-02b-inline-image-$$"
database="campusos_v12_02b_richtext_read"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-inline-image-linux.json"
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
  echo "V12-02b inline images: $name"
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
[[ "$(psql_scalar 'SELECT count(*) FROM schema_migrations;')" == 16 ]] || { echo "expected 16 migrations" >&2; exit 1; }
run_check image-tests env CAMPUSOS_RICHTEXT_TEST_DATABASE_URL="$database_url" \
 go test -race -count=1 -v -run '^(TestRichTextInlineImage.*|TestInlineImage.*|TestContentImageChecked.*|TestRichTextAuthorized.*|TestBoundProjection.*|TestFeatureThreadQuery.*)$' \
 ./internal/modules/core/identity/port ./internal/modules/core/community ./internal/modules/core/userstorage ./internal/modules/features/richtext
run_check composition go test -race -count=1 ./internal/modules/features/richtext ./internal/server ./internal/modules/core/identity/port ./internal/modules/core/community
run_check web-components bash -c 'cd web && pnpm exec vitest run --pool=forks --poolOptions.forks.singleFork=true'
run_check web-format bash -c 'cd web && pnpm format:check'
run_check web-build bash -c 'cd web && pnpm build'
run_check database bash scripts/database-check.sh all
run_check go-all go test ./... -count=1
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check
python3 - "$work_dir" "$report" "$pg_version" <<'REPORT'
import hashlib,json,platform,re,subprocess,sys
from datetime import datetime,timezone
from pathlib import Path
work,report=map(Path,sys.argv[1:3])
tests=(work/'image-tests.log').read_text()
required=[]
for folder in ['internal/modules/core/userstorage','internal/modules/core/community','internal/modules/core/identity/port','internal/modules/features/richtext']:
 for path in Path(folder).glob('*test.go'):
  for name in re.findall(r'^func (Test\w+)\(',path.read_text(),re.M):
   if re.match(r'^Test(RichTextInlineImage|InlineImage|ContentImageChecked|RichTextAuthorized|BoundProjection|FeatureThreadQuery)',name): required.append(name)
actual=re.findall(r'^--- PASS: (\w+)(?: |$)',tests,re.M)
assert len(required)==20 and sorted(actual)==sorted(required), f'required target tests differ: {required}, {actual}'
assert not re.search(r'^\s*--- (?:FAIL|SKIP): ',tests,re.M),'target tests skipped or failed'
assert 'WARNING: DATA RACE' not in tests
sha=lambda path:hashlib.sha256(Path(path).read_bytes()).hexdigest()
paths=['Makefile','internal/transport/httpapi/router.go','modules/core/user-storage/module.yaml','scripts/v12-02b-inline-image-drill.sh']
for folder in ['internal/modules/core/community','internal/modules/core/identity/port','internal/modules/core/identity/policy','internal/modules/core/contenteditor','internal/modules/core/userstorage','internal/modules/features/richtext']:
 paths += [str(p) for p in Path(folder).rglob('*.go')]
paths += [str(p) for p in Path('web/src/modules/content-editor').glob('*') if p.suffix in ['.ts','.vue']]
paths += ['web/src/modules/mutual-aid/pages/MutualAidDetailView.vue','web/src/modules/secondhand/pages/SecondhandDetailView.vue','web/src/modules/personal-documents/pages/PersonalDocumentsView.vue','web/src/modules/identity/session.ts','web/src/modules/community/components/SafeContentEditor.vue','web/src/modules/community/pages/CreateThreadView.vue','web/src/modules/community/pages/ThreadDetailView.vue','web/tests/checked-images.spec.ts','web/tests/structured-content.spec.ts','web/package.json','web/pnpm-lock.yaml']
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
dependencies=['v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-document-read-linux.json','v12-02b-typed-feature-read-linux.json']
r={
 'schema':'campusos.v12-02b-inline-image-acceptance/v1','generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02b','stage_status':'partial','slice_status':'completed',
 'implementation':'passed','automation':'passed','target_environment':'passed',
 'scope':'Legacy Core and RichText inline image byte and owner inventory endpoints; authoritative stored parent reference and current pure parent Policy, byte snapshots and full current User Session recheck; checked Web image/HTML/cover rendering.',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),'postgres_version_num':int(sys.argv[3]),'database':'owned disposable PostgreSQL16 loopback tmpfs; real Community/RichText/Reliability repositories and actual local image bytes'},
 'checks':{name:'passed' for name in required},
 'coverage':{'new_required_tests':7,'read_regression_tests':13,'real_postgres_tests':5,'raw_states':36,'core_memory_domain_checks':144,'richtext_memory_state_checks':144,'image_postgres_state_checks':216,'image_postgres_committed_races':8,'http':'real domain JWT and current SessionService with Memory Identity/Community/RichText; actual filesystem images; missing/unbound proof, same-ID Admin, cross-owner, private/public/unbound refs and revocation','web':'Vitest component tests, format and production build; browser rendering not separately accepted'},
 'source_sha256':{path:sha(path) for path in sorted(set(paths))},
 'dependencies':{prefix+name:sha(prefix+name) for name in dependencies},
 'common_gates_status':'passed',
 'logs':{path.stem:{'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())),'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'boundaries':['This slice does not complete V12-02b. Private files, author/governance writes, Admin lists, batch and effective permission preview remain.','Unbound legacy uploads are private independent owner inventory. Public clients supply an untrusted thread ID which must freshly resolve to an actual image/cover reference from the same owner. Missing ports never fall back to paths.','No new migration. Legacy filesystem images have bounded byte/mtime snapshots; this does not claim persistent nonreused Object versions, cross-module atomic reads or retraction of already delivered bytes.','HTTP components use Memory Identity; PG resources/local bytes are separately accepted. No full production server, Windows or browser acceptance claimed.'],
}
report.parent.mkdir(parents=True,exist_ok=True)
report.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n')
print(f'V12-02b inline image slice passed: {len(required)} required tests; stage remains partial')
REPORT
