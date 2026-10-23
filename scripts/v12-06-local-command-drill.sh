#!/usr/bin/env bash
# Local CLI contract + real Linux Container regression, not Windows acceptance.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_LOCAL_COMMAND_DIR:?set owned absolute evidence directory}"
case "$work" in "$root"/.cache/v12-06-local-command/acceptance.*) ;; *) echo 'requires owned local-command evidence directory' >&2; exit 2;; esac
export GOCACHE="$root/.cache/v12-g0/go-cache" TMPDIR="$work/tmp"
mkdir -p "$TMPDIR" "$work/home" "$work/native"
native(){
 local name="$1" package="$2" pattern="$3" binary="$4"
 (cd "$package"; env -i PATH=/usr/bin:/bin HOME="$work/home" TMPDIR="$TMPDIR" GORACE=halt_on_error=1 CAMPUSOS_V5_CONTAINER_RUNTIME_REFERENCE="$reference" CAMPUSOS_V5_CONTAINER_TEST_REFERENCE="$reference" CAMPUSOS_V5_CONTAINER_TEST_CONFIG_ID="$config_id" CAMPUSOS_V5_CONTAINER_TEST_PROOF="$work/image-proof.json" "$work/native/$binary.test" -test.run "$pattern" -test.v -test.count=1 -test.timeout=150s) >"$work/native/$name.log" 2>"$work/native/$name.stderr"
 go tool test2json -t <"$work/native/$name.log" >"$work/native/$name.jsonl"
}
if [[ "$mode" == target ]]; then
 python3 scripts/v12-06-local-command-report.py freeze "$work"
 uname -srm >"$work/environment.txt"
 go version >>"$work/environment.txt"
 /usr/bin/docker --host=unix:///var/run/docker.sock info --format '{{.OSType}} cgroup={{.CgroupVersion}}' >>"$work/environment.txt"
 mkdir "$work/image"
 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS=-buildvcs=false go build -o "$work/image/fixture" ./internal/plugin/containerfixture >"$work/fixture-build.log" 2>&1
 cat >"$work/image/Dockerfile" <<'DOCKERFILE'
FROM scratch
COPY fixture /fixture
USER 65534:65534
ENTRYPOINT ["/fixture"]
DOCKERFILE
 tag="localhost/campusos-owned-v12-06-runtime:local-command-$(date +%s)-$$"
 cleanup(){ /usr/bin/docker --host=unix:///var/run/docker.sock image rm "$tag" >"$work/image-cleanup.log" 2>&1 || true; }
 trap cleanup EXIT
 /usr/bin/docker --host=unix:///var/run/docker.sock build --network=none --tag "$tag" "$work/image" >"$work/image-build.log" 2>&1
 /usr/bin/docker --host=unix:///var/run/docker.sock image inspect --format '{{.Id}}' "$tag" >"$work/config-id.txt"
 /usr/bin/docker --host=unix:///var/run/docker.sock image inspect --format '{{json .RepoDigests}}' "$tag" >"$work/repo-digests.json"
 config_id="$(cat "$work/config-id.txt")"
 reference="$(python3 -c 'import json,sys; rows=json.load(open(sys.argv[1])); assert len(rows)==1 and rows[0].startswith("localhost/campusos-owned-v12-06-runtime@sha256:"); print(rows[0])' "$work/repo-digests.json")"
 go test -c -race -o "$work/native/container.test" ./internal/plugin/container >"$work/native/container-build.log" 2>&1
 go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/plugin-build.log" 2>&1
 for binary in container plugin; do go version -m "$work/native/$binary.test" >"$work/native/$binary-build-info.txt"; done
 file "$work/native/container.test" "$work/native/plugin.test" "$work/image/fixture" >"$work/native/binaries.txt"
 GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOFLAGS=-buildvcs=false go test -c -o "$work/native/container-windows.test.exe" ./internal/plugin/container >"$work/native/windows-test-build.log" 2>&1
 GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOFLAGS=-buildvcs=false go build -o "$work/native/server-windows.exe" ./cmd/server >"$work/native/windows-server-build.log" 2>&1
 for binary in container-windows.test.exe server-windows.exe; do go version -m "$work/native/$binary" >"$work/native/$binary-build-info.txt"; done
 file "$work/native/container-windows.test.exe" "$work/native/server-windows.exe" >"$work/native/windows-compile-only.txt"
 native contract internal/plugin/container '^TestV5ContainerLocal' container
 native metadata internal/plugin/container '^TestV5ContainerImage' container
 native capture internal/plugin '^TestV5ContainerProgram' plugin
 native image internal/plugin '^TestV5ContainerImageActualDocker$' plugin
 native execution internal/plugin '^TestV5ContainerNativeIsolation$' plugin
 cleanup
 trap - EXIT
 python3 scripts/v12-06-local-command-report.py verify "$work"
elif [[ "$mode" == common || "$mode" == documents ]]; then
 python3 scripts/v12-06-local-command-report.py verify "$work"
 list="$work/common-passed.txt"
 prefix=common
 if [[ "$mode" == documents ]]; then list="$work/final-document-passed.txt"; prefix=final-document; fi
 : >"$list"
 check(){
  local name="$1"; shift
  if "$@" >"$work/$prefix-$name.log" 2>&1; then echo "$name" >>"$list"
  else tail -n 30 "$work/$prefix-$name.log" >&2; return 1; fi
 }
 if [[ "$mode" == common ]]; then
  check full-go go test ./... -count=1
  check affected-race go test -race ./internal/plugin/... ./internal/server ./pkg/config -count=1
  check architecture make architecture-check
  check docker-dev make docker-dev-test
  check docker-deploy make docker-deploy-check
 fi
 check docs-links make docs-links
 check docs-build make docs-build
 if [[ "$mode" == documents ]]; then check architecture make architecture-check; fi
 check line-endings python3 scripts/check-line-endings.py --include-untracked
 check diff git diff --check
elif [[ "$mode" == final ]]; then
 python3 scripts/v12-06-local-command-report.py seal "$work"
else
 echo 'usage: v12-06-local-command-drill.sh target|common|documents|final' >&2; exit 2
fi
