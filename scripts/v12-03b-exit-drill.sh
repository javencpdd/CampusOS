#!/usr/bin/env bash
# Full03b exit: owned PostgreSQL, actual native processes, current source seal.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-all}"
if [[ "$mode" == all ]]; then
 mkdir -p .cache/v12-03b-exit
 work="$(mktemp -d "$root/.cache/v12-03b-exit/acceptance.XXXXXX")"
 export CAMPUSOS_V5_EXIT_DIR="$work"
 echo "03b exit acceptance: $work"
 python3 scripts/v12-03b-exit-report.py --freeze "$work"
 bash "$0" target
 python3 scripts/v12-03b-exit-report.py --build "$work"
 exit 0
fi
work="${CAMPUSOS_V5_EXIT_DIR:?set owned exit directory}"
export GOCACHE="$root/.cache/v12-g0/go-cache" TMPDIR="$work/tmp"
mkdir -p "$TMPDIR" "$work/native"
check(){ local name="$1";shift;echo "03b gate: $name";if "$@" >"$work/common-$name.log" 2>&1;then echo "$name" >>"$work/common-passed.txt";else tail -n 35 "$work/common-$name.log" >&2;return 1;fi; }
if [[ "$mode" == target ]]; then
 go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/plugin-build.log" 2>&1
 (cd internal/plugin;env -i PATH="$PATH" TMPDIR="$TMPDIR" "$work/native/plugin.test" -test.run '^TestV5|^TestPluginV5' -test.v -test.count=1) >"$work/native/plugin.log" 2>"$work/native/plugin.stderr"
 go tool test2json -p github.com/campusos/CampusOS/internal/plugin -t <"$work/native/plugin.log" >"$work/native/plugin.jsonl"
 go test -c -race -o "$work/native/server.test" ./internal/server >"$work/native/server-build.log" 2>&1
 (cd internal/server;env -i PATH="$PATH" TMPDIR="$TMPDIR" "$work/native/server.test" -test.run '^TestPluginV5IdentityAssembly' -test.v -test.count=1) >"$work/native/assembly.log" 2>"$work/native/assembly.stderr"
 go tool test2json -p github.com/campusos/CampusOS/internal/server -t <"$work/native/assembly.log" >"$work/native/assembly.jsonl"
 go build -race -buildvcs=false -o "$work/native/campusos-server" ./cmd/server >"$work/native/server-binary-build.log" 2>&1
 export DRILL_DB_CONTAINER="campusos-v12-03b-exit-$(date +%s)-$$"
 cleanup(){ docker rm -f -v "$DRILL_DB_CONTAINER" >"$work/cleanup.log" 2>&1;rm -f "$work/private.env"; }
 trap cleanup EXIT
 secret="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
 docker run -d --name "$DRILL_DB_CONTAINER" -v "$root:$root:ro" -w "$root" -e POSTGRES_DB=campusos_v12_03b_exit -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$secret" -p 127.0.0.1::5432 postgres:16-alpine >"$work/container-create.log"
 for attempt in $(seq 1 80);do if docker exec "$DRILL_DB_CONTAINER" pg_isready -U campusos -d campusos_v12_03b_exit >/dev/null 2>&1;then break;fi;sleep 0.5;done
 port="$(docker port "$DRILL_DB_CONTAINER" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
 mkdir -p "$work/bin"
 umask 077
 printf 'export PGPASSWORD=%q\nexport DB_PASSWORD=%q\nexport DB_HOST=127.0.0.1\nexport DB_PORT=5432\nexport DB_USER=campusos\nexport DB_NAME=campusos_v12_03b_exit\nexport CAMPUSOS_V5_DATABASE_URL=%q\n' "$secret" "$secret" "postgres://campusos:$secret@127.0.0.1:$port/campusos_v12_03b_exit?sslmode=disable" >"$work/private.env"
 cat >"$work/bin/psql" <<'SHIM'
#!/usr/bin/env bash
exec docker exec -i -e PGPASSWORD "$DRILL_DB_CONTAINER" psql "$@"
SHIM
 chmod +x "$work/bin/psql"
 source "$work/private.env"
 unset secret
 export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host POSTGRES_CONTAINER="$DRILL_DB_CONTAINER" PATH="$work/bin:$PATH"
 sql(){ docker exec -i -e PGPASSWORD "$DRILL_DB_CONTAINER" psql -h 127.0.0.1 -U campusos -d "$DB_NAME" -qAtv ON_ERROR_STOP=1 "$@"; }
 migrate(){ bash scripts/migrate.sh "$1" >"$work/migration-$2.log" 2>&1; }
 snap(){ sql -f scripts/fixtures/v12-03b-install-legacy-snapshot.sql >"$work/legacy-$1.json"; }
 v5snap(){ sql -c "SELECT jsonb_build_object('releases',(SELECT jsonb_agg(to_jsonb(r) ORDER BY record_id) FROM plugin_package_releases r),'global',(SELECT jsonb_agg(to_jsonb(r) ORDER BY record_id) FROM plugin_installations r),'operations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY record_id) FROM plugin_operations r),'grants',(SELECT jsonb_agg(to_jsonb(r) ORDER BY record_id) FROM plugin_v5_admin_grants r),'identities',(SELECT jsonb_agg(to_jsonb(r) ORDER BY publisher,plugin_key,version) FROM plugin_package_identities r),'generation',(SELECT last_value FROM plugin_v5_generations))" >"$work/v5-$1.json"; }
 sql -c 'SHOW server_version_num' >"$work/pg-version.txt"
 migrate up empty-up
 sql -c "SELECT nextval('plugin_v5_generations')" >"$work/epoch-before-down.txt"
 migrate down empty-down
 [[ "$(sql -c 'SELECT max(version)::integer FROM schema_migrations')" == 26 ]]
 sql -c "SELECT nextval('plugin_v5_generations')" >"$work/epoch-after-down.txt"
 sql -f scripts/fixtures/v12-02b-storage-quota-facts-legacy.sql >"$work/legacy-seed.log"
 sql -c "INSERT INTO plugins(id,name,display_name,version,runtime,manifest) VALUES(270001,'legacy-v5-kept','Historical plugin','1.0.0','grpc','{\"historical\":true}'); INSERT INTO plugin_versions(id,plugin_id,version,package_digest,manifest_api_version,host_api_version,permission_fingerprint,manifest) VALUES(270002,270001,'1.0.0',repeat('a',64),'campusos.plugin/v1','v1',repeat('b',64),'{\"historical\":true}'); INSERT INTO plugin_package_identities(publisher,plugin_key,version,package_digest) VALUES('campusos','historical-identity','1.0.26','sha256:'||repeat('c',64));" >>"$work/legacy-seed.log"
 snap before
 migrate up preservation-up
 snap after-up
 cmp "$work/legacy-before.json" "$work/legacy-after-up.json"
 migrate down preservation-down
 snap after-down
 cmp "$work/legacy-before.json" "$work/legacy-after-down.json"
 migrate up preservation-reup
 snap after-reup
 cmp "$work/legacy-before.json" "$work/legacy-after-reup.json"
 sql -c "SELECT nextval('plugin_v5_generations')" >"$work/epoch-after-reup.txt"
 export CAMPUSOS_V5_OUTPUT="$work/lifecycle"
 (cd internal/plugin;env -i PATH="$PATH" TMPDIR="$TMPDIR" CAMPUSOS_V5_DATABASE_URL="$CAMPUSOS_V5_DATABASE_URL" CAMPUSOS_V5_OUTPUT="$CAMPUSOS_V5_OUTPUT" "$work/native/plugin.test" -test.run '^TestPostgresV5LifecycleAtomicityAndRecovery$' -test.v -test.count=1) >"$work/native/pg.log" 2>"$work/native/pg.stderr"
 go tool test2json -p github.com/campusos/CampusOS/internal/plugin -t <"$work/native/pg.log" >"$work/native/pg.jsonl"
 snap before-restart
 v5snap before-restart
 docker restart "$DRILL_DB_CONTAINER" >"$work/postgres-restart.log"
 port="$(docker port "$DRILL_DB_CONTAINER" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
 export CAMPUSOS_V5_DATABASE_URL="postgres://campusos:$DB_PASSWORD@127.0.0.1:$port/campusos_v12_03b_exit?sslmode=disable"
 for attempt in $(seq 1 80);do if sql -c 'SELECT 1' >/dev/null 2>&1;then break;fi;sleep 0.5;done
 snap after-restart
 v5snap after-restart
 cmp "$work/legacy-before-restart.json" "$work/legacy-after-restart.json"
 cmp "$work/v5-before-restart.json" "$work/v5-after-restart.json"
 (cd internal/plugin;env -i PATH="$PATH" TMPDIR="$TMPDIR" CAMPUSOS_V5_DATABASE_URL="$CAMPUSOS_V5_DATABASE_URL" CAMPUSOS_V5_OUTPUT="$CAMPUSOS_V5_OUTPUT" "$work/native/plugin.test" -test.run '^TestPostgresV5Restore$' -test.v -test.count=1) >"$work/native/restore.log" 2>"$work/native/restore.stderr"
 go tool test2json -p github.com/campusos/CampusOS/internal/plugin -t <"$work/native/restore.log" >"$work/native/restore.jsonl"
 mkdir -p "$work/server"
 (cd internal/server;env -i PATH="$PATH" TMPDIR="$TMPDIR" CAMPUSOS_V5_SERVER_DATABASE_URL="$CAMPUSOS_V5_DATABASE_URL" CAMPUSOS_V5_SERVER_BINARY="$work/native/campusos-server" CAMPUSOS_V5_SERVER_OUTPUT="$work/server" "$work/native/server.test" -test.run '^TestPluginV5ActualServerInstallationBoundary$' -test.v -test.count=1) >"$work/native/http.log" 2>"$work/native/http.stderr"
 go tool test2json -p github.com/campusos/CampusOS/internal/server -t <"$work/native/http.log" >"$work/native/http.jsonl"
 snap before-guard
 v5snap before-guard
 if bash scripts/migrate.sh down >"$work/migration-nonempty-down-rejected.log" 2>&1;then echo 'nonempty27 rollback unexpectedly allowed' >&2;exit 1;fi
 [[ "$(sql -c 'SELECT max(version)::integer FROM schema_migrations')" == 27 ]]
 snap after-guard
 v5snap after-guard
 cmp "$work/legacy-before-guard.json" "$work/legacy-after-guard.json"
 cmp "$work/v5-before-guard.json" "$work/v5-after-guard.json"
 migrate check final-check
 unset CAMPUSOS_V5_DATABASE_URL CAMPUSOS_V5_OUTPUT
 bash "$0" common
elif [[ "$mode" == common ]];then
 python3 scripts/v12-03b-exit-report.py --verify-source "$work"
 : >"$work/common-passed.txt"
 check full-go go test ./... -count=1
 check related-race go test -race ./internal/plugin/... ./internal/server ./internal/projectaudit ./internal/transport/httpapi ./internal/modules/core/identity/port ./pkg/apperror -count=1
 check sdk pnpm --dir sdk/typescript build
 check contracts make contracts-check
 check architecture make architecture-check
 check database make database-check
 check admin-build pnpm --dir admin build
 check er-tests python3 -m unittest migrations.tools.test_generate_er
 check readme make readme-check
 check docs-links make docs-links
 check docs-build make docs-build
 check line-endings python3 scripts/check-line-endings.py --include-untracked
 check diff git diff --check
elif [[ "$mode" == final ]];then
 : >"$work/final-docs-passed.txt"
 final(){ local name="$1";shift;echo "03b final: $name";if "$@" >"$work/final-$name.log" 2>&1;then echo "$name" >>"$work/final-docs-passed.txt";else tail -n 35 "$work/final-$name.log" >&2;return 1;fi; }
 final docs-links make docs-links
 final docs-build make docs-build
 final line-endings python3 scripts/check-line-endings.py --include-untracked
 final diff git diff --check
 final architecture make architecture-check
 final skill-bridges python3 skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --check
 final onboarding-skill python3 "${CODEX_HOME:-$HOME/.codex}/skills/.system/skill-creator/scripts/quick_validate.py" skills/sources/campusos-project-onboarding
 python3 scripts/v12-03b-exit-report.py --finalize "$work"
else echo 'usage: v12-03b-exit-drill.sh all|target|common|final' >&2;exit 2;fi
