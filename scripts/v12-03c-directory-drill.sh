#!/usr/bin/env bash
# First03c slice only. Owned PostgreSQL + native Linux host-port acceptance.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_DIRECTORY_DIR:?set absolute owned evidence directory}"
case "$work" in "$root"/.cache/v12-03c-directory/acceptance.*) ;; *) echo 'requires owned03c evidence directory' >&2;exit 2;; esac
export GOCACHE="$root/.cache/v12-g0/go-cache" TMPDIR="$work/tmp"
mkdir -p "$TMPDIR" "$work/native"
check(){ local name="$1";shift;if "$@" >"$work/common-$name.log" 2>&1;then echo "$name" >>"$work/common-passed.txt";else tail -n 30 "$work/common-$name.log" >&2;return 1;fi; }
native(){ local name="$1" binary="$2" package="$3" pattern="$4";shift 4
 (cd "$package";env -i PATH="$PATH" TMPDIR="$TMPDIR" "$@" "$work/native/$binary.test" -test.run "$pattern" -test.v -test.count=1) >"$work/native/$name.log" 2>"$work/native/$name.stderr"
 go tool test2json -t <"$work/native/$name.log" >"$work/native/$name.jsonl"
}
if [[ "$mode" == target ]]; then
 python3 scripts/v12-03c-directory-report.py freeze "$work"
 uname -srm >"$work/environment.txt"
 go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/plugin-build.log" 2>&1
 go test -c -race -o "$work/native/server.test" ./internal/server >"$work/native/server-build.log" 2>&1
 file "$work/native/plugin.test" "$work/native/server.test" >"$work/native/binaries.txt"
 native plugin plugin internal/plugin '^TestV5Contribution'
 native assembly server internal/server '^TestV5ContributionDirectoryAssembly$'
 owned_container="campusos-v12-03c-directory-$(date +%s)-$$"
 cleanup(){ docker rm -f -v "$owned_container" >"$work/cleanup.log" 2>&1; }
 trap cleanup EXIT
 umask 077
 owned_password="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
 docker run -d --name "$owned_container" -e POSTGRES_DB=campusos_v12_03c_directory -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$owned_password" -p 127.0.0.1::5432 postgres:16-alpine >"$work/container-create.log"
 export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$owned_container" DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_NAME=campusos_v12_03c_directory DB_PASSWORD="$owned_password"
 for attempt in $(seq 1 80);do if docker exec "$owned_container" pg_isready -h 127.0.0.1 -U campusos -d "$DB_NAME" >/dev/null 2>&1;then break;fi;sleep 0.5;done
 bash scripts/migrate.sh up >"$work/migration-up.log" 2>&1
 docker exec "$owned_container" psql -U campusos -d "$DB_NAME" -Atc 'SHOW server_version_num' >"$work/pg-version.txt"
 run_pg(){
  local phase="$1" pattern="$2" port
  port="$(docker port "$owned_container" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
  native "$phase" plugin internal/plugin "$pattern" "CAMPUSOS_V5_CONTRIBUTION_DATABASE_URL=postgres://campusos:$owned_password@127.0.0.1:$port/$DB_NAME?sslmode=disable" "CAMPUSOS_V5_CONTRIBUTION_OUTPUT=$work/pg"
 }
 run_pg pg '^TestPostgresV5ContributionDirectory$'
 docker restart "$owned_container" >"$work/postgres-restart.log"
 for attempt in $(seq 1 80);do if docker exec "$owned_container" pg_isready -h 127.0.0.1 -U campusos -d "$DB_NAME" >/dev/null 2>&1;then break;fi;sleep 0.5;done
 run_pg reload '^TestPostgresV5ContributionReload$'
 bash scripts/migrate.sh check >"$work/migration-check.log" 2>&1
elif [[ "$mode" == common ]];then
 python3 scripts/v12-03c-directory-report.py verify "$work"
 : >"$work/common-passed.txt"
 check full-go go test ./... -count=1
 check related-race go test -race ./internal/plugin/... ./internal/server -count=1
 check contracts make contracts-check
 check architecture make architecture-check
 check docs-links make docs-links
 check docs-build make docs-build
 check line-endings python3 scripts/check-line-endings.py --include-untracked
 check diff git diff --check
else echo 'usage: v12-03c-directory-drill.sh target|common' >&2;exit 2;fi
