#!/usr/bin/env bash
# First08a slice: independently built headless author package, not stage exit.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_AUTHOR_DIR:?set absolute new owned evidence directory}"
case "$work" in "$root"/.cache/v12-08a-author/acceptance.*) ;; *) exit 2;; esac
export GOCACHE="$root/.cache/v12-g0/go-cache" GOFLAGS=-buildvcs=false TMPDIR="$work/tmp"
mkdir -p "$work/tmp" "$work/home" "$work/native"
docker_local(){ env -i PATH=/usr/bin:/bin HOME="$work/home" DOCKER_CONFIG="$work/home" /usr/bin/docker --host=unix:///var/run/docker.sock --config="$work/home" "$@"; }
check(){ local name="$1"; shift; if "$@" >"$work/common-$name.log" 2>&1; then echo "$name" >>"$work/common-passed.txt"; else tail -n 20 "$work/common-$name.log" >&2; return 1; fi; }
migrate_local(){ env -i PATH=/usr/bin:/bin HOME="$work/home" DOCKER_CONFIG="$work/home" DOCKER_HOST=unix:///var/run/docker.sock CAMPUSOS_SKIP_DOTENV=true MIGRATIONS_DIR=migrations PSQL_MODE=docker POSTGRES_CONTAINER="$owned" DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_NAME=campusos_v12_08a_author DB_PASSWORD="$password" /bin/bash scripts/migrate.sh "$@"; }
schema(){ docker_local exec "$owned" pg_dump -U campusos -d campusos_v12_08a_author --schema-only --no-owner --no-privileges | python3 -c 'import sys;sys.stdout.writelines(line for line in sys.stdin if not line.startswith(("\\restrict ","\\unrestrict ")))' >"$work/schema-$1.sql"; }
if [[ "$mode" == build ]]; then
 python3 scripts/v12-08a-author-report.py freeze "$work"
 CAMPUSOS_AUTHOR_GO="${CAMPUSOS_AUTHOR_GO:?explicit local Go1.25 executable required}" bash scripts/build-v5-chat-echo.sh --out "$work/author-build" >"$work/author-build.log" 2>&1
 go build -o "$work/native/campusosctl" ./cmd/campusosctl >"$work/native/cli-build.log" 2>&1
 go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/test-build.log" 2>&1
 chmod 0555 "$work/native/plugin.test" "$work/native/campusosctl"
 go version -m "$work/native/plugin.test" >"$work/native/test-build-info.txt"
 file "$work/native/plugin.test" "$work/native/campusosctl" "$work/author-build/image/provider" >"$work/native/binaries.txt"
 go test -race -json ./sdk/go ./plugins/campusos.chat-echo -count=1 >"$work/native/unit.jsonl" 2>"$work/native/unit.stderr"
elif [[ "$mode" == target ]]; then
 python3 scripts/v12-08a-author-report.py source "$work"
 uname -srm >"$work/environment.txt"
 docker_local info --format '{{.OSType}} cgroup={{.CgroupVersion}}' >>"$work/environment.txt"
 { docker_local ps --all --filter=label=campusos.container.worker=v5 --format '{{.Names}}'; docker_local ps --all --filter=label=campusos.wasm.worker=v5 --format '{{.Names}}'; } | sort >"$work/workers-before.txt"
 suffix="$(date +%s)-$$"
 tag="localhost/campusos-owned-v12-08a-author:$suffix"
 owned="campusos-v12-08a-author-$suffix"
 image_attempted=false; pg_attempted=false
 cleanup(){ local rc=0; rm -f "$work/signing-private.b64"; if [[ "$pg_attempted" == true ]]; then docker_local rm --force --volumes "$owned" >"$work/pg-cleanup.log" 2>&1 || rc=1; fi; if [[ "$image_attempted" == true ]]; then docker_local image rm "$tag" >"$work/image-cleanup.log" 2>&1 || rc=1; fi; return "$rc"; }
 trap cleanup EXIT
 image_attempted=true
 docker_local build --pull=false --network=none --tag "$tag" --iidfile "$work/image-id.txt" "$work/author-build/image" >"$work/image-build.log" 2>&1
 docker_local image inspect --format '{{json .RepoDigests}}' "$tag" >"$work/repo-digests.json"
 python3 - "$work" <<'PY'
import json,re,sys,shutil
from pathlib import Path
w=Path(sys.argv[1]); refs=json.loads((w/'repo-digests.json').read_text())
assert len(refs)==1 and re.fullmatch(r'localhost/campusos-owned-v12-08a-author@sha256:[0-9a-f]{64}',refs[0])
shutil.copytree(w/'author-build/package-template',w/'package')
p=w/'package/plugin.json'; m=json.loads(p.read_text()); assert m['backend']['artifact']['image'].endswith('0'*64)
m['backend']['artifact']['image']=refs[0]; p.write_text(json.dumps(m,indent=2)+'\n')
PY
 node --input-type=module - "$work" <<'JS'
import {generateKeyPairSync} from 'node:crypto';
import {writeFileSync} from 'node:fs';
const work=process.argv[2], {privateKey,publicKey}=generateKeyPairSync('ed25519');
const seed=privateKey.export({format:'der',type:'pkcs8'}).subarray(-32),pub=publicKey.export({format:'der',type:'spki'}).subarray(-32);
writeFileSync(work+'/signing-private.b64',Buffer.concat([seed,pub]).toString('base64'),{mode:0o600});
writeFileSync(work+'/trust.json',JSON.stringify({contract:'campusos.plugin-trust/v1',keys:[{publisher:'campusos',key_id:'owned08a',public_key:pub.toString('base64'),status:'active'}]})+'\n');
JS
 "$work/native/campusosctl" plugin validate "$work/package" >"$work/cli-validate.json"
 "$work/native/campusosctl" plugin pack "$work/package" --out "$work/echo.tar.gz" >"$work/cli-pack.json"
 "$work/native/campusosctl" plugin pack "$work/package" --out "$work/echo-repeat.tar.gz" >"$work/cli-pack-repeat.json"
 cmp "$work/echo.tar.gz" "$work/echo-repeat.tar.gz"
 "$work/native/campusosctl" plugin sign "$work/echo.tar.gz" --key-id owned08a --key-file "$work/signing-private.b64" --out "$work/echo.release.json" >"$work/cli-sign.json"
 rm "$work/signing-private.b64"
 "$work/native/campusosctl" plugin verify "$work/echo.tar.gz" --release "$work/echo.release.json" --trust "$work/trust.json" >"$work/cli-verify.json"
 password="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
 pg_attempted=true
 docker_local run --pull=never -d --name "$owned" --tmpfs /var/lib/postgresql/data -e POSTGRES_DB=campusos_v12_08a_author -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$password" -p 127.0.0.1::5432 postgres:16-alpine >"$work/pg-create.log"
 for attempt in $(seq 1 80); do if docker_local exec "$owned" pg_isready -U campusos -d campusos_v12_08a_author >/dev/null 2>&1; then break; fi; sleep 0.5; done
 migrate_local up >"$work/migration.log" 2>&1
 schema before
 port="$(docker_local port "$owned" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
 [[ "$port" =~ ^[0-9]+$ ]]
 (cd internal/plugin; env -i PATH=/usr/bin:/bin HOME="$work/home" TMPDIR="$work/tmp" GORACE=halt_on_error=1 CAMPUSOS_V5_AUTHOR_DATABASE_URL="postgres://campusos:$password@127.0.0.1:$port/campusos_v12_08a_author?sslmode=disable" CAMPUSOS_V5_AUTHOR_ARTIFACTS="$work" "$work/native/plugin.test" -test.run '^TestV5AuthorContainerActualPostgres$' -test.v -test.count=1 -test.timeout=180s) >"$work/native/actual.log" 2>"$work/native/actual.stderr"
 go tool test2json -t <"$work/native/actual.log" >"$work/native/actual.jsonl"
 schema after
 cmp "$work/schema-before.sql" "$work/schema-after.sql"
 migrate_local check >"$work/migration-check.log" 2>&1
 { docker_local ps --all --filter=label=campusos.container.worker=v5 --format '{{.Names}}'; docker_local ps --all --filter=label=campusos.wasm.worker=v5 --format '{{.Names}}'; } | sort >"$work/workers-after.txt"
 cmp "$work/workers-before.txt" "$work/workers-after.txt"
 trap - EXIT; cleanup
 python3 scripts/v12-08a-author-report.py verify "$work"
elif [[ "$mode" == common ]]; then
 python3 scripts/v12-08a-author-report.py verify "$work"
 : >"$work/common-passed.txt"
 check full-go go test ./... -count=1
 check affected-race go test -race ./sdk/go ./plugins/campusos.chat-echo ./cmd/campusosctl ./internal/plugin/... -count=1
 check contracts make contracts-check
 check architecture make architecture-check
 check author-syntax bash -n scripts/build-v5-chat-echo.sh scripts/build-v5-examples.sh scripts/v12-08a-author-drill.sh
 check docs-links make docs-links
 check docs-build make docs-build
 check line-endings python3 scripts/check-line-endings.py --include-untracked
 check diff git diff --check
elif [[ "$mode" == documents ]]; then
 python3 scripts/v12-08a-author-report.py verify "$work"
 : >"$work/document-passed.txt"
 for name in docs-links docs-build line-endings diff; do
  case "$name" in docs-links) args=(make docs-links);; docs-build) args=(make docs-build);; line-endings) args=(python3 scripts/check-line-endings.py --include-untracked);; diff) args=(git diff --check);; esac
  "${args[@]}" >"$work/document-$name.log" 2>&1
  echo "$name" >>"$work/document-passed.txt"
 done
elif [[ "$mode" == final ]]; then
 python3 scripts/v12-08a-author-report.py seal "$work"
else exit 2; fi
