#!/usr/bin/env bash
# Current User Personal Space style variantss, actual article binding, current quota ledger and exact cleanup.
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if (( $# != 0 )); then echo "usage: $0" >&2; exit 2; fi
mkdir -p .cache/v12-02b-space-style-variants/tmp
export TMPDIR="$repo_root/.cache/v12-02b-space-style-variants/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-space-style-variants/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-space-style-variants/drill.XXXXXX")"
container="campusos-v12-02b-space-style-variants-$$"
database="campusos_v12_02b_space_files"
pg_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
report="docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-space-style-variants-linux.json"
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
  echo "V12-02b Personal Space style variants: $name"
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
run_check preservation-down bash scripts/migrate.sh down
psql_scalar "$space_original_query" >"$work_dir/legacy-after-down.json"
cmp "$work_dir/legacy-before.json" "$work_dir/legacy-after-down.json"
run_check preservation-reup bash scripts/migrate.sh up
psql_scalar "$space_original_query" >"$work_dir/legacy-after-reup.json"
cmp "$work_dir/legacy-before.json" "$work_dir/legacy-after-reup.json"
space_reup_min="$(psql_scalar 'SELECT min(facts_version) FROM (SELECT facts_version FROM user_spaces UNION ALL SELECT facts_version FROM user_space_style_snapshots)x;')"
(( space_reup_min > space_issued_highwater ))
printf '%s\n' "$space_initial_highwater $space_issued_highwater $space_reup_min" >"$work_dir/nonce-preservation.txt"
required_regex="$(python3 - <<'SELECT'
import json,re
from pathlib import Path
known=set(json.loads(Path('docs/项目计划书v1/项目计划v1.2/evidence/v12-02b-space-style-linux.json').read_text())['checks'])
names=[]
for folder in ['internal/modules/features/richtext','internal/modules/core/community/service','internal/modules/core/userstorage','internal/modules/features/personaldocuments','internal/server','internal/modules/core/identity/policy','internal/modules/core/identity/service','internal/projectaudit','internal/modules/core/identity/port','internal/modules/core/community','internal/modules/features/personalspace','internal/platform/reliability','internal/modules/features/appearance/runtime']:
 for path in Path(folder).glob('*test.go'):
  for name in re.findall(r'^func (Test\w+)\(',path.read_text(),re.M):
   if name in known or name.startswith('TestSpaceStyleVariants'):names.append(name)
names=sorted(set(names));assert len(names)==189 and len([n for n in names if n.startswith('TestSpaceStyleVariants')])==8
print('^('+'|'.join(names)+')$')
SELECT
)"
run_check target-tests env CAMPUSOS_SPACE_FILES_TEST_DATABASE_URL="$database_url" CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$author_database_url" CAMPUSOS_RICHTEXT_TEST_DATABASE_URL="$read_database_url" CAMPUSOS_ASSET_EMPTY_TEST_DATABASE_URL="$empty_database_url" \
 go test -race -p 1 -count=1 -v -run "$required_regex" ./internal/modules/features/richtext ./internal/modules/core/community/service ./internal/modules/core/userstorage ./internal/modules/features/personaldocuments ./internal/server ./internal/modules/core/identity/policy ./internal/modules/core/identity/service ./internal/projectaudit ./internal/modules/core/identity/port ./internal/modules/core/community ./internal/modules/features/personalspace ./internal/platform/reliability ./internal/modules/features/appearance/runtime
run_check composition go test -race -count=1 ./internal/modules/features/richtext ./internal/modules/core/community ./internal/modules/core/community/service ./internal/modules/features/personaldocuments ./internal/modules/core/userstorage ./internal/modules/core/identity/service ./internal/server ./internal/platform/reliability ./internal/modules/features/appearance/runtime
run_check windows-cross-compile env GOOS=windows GOARCH=amd64 go test -c ./internal/modules/features/personalspace -o "$work_dir/personalspace-windows.test.exe"
run_check web-component pnpm --dir web exec vitest run tests/space-profile-view.spec.ts --maxWorkers=1 --minWorkers=1
run_check web-format pnpm --dir web format:check
run_check web-build pnpm --dir web build
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
previous=json.loads(Path(prefix+'v12-02b-space-style-linux.json').read_text())
known=set(previous['checks']);required=[]
for folder in ['internal/modules/features/richtext','internal/modules/core/community/service','internal/modules/core/userstorage','internal/modules/features/personaldocuments','internal/server','internal/modules/core/identity/policy','internal/modules/core/identity/service','internal/projectaudit','internal/modules/core/identity/port','internal/modules/core/community','internal/modules/features/personalspace','internal/platform/reliability','internal/modules/features/appearance/runtime']:
 for path in Path(folder).glob('*test.go'):
  for name in re.findall(r'^func (Test\w+)\(',path.read_text(),re.M):
   if name in known or name.startswith('TestSpaceStyleVariants'):required.append(name)
required=sorted(set(required));new=[n for n in required if n.startswith('TestSpaceStyleVariants')]
actual=re.findall(r'^--- PASS: (\w+)(?: |$)',tests,re.M)
assert len(new)==8 and len(required)==189 and sorted(actual)==sorted(required), f'required target tests differ: {required}, {actual}'
assert not re.search(r'^\s*--- (?:FAIL|SKIP): ',tests,re.M),'target tests skipped or failed'
assert 'WARNING: DATA RACE' not in tests
sha=lambda path:hashlib.sha256(Path(path).read_bytes()).hexdigest()
paths=['docs/api/http-routes-v0.6.json','docs/api/HTTP路由与授权矩阵-v0.6.md','docs/api/openapi-v0.6-current.yaml','cmd/campusos-contracts/main.go','internal/modules/core/identity/module.go','internal/modules/core/identity/module_test.go','admin/src/modules/architecture/pages/SystemArchitectureView.vue','admin/package.json','admin/pnpm-lock.yaml','migrations/README.md','scripts/schema-contract.sql','skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py','Makefile','internal/transport/httpapi/router.go','modules/core/user-storage/module.yaml','scripts/v12-02b-space-style-variants-drill.sh']
paths += ['web/src/modules/space/api.ts','web/src/modules/space/pages/SpaceSettingsView.vue','web/tests/space-profile-view.spec.ts']
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
dependencies=['v12-02b-space-style-linux.json','v12-02b-avatar-retention-linux.json','v12-02b-avatar-upload-linux.json','v12-02b-avatar-select-linux.json','v12-02b-space-owner-linux.json','v12-02b-space-facts-linux.json','v12-02b-image-upload-linux.json','v12-02b-asset-purge-linux.json','v12-02b-object-delete-linux.json','v12-02a-exit-linux.json','v12-02d-exit-linux.json','v12-02b-document-read-linux.json','v12-02b-typed-feature-read-linux.json','v12-02b-inline-image-linux.json','v12-02b-space-files-linux.json','v12-02b-author-write-linux.json','v12-02b-author-lifecycle-linux.json','v12-02b-author-create-linux.json','v12-02b-typed-author-create-linux.json','v12-02b-richtext-author-create-linux.json','v12-02b-typed-author-write-linux.json','v12-02b-richtext-author-write-linux.json','v12-02b-document-author-write-linux.json','v12-02b-admin-principal-linux.json','v12-02b-admin-permission-facts-linux.json','v12-02b-board-governance-authority-linux.json','v12-02b-business-actor-linux.json','v12-02b-thread-governance-linux.json','v12-02b-post-facts-linux.json','v12-02b-post-author-write-linux.json','v12-02b-post-governance-linux.json','v12-02b-post-read-linux.json','v12-02b-event-metadata-linux.json','v12-02b-admin-flags-linux.json','v12-02b-admin-review-linux.json','v12-02b-admin-lifecycle-linux.json','v12-02b-admin-purge-linux.json','v12-02b-article-facts-linux.json','v12-02b-richtext-governance-linux.json','v12-02b-user-scope-facts-linux.json','v12-02b-user-scope-memory-linux.json','v12-02b-user-flags-linux.json','v12-02b-moderation-preview-linux.json','v12-02b-richtext-asset-facts-linux.json','v12-02b-asset-memory-linux.json','v12-02b-attachment-metadata-linux.json','v12-02b-asset-read-linux.json','v12-02b-object-quota-linux.json','v12-02b-attachment-upload-linux.json','v12-02b-asset-purge-linux.json']
r={
 'schema':'campusos.v12-02b-space-style-variants-acceptance/v1','generated_at':datetime.now(timezone.utc).isoformat(),
 'stage':'V12-02b','stage_status':'partial','slice_status':'completed',
 'implementation':'passed','automation':'passed','target_environment':'passed',
 'scope':'Actual HTML/ZIP/source Space style application through typed Appearance/current resolver: fixed owner variant Policies and complete current User/Space23/all Snapshot23/current config, trusted detached bounded HTML/ZIP construction and whole strict source Package/RawFiles/root rechecks before and after metadata. Existing exact style/history/required audit/outbox transaction, safe IDs; strict HTTP/current JWT, bounded single-file multipart closed before temp cleanup, Web waits ack then current checked profile read. Public declared administrator source assets retain their separate intended public meaning.',
 'environment':{'os':platform.system(),'architecture':platform.machine(),'go':subprocess.check_output(['go','version'],text=True).strip(),'postgres_version_num':int(sys.argv[3]),'database':'four owned databases in disposable PostgreSQL16 loopback tmpfs; existing richtext read PG resources plus current author DB verified User Session/Object byte repositories'},
 'checks':{name:'passed' for name in required},
 'coverage':{'new_required_tests':8,'previous_regressions':181,'real_postgres_tests':59,'impact_selected_tests':189,'native':'Eight new tops: HTML current/unsafe/current proof/required rollback; ZIP detached bounded input/wrong target/short input/typed nil/no read for denied actor and input-time revocation; strict source delivery/current/required rollback and five actual root/body/preview/credential/post-write changes; real JWT three routes strict DTO/multipart, safe ack, forced disk multipart cleanup and revoked session. Actual production Module/App test extended to all six style write paths and missing proof/legacy target, zero old TrackOperation. Fixed owner Policy352 combinations. Twenty-two actual Web components include HTML/ZIP/source success and denial preserving view. Real source route/OpenAPI contracts verified.', 'postgres':'Two new PG tops:30 actual executions (6 success/24 actual P0001 required Space/snapshot/audit/outbox failures) for3 variants/default-existing history; actual User/Session/Space/all existing snapshot row/empty owner admission locks and whole metadata/history/current Session rollback. Ten actual ZIP input session/Space ABA, source root/body/postwrite/preview changes, actual extra SQL profile/history/session and disabled guard scenes with ran. Previous181/57PG rerun and current23 whole-data/nonreuse preservation; Windows compile only.', 'migration_count':int(sys.argv[4]),'ddl':'No new DDL; uses accepted23 actual facts and retained typed actor18. Current23 preservation regression.'},
 'source_sha256':{path:sha(path) for path in sorted(set(paths))},
 'dependencies':{prefix+name:sha(prefix+name) for name in dependencies},
 'common_gates_status':'passed',
 'logs':{path.stem:{'status':'passed','exit_code':0,'local_log':str(path.relative_to(Path.cwd())),'log_sha256':sha(path)} for path in sorted(work.glob('*.log'))},
 'boundaries':['V12-02b remains partial: checked style validation/preview/export/examples/source catalog, independent Admin Space, Admin lists/history/batch/preview/current SPA/browser and whole stage exit remain.', 'All six actual Space style write routes now use typed Appearance/current resolver, fixed server operations and required audit/outbox. Missing proof or new typed target closes; client cannot select owner/op/path/check/rawfacts.', 'HTML and ZIP are detached request input after owner authorization, not resource authority. ZIP is bounded16MiB/strict delivery/target and does not extract user disk files. Source packages bind actual RESOURCE_DIR/space-style-packs path and complete strict Package including hidden RawFiles; root/content changes reject before and after exact metadata.', 'Single-file multipart rejects all extra values/files, has request byte limit and closes the opened file before removing temporary form files. Safe errors/acks omit host roots. Public declared administrator source assets remain intentionally public.', 'Current source catalog read still needs safe current projection: SourcePackInfo.Path currently contains a physical resource path and is explicitly remaining work. Twenty-two Web components and actual Identity/module HTTP are distinct from full owned browser acceptance.', 'No new DDL, current23/next24;189 required/59 real PG and four-owned preservation/common gates; Windows only compiled; no shared DB reset/commit/push.'],

}
report.parent.mkdir(parents=True,exist_ok=True)
report.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n')
print(f'V12-02b Personal Space style variants slice passed: {len(required)} required tests; stage remains partial')
REPORT
