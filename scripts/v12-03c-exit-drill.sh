#!/usr/bin/env bash
#03c host-contract exit. Only unique owned PostgreSQL; no real Wasm/Container runner.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_MANAGED_DIR:?set absolute owned evidence directory}"
case "$work" in "$root"/.cache/v12-03c-exit/acceptance.*) ;; *) echo 'requires owned03c exit evidence directory' >&2;exit 2;; esac
export GOCACHE="$root/.cache/v12-g0/go-cache" TMPDIR="$work/tmp"
mkdir -p "$TMPDIR" "$work/native"
check(){ local name="$1";shift;if "$@" >"$work/common-$name.log" 2>&1;then echo "$name" >>"$work/common-passed.txt";else tail -n 35 "$work/common-$name.log" >&2;return 1;fi; }
native(){ local name="$1" binary="$2" package="$3" pattern="$4";shift 4
 (cd "$package";env -i PATH="$PATH" TMPDIR="$TMPDIR" "$@" "$work/native/$binary.test" -test.run "$pattern" -test.v -test.count=1) >"$work/native/$name.log" 2>"$work/native/$name.stderr"
 go tool test2json -t <"$work/native/$name.log" >"$work/native/$name.jsonl"
}
if [[ "$mode" == target ]];then
 [[ ! -e "$work/source-freeze.json" ]] || { echo 'use a fresh owned target directory' >&2;exit 2; }
 python3 scripts/v12-03c-exit-report.py freeze "$work"
 uname -srm >"$work/environment.txt"
 go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/plugin-build.log" 2>&1
 go test -c -race -o "$work/native/server.test" ./internal/server >"$work/native/server-build.log" 2>&1
 file "$work/native/plugin.test" "$work/native/server.test" >"$work/native/binaries.txt"
 native plugin plugin internal/plugin '^TestV5|^TestPluginV5' "CAMPUSOS_V5_DISPATCH_DECISIONS=$work/native/dispatch-decisions.json"
 node scripts/check-v12-03c-native-dispatch.mjs "$work/native/dispatch-decisions.json" >"$work/native/dispatch-oracle.log"
 native assembly server internal/server '^TestV5(ChatDispatcherAssemblyHasNoFakeRunner|ContributionDirectoryAssembly)$'
 owned_container="campusos-v12-03c-exit-$(date +%s)-$$"
 cleanup(){ docker rm -f -v "$owned_container" >"$work/cleanup.log" 2>&1; }
 trap cleanup EXIT
 umask 077
 owned_password="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
 docker run -d --name "$owned_container" -v "$root:$root:ro" -w "$root" -e POSTGRES_DB=campusos_v12_03c_exit -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$owned_password" -p 127.0.0.1::5432 postgres:16-alpine >"$work/container-create.log"
 export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$owned_container" DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_NAME=campusos_v12_03c_exit DB_PASSWORD="$owned_password"
 sql(){ docker exec -i "$owned_container" psql -U campusos -d "$DB_NAME" -qAtv ON_ERROR_STOP=1 "$@"; }
 ready(){ for attempt in $(seq 1 80);do if docker exec "$owned_container" pg_isready -h 127.0.0.1 -U campusos -d "$DB_NAME" >/dev/null 2>&1;then return;fi;sleep 0.5;done;return 1; }
 migrate(){ bash scripts/migrate.sh "$1" >"$work/migration-$2.log" 2>&1; }
 snap(){ sql -f scripts/fixtures/v12-03c-legacy-snapshot.sql >"$work/legacy-$1.json"; }
 queue_snap(){ sql -c 'SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id), '\''[]'\''::jsonb) FROM plugin_v5_event_deliveries t' >"$work/queue-$1.json"; }
 ready
 sql -c 'SHOW server_version_num' >"$work/pg-version.txt"
 migrate up empty-up
 migrate down empty-down
 [[ "$(sql -c 'SELECT max(version)::int FROM schema_migrations')" == 27 ]]
 sql -f scripts/fixtures/v12-02b-storage-quota-facts-legacy.sql >"$work/legacy-seed.log"
 sql -c "INSERT INTO plugins(id,name,display_name,version,runtime,manifest) VALUES(303100,'03c-historical','Historical preserved','1.0.0','grpc','{\"historical\":true}'); SELECT nextval('plugin_v5_generations');" >>"$work/legacy-seed.log"
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
 sql -f scripts/schema-contract.sql >"$work/schema-contract.log"
 run_pg(){ local phase="$1" pattern="$2" port
  port="$(docker port "$owned_container" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
  native "$phase" plugin internal/plugin "$pattern" "CAMPUSOS_V5_MANAGED_DATABASE_URL=postgres://campusos:$owned_password@127.0.0.1:$port/$DB_NAME?sslmode=disable" "CAMPUSOS_V5_MANAGED_OUTPUT=$work/pg"
 }
 run_pg pg '^TestPostgresV5ManagedExit$'
 snap before-restart
 queue_snap before-restart
 docker restart "$owned_container" >"$work/postgres-restart.log"
 ready
 snap after-restart
 queue_snap after-restart
 cmp "$work/legacy-before-restart.json" "$work/legacy-after-restart.json"
 cmp "$work/queue-before-restart.json" "$work/queue-after-restart.json"
 run_pg reload '^TestPostgresV5ManagedRestart$'
 snap before-guard
 queue_snap before-guard
 if bash scripts/migrate.sh down >"$work/migration-nonempty-down-rejected.log" 2>&1;then echo 'nonempty28 rollback unexpectedly allowed' >&2;exit 1;fi
 [[ "$(sql -c 'SELECT max(version)::int FROM schema_migrations')" == 28 ]]
 snap after-guard
 queue_snap after-guard
 cmp "$work/legacy-before-guard.json" "$work/legacy-after-guard.json"
 cmp "$work/queue-before-guard.json" "$work/queue-after-guard.json"
 migrate check final-check
 bash "$0" common
elif [[ "$mode" == common ]];then
 [[ "${DB_NAME:-}" == campusos_v12_03c_exit && "${POSTGRES_CONTAINER:-}" == campusos-v12-03c-exit-* && "${PSQL_MODE:-}" == docker ]] || { echo 'common database gate requires the live owned target container' >&2;exit 2; }
 python3 scripts/v12-03c-exit-report.py verify "$work"
 : >"$work/common-passed.txt"
 check full-go go test ./... -count=1
 check related-race go test -race ./internal/plugin/... ./internal/server ./internal/projectaudit ./internal/transport/httpapi -count=1
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
 : >"$work/final-passed.txt"
 final(){ local name="$1";shift;if "$@" >"$work/final-$name.log" 2>&1;then echo "$name" >>"$work/final-passed.txt";else tail -n 35 "$work/final-$name.log" >&2;return 1;fi; }
 final docs-links make docs-links
 final docs-build make docs-build
 final line-endings python3 scripts/check-line-endings.py --include-untracked
 final diff git diff --check
 final architecture make architecture-check
 final skill-bridges python3 skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --check
 final onboarding-skill python3 "${CODEX_HOME:-$HOME/.codex}/skills/.system/skill-creator/scripts/quick_validate.py" skills/sources/campusos-project-onboarding
else echo 'usage: v12-03c-exit-drill.sh target|common|final' >&2;exit 2;fi
