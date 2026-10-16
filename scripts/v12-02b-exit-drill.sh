#!/usr/bin/env bash
# Re-run all historical V12-02b assertions against current source in owned databases.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
(( $# == 0 )) || { echo 'usage: scripts/v12-02b-exit-drill.sh' >&2; exit 2; }
mkdir -p .cache/v12-02b-exit/tmp
export TMPDIR="$repo_root/.cache/v12-02b-exit/tmp" GOTMPDIR="$repo_root/.cache/v12-02b-exit/tmp"
export GOCACHE="$repo_root/.cache/v12-g0/go-cache"
work_dir="$(mktemp -d "$repo_root/.cache/v12-02b-exit/drill.XXXXXX")"
container="campusos-v12-02b-exit-$$"
pg_password="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
started=false
cleanup(){
 local status=$?
 if [[ "$started" == true ]];then
  if (( status != 0 ));then docker logs "$container" >"$work_dir/postgres-failure.log" 2>&1 || true;fi
  docker stop "$container" >/dev/null 2>&1 || true
 fi
 echo "$status" >"$work_dir/.exit_code"
 echo "V12-02b exit status $status; evidence: $work_dir"
}
trap cleanup EXIT
mkdir -p "$work_dir/bin" "$work_dir/check-state"
run_check(){
 local name="$1";shift
 echo "V12-02b exit: $name"
 if "$@" >"$work_dir/$name.log" 2>&1;then echo 0 >"$work_dir/check-state/$name.exit_code";return;fi
 echo 1 >"$work_dir/check-state/$name.exit_code"
 tail -n 70 "$work_dir/$name.log" >&2
 return 1
}
cat >"$work_dir/bin/psql" <<'SHIM'
#!/usr/bin/env bash
exec docker exec -i -e PGPASSWORD "$DRILL_DB_CONTAINER" psql "$@"
SHIM
chmod +x "$work_dir/bin/psql"
export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host PATH="$work_dir/bin:$PATH" DRILL_DB_CONTAINER="$container"
export DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_PASSWORD="$pg_password" DB_NAME=campusos_v12_02b_space_files
psql_scalar(){ docker exec -i -e PGPASSWORD="$pg_password" "$container" psql -h 127.0.0.1 -U campusos -d "$DB_NAME" -qAtv ON_ERROR_STOP=1 -c "$1"; }
run_check test-plan python3 scripts/v12-02b-exit-plan.py "$work_dir/test-plan.json"
python3 - "$work_dir" <<'FREEZE'
import hashlib,json,subprocess,sys
from pathlib import Path
root=Path('.')
paths=subprocess.check_output(['git','ls-files','--cached','--others','--exclude-standard'],text=True).splitlines()
extensions={'.go','.ts','.mts','.vue','.mjs','.py','.sh','.ps1','.sql','.yaml','.json','.mod','.sum','.toml','.lock','.css','.html'}
prefixes=('internal/','cmd/','pkg/','admin/src/','admin/tests/','web/src/','web/tests/','migrations/','modules/','scripts/','sdk/','packages/','schemas/','contracts/','skills/sources/')
configs={'Makefile','go.mod','go.sum','admin/package.json','admin/pnpm-lock.yaml','admin/vite.config.ts','admin/vitest.config.ts','admin/tsconfig.json','web/package.json','web/pnpm-lock.yaml','web/vite.config.ts','web/vitest.config.ts','web/tsconfig.json'}
source={p:hashlib.sha256(Path(p).read_bytes()).hexdigest()for p in paths if Path(p).is_file()and (p in configs or p.startswith(prefixes)and Path(p).suffix in extensions)}
work=Path(sys.argv[1]);(work/'source-freeze.json').write_text(json.dumps(source,ensure_ascii=False,indent=2)+'\n')
(work/'head.txt').write_text(subprocess.check_output(['git','rev-parse','HEAD'],text=True))
print('Frozen',len(source),'current source files')
FREEZE
docker run --rm -d --name "$container" --tmpfs /var/lib/postgresql/data:rw,size=2g -v "$repo_root:$repo_root:ro" -w "$repo_root" -e POSTGRES_DB="$DB_NAME" -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$pg_password" -p 127.0.0.1::5432 postgres:16-alpine >/dev/null
started=true
for i in $(seq 1 80);do if psql_scalar 'SELECT 1' >/dev/null 2>&1;then break;fi;if (( i == 80 ));then exit 1;fi;sleep 0.5;done
pg_version="$(psql_scalar 'SHOW server_version_num')";(( pg_version >= 160000 && pg_version < 170000 )) || exit 1
port="$(docker port "$container" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')";[[ "$port" =~ ^[0-9]+$ ]] || exit 1
printf '%s\n' "$pg_version" >"$work_dir/pg-version.txt"
run_check migrations-space-files bash scripts/migrate.sh up
for suffix in author_write richtext_read asset_read_empty admin_spa identity preservation public list owner owner_list document_read typed_read;do
 psql_scalar "CREATE DATABASE campusos_v12_02b_$suffix" >"$work_dir/create-$suffix.log"
 run_check "migrations-$suffix" env DB_NAME="campusos_v12_02b_$suffix" bash scripts/migrate.sh up
done
# A failed target run may reuse only already-passed costly checks whose entire
# behavioral source input set is byte-identical. Failed targets are always rerun.
if [[ -n "${V12_EXIT_PRIOR_PASSED_WORK:-}" ]];then
 python3 - "$V12_EXIT_PRIOR_PASSED_WORK" "$work_dir" <<'REUSE'
from pathlib import Path
import json,hashlib,shutil,sys
old,work=map(lambda p:Path(p).resolve(),sys.argv[1:])
assert old.parent==work.parent and old.name.startswith('drill.')and(old/'.exit_code').read_text().strip()=='1'
a=json.loads((old/'source-freeze.json').read_text());b=json.loads((work/'source-freeze.json').read_text());assert a.keys()==b.keys()
changed=sorted(p for p in a if a[p]!=b[p]);allowed={'scripts/v12-02b-exit-drill.sh','scripts/v12-02b-exit-report.py'}
assert set(changed)<=allowed,('behavioral inputs changed; full verification required',changed)
sha=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
reused=[]
for name in ['preservation','web-component','admin-component']:
 state=old/'check-state'/(name+'.exit_code');assert state.read_text().strip()=='0'
 log=old/(name+'.log');shutil.copy2(log,work/log.name);shutil.copy2(state,work/'check-state'/state.name)
 reused.append({'check':name,'source_work':str(old),'source_log_sha256':sha(log),'reason':'all behavioral source inputs unchanged; only exit orchestration/evidence scripts changed'})
shutil.copytree(old/'preservation',work/'preservation')
proof={'checks':reused,'original_freeze':str(old/'source-freeze.json'),'original_freeze_sha256':sha(old/'source-freeze.json'),'original_failed_target':str(old/'target-main.log'),'original_failed_target_sha256':sha(old/'target-main.log'),'orchestration_changes':{p:{'before':a[p],'after':b[p]}for p in changed}}
(work/'reused-checks.json').write_text(json.dumps(proof,ensure_ascii=False,indent=2)+'\n')
print('Reused3passedchecks after validating unchanged behavioral inputs; all target assertions will rerun')
REUSE
fi
if [[ ! -f "$work_dir/check-state/preservation.exit_code" ]];then run_check preservation env DB_NAME=campusos_v12_02b_preservation python3 scripts/v12-02b-exit-preservation.py "$work_dir/preservation";fi
if [[ ! -f "$work_dir/check-state/web-component.exit_code" ]];then run_check web-component pnpm --dir web exec vitest run --maxWorkers=1 --minWorkers=1;fi
run_check web-format pnpm --dir web format:check
run_check web-build pnpm --dir web build
if [[ ! -f "$work_dir/check-state/admin-component.exit_code" ]];then run_check admin-component pnpm --dir admin exec vitest run --maxWorkers=1 --minWorkers=1;fi
run_check admin-build pnpm --dir admin build
run_check admin-event-browser node scripts/v12-02b-event-metadata-browser.mjs
run_check server-build go build -o "$work_dir/campusos-server" ./cmd/server
run_check author-legacy-fixture env PGPASSWORD="$pg_password" psql -h 127.0.0.1 -U campusos -d campusos_v12_02b_author_write -v ON_ERROR_STOP=1 -f scripts/fixtures/v12-02b-business-actor-legacy.sql
url_base="postgres://campusos:$pg_password@127.0.0.1:$port"
mapfile -t shards < <(python3 -c 'import json,sys;print("\n".join(s["name"]for s in json.load(open(sys.argv[1]))["shards"]))' "$work_dir/test-plan.json")
for shard in "${shards[@]}";do
 regex="$(python3 -c 'import json,sys;s=next(s for s in json.load(open(sys.argv[1]))["shards"]if s["name"]==sys.argv[2]);print("^("+"|".join(s["tests"])+")$")' "$work_dir/test-plan.json" "$shard")"
 mapfile -t packages < <(python3 -c 'import json,sys;s=next(s for s in json.load(open(sys.argv[1]))["shards"]if s["name"]==sys.argv[2]);print("\n".join(s["packages"]))' "$work_dir/test-plan.json" "$shard")
 if [[ "$shard" == main ]];then
  run_check target-main env -u CAMPUSOS_COMMUNITY_TEST_DATABASE_URL \
   CAMPUSOS_SPACE_FILES_TEST_DATABASE_URL="$url_base/campusos_v12_02b_space_files?sslmode=disable" \
   CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL="$url_base/campusos_v12_02b_author_write?sslmode=disable" \
   CAMPUSOS_RICHTEXT_TEST_DATABASE_URL="$url_base/campusos_v12_02b_richtext_read?sslmode=disable" \
   CAMPUSOS_ASSET_EMPTY_TEST_DATABASE_URL="$url_base/campusos_v12_02b_asset_read_empty?sslmode=disable" \
   CAMPUSOS_IDENTITY_TEST_DATABASE_URL="$url_base/campusos_v12_02b_identity?sslmode=disable" \
   CAMPUSOS_DOCUMENT_TEST_DATABASE_URL="$url_base/campusos_v12_02b_document_read?sslmode=disable" \
   CAMPUSOS_TYPED_FEATURE_TEST_DATABASE_URL="$url_base/campusos_v12_02b_typed_read?sslmode=disable" \
   CAMPUSOS_ADMIN_SPA_TEST_DATABASE_URL="$url_base/campusos_v12_02b_admin_spa?sslmode=disable" \
   CAMPUSOS_ADMIN_SPA_SERVER_BINARY="$work_dir/campusos-server" CAMPUSOS_ADMIN_SPA_BROWSER_OUTPUT="$work_dir/browser" CAMPUSOS_THREAD_OWNER_POLICY_OUTPUT="$work_dir/owner-policy.jsonl" \
   go test -json -race -p 1 -count=1 -run "$regex" "${packages[@]}"
 else
  suffix="${shard#community_}"
  run_check "target-$shard" env CAMPUSOS_COMMUNITY_TEST_DATABASE_URL="$url_base/campusos_v12_02b_$suffix?sslmode=disable" go test -json -race -p 1 -count=1 -run "$regex" "${packages[@]}"
 fi
done
run_check owner-policy-oracle node scripts/check-v12-thread-owner-read-contract.mjs --go-output "$work_dir/owner-policy.jsonl" --report "$work_dir/owner-policy.json"
run_check composition go test -race -p 1 -count=1 ./internal/modules/core/identity/service ./internal/modules/core/identity/handler ./internal/modules/core/community ./internal/modules/core/community/service ./internal/modules/core/moderation ./internal/modules/core/userstorage ./internal/modules/features/richtext ./internal/modules/features/personalspace ./internal/modules/features/personaldocuments ./internal/modules/features/appearance/runtime ./internal/modules/features/mutualaid ./internal/modules/features/secondhand ./internal/server ./pkg/middleware
run_check windows-space-compile env GOOS=windows GOARCH=amd64 go test -c ./internal/modules/features/personalspace -o "$work_dir/space-windows.test.exe"
run_check windows-community-compile env GOOS=windows GOARCH=amd64 go test -c ./internal/modules/core/community -o "$work_dir/community-windows.test.exe"
run_check go-all go test ./... -count=1
for suffix in space_files author_write richtext_read asset_read_empty admin_spa identity preservation public list owner owner_list document_read typed_read;do
 run_check "database-$suffix" env DB_NAME="campusos_v12_02b_$suffix" bash scripts/database-check.sh all
done
run_check migration-parser python3 -m unittest migrations.tools.test_generate_er
run_check architecture-sync python3 skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
run_check contracts make contracts-check
run_check architecture make architecture-check
run_check docs-build make docs-build
run_check docs-links make docs-links
run_check line-endings python3 scripts/check-line-endings.py --include-untracked
run_check diff git diff --check
python3 scripts/v12-02b-exit-report.py "$work_dir"
echo 'All current-source V12-02b target/common checks passed; finalize synchronized documents before marking the phase complete.'
