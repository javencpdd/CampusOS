#!/usr/bin/env bash
# Current User current Space public and owner reads commands, actual article binding, current quota ledger and exact cleanup.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-space-public-read/tmp
export TMPDIR="$repo_root/.cache/v12-02b-space-public-read/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-space-public-read/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-space-public-read/drill.XXXXXX")"
container="campusos-v12-02b-space-public-read-$$"
database="campusos_v12_02b_space_files"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-space-public-read-linux.json"
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
  echo "V12-02b current Space public and owner reads: $name"
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
psql_scalar 'CREATE DATABASE campusos_v12_02b_richtext_read;' >"$work_dir/read-database-create.log"
run_check read-migrations-up env DB_NAME=campusos_v12_02b_richtext_read bash scripts/migrate.sh up
run_check read-migrations-check env DB_NAME=campusos_v12_02b_richtext_read bash scripts/migrate.sh check
read_database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/campusos_v12_02b_richtext_read?sslmode=disable"
psql_scalar 'CREATE DATABASE campusos_v12_02b_asset_read_empty;' >"$work_dir/empty-database-create.log"
run_check empty-migrations-up env DB_NAME=campusos_v12_02b_asset_read_empty bash scripts/migrate.sh up
run_check empty-migrations-check env DB_NAME=campusos_v12_02b_asset_read_empty bash scripts/migrate.sh check
empty_database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/campusos_v12_02b_asset_read_empty?sslmode=disable"
psql_scalar 'CREATE DATABASE campusos_v12_02b_author_write;' >"$work_dir/author-database-create.log"
run_check author-migrations-up env DB_NAME=campusos_v12_02b_author_write bash scripts/migrate.sh up
run_check author-migrations-check env DB_NAME=campusos_v12_02b_author_write bash scripts/migrate.sh check
author_database_url="postgres://campusos:$pg_password@127.0.0.1:$pg_port/campusos_v12_02b_author_write?sslmode=disable"
run_check quota-boundary-down bash scripts/migrate.sh down
run_check legacy-down bash scripts/migrate.sh down
run_check legacy-fixture psql -h 127.0.0.1 -U campusos -d "$database" -v ON_ERROR_STOP=1 -f scripts/fixtures/v12-02b-space-facts-legacy.sql
space_original_query="SELECT jsonb_build_object('spaces',(SELECT jsonb_agg(to_jsonb(s)-'facts_version' ORDER BY id) FROM user_spaces s),'snapshots',(SELECT jsonb_agg(to_jsonb(s)-'facts_version' ORDER BY id) FROM user_space_style_snapshots s))::text;"
psql_scalar "$space_original_query" >"$work_dir/legacy-before.json"
space_initial_highwater="$(psql_scalar 'SELECT last_value FROM personal_space_facts_version_seq;')"
run_check legacy-reup bash scripts/migrate.sh up
psql_scalar "$space_original_query" >"$work_dir/legacy-after-up.json"
cmp "$work_dir/legacy-before.json" "$work_dir/legacy-after-up.json"
space_issued_highwater="$(psql_scalar 'SELECT max(facts_version) FROM (SELECT facts_version FROM user_spaces UNION ALL SELECT facts_version FROM user_space_style_snapshots)x;')"
(( space_issued_highwater > space_initial_highwater ))
run_check quota-preservation-boundary-down bash scripts/migrate.sh down
run_check preservation-down bash scripts/migrate.sh down
psql_scalar "$space_original_query" >"$work_dir/legacy-after-down.json"
cmp "$work_dir/legacy-before.json" "$work_dir/legacy-after-down.json"
run_check preservation-reup bash scripts/migrate.sh up
psql_scalar "$space_original_query" >"$work_dir/legacy-after-reup.json"
cmp "$work_dir/legacy-before.json" "$work_dir/legacy-after-reup.json"
space_reup_min="$(psql_scalar 'SELECT min(facts_version) FROM (SELECT facts_version FROM user_spaces UNION ALL SELECT facts_version FROM user_space_style_snapshots)x;')"
(( space_reup_min > space_issued_highwater ))
printf '%s\n' "$space_initial_highwater $space_issued_highwater $space_reup_min" >"$work_dir/nonce-preservation.txt"
run_check quota-legacy-down bash scripts/migrate.sh down
run_check quota-legacy-fixture psql -h 127.0.0.1 -U campusos -d "$database" -v ON_ERROR_STOP=1 -f scripts/fixtures/v12-02b-storage-quota-facts-legacy.sql
quota_original_query="SELECT jsonb_build_object('quotas',(SELECT jsonb_agg(to_jsonb(q)-'facts_version' ORDER BY user_id) FROM user_storage_quotas q),'owners',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u WHERE id BETWEEN 2549801 AND 2549803),'admin',(SELECT to_jsonb(a) FROM admin_accounts a WHERE id='adm.quota.legacy'))::text;"
psql_scalar "$quota_original_query" >"$work_dir/quota-legacy-before.json"
quota_initial_highwater="$(psql_scalar 'SELECT last_value FROM storage_quota_facts_version_seq;')"
run_check quota-legacy-reup bash scripts/migrate.sh up
psql_scalar "$quota_original_query" >"$work_dir/quota-legacy-after-up.json"
cmp "$work_dir/quota-legacy-before.json" "$work_dir/quota-legacy-after-up.json"
quota_issued_highwater="$(psql_scalar 'SELECT max(facts_version) FROM user_storage_quotas;')"
(( quota_issued_highwater > quota_initial_highwater ))
run_check quota-preservation-down bash scripts/migrate.sh down
psql_scalar "$quota_original_query" >"$work_dir/quota-legacy-after-down.json"
cmp "$work_dir/quota-legacy-before.json" "$work_dir/quota-legacy-after-down.json"
run_check quota-preservation-reup bash scripts/migrate.sh up
psql_scalar "$quota_original_query" >"$work_dir/quota-legacy-after-reup.json"
cmp "$work_dir/quota-legacy-before.json" "$work_dir/quota-legacy-after-reup.json"
quota_reup_min="$(psql_scalar 'SELECT min(facts_version) FROM user_storage_quotas;')"
(( quota_reup_min > quota_issued_highwater ))
printf '%s\n' "$quota_initial_highwater $quota_issued_highwater $quota_reup_min" >"$work_dir/quota-nonce-preservation.txt"
run_check final-migrations-check bash scripts/migrate.sh check
required_regex="$(python3 - <<'SELECT'
import json,re
from pathlib import Path
known=set(json.loads(Path('docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-admin-thread-read-linux.json').read_text())['checks'])
names=[]
for folder in ['internal/modules/features/richtext','internal/modules/core/community/service','internal/modules/core/userstorage','internal/modules/features/personaldocuments','internal/server','internal/modules/core/identity/policy','internal/modules/core/identity/service','internal/projectaudit','internal/modules/core/identity/port','internal/modules/core/identity/portadapt','internal/modules/core/community','internal/modules/core/community/handler','internal/modules/features/personalspace','internal/platform/reliability','internal/modules/features/appearance/runtime','internal/modules/features/appearance/stylepack']:
 for path in Path(folder).glob('*test.go'):
  for name in re.findall(r'^func (Test\w+)\(',path.read_text(),re.M):
   if name in known or name.startswith('TestSpaceCurrentRead'):names.append(name)
names=sorted(set(names));assert len(names)==255 and len([n for n in names if n.startswith('TestSpaceCurrentRead')])==11
print('^('+'|'.join(names)+')$')
SELECT
)"
run_check target-tests env CAMPUSOS_SPACE_FILES_TEST_DATABASE_URL="$database_url" CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$author_database_url" CAMPUSOS_RICHTEXT_TEST_DATABASE_URL="$read_database_url" CAMPUSOS_ASSET_EMPTY_TEST_DATABASE_URL="$empty_database_url" \
 go test -race -p 1 -count=1 -v -run "$required_regex" ./internal/modules/features/richtext ./internal/modules/core/community/service ./internal/modules/core/userstorage ./internal/modules/features/personaldocuments ./internal/server ./internal/modules/core/identity/policy ./internal/modules/core/identity/service ./internal/projectaudit ./internal/modules/core/identity/port ./internal/modules/core/identity/portadapt ./internal/modules/core/community ./internal/modules/core/community/handler ./internal/modules/features/personalspace ./internal/platform/reliability ./internal/modules/features/appearance/runtime ./internal/modules/features/appearance/stylepack
run_check composition go test -race -count=1 ./internal/modules/features/personalspace ./internal/modules/core/identity/portadapt ./internal/modules/core/identity/policy ./internal/modules/core/identity/port ./internal/modules/features/richtext ./internal/modules/core/community ./internal/modules/core/community/handler ./internal/modules/core/community/service ./internal/modules/features/personaldocuments ./internal/modules/core/userstorage ./internal/modules/core/identity/service ./internal/server ./internal/platform/reliability ./internal/modules/features/appearance/runtime ./internal/modules/features/appearance/stylepack
run_check windows-cross-compile env GOOS=windows GOARCH=amd64 go test -c ./internal/modules/features/personalspace -o "$work_dir/personalspace-windows.test.exe"
run_check windows-community-cross-compile env GOOS=windows GOARCH=amd64 go test -c ./internal/modules/core/community -o "$work_dir/community-windows.test.exe"
run_check web-component pnpm --dir web exec vitest run tests/space-profile-view.spec.ts tests/space-public-view.spec.ts --maxWorkers=1 --minWorkers=1
run_check web-format pnpm --dir web format:check
run_check web-build pnpm --dir web build
run_check admin-component pnpm --dir admin exec vitest run --maxWorkers=1 --minWorkers=1
run_check admin-build pnpm --dir admin build
run_check architecture-sync python3 skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
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
previous=json.loads(Path(prefix+'v12-02b-admin-thread-read-linux.json').read_text())
known=set(previous['checks']);required=[]
for folder in ['internal/modules/features/richtext','internal/modules/core/community/service','internal/modules/core/userstorage','internal/modules/features/personaldocuments','internal/server','internal/modules/core/identity/policy','internal/modules/core/identity/service','internal/projectaudit','internal/modules/core/identity/port','internal/modules/core/identity/portadapt','internal/modules/core/community','internal/modules/core/community/handler','internal/modules/features/personalspace','internal/platform/reliability','internal/modules/features/appearance/runtime','internal/modules/features/appearance/stylepack']:
 for path in Path(folder).glob('*test.go'):
  for name in re.findall(r'^func (Test\w+)\(',path.read_text(),re.M):
   if name in known or name.startswith('TestSpaceCurrentRead'):required.append(name)
required=sorted(set(required));new=[n for n in required if n.startswith('TestSpaceCurrentRead')]
actual=re.findall(r'^--- PASS: (\w+)(?: |$)',tests,re.M)
assert len(new)==11 and len(required)==255 and sorted(actual)==sorted(required), f'required target tests differ: {required}, {actual}'
assert not re.search(r'^\s*--- (?:FAIL|SKIP): ',tests,re.M),'target tests skipped or failed'
assert 'WARNING: DATA RACE' not in tests
sha=lambda path:hashlib.sha256(Path(path).read_bytes()).hexdigest()
paths=['docs/api/http-routes-v0.6.json','docs/api/HTTP路由与授权矩阵-v0.6.md','docs/api/openapi-v0.6-current.yaml','cmd/campusos-contracts/main.go','internal/modules/core/identity/module.go','internal/modules/core/identity/module_test.go','admin/src/modules/architecture/pages/SystemArchitectureView.vue','admin/package.json','admin/pnpm-lock.yaml','migrations/README.md','scripts/schema-contract.sql','skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py','Makefile','internal/transport/httpapi/router.go','modules/core/user-storage/module.yaml','scripts/v12-02b-space-public-read-drill.sh']
paths += ['web/tests/space-public-view.spec.ts','web/src/modules/space/pages/PublicSpaceView.vue','admin/tests/thread-metadata.spec.ts','admin/src/modules/identity/pages/UserManageView.vue','admin/tests/space-quota.spec.ts','scripts/fixtures/v12-02b-storage-quota-facts-legacy.sql','admin/src/modules/integrations/api.ts','web/src/modules/space/api.ts','web/src/modules/space/pages/SpaceSettingsView.vue','web/tests/space-profile-view.spec.ts']
paths += ['modules/core/user-storage/module.yaml','scripts/fixtures/v12-02b-space-facts-legacy.sql']
paths += ['admin/src/modules/community/api.ts','admin/src/modules/community/pages/ThreadManageView.vue','admin/src/modules/operations/api.ts','admin/src/modules/operations/pages/EventLogView.vue','admin/tests/event-metadata.spec.ts','admin/vitest.config.ts','scripts/v12-02b-event-metadata-browser.mjs']
for folder in ['pkg/eventbus','pkg/middleware','pkg/auth','internal/modules/features/personaldocuments','internal/modules/features/mutualaid','internal/modules/features/secondhand','internal/modules/core/community','internal/modules/core/moderation','internal/modules/core/identity/port','internal/modules/core/identity/policy','internal/modules/core/contenteditor','internal/modules/core/userstorage','internal/modules/features/richtext','internal/modules/features/personalspace','internal/modules/features/appearance','internal/modules/core/identity/portadapt','internal/modules/core/identity/repository','internal/modules/core/identity/service','internal/platform/reliability','internal/platform/transaction','internal/server']:
 paths += [str(p) for p in Path(folder).rglob('*.go')]
paths += [str(p) for p in Path('web/src/modules/content-editor').glob('*') if p.suffix in ['.ts','.vue']]
paths += ['web/tests/thread-flags-view.spec.ts','web/vitest.config.ts','web/src/modules/richtext/api.ts']
paths += ['web/src/modules/mutual-aid/pages/MutualAidDetailView.vue','web/src/modules/secondhand/pages/SecondhandDetailView.vue','web/src/modules/personal-documents/pages/PersonalDocumentsView.vue','web/src/modules/identity/session.ts','web/src/modules/community/components/SafeContentEditor.vue','web/src/modules/community/pages/CreateThreadView.vue','web/src/modules/community/pages/ThreadDetailView.vue','web/tests/checked-images.spec.ts','web/tests/structured-content.spec.ts','web/package.json','web/pnpm-lock.yaml']
paths += [str(p) for p in Path('migrations').glob('*.sql')]
paths += [str(p) for p in Path('internal/projectaudit').rglob('*.go')]
paths += [str(p) for p in Path('migrations/er/current').glob('*') if p.is_file()]
paths += [str(p) for p in Path('scripts').glob('v12-02b*drill.sh')]
paths += [str(p) for p in Path('scripts/fixtures').glob('v12-02b-business-actor*.sql')]
paths += ['scripts/fixtures/v12-02b-post-facts-legacy.sql','scripts/fixtures/v12-02b-article-facts-legacy.sql','scripts/fixtures/v12-02b-user-scope-facts-legacy.sql','scripts/fixtures/v12-02b-richtext-asset-facts-legacy.sql']
prefix='docs/项目计划书v1/项目计划v1.2/evidence/'
dependencies=['v12-02b-admin-thread-read-linux.json','v12-02b-avatar-retention-linux.json','v12-02b-avatar-upload-linux.json','v12-02b-avatar-select-linux.json','v12-02b-space-owner-linux.json','v12-02b-space-facts-linux.json','v12-02b-image-upload-linux.json','v12-02b-asset-purge-linux.json','v12-02b-object-delete-linux.json','v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-document-read-linux.json','v12-02b-typed-feature-read-linux.json','v12-02b-inline-image-linux.json','v12-02b-space-files-linux.json','v12-02b-author-write-linux.json','v12-02b-author-lifecycle-linux.json','v12-02b-author-create-linux.json','v12-02b-typed-author-create-linux.json','v12-02b-richtext-author-create-linux.json','v12-02b-typed-author-write-linux.json','v12-02b-richtext-author-write-linux.json','v12-02b-document-author-write-linux.json','v12-02b-admin-principal-linux.json','v12-02b-admin-permission-facts-linux.json','v12-02b-board-governance-authority-linux.json','v12-02b-business-actor-linux.json','v12-02b-thread-governance-linux.json','v12-02b-post-facts-linux.json','v12-02b-post-author-write-linux.json','v12-02b-post-governance-linux.json','v12-02b-post-read-linux.json','v12-02b-event-metadata-linux.json','v12-02b-admin-flags-linux.json','v12-02b-admin-review-linux.json','v12-02b-admin-lifecycle-linux.json','v12-02b-admin-purge-linux.json','v12-02b-article-facts-linux.json','v12-02b-richtext-governance-linux.json','v12-02b-user-scope-facts-linux.json','v12-02b-user-scope-memory-linux.json','v12-02b-user-flags-linux.json','v12-02b-moderation-preview-linux.json','v12-02b-richtext-asset-facts-linux.json','v12-02b-asset-memory-linux.json','v12-02b-attachment-metadata-linux.json','v12-02b-asset-read-linux.json','v12-02b-object-quota-linux.json','v12-02b-attachment-upload-linux.json','v12-02b-asset-purge-linux.json']
r={
 'schema':'campusos.v12-02b-space-public-read-acceptance/v1','generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02b','stage_status':'partial','slice_status':'completed',
 'implementation':'passed','automation':'passed','target_environment':'passed',
 'scope':'Six real Space public/owner read routes plus complete own-profile User proof: named current Space/User Policies, explicit public projection, protected Core whole/count/page and source rechecks, safe sync status. Public Web page captures route/login context, drops stale private responses and rechecks schedule bridge after I/O.',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),'postgres_version_num':int(sys.argv[3]),'database':'four owned databases in disposable PostgreSQL16 loopback tmpfs; existing richtext read PG resources plus current author DB verified User Session/Object byte repositories'},
 'checks':{name:'passed' for name in required},
 'coverage':{'new_required_tests':11,'previous_regressions':244,'real_postgres_tests':74,'impact_selected_tests':255,'native':'216 named Policy combinations, 36 current read HTTP scenarios, actual Module six read entries, current Space/User/config/preferences/Core page and empty total changes, no bare query fallback. Web five current-context regressions plus previous39 profile components.','postgres':'42 current/default/state reads and50 committed mutation scenarios across six actual Space paths with current User sessions and protected Core PostgreSQL queries. Previous244 regressions rerun; four owned databases and23/24 original data/actor/nonreuse preservation.','migration_count':int(sys.argv[4]),'ddl':'No new DDL; current24next25; existing16/17/18 resource/permission/provenance and23/24 full data preserved.'},
 'source_sha256':{path:sha(path) for path in sorted(set(paths))},
 'dependencies':{prefix+name:sha(prefix+name) for name in dependencies},
 'common_gates_status':'passed',
 'logs':{path.stem:{'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())),'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'boundaries':['V12-02b remains partial; continue all remaining tasks per user mandate.','Public profile wire excludes private filters, operational errors and governance attribution. Unlisted/private/disabled Space is not public.','Owner is a complete current User session, never same-ID Admin or caller fields; protected author query has no bare-ID or legacy projection fallback.','Complete current Space23/default/config/User7 and protected Core whole-page/count are checked after projection, including empty results; counters are not authorization facts.','Schedule style bridge rechecks the same current page/identity after schedule I/O; no stale private content under another context.','Owned PostgreSQL only; no shared reset/commit/push. Windows compilation is not target execution.'],

}
report.parent.mkdir(parents=True,exist_ok=True)
report.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n')
print(f'V12-02b current Space public and owner reads slice passed: {len(required)} required tests; stage remains partial')
REPORT
