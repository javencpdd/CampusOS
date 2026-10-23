#!/usr/bin/env bash
#08a exit: independently signed PDF/headless on one read-only production composition.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_ECOSYSTEM_DIR:?set absolute new owned evidence directory}"
case "$work" in "$root"/.cache/v12-08a-ecosystem/acceptance.*) ;; *) exit 2;; esac
export GOCACHE="$root/.cache/v12-g0/go-cache" GOFLAGS=-buildvcs=false TMPDIR="$work/tmp"
mkdir -p "$work/tmp" "$work/home" "$work/native" "$work/input"
native_path="$(dirname "$(command -v node)"):/usr/bin:/bin"
docker_local(){ env -i PATH=/usr/bin:/bin HOME="$work/home" DOCKER_CONFIG="$work/home" /usr/bin/docker --host=unix:///var/run/docker.sock --config="$work/home" "$@"; }
check(){ local name="$1"; shift; if "$@" >"$work/common-$name.log" 2>&1; then echo "$name" >>"$work/common-passed.txt"; else tail -n 20 "$work/common-$name.log" >&2; return 1; fi; }
migrate_local(){ env -i PATH=/usr/bin:/bin HOME="$work/home" DOCKER_CONFIG="$work/home" DOCKER_HOST=unix:///var/run/docker.sock CAMPUSOS_SKIP_DOTENV=true MIGRATIONS_DIR=migrations PSQL_MODE=docker POSTGRES_CONTAINER="$owned" DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_NAME="${target_database:-campusos_v12_08a_exit}" DB_PASSWORD="$password" /bin/bash scripts/migrate.sh "$@"; }
schema(){ docker_local exec "$owned" pg_dump -U campusos -d campusos_v12_08a_exit --schema-only --no-owner --no-privileges | python3 -c 'import sys;sys.stdout.writelines(line for line in sys.stdin if not line.startswith(("\\restrict ","\\unrestrict ")))' >"$work/schema-$1.sql"; }
if [[ "$mode" == build ]]; then
 python3 scripts/v12-08a-ecosystem-report.py freeze "$work"
 pnpm --dir sdk/typescript build >"$work/sdk-build.log" 2>&1
 pnpm --dir web build >"$work/web-build.log" 2>&1
 pnpm --dir admin build >"$work/admin-build.log" 2>&1
 bash scripts/build-v5-examples.sh >"$work/pdf-build.log" 2>&1
 python3 scripts/v12-08a-ecosystem-report.py source "$work"
 go build -race -o "$work/native/campusos-server" ./cmd/server >"$work/native/server-build.log" 2>&1
 go build -o "$work/native/campusosctl" ./cmd/campusosctl >"$work/native/cli-build.log" 2>&1
 go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/plugin-test-build.log" 2>&1
 go test -c -race -o "$work/native/server.test" ./internal/server >"$work/native/test-build.log" 2>&1
 cp "$work/native/server.test" "$work/native/contract-host"
 cmp "$work/native/server.test" "$work/native/contract-host"
 go version -m "$work/native/campusos-server" >"$work/native/server-build-info.txt"
 go version -m "$work/native/server.test" >"$work/native/test-build-info.txt"
 file "$work/native/campusos-server" "$work/native/server.test" "$work/native/contract-host" "$work/native/campusosctl" "$work/native/plugin.test" >"$work/native/binaries.txt"
 # Keep owner-write bits: the target must prove EROFS from its mount, not EACCES
 # from file modes. The already-built bytes are frozen separately below.
 chmod 0755 "$work/native/campusos-server" "$work/native/server.test" "$work/native/contract-host" "$work/native/campusosctl" "$work/native/plugin.test"
 CAMPUSOS_AUTHOR_GO="${CAMPUSOS_AUTHOR_GO:?explicit local Go1.25 executable required}" bash scripts/build-v5-chat-echo.sh --out "$work/author-build" >"$work/author-build.log" 2>&1
 tag="localhost/campusos-owned-v12-08a-ecosystem:$(date +%s)-$$"
 printf '%s\n' "$tag" >"$work/image-tag.txt"
 docker_local build --pull=false --network=none --tag "$tag" --iidfile "$work/image-id.txt" "$work/author-build/image" >"$work/image-build.log" 2>&1
 docker_local image inspect --format '{{json .RepoDigests}}' "$tag" >"$work/repo-digests.json"
 python3 - "$work" <<'PYTHON'
import json,re,sys,shutil
from pathlib import Path
w=Path(sys.argv[1]); refs=json.loads((w/'repo-digests.json').read_text())
assert len(refs)==1 and re.fullmatch(r'localhost/campusos-owned-v12-08a-ecosystem@sha256:[0-9a-f]{64}',refs[0])
for version in ['1.1.0','1.1.1']:
 dest=w/('echo-'+version);shutil.copytree(w/'author-build/package-template',dest)
 p=dest/'plugin.json';m=json.loads(p.read_text());assert m['backend']['artifact']['image'].endswith('0'*64)
 m['backend']['artifact']['image']=refs[0];m['version']=version;p.write_text(json.dumps(m,indent=2)+'\n')
PYTHON
 node --input-type=module - "$work" <<'JS'
import {generateKeyPairSync} from 'node:crypto';
import {writeFileSync} from 'node:fs';
const work=process.argv[2], {privateKey,publicKey}=generateKeyPairSync('ed25519');
const seed=privateKey.export({format:'der',type:'pkcs8'}).subarray(-32),pub=publicKey.export({format:'der',type:'spki'}).subarray(-32);
writeFileSync(work+'/signing-private.b64',Buffer.concat([seed,pub]).toString('base64'),{mode:0o600});
writeFileSync(work+'/input/trust.json',JSON.stringify({contract:'campusos.plugin-trust/v1',keys:[{publisher:'campusos',key_id:'owned08a-exit',public_key:pub.toString('base64'),status:'active'}]})+'\n');
JS
 trap 'rm -f "$work/signing-private.b64"' EXIT
 for name in pdf-viewer echo-1.1.0 echo-1.1.1; do
  source="$work/$name"; if [[ "$name" == pdf-viewer ]]; then source=plugins/campusos.pdf-viewer; fi
  "$work/native/campusosctl" plugin validate "$source" >"$work/cli-$name-validate.json"
  "$work/native/campusosctl" plugin pack "$source" --out "$work/input/$name.tar.gz" >"$work/cli-$name-pack.json"
  "$work/native/campusosctl" plugin pack "$source" --out "$work/$name-repeat.tar.gz" >"$work/cli-$name-pack-repeat.json"
  cmp "$work/input/$name.tar.gz" "$work/$name-repeat.tar.gz"
  "$work/native/campusosctl" plugin sign "$work/input/$name.tar.gz" --key-id owned08a-exit --key-file "$work/signing-private.b64" --out "$work/input/$name.release.json" >"$work/cli-$name-sign.json"
  "$work/native/campusosctl" plugin verify "$work/input/$name.tar.gz" --release "$work/input/$name.release.json" --trust "$work/input/trust.json" >"$work/cli-$name-verify.json"
 done
 rm "$work/signing-private.b64"; trap - EXIT
 go test -race -json ./sdk/go ./plugins/campusos.chat-echo -count=1 >"$work/native/sdk.jsonl" 2>"$work/native/sdk.stderr"
 env -i PATH="$native_path" HOME="$work/home" TMPDIR="$work/tmp" GORACE=halt_on_error=1 "$work/native/server.test" -test.run '^TestV5ReadOnlyHostRejectsFileModeOnlyEvidence$' -test.v -test.count=1 >"$work/native/unit.log" 2>"$work/native/unit.stderr"
 go tool test2json -t <"$work/native/unit.log" >"$work/native/unit.jsonl"
 python3 scripts/v12-08a-ecosystem-report.py built "$work"
elif [[ "$mode" == target || "$mode" == diagnose ]]; then
 python3 scripts/v12-08a-ecosystem-report.py source "$work"
 [[ -s "$work/production-build-freeze.json" ]]
 uname -srm >"$work/environment.txt"
 bwrap --version >>"$work/environment.txt"
 docker_local info --format '{{.OSType}} cgroup={{.CgroupVersion}}' >>"$work/environment.txt"
 { docker_local ps --all --filter=label=campusos.container.worker=v5 --format '{{.Names}}'; docker_local ps --all --filter=label=campusos.wasm.worker=v5 --format '{{.Names}}'; } | sort >"$work/workers-before.txt"
 owned="campusos-v12-08a-ecosystem-$(date +%s)-$$"
 cleanup(){ docker_local logs "$owned" >"$work/pg-server.log" 2>&1; docker_local rm --force --volumes "$owned" >"$work/pg-cleanup.log" 2>&1; docker_local image rm "$(cat "$work/image-tag.txt")" >"$work/image-cleanup.log" 2>&1; }
 trap cleanup EXIT
 password="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
 docker_local run --pull=never -d --name "$owned" --tmpfs /var/lib/postgresql/data -e POSTGRES_DB=campusos_v12_08a_exit -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$password" -p 127.0.0.1::5432 postgres:16-alpine >"$work/pg-create.log"
 for attempt in $(seq 1 80); do if docker_local exec "$owned" pg_isready -U campusos -d campusos_v12_08a_exit >/dev/null 2>&1; then break; fi; sleep .5; done
 migrate_local up >"$work/migration.log" 2>&1
 docker_local image inspect "$(cat "$work/image-tag.txt")" >"$work/image-before.json"
 schema before
 docker_local exec "$owned" psql -U campusos -d campusos_v12_08a_exit -At -c 'SHOW server_version_num' >"$work/pg-version.txt"
 port="$(docker_local port "$owned" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
 [[ "$port" =~ ^[0-9]+$ ]]
 # Separate database for a deterministic negative lock-order fixture. It never
 # manufactures observed ready or starts a guest; joint execution is below.
 docker_local exec "$owned" createdb -U campusos campusos_v12_08a_lock >"$work/lock-db-create.log" 2>&1
 target_database=campusos_v12_08a_lock migrate_local up >"$work/lock-migration.log" 2>&1
 env -i PATH="$native_path" HOME="$work/home" TMPDIR="$work/tmp" GORACE=halt_on_error=1 CAMPUSOS_V5_LOCK_DATABASE_URL="postgres://campusos:$password@127.0.0.1:$port/campusos_v12_08a_lock?sslmode=disable" CAMPUSOS_V5_LOCK_INPUT="$work/input" CAMPUSOS_V5_LOCK_OUTPUT="$work" "$work/native/plugin.test" -test.run '^TestV5RuntimeSpecLockOrderActualPostgres$' -test.v -test.count=1 -test.timeout=30s >"$work/native/lock-order.log" 2>"$work/native/lock-order.stderr"
 go tool test2json -t <"$work/native/lock-order.log" >"$work/native/lock-order.jsonl"

 # Native API, gateway, static SPA servers and Chrome inherit this mount
 # namespace. Only owned runtime/evidence and private temporary data can write.
 # The native host is the explicitly enabled trusted daemon client. No API
 # container or guest container receives a daemon socket or host bind mount.
 diagnostic=false; if [[ "$mode" == diagnose ]]; then diagnostic=true; fi
 env -i PATH="$native_path" HOME="$work/home" TMPDIR="$work/tmp" GORACE=halt_on_error=1 CAMPUSOS_V5_ECOSYSTEM_DIAGNOSTIC="$diagnostic" CAMPUSOS_V5_READONLY_DATABASE_URL="postgres://campusos:$password@127.0.0.1:$port/campusos_v12_08a_exit?sslmode=disable" CAMPUSOS_V5_READONLY_OUTPUT="$work" CAMPUSOS_V5_READONLY_SERVER_BINARY="$work/native/contract-host" \
 bwrap --die-with-parent --ro-bind / / --proc /proc --dev /dev --tmpfs /tmp --bind "$work" "$work" --ro-bind "$work/native" "$work/native" --ro-bind "$work/input" "$work/input" --cap-drop ALL --chdir "$root/internal/server" -- \
 "$work/native/server.test" -test.run '^TestPluginV5EcosystemReadOnlyHostBrowser$' -test.v -test.count=1 -test.timeout=1500s >"$work/native/actual.log" 2>"$work/native/actual.stderr"
 go tool test2json -t <"$work/native/actual.log" >"$work/native/actual.jsonl"
 schema after
 docker_local image inspect "$(cat "$work/image-tag.txt")" >"$work/image-after.json"
 cmp "$work/image-before.json" "$work/image-after.json"
 cmp "$work/schema-before.sql" "$work/schema-after.sql"
 migrate_local check >"$work/migration-check.log" 2>&1
 docker_local exec "$owned" psql -U campusos -d campusos_v12_08a_exit -At -c 'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1' >"$work/schema-version.txt"
 { docker_local ps --all --filter=label=campusos.container.worker=v5 --format '{{.Names}}'; docker_local ps --all --filter=label=campusos.wasm.worker=v5 --format '{{.Names}}'; } | sort >"$work/workers-after.txt"
 cmp "$work/workers-before.txt" "$work/workers-after.txt"
 trap - EXIT; cleanup
 if [[ "$mode" == target ]]; then python3 scripts/v12-08a-ecosystem-report.py verify "$work"; else echo "diagnostic only; not stage acceptance"; fi
elif [[ "$mode" == common ]]; then
 python3 scripts/v12-08a-ecosystem-report.py verify "$work"
 : >"$work/common-passed.txt"
 check full-go go test ./... -count=1
 check affected-race go test -race -p 1 ./internal/server ./internal/plugin/... ./cmd/campusosctl -count=1
 check contracts make contracts-check
 check architecture make architecture-check
 check runner-syntax bash -n scripts/v12-08a-ecosystem-drill.sh
 check browser-syntax node --check scripts/v12-08a-ecosystem-browser.mjs
 check docs-links make docs-links
 check docs-build make docs-build
 check line-endings python3 scripts/check-line-endings.py --include-untracked
 check diff git diff --check
elif [[ "$mode" == documents ]]; then
 python3 scripts/v12-08a-ecosystem-report.py verify "$work"
 : >"$work/document-passed.txt"
 for name in docs-links docs-build line-endings diff; do
  case "$name" in docs-links) args=(make docs-links);; docs-build) args=(make docs-build);; line-endings) args=(python3 scripts/check-line-endings.py --include-untracked);; diff) args=(git diff --check);; esac
  "${args[@]}" >"$work/document-$name.log" 2>&1
  echo "$name" >>"$work/document-passed.txt"
 done
elif [[ "$mode" == final ]]; then
 python3 scripts/v12-08a-ecosystem-report.py seal "$work"
else exit 2; fi
