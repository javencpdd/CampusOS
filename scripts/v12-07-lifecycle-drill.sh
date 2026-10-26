#!/usr/bin/env bash
# V12-07 Linux exit: actual upgrade/rollback, owner snapshots and uninstall.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_UPGRADE_DIR:?set an absolute owned evidence directory}"
case "$work" in "$root"/.cache/v12-07-lifecycle/acceptance.*) ;; *) echo 'requires owned07 evidence directory' >&2; exit 2;; esac
export GOCACHE="$root/.cache/v12-g0/go-cache" GOFLAGS=-buildvcs=false TMPDIR="$work/tmp"
mkdir -p "$work/tmp" "$work/home" "$work/native"
docker_local(){ env -i PATH=/usr/bin:/bin HOME="$work/home" DOCKER_CONFIG="$work/home" /usr/bin/docker --host=unix:///var/run/docker.sock --config="$work/home" "$@"; }
migrate_local(){
 env -i PATH=/usr/bin:/bin HOME="$work/home" DOCKER_CONFIG="$work/home" DOCKER_HOST=unix:///var/run/docker.sock CAMPUSOS_SKIP_DOTENV=true MIGRATIONS_DIR=migrations PSQL_MODE=docker POSTGRES_CONTAINER="$owned" DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_NAME=campusos_v12_07_upgrade DB_PASSWORD="$password" /bin/bash scripts/migrate.sh "$@"
}
check(){
 local name="$1"; shift
 if "$@" >"$work/common-$name.log" 2>&1; then echo "$name" >>"$work/common-passed.txt"
 else tail -n 25 "$work/common-$name.log" >&2; return 1; fi
}
native(){
 local name="$1" pattern="$2"
 (cd internal/plugin; env -i PATH=/usr/bin:/bin HOME="$work/home" TMPDIR="$TMPDIR" GORACE=halt_on_error=1 CAMPUSOS_V5_UPGRADE_DATABASE_URL="${CAMPUSOS_V5_UPGRADE_DATABASE_URL:-}" CAMPUSOS_V5_UPGRADE_REFERENCE="${CAMPUSOS_V5_UPGRADE_REFERENCE:-}" CAMPUSOS_V5_LIFECYCLE_DATABASE_URL="${CAMPUSOS_V5_UPGRADE_DATABASE_URL:-}" CAMPUSOS_V5_LIFECYCLE_REFERENCE="${CAMPUSOS_V5_UPGRADE_REFERENCE:-}" CAMPUSOS_WASM_WORKER_TEST_IMAGE="${CAMPUSOS_WASM_WORKER_TEST_IMAGE:-}" "$work/native/plugin.test" -test.run "$pattern" -test.v -test.count=1 -test.timeout=600s) >"$work/native/$name.log" 2>"$work/native/$name.stderr"
 go tool test2json -t <"$work/native/$name.log" >"$work/native/$name.jsonl"
}
if [[ "$mode" == build ]]; then
 python3 scripts/v12-07-lifecycle-report.py freeze "$work"
 mkdir "$work/image"
 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$work/image/fixture" ./internal/plugin/containerfixture >"$work/fixture-build.log" 2>&1
 chmod 0755 "$work/image/fixture"
 cat >"$work/image/Dockerfile" <<'DOCKERFILE'
FROM scratch
COPY fixture /fixture
USER 65534:65534
ENTRYPOINT ["/fixture"]
DOCKERFILE
 mkdir "$work/wasm-image"
 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$work/wasm-image/campusos-wasm-worker" ./cmd/campusos-wasm-worker >"$work/wasm-worker-build.log" 2>&1
 cp deploy/docker/Dockerfile.wasm-worker "$work/wasm-image/Dockerfile"
 go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/plugin-build.log" 2>&1
 go version -m "$work/native/plugin.test" >"$work/native/plugin-build-info.txt"
 file "$work/native/plugin.test" "$work/image/fixture" "$work/wasm-image/campusos-wasm-worker" >"$work/native/binaries.txt"
 native unit '^TestV5(Checkpoint.*|UIOnlyUpgradeActivatesAssetsWithoutRunner|Upgrade(StoppedRequiresExplicitReadiness|StopPathsDoNotPromoteCandidate|DisableCancelsPendingVersion|PrepareSwitchAndVerifiableRollback)|VersionOrdering)$'
elif [[ "$mode" == target ]]; then
 python3 scripts/v12-07-lifecycle-report.py source "$work"
 [[ -s "$work/native/plugin.test" && -s "$work/native/unit.jsonl" ]]
 uname -srm >"$work/environment.txt"
 go version >>"$work/environment.txt"
 docker_local info --format '{{.OSType}} cgroup={{.CgroupVersion}} driver={{.CgroupDriver}}' >>"$work/environment.txt"
 { docker_local ps --all --filter=label=campusos.container.worker=v5 --format '{{.Names}}'; docker_local ps --all --filter=label=campusos.wasm.worker=v5 --format '{{.Names}}'; } | sort >"$work/workers-before.txt"
 suffix="$(date +%s)-$$"
 tag="localhost/campusos-owned-v12-07-upgrade:$suffix"
 wasm_tag="localhost/campusos-owned-v12-07-lifecycle-wasm:$suffix"
 owned="campusos-v12-07-upgrade-$suffix"
 image_attempted=false
 wasm_attempted=false
 pg_attempted=false
 cleanup(){
  local rc=0
  if [[ "$pg_attempted" == true ]]; then docker_local rm --force --volumes "$owned" >"$work/pg-cleanup.log" 2>&1 || rc=1; fi
  if [[ "$image_attempted" == true ]]; then docker_local image rm "$tag" >"$work/image-cleanup.log" 2>&1 || rc=1; fi
  if [[ "$wasm_attempted" == true ]]; then docker_local image rm "$wasm_tag" >"$work/wasm-image-cleanup.log" 2>&1 || rc=1; fi
  return "$rc"
 }
 trap cleanup EXIT
 image_attempted=true
 docker_local build --pull=false --network=none --tag "$tag" --iidfile "$work/image-id.txt" "$work/image" >"$work/image-build.log" 2>&1
 docker_local image inspect --format '{{json .RepoDigests}}' "$tag" >"$work/repo-digests.json"
 export CAMPUSOS_V5_UPGRADE_REFERENCE="$(python3 -c 'import json,re,sys;rows=json.load(open(sys.argv[1]));assert len(rows)==1 and re.fullmatch(r"localhost/campusos-owned-v12-07-upgrade@sha256:[0-9a-f]{64}",rows[0]);print(rows[0])' "$work/repo-digests.json")"
 wasm_attempted=true
 docker_local build --pull=false --network=none --tag "$wasm_tag" --iidfile "$work/wasm-image-id.txt" "$work/wasm-image" >"$work/wasm-image-build.log" 2>&1
 export CAMPUSOS_WASM_WORKER_TEST_IMAGE="$(docker_local image inspect --format '{{.Id}}' "$wasm_tag")"
 password="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
 pg_attempted=true
 docker_local run --pull=never -d --name "$owned" --tmpfs /var/lib/postgresql/data -e POSTGRES_DB=campusos_v12_07_upgrade -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$password" -p 127.0.0.1::5432 postgres:16-alpine >"$work/pg-create.log"
 for attempt in $(seq 1 80); do if docker_local exec "$owned" pg_isready -U campusos -d campusos_v12_07_upgrade >/dev/null 2>&1; then break; fi; sleep 0.5; done
 migrate_local up >"$work/migration.log" 2>&1
 port="$(docker_local port "$owned" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
 [[ "$port" =~ ^[0-9]+$ ]]
 export CAMPUSOS_V5_UPGRADE_DATABASE_URL="postgres://campusos:$password@127.0.0.1:$port/campusos_v12_07_upgrade?sslmode=disable"
 native actual '^TestV5(LifecycleActualPostgres|UIOnlyLifecycleActualPostgres|UpgradeCancelActualPostgres)$'
 migrate_local check >"$work/migration-check.log" 2>&1
 docker_local exec "$owned" psql -U campusos -d campusos_v12_07_upgrade -At -c 'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1' >"$work/schema-version.txt"
 { docker_local ps --all --filter=label=campusos.container.worker=v5 --format '{{.Names}}'; docker_local ps --all --filter=label=campusos.wasm.worker=v5 --format '{{.Names}}'; } | sort >"$work/workers-after.txt"
 cmp "$work/workers-before.txt" "$work/workers-after.txt"
 trap - EXIT
 cleanup
 python3 scripts/v12-07-lifecycle-report.py verify "$work"
elif [[ "$mode" == common ]]; then
 python3 scripts/v12-07-lifecycle-report.py verify "$work"
 : >"$work/common-passed.txt"
 check full-go go test ./... -count=1
 check affected-race go test -race ./internal/plugin/... ./internal/server -count=1
 check contracts make contracts-check
 check architecture make architecture-check
 check admin-build /bin/bash -c 'cd admin && pnpm build'
 check web-directed /bin/bash -c 'cd web && pnpm test:component tests/plugin-v5-center.spec.ts'
 check admin-directed /bin/bash -c 'cd admin && pnpm test:component tests/plugin-v5-management.spec.ts'
 check web-format /bin/bash -c 'cd web && pnpm format:check'
 check web-build /bin/bash -c 'cd web && pnpm build'
 check docs-links make docs-links
 check docs-build make docs-build
 check line-endings python3 scripts/check-line-endings.py --include-untracked
 check diff git diff --check
elif [[ "$mode" == documents ]]; then
 python3 scripts/v12-07-lifecycle-report.py verify "$work"
 : >"$work/document-passed.txt"
 for name in docs-links docs-build line-endings diff; do
  case "$name" in
   docs-links) args=(make docs-links);;
   docs-build) args=(make docs-build);;
   line-endings) args=(python3 scripts/check-line-endings.py --include-untracked);;
   diff) args=(git diff --check);;
  esac
  "${args[@]}" >"$work/document-$name.log" 2>&1
  echo "$name" >>"$work/document-passed.txt"
 done
elif [[ "$mode" == final ]]; then
 python3 scripts/v12-07-lifecycle-report.py seal "$work"
else
 echo 'usage: build|target|common|documents|final' >&2; exit 2
fi
