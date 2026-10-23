#!/usr/bin/env bash
# First06 slice: signed Container reference -> real local Docker image admission.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_CONTAINER_IMAGE_DIR:?set owned absolute evidence directory}"
case "$work" in "$root"/.cache/v12-06-container-image/acceptance.*) ;; *) echo 'requires owned06 image evidence directory' >&2; exit 2;; esac
export GOCACHE="$root/.cache/v12-g0/go-cache" TMPDIR="$work/tmp"
mkdir -p "$TMPDIR" "$work/home" "$work/native"
check(){
 local name="$1"; shift
 if "$@" >"$work/common-$name.log" 2>&1; then echo "$name" >>"$work/common-passed.txt"
 else tail -n 35 "$work/common-$name.log" >&2; return 1; fi
}
native(){
 local name="$1" package="$2" pattern="$3" binary="$4"
 (cd "$package"; env -i PATH="$PATH" HOME="$work/home" TMPDIR="$TMPDIR" GORACE=halt_on_error=1 CAMPUSOS_V5_CONTAINER_TEST_REFERENCE="$CAMPUSOS_V5_CONTAINER_TEST_REFERENCE" CAMPUSOS_V5_CONTAINER_TEST_CONFIG_ID="$CAMPUSOS_V5_CONTAINER_TEST_CONFIG_ID" CAMPUSOS_V5_CONTAINER_TEST_PROOF="$work/actual-image-proof.json" "$work/native/$binary.test" -test.run "$pattern" -test.v -test.count=1 -test.timeout=45s) >"$work/native/$name.log" 2>"$work/native/$name.stderr"
 go tool test2json -t <"$work/native/$name.log" >"$work/native/$name.jsonl"
}
if [[ "$mode" == target ]]; then
 python3 scripts/v12-06-container-image-report.py freeze "$work"
 uname -srm >"$work/environment.txt"
 go version >>"$work/environment.txt"
 docker info --format '{{.OSType}} cgroup={{.CgroupVersion}}' >>"$work/environment.txt"
 mkdir "$work/image"
 printf '%s\n' "owned06-$(basename "$work")" >"$work/image/marker"
 cat >"$work/image/Dockerfile" <<'DOCKERFILE'
FROM scratch
COPY marker /marker
USER 65534:65534
ENTRYPOINT ["/not-executed"]
DOCKERFILE
 tag="localhost/campusos-owned-v12-06-image:$(date +%s)-$$"
 cleanup(){ docker image rm "$tag" >"$work/image-cleanup.log" 2>&1; }
 trap cleanup EXIT
 docker ps --all --quiet --no-trunc | sort >"$work/containers-before.txt"
 docker build --network=none --tag "$tag" --iidfile "$work/image-build-id.txt" "$work/image" >"$work/image-build.log" 2>&1
 docker image inspect --format '{{json .RepoDigests}}' "$tag" >"$work/repo-digests.json"
 docker image inspect --format '{{.Id}}' "$tag" >"$work/config-id.txt"
 export CAMPUSOS_V5_CONTAINER_TEST_REFERENCE="$(python3 -c 'import json,sys; rows=json.load(open(sys.argv[1])); assert len(rows)==1 and rows[0].startswith("localhost/campusos-owned-v12-06-image@sha256:"); print(rows[0])' "$work/repo-digests.json")"
 export CAMPUSOS_V5_CONTAINER_TEST_CONFIG_ID="$(cat "$work/config-id.txt")"
 go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/plugin-build.log" 2>&1
 go test -c -race -o "$work/native/image.test" ./internal/plugin/container >"$work/native/image-build.log" 2>&1
 file "$work/native/plugin.test" "$work/native/image.test" >"$work/native/binaries.txt"
 for binary in plugin image; do go version -m "$work/native/$binary.test" >"$work/native/$binary-build-info.txt"; done
 native capture internal/plugin '^TestV5ContainerProgram' plugin
 native metadata internal/plugin/container '^TestV5ContainerImage' image
 native actual internal/plugin '^TestV5ContainerImageActualDocker$' plugin
 docker ps --all --quiet --no-trunc | sort >"$work/containers-after.txt"
 cmp "$work/containers-before.txt" "$work/containers-after.txt"
 cleanup
 trap - EXIT
 python3 scripts/v12-06-container-image-report.py verify "$work"
elif [[ "$mode" == common ]]; then
 python3 scripts/v12-06-container-image-report.py verify "$work"
 : >"$work/common-passed.txt"
 check full-go go test ./... -count=1
 check affected-race go test -race ./internal/plugin/... ./internal/server -count=1
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
 python3 scripts/v12-06-container-image-report.py verify "$work"
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
 python3 scripts/v12-06-container-image-report.py seal "$work"
else
 echo 'usage: v12-06-container-image-drill.sh target|common|documents|final' >&2; exit 2
fi
