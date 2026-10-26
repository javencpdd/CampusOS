#!/usr/bin/env bash
# V12-06: signed arbitrary Container image, owned PG, actual cmd/server, no shared reset.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_CONTAINER_RUNTIME_DIR:?set absolute owned evidence directory}"
case "$work" in "$root"/.cache/v12-06-runtime/acceptance.*) ;; *) echo 'requires owned06 runtime evidence directory' >&2; exit 2;; esac
export GOCACHE="$root/.cache/v12-g0/go-cache" TMPDIR="$work/tmp"
mkdir -p "$TMPDIR" "$work/home" "$work/native"
check(){
 local name="$1"; shift
 if "$@" >"$work/common-$name.log" 2>&1; then echo "$name" >>"$work/common-passed.txt"
 else tail -n 35 "$work/common-$name.log" >&2; return 1; fi
}
native(){
 local name="$1" package="$2" pattern="$3" binary="$4"
 (cd "$package"; env -i PATH="$PATH" HOME="$work/home" TMPDIR="$TMPDIR" GORACE=halt_on_error=1 CAMPUSOS_WASM_WORKER_TEST_IMAGE="$CAMPUSOS_WASM_WORKER_TEST_IMAGE" CAMPUSOS_V5_CONTAINER_DATABASE_URL="$CAMPUSOS_V5_CONTAINER_DATABASE_URL" CAMPUSOS_V5_CONTAINER_RUNTIME_REFERENCE="$CAMPUSOS_V5_CONTAINER_RUNTIME_REFERENCE" CAMPUSOS_V5_CONTAINER_SERVER_BINARY="$work/native/server" CAMPUSOS_V5_CONTAINER_SERVER_PROOF_DIR="$work/server" "$work/native/$binary.test" -test.run "$pattern" -test.v -test.count=1 -test.timeout=240s) >"$work/native/$name.log" 2>"$work/native/$name.stderr"
 go tool test2json -t <"$work/native/$name.log" >"$work/native/$name.jsonl"
}
if [[ "$mode" == target ]]; then
 if [[ "${CAMPUSOS_V5_CONTAINER_DIRECTED:-false}" != true ]]; then python3 scripts/v12-06-runtime-report.py freeze "$work"; fi
 uname -srm >"$work/environment.txt"
 go version >>"$work/environment.txt"
 docker info --format '{{.OSType}} cgroup={{.CgroupVersion}} driver={{.CgroupDriver}}' >>"$work/environment.txt"
 mkdir "$work/image"
 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS=-buildvcs=false go build -o "$work/image/fixture" ./internal/plugin/containerfixture >"$work/worker-build.log" 2>&1
 chmod 0755 "$work/image/fixture"
 cat >"$work/image/Dockerfile" <<'DOCKERFILE'
FROM scratch
COPY fixture /fixture
USER 65534:65534
ENTRYPOINT ["/fixture"]
DOCKERFILE
 tag="localhost/campusos-owned-v12-06-runtime:$(date +%s)-$$"
 owned="campusos-v12-06-runtime-$(date +%s)-$$"
 cleanup(){
  docker rm -f -v "$owned" >"$work/cleanup.log" 2>&1 || true
  docker image rm "$tag" >"$work/image-cleanup.log" 2>&1 || true
  docker image rm "$wasm_tag" >"$work/wasm-image-cleanup.log" 2>&1 || true
 }
 wasm_tag="localhost/campusos-owned-v12-06-wasm:$(date +%s)-$$"
 trap cleanup EXIT
 docker build --network=none --tag "$tag" --iidfile "$work/image-id.txt" "$work/image" >"$work/image-build.log" 2>&1
 docker image inspect --format '{{json .RepoDigests}}' "$tag" >"$work/repo-digests.json"
 export CAMPUSOS_V5_CONTAINER_RUNTIME_REFERENCE="$(python3 -c 'import json,sys;rows=json.load(open(sys.argv[1]));assert len(rows)==1 and rows[0].startswith("localhost/campusos-owned-v12-06-runtime@sha256:");print(rows[0])' "$work/repo-digests.json")"
 mkdir "$work/wasm-image"
 CGO_ENABLED=0 GOFLAGS=-buildvcs=false go build -o "$work/wasm-image/campusos-wasm-worker" ./cmd/campusos-wasm-worker >"$work/wasm-worker-build.log" 2>&1
 cp deploy/docker/Dockerfile.wasm-worker "$work/wasm-image/Dockerfile"
 docker build --network=none --tag "$wasm_tag" --iidfile "$work/wasm-image-id.txt" "$work/wasm-image" >"$work/wasm-image-build.log" 2>&1
 export CAMPUSOS_WASM_WORKER_TEST_IMAGE="$(cat "$work/wasm-image-id.txt")"
 go test -c -race -o "$work/native/wasm.test" ./internal/plugin/wasm >"$work/native/wasm-build.log" 2>&1
 go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/plugin-build.log" 2>&1
 go test -c -race -o "$work/native/wire.test" ./internal/plugin/containerwire >"$work/native/wire-build.log" 2>&1
 go test -c -race -o "$work/native/image.test" ./internal/plugin/container >"$work/native/image-build.log" 2>&1
 go build -race -o "$work/native/server" ./cmd/server >"$work/native/server-build.log" 2>&1
 file "$work/native/plugin.test" "$work/native/wire.test" "$work/native/image.test" "$work/native/wasm.test" "$work/native/server" "$work/image/fixture" "$work/wasm-image/campusos-wasm-worker" >"$work/native/binaries.txt"
 for binary in plugin.test wire.test image.test wasm.test server; do go version -m "$work/native/$binary" >"$work/native/$binary-build-info.txt"; done
 password="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
 docker run -d --name "$owned" --tmpfs /var/lib/postgresql/data -e POSTGRES_DB=campusos_v12_06_container -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$password" -p 127.0.0.1::5432 postgres:16-alpine >"$work/container-create.log"
 for attempt in $(seq 1 80); do if docker exec "$owned" pg_isready -U campusos -d campusos_v12_06_container >/dev/null 2>&1; then break; fi; sleep 0.5; done
 export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=docker POSTGRES_CONTAINER="$owned" DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_NAME=campusos_v12_06_container DB_PASSWORD="$password"
 bash scripts/migrate.sh up >"$work/migration.log" 2>&1
 port="$(docker port "$owned" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
 export CAMPUSOS_V5_CONTAINER_DATABASE_URL="postgres://campusos:$password@127.0.0.1:$port/campusos_v12_06_container?sslmode=disable"
 native capture internal/plugin '^TestV5ContainerProgram' plugin
 native metadata internal/plugin/container '^TestV5ContainerImage' image
 native coordinator internal/plugin '^TestV5RuntimeRouter' plugin
 native wire internal/plugin/containerwire '^TestV5ContainerWire' wire
 native native internal/plugin '^TestV5ContainerNativeIsolation$' plugin
 native wasm-regression internal/plugin/wasm '^TestV5NativeWorker' wasm
 native actual internal/plugin '^TestV5Container(RuntimeActualPostgres|ActualServer)$' plugin
 bash scripts/migrate.sh check >"$work/migration-check.log" 2>&1
 docker exec "$owned" psql -U campusos -d campusos_v12_06_container -At -c 'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1' >"$work/schema-version.txt"
 make database-check >"$work/database-check.log" 2>&1
 cleanup
 trap - EXIT
 if [[ "${CAMPUSOS_V5_CONTAINER_DIRECTED:-false}" != true ]]; then python3 scripts/v12-06-runtime-report.py verify "$work"; fi
elif [[ "$mode" == common ]]; then
 if [[ "${CAMPUSOS_V5_CONTAINER_DIRECTED:-false}" != true ]]; then python3 scripts/v12-06-runtime-report.py verify "$work"; fi
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
 if [[ "${CAMPUSOS_V5_CONTAINER_DIRECTED:-false}" != true ]]; then python3 scripts/v12-06-runtime-report.py verify "$work"; fi
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
 python3 scripts/v12-06-runtime-report.py seal "$work"
else
 echo 'usage: v12-06-runtime-drill.sh target|common|documents|final' >&2; exit 2
fi
