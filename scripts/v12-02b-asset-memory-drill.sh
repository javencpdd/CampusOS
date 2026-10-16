#!/usr/bin/env bash
# Current User actual-resource bounded moderation preview with the same named command policies.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-asset-memory/tmp
export TMPDIR="$repo_root/.cache/v12-02b-asset-memory/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-asset-memory/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-asset-memory/drill.XXXXXX")"
container="campusos-v12-02b-asset-memory-$$"
database="campusos_v12_02b_author_write"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-asset-memory-linux.json"
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
  echo "V12-02b asset Memory facts: $name"
  if "$@" >"$work_dir/$name.log" 2>&1; then return; fi
  tail -n 80 "$work_dir/$name.log" >&2
  return 1
}
psql_scalar() {
  docker exec -i -e PGPASSWORD="$pg_password" "$container" psql -h 127.0.0.1 -U campusos -d "$database" -qAtv ON_ERROR_STOP=1 -c "$1"
}
mkdir -p "$work_dir/bin"
cat >"$work_dir/bin/psql" <<'SHIM'
#!/usr/bin/env bash
exec docker exec -i -e PGPASSWORD "$DRILL_DB_CONTAINER" psql "$@"
SHIM
chmod +x "$work_dir/bin/psql"
export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host PATH="$work_dir/bin:$PATH"
export DRILL_DB_CONTAINER="$container" DRILL_REPO="$repo_root"
export DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME="$database"
docker run --rm -d --name "$container" --tmpfs /var/lib/postgresql/data:rw,size=512m \
  -v "$repo_root:$repo_root:ro" -w "$repo_root" \
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
required_regex="$(python3 - <<'SELECT'
import json,re
from pathlib import Path
known=set(json.loads(Path('docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-richtext-asset-facts-linux.json').read_text())['checks'])
names=[]
for folder in ['internal/modules/features/richtext','internal/modules/core/community/service']:
 for path in Path(folder).glob('*test.go'):
  for name in re.findall(r'^func (Test\w+)\(',path.read_text(),re.M):
   if (folder.endswith('richtext') and name in known) or name.startswith(('TestAssetBindingMemory','TestFeatureAuthorCommand','TestFeatureGovernanceParticipant')):names.append(name)
names=sorted(set(names));assert len(names)==38 and len([n for n in names if n.startswith('TestAssetBindingMemory')])==2
print('^('+'|'.join(names)+')$')
SELECT
)"
run_check target-tests env CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$database_url" \
 go test -race -p 1 -count=1 -v -run "$required_regex" ./internal/modules/features/richtext ./internal/modules/core/community/service
run_check composition go test -race -count=1 ./internal/modules/features/richtext ./internal/modules/core/community ./internal/modules/core/community/service ./internal/modules/features/personaldocuments ./internal/modules/core/userstorage ./internal/modules/core/identity/service ./internal/server
run_check database bash scripts/database-check.sh all
run_check go-all go test ./... -count=1
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check
python3 - "$work_dir" "$report" "$pg_version" "$expected_migrations" <<'REPORT'
import hashlib,json,platform,re,subprocess,sys
from datetime import datetime,timezone
from pathlib import Path
work,report=map(Path,sys.argv[1:3])
tests=(work/'target-tests.log').read_text()
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
previous=json.loads(Path(prefix+'v12-02b-richtext-asset-facts-linux.json').read_text())
known=set(previous['checks']);required=[]
for folder in ['internal/modules/features/richtext','internal/modules/core/community/service']:
 for path in Path(folder).glob('*test.go'):
  for name in re.findall(r'^func (Test\w+)\(',path.read_text(),re.M):
   if (folder.endswith('richtext') and name in known) or name.startswith(('TestAssetBindingMemory','TestFeatureAuthorCommand','TestFeatureGovernanceParticipant')):required.append(name)
required=sorted(set(required));new=[n for n in required if n.startswith('TestAssetBindingMemory')]
actual=re.findall(r'^--- PASS: (\w+)(?: |$)',tests,re.M)
assert len(new)==2 and len(required)==38 and sorted(actual)==sorted(required), f'required target tests differ: {required}, {actual}'
assert not re.search(r'^\s*--- (?:FAIL|SKIP): ',tests,re.M),'target tests skipped or failed'
assert 'WARNING: DATA RACE' not in tests
sha=lambda path:hashlib.sha256(Path(path).read_bytes()).hexdigest()
paths=['docs/api/http-routes-v0.6.json','docs/api/HTTP路由与授权矩阵-v0.6.md','docs/api/openapi-v0.6-current.yaml','cmd/campusos-contracts/main.go','internal/modules/core/identity/module.go','internal/modules/core/identity/module_test.go','admin/src/modules/architecture/pages/SystemArchitectureView.vue','admin/package.json','admin/pnpm-lock.yaml','migrations/README.md','scripts/schema-contract.sql','skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py','Makefile','internal/transport/httpapi/router.go','modules/core/user-storage/module.yaml','scripts/v12-02b-asset-memory-drill.sh']
paths += ['admin/src/modules/community/api.ts','admin/src/modules/community/pages/ThreadManageView.vue','admin/src/modules/operations/api.ts','admin/src/modules/operations/pages/EventLogView.vue','admin/tests/event-metadata.spec.ts','admin/vitest.config.ts','scripts/v12-02b-event-metadata-browser.mjs']
for folder in ['pkg/eventbus','pkg/middleware','pkg/auth','internal/modules/features/personaldocuments','internal/modules/features/mutualaid','internal/modules/features/secondhand','internal/modules/core/community','internal/modules/core/moderation','internal/modules/core/identity/port','internal/modules/core/identity/policy','internal/modules/core/contenteditor','internal/modules/core/userstorage','internal/modules/features/richtext','internal/modules/features/personalspace','internal/modules/core/identity/portadapt','internal/modules/core/identity/repository','internal/modules/core/identity/service','internal/platform/reliability','internal/platform/transaction']:
 paths += [str(p) for p in Path(folder).rglob('*.go')]
paths += [str(p) for p in Path('web/src/modules/content-editor').glob('*') if p.suffix in ['.ts','.vue']]
paths += ['web/tests/thread-flags-view.spec.ts','web/vitest.config.ts']
paths += ['web/src/modules/mutual-aid/pages/MutualAidDetailView.vue','web/src/modules/secondhand/pages/SecondhandDetailView.vue','web/src/modules/personal-documents/pages/PersonalDocumentsView.vue','web/src/modules/identity/session.ts','web/src/modules/community/components/SafeContentEditor.vue','web/src/modules/community/pages/CreateThreadView.vue','web/src/modules/community/pages/ThreadDetailView.vue','web/tests/checked-images.spec.ts','web/tests/structured-content.spec.ts','web/package.json','web/pnpm-lock.yaml']
paths += [str(p) for p in Path('migrations').glob('*.sql')]
paths += [str(p) for p in Path('internal/projectaudit').rglob('*.go')]
paths += [str(p) for p in Path('migrations/er/current').glob('*') if p.is_file()]
paths += [str(p) for p in Path('scripts').glob('v12-02b*drill.sh')]
paths += [str(p) for p in Path('scripts/fixtures').glob('v12-02b-business-actor*.sql')]
paths += ['scripts/fixtures/v12-02b-post-facts-legacy.sql','scripts/fixtures/v12-02b-article-facts-legacy.sql','scripts/fixtures/v12-02b-user-scope-facts-legacy.sql','scripts/fixtures/v12-02b-richtext-asset-facts-legacy.sql']
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
dependencies=['v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-document-read-linux.json','v12-02b-typed-feature-read-linux.json','v12-02b-inline-image-linux.json','v12-02b-space-files-linux.json','v12-02b-author-write-linux.json','v12-02b-author-lifecycle-linux.json','v12-02b-author-create-linux.json','v12-02b-typed-author-create-linux.json','v12-02b-richtext-author-create-linux.json','v12-02b-typed-author-write-linux.json','v12-02b-richtext-author-write-linux.json','v12-02b-document-author-write-linux.json','v12-02b-admin-principal-linux.json','v12-02b-admin-permission-facts-linux.json','v12-02b-board-governance-authority-linux.json','v12-02b-business-actor-linux.json','v12-02b-thread-governance-linux.json','v12-02b-post-facts-linux.json','v12-02b-post-author-write-linux.json','v12-02b-post-governance-linux.json','v12-02b-post-read-linux.json','v12-02b-event-metadata-linux.json','v12-02b-admin-flags-linux.json','v12-02b-admin-review-linux.json','v12-02b-admin-lifecycle-linux.json','v12-02b-admin-purge-linux.json','v12-02b-article-facts-linux.json','v12-02b-richtext-governance-linux.json','v12-02b-user-scope-facts-linux.json','v12-02b-user-scope-memory-linux.json','v12-02b-user-flags-linux.json','v12-02b-moderation-preview-linux.json','v12-02b-richtext-asset-facts-linux.json']
r={
 'schema':'campusos.v12-02b-asset-memory-acceptance/v1','generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02b','stage_status':'partial','slice_status':'completed',
 'implementation':'passed','automation':'passed','target_environment':'passed',
 'scope':'RichText Memory asset/binding storage parity: actual parent-scoped full internal JSON, fresh buffers and nested date clones, two private per-ID nonce maps and monotonic shared allocation; mutation/ABA/reinsert visible, invalid reorder and batch exhaustion fail before mutation, rollback restores current content/audit/private facts without reusing high water. No business authentication or authorization claimed.',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),'postgres_version_num':int(sys.argv[3]),'database':'owned disposable PostgreSQL16 loopback tmpfs; real Community Thread/Governance/Identity User+Session/Reliability repositories and native transaction credential lock tests'},
 'checks':{name:'passed' for name in required},
 'coverage':{'new_required_tests':2,'previous_regressions':36,'real_postgres_tests':10,'impact_selected_tests':38,'native':'Scope and actual relationships ignore embedded DTO; all private fields and nonreuse/name/order/reinsert; array/buffer/date/nested Article alias protection; txn/empty/cancel; rollback restores audits/content/current nonce but preserves high water; timestamp-only no advance; four invalid reorder modes and five exhaustion modes atomic.', 'postgres':'Existing source Asset/Binding two PG, Article two PG, RichText author create/write four PG and governance two PG, all rerun with actual owned22 DB. Memory-only change does not claim repeat of all prior238; original83 evidence retained for unaffected paths and original22/21/20/19/18 data preservation.', 'migration_count':int(sys.argv[4]),'ddl':'No new SQL or PG implementation changes;22 schema unchanged from83; fresh owned migrations/check/schema/databaseaudit validate assembly.'},
 'source_sha256':{path:sha(path) for path in sorted(set(paths))},
 'dependencies':{prefix+name:sha(prefix+name) for name in dependencies},
 'common_gates_status':'passed',
 'logs':{path.stem:{'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())),'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'boundaries':['V12-02b remains partial; RichText facts-aware reads and actual attachments/assets/images writes, PersonalSpace writes, Admin lists/history/batch/preview/frontend identity and exit remain pending.', 'Only internal Memory storage parity accepted; positive hidden facts do not authenticate or authorize callers. Model and Principal wire unchanged.', 'No new DDL or PG method changes, no shared developer DB reset/commit/push; current owned source10 PG and36 affected regression tests cover interface/clone changes; broader stage exit remains required.'],

}
report.parent.mkdir(parents=True,exist_ok=True)
report.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n')
print(f'V12-02b asset Memory facts slice passed: {len(required)} required tests; stage remains partial')
REPORT
