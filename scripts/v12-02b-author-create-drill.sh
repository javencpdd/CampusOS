#!/usr/bin/env bash
# Authenticated richtext reads acceptance on an owned PostgreSQL 16.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-author-create/tmp
export TMPDIR="$repo_root/.cache/v12-02b-author-create/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-author-create/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-author-create/drill.XXXXXX")"
container="campusos-v12-02b-author-create-$$"
database="campusos_v12_02b_author_write"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-author-create-linux.json"
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
  echo "V12-02b author create: $name"
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
run_check image-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -count=1 -v -run '^(TestRichTextInlineImage.*|TestInlineImageHTTP.*|TestContentImageChecked.*|TestRichTextAuthorized(AssetPageAndMissingProof|AttachmentAndAssetCloseOnRevocation|HTTPRealSessionAndSafeBytes|ReadRechecksBodyAndCoreFacts|ReadStateMatrix)|TestBoundProjection.*|TestFeatureThreadQuery.*|TestSpaceFiles(Current.*|Revocation.*|HTTP.*)|TestAuthorUpdate.*|TestThreadAuthorityWrite.*|TestAuthorLifecycle.*|TestThreadOwnerLifecycle.*|TestAuthorCreate.*|TestCreateThreadUsesJWTContext)$' \
 ./internal/modules/core/identity/port ./internal/modules/core/community ./internal/modules/core/userstorage ./internal/modules/features/richtext ./internal/modules/features/personalspace ./internal/modules/core/community/service ./internal/modules/core/community/repository ./internal/modules/core/community/handler ./internal/modules/core/identity/policy
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
for folder in ['internal/modules/core/userstorage','internal/modules/core/community','internal/modules/core/identity/port','internal/modules/features/richtext','internal/modules/features/personalspace','internal/modules/core/community/service','internal/modules/core/community/repository','internal/modules/core/community/handler','internal/modules/core/identity/policy']:
 for path in Path(folder).glob('*test.go'):
  for name in re.findall(r'^func (Test\w+)\(',path.read_text(),re.M):
   if (re.match(r'^Test(RichTextInlineImage|InlineImageHTTP|ContentImageChecked|RichTextAuthorized|BoundProjection|FeatureThreadQuery|SpaceFiles|AuthorUpdate|ThreadAuthorityWrite|AuthorLifecycle|ThreadOwnerLifecycle|AuthorCreate|CreateThreadUsesJWTContext)',name) and 'PostgreSQL' not in name) or name.startswith(('TestAuthorUpdatePostgreSQL','TestAuthorLifecyclePostgreSQL','TestAuthorCreatePostgreSQL')): required.append(name)
actual=re.findall(r'^--- PASS: (\w+)(?: |$)',tests,re.M)
assert len(required)==41 and sorted(actual)==sorted(required), f'required target tests differ: {required}, {actual}'
assert not re.search(r'^\s*--- (?:FAIL|SKIP): ',tests,re.M),'target tests skipped or failed'
assert 'WARNING: DATA RACE' not in tests
sha=lambda path:hashlib.sha256(Path(path).read_bytes()).hexdigest()
paths=['Makefile','internal/transport/httpapi/router.go','modules/core/user-storage/module.yaml','scripts/v12-02b-author-create-drill.sh']
for folder in ['internal/modules/core/community','internal/modules/core/identity/port','internal/modules/core/identity/policy','internal/modules/core/contenteditor','internal/modules/core/userstorage','internal/modules/features/richtext','internal/modules/features/personalspace','internal/modules/core/identity/portadapt','internal/modules/core/identity/repository','internal/modules/core/identity/service','internal/platform/reliability','internal/platform/transaction']:
 paths += [str(p) for p in Path(folder).rglob('*.go')]
paths += [str(p) for p in Path('web/src/modules/content-editor').glob('*') if p.suffix in ['.ts','.vue']]
paths += ['web/src/modules/mutual-aid/pages/MutualAidDetailView.vue','web/src/modules/secondhand/pages/SecondhandDetailView.vue','web/src/modules/personal-documents/pages/PersonalDocumentsView.vue','web/src/modules/identity/session.ts','web/src/modules/community/components/SafeContentEditor.vue','web/src/modules/community/pages/CreateThreadView.vue','web/src/modules/community/pages/ThreadDetailView.vue','web/tests/checked-images.spec.ts','web/tests/structured-content.spec.ts','web/package.json','web/pnpm-lock.yaml']
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
dependencies=['v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-document-read-linux.json','v12-02b-typed-feature-read-linux.json','v12-02b-inline-image-linux.json','v12-02b-space-files-linux.json','v12-02b-author-write-linux.json','v12-02b-author-lifecycle-linux.json']
r={
 'schema':'campusos.v12-02b-author-create-acceptance/v1','generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02b','stage_status':'partial','slice_status':'completed',
 'implementation':'passed','automation':'passed','target_environment':'passed',
 'scope':'Authenticated discussion creation derives actual User from current complete Session, current raw board/group/type-policy snapshots and locks, before/after credential+authority recheck and required audit/outbox atomic commit. An uncreated resource is never represented as an editable thread.',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),'postgres_version_num':int(sys.argv[3]),'database':'owned disposable PostgreSQL16 loopback tmpfs; real Community Thread/Governance/Identity User+Session/Reliability repositories and native transaction credential lock tests'},
 'checks':{name:'passed' for name in required},
 'coverage':{'new_required_tests':5,'existing_creator_http_updated':1,'previous_author_read_regression_tests':35,'real_postgres_tests':7,'creator_committed_authority_changes':4,'creator_audit_rollback':'actual Thread/revision/outbox/platform audit rollback when required audit fails','credential_locks':'actual User and Session rows remain locked within write transaction; second connection FOR UPDATE NOWAIT yields 55P03','http':'actual User/Admin domain JWT and SessionService Memory Identity; current owner edit, other/admin/missing proof/forged authority DTO/revoked credentials rejected','audit':'Memory and PG actual content state/revisions/outbox/platform command audit rollback on required audit failure'},
 'source_sha256':{path:sha(path) for path in sorted(set(paths))},
 'dependencies':{prefix+name:sha(prefix+name) for name in dependencies},
 'common_gates_status':'passed',
 'logs':{path.stem:{'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())),'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'boundaries':['This slice does not complete V12-02b. Typed Feature author writes, governance/Admin/MFA actor audit, Admin lists, batch and effective permission preview remain.','Raw service helpers remain trusted internal primitives; HTTP author update uses the checked entry and requires actual transaction/audit adapters.','No new migration. This only delivers User author updates; Admin is never an author substitute.','HTTP components use Memory Identity; PG resource+credential+atomic audit commands are separately accepted. No full production server, Windows or browser acceptance claimed.'],
}
report.parent.mkdir(parents=True,exist_ok=True)
report.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n')
print(f'V12-02b author create slice passed: {len(required)} required tests; stage remains partial')
REPORT
