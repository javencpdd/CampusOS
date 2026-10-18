#!/usr/bin/env bash
# One Admin configuration slice, using an owned PG16 and actual built server/UI.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_CONFIG_UI_DIR:?set an absolute owned evidence directory}"
case "$work" in "$root"/.cache/v12-04-config/acceptance.*) ;; *) echo 'requires owned04 config evidence directory' >&2;exit 2;; esac
mkdir -p "$work/native" "$work/tmp"
export GOCACHE="$root/.cache/v12-g0/go-cache" TMPDIR="$work/tmp"
check(){ local name="$1";shift;if "$@" >"$work/common-$name.log" 2>&1;then echo "$name" >>"$work/common-passed.txt";else tail -n 30 "$work/common-$name.log" >&2;return 1;fi; }
if [[ "$mode" == target ]];then
 [[ ! -e "$work/source-freeze.json" ]] || { echo 'use a fresh evidence directory' >&2;exit 2; }
 python3 scripts/v12-04-config-report.py freeze "$work"
 uname -srm >"$work/environment.txt"
 pnpm --dir admin build >"$work/admin-build.log" 2>&1
 go build -race -buildvcs=false -o "$work/native/campusos-server" ./cmd/server >"$work/native/server-build.log" 2>&1
 go test -c -race -o "$work/native/server.test" ./internal/server >"$work/native/fixture-build.log" 2>&1
 file "$work/native/campusos-server" "$work/native/server.test" >"$work/native/binaries.txt"
 owned_container="campusos-v12-04-config-$(date +%s)-$$"
 cleanup(){ docker rm -f -v "$owned_container" >"$work/cleanup.log" 2>&1; }
 trap cleanup EXIT
 umask 077
 owned_password="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
 docker run -d --name "$owned_container" -v "$root:$root:ro" -w "$root" -e POSTGRES_DB=campusos_v12_04_config_ui -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$owned_password" -p 127.0.0.1::5432 postgres:16-alpine >"$work/container-create.log"
 for attempt in $(seq 1 80);do if docker exec "$owned_container" pg_isready -h 127.0.0.1 -U campusos -d campusos_v12_04_config_ui >/dev/null 2>&1;then break;fi;sleep .5;done
 export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$owned_container" DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_NAME=campusos_v12_04_config_ui DB_PASSWORD="$owned_password"
 docker exec "$owned_container" psql -U campusos -d "$DB_NAME" -qAtc 'SHOW server_version_num' >"$work/pg-version.txt"
 bash scripts/migrate.sh up >"$work/migration-up.log" 2>&1
 bash scripts/migrate.sh check >"$work/migration-check.log" 2>&1
 owned_port="$(docker port "$owned_container" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
 (cd internal/server;env -i PATH="$PATH" TMPDIR="$TMPDIR" CAMPUSOS_V5_CONFIG_UI_DATABASE_URL="postgres://campusos:$owned_password@127.0.0.1:$owned_port/$DB_NAME?sslmode=disable" CAMPUSOS_V5_CONFIG_UI_SERVER_BINARY="$work/native/campusos-server" CAMPUSOS_V5_CONFIG_UI_OUTPUT="$work/browser" "$work/native/server.test" -test.run '^TestPluginV5ConfigActualServerBrowser$' -test.v -test.count=1) >"$work/native/target.log" 2>"$work/native/target.stderr"
 go tool test2json -t <"$work/native/target.log" >"$work/native/target.jsonl"
 python3 scripts/v12-04-config-report.py verify "$work"
elif [[ "$mode" == common ]];then
 python3 scripts/v12-04-config-report.py verify "$work"
 : >"$work/common-passed.txt"
 check admin-components pnpm --dir admin test:component
 check admin-build pnpm --dir admin build
 check full-go go test ./... -count=1
 check contracts make contracts-check
 check architecture make architecture-check
 check readme make readme-check
 check docs-links make docs-links
 check docs-build make docs-build
 check line-endings python3 scripts/check-line-endings.py --include-untracked
 check diff git diff --check
elif [[ "$mode" == final ]];then
 : >"$work/final-passed.txt"
 for gate in docs-links docs-build architecture;do
  make_target="$gate"
  if [[ "$gate" == architecture ]];then make_target=architecture-check;fi
  make "$make_target" >"$work/final-$gate.log" 2>&1
  echo "$gate" >>"$work/final-passed.txt"
 done
 python3 scripts/check-line-endings.py --include-untracked >"$work/final-line-endings.log" 2>&1;echo line-endings >>"$work/final-passed.txt"
 git diff --check >"$work/final-diff.log" 2>&1;echo diff >>"$work/final-passed.txt"
else echo 'usage: v12-04-config-drill.sh target|common|final' >&2;exit 2;fi
