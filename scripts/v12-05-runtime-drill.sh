#!/usr/bin/env bash
# Full05: fixed host-built Wasm helper, owned PG, actual cmd/server, no shared reset.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_RUNTIME_DIR:?set absolute owned evidence directory}"
case "$work" in "$root"/.cache/v12-05-runtime/acceptance.*) ;; *) echo 'requires owned05 runtime evidence directory' >&2; exit 2;; esac
export GOCACHE="$root/.cache/v12-g0/go-cache" TMPDIR="$work/tmp"
mkdir -p "$TMPDIR" "$work/home" "$work/native"
check(){
 local name="$1"; shift
 if "$@" >"$work/common-$name.log" 2>&1; then echo "$name" >>"$work/common-passed.txt"
 else tail -n 35 "$work/common-$name.log" >&2; return 1; fi
}
native(){
 local name="$1" package="$2" pattern="$3" binary="$4"
 (cd "$package"; env -i PATH="$PATH" HOME="$work/home" TMPDIR="$TMPDIR" GORACE=halt_on_error=1 CAMPUSOS_V5_WASM_DATABASE_URL="$CAMPUSOS_V5_WASM_DATABASE_URL" CAMPUSOS_WASM_WORKER_TEST_IMAGE="$CAMPUSOS_WASM_WORKER_TEST_IMAGE" CAMPUSOS_V5_WASM_SERVER_BINARY="$work/native/server" "$work/native/$binary.test" -test.run "$pattern" -test.v -test.count=1 -test.timeout=240s) >"$work/native/$name.log" 2>"$work/native/$name.stderr"
 go tool test2json -t <"$work/native/$name.log" >"$work/native/$name.jsonl"
}
if [[ "$mode" == target ]]; then
 python3 scripts/v12-05-runtime-report.py freeze "$work"
 uname -srm >"$work/environment.txt"
 go version >>"$work/environment.txt"
 docker info --format '{{.OSType}} cgroup={{.CgroupVersion}} driver={{.CgroupDriver}}' >>"$work/environment.txt"
 mkdir "$work/image"
 CGO_ENABLED=0 GOFLAGS=-buildvcs=false go build -o "$work/image/campusos-wasm-worker" ./cmd/campusos-wasm-worker >"$work/worker-build.log" 2>&1
 chmod 0755 "$work/image/campusos-wasm-worker"
 cp deploy/docker/Dockerfile.wasm-worker "$work/image/Dockerfile"
 docker build --network=none --iidfile "$work/image-id.txt" "$work/image" >"$work/image-build.log" 2>&1
 export CAMPUSOS_WASM_WORKER_TEST_IMAGE="$(cat "$work/image-id.txt")"
 go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/plugin-build.log" 2>&1
 go test -c -race -o "$work/native/wasm.test" ./internal/plugin/wasm >"$work/native/wasm-build.log" 2>&1
 go build -race -o "$work/native/server" ./cmd/server >"$work/native/server-build.log" 2>&1
 file "$work/native/plugin.test" "$work/native/wasm.test" "$work/native/server" "$work/image/campusos-wasm-worker" >"$work/native/binaries.txt"
 for binary in plugin.test wasm.test server; do go version -m "$work/native/$binary" >"$work/native/$binary-build-info.txt"; done
 owned="campusos-v12-05-runtime-$(date +%s)-$$"
 cleanup(){
  docker rm -f -v "$owned" >"$work/cleanup.log" 2>&1
 }
 trap cleanup EXIT
 password="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
 docker run -d --name "$owned" --tmpfs /var/lib/postgresql/data -e POSTGRES_DB=campusos_v12_05_wasm -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$password" -p 127.0.0.1::5432 postgres:16-alpine >"$work/container-create.log"
 for attempt in $(seq 1 80); do if docker exec "$owned" pg_isready -U campusos -d campusos_v12_05_wasm >/dev/null 2>&1; then break; fi; sleep 0.5; done
 export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$owned" DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_NAME=campusos_v12_05_wasm DB_PASSWORD="$password"
 bash scripts/migrate.sh up >"$work/migration.log" 2>&1
 port="$(docker port "$owned" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
 export CAMPUSOS_V5_WASM_DATABASE_URL="postgres://campusos:$password@127.0.0.1:$port/campusos_v12_05_wasm?sslmode=disable"
 native capture internal/plugin '^TestV5WasmProgram' plugin
 native coordinator internal/plugin '^TestV5WasmCoordinator' plugin
 native sandbox internal/plugin/wasm '^TestV5(Sandbox|WasmTypedHost|Worker|NativeWorker)' wasm
 native actual internal/plugin '^TestV5Wasm(RuntimeActualPostgres|ActualServer)$' plugin
 bash scripts/migrate.sh check >"$work/migration-check.log" 2>&1
 docker exec "$owned" psql -U campusos -d campusos_v12_05_wasm -At -c 'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1' >"$work/schema-version.txt"
 make database-check >"$work/database-check.log" 2>&1
 cleanup
 trap - EXIT
 python3 scripts/v12-05-runtime-report.py verify "$work"
elif [[ "$mode" == common ]]; then
 python3 scripts/v12-05-runtime-report.py verify "$work"
 : >"$work/common-passed.txt"
 check full-go go test ./... -count=1
 check affected-race go test -race ./internal/plugin/... ./internal/server ./pkg/config -count=1
 check contracts make contracts-check
 check architecture make architecture-check
 check docker-dev make docker-dev-test
 check docker-deploy make docker-deploy-check
 check readme make readme-check
 check docs-links make docs-links
 check docs-build make docs-build
 check line-endings python3 scripts/check-line-endings.py --include-untracked
 check diff git diff --check
elif [[ "$mode" == documents ]]; then
 python3 scripts/v12-05-runtime-report.py verify "$work"
 : >"$work/final-document-passed.txt"
 document(){
  local name="$1"; shift
  if "$@" >"$work/final-document-$name.log" 2>&1; then echo "$name" >>"$work/final-document-passed.txt"
  else tail -n 35 "$work/final-document-$name.log" >&2; return 1; fi
 }
 document docs-links make docs-links
 document docs-build make docs-build
 document architecture make architecture-check
 document line-endings python3 scripts/check-line-endings.py --include-untracked
 document diff git diff --check
elif [[ "$mode" == final ]]; then
 python3 scripts/v12-05-runtime-report.py seal "$work"
else
 echo 'usage: v12-05-runtime-drill.sh target|common|documents|final' >&2; exit 2
fi
