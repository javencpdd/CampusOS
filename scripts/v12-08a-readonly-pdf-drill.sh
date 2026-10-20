#!/usr/bin/env bash
#08a PDF slice: already-built native API/SPA on real read-only mounts.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_READONLY_DIR:?set absolute new owned evidence directory}"
case "$work" in "$root"/.cache/v12-08a-readonly-pdf/acceptance.*) ;; *) exit 2;; esac
export GOCACHE="$root/.cache/v12-g0/go-cache" GOFLAGS=-buildvcs=false TMPDIR="$work/tmp"
mkdir -p "$work/tmp" "$work/home" "$work/native" "$work/input"
native_path="$(dirname "$(command -v node)"):/usr/bin:/bin"
docker_local(){ env -i PATH=/usr/bin:/bin HOME="$work/home" DOCKER_CONFIG="$work/home" /usr/bin/docker --host=unix:///var/run/docker.sock --config="$work/home" "$@"; }
check(){ local name="$1"; shift; if "$@" >"$work/common-$name.log" 2>&1; then echo "$name" >>"$work/common-passed.txt"; else tail -n 20 "$work/common-$name.log" >&2; return 1; fi; }
migrate_local(){ env -i PATH=/usr/bin:/bin HOME="$work/home" DOCKER_CONFIG="$work/home" DOCKER_HOST=unix:///var/run/docker.sock CAMPUSOS_SKIP_DOTENV=true MIGRATIONS_DIR=migrations PSQL_MODE=docker POSTGRES_CONTAINER="$owned" DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=campusos DB_NAME=campusos_v12_08a_readonly_pdf DB_PASSWORD="$password" /bin/bash scripts/migrate.sh "$@"; }
schema(){ docker_local exec "$owned" pg_dump -U campusos -d campusos_v12_08a_readonly_pdf --schema-only --no-owner --no-privileges | python3 -c 'import sys;sys.stdout.writelines(line for line in sys.stdin if not line.startswith(("\\restrict ","\\unrestrict ")))' >"$work/schema-$1.sql"; }
if [[ "$mode" == build ]]; then
 python3 scripts/v12-08a-readonly-pdf-report.py freeze "$work"
 pnpm --dir sdk/typescript build >"$work/sdk-build.log" 2>&1
 pnpm --dir web build >"$work/web-build.log" 2>&1
 pnpm --dir admin build >"$work/admin-build.log" 2>&1
 bash scripts/build-v5-examples.sh >"$work/pdf-build.log" 2>&1
 python3 scripts/v12-08a-readonly-pdf-report.py source "$work"
 go build -race -o "$work/native/campusos-server" ./cmd/server >"$work/native/server-build.log" 2>&1
 go build -o "$work/native/campusosctl" ./cmd/campusosctl >"$work/native/cli-build.log" 2>&1
 go test -c -race -o "$work/native/server.test" ./internal/server >"$work/native/test-build.log" 2>&1
 go version -m "$work/native/campusos-server" >"$work/native/server-build-info.txt"
 go version -m "$work/native/server.test" >"$work/native/test-build-info.txt"
 file "$work/native/campusos-server" "$work/native/server.test" "$work/native/campusosctl" >"$work/native/binaries.txt"
 # Keep owner-write bits: the target must prove EROFS from its mount, not EACCES
 # from file modes. The already-built bytes are frozen separately below.
 chmod 0755 "$work/native/campusos-server" "$work/native/server.test" "$work/native/campusosctl"
 node --input-type=module - "$work" <<'JS'
import {generateKeyPairSync} from 'node:crypto';
import {writeFileSync} from 'node:fs';
const work=process.argv[2], {privateKey,publicKey}=generateKeyPairSync('ed25519');
const seed=privateKey.export({format:'der',type:'pkcs8'}).subarray(-32),pub=publicKey.export({format:'der',type:'spki'}).subarray(-32);
writeFileSync(work+'/signing-private.b64',Buffer.concat([seed,pub]).toString('base64'),{mode:0o600});
writeFileSync(work+'/input/trust.json',JSON.stringify({contract:'campusos.plugin-trust/v1',keys:[{publisher:'campusos',key_id:'owned08a-pdf',public_key:pub.toString('base64'),status:'active'}]})+'\n');
JS
 trap 'rm -f "$work/signing-private.b64"' EXIT
 "$work/native/campusosctl" plugin validate plugins/campusos.pdf-viewer >"$work/cli-validate.json"
 "$work/native/campusosctl" plugin pack plugins/campusos.pdf-viewer --out "$work/input/pdf-viewer.tar.gz" >"$work/cli-pack.json"
 "$work/native/campusosctl" plugin pack plugins/campusos.pdf-viewer --out "$work/pdf-repeat.tar.gz" >"$work/cli-pack-repeat.json"
 cmp "$work/input/pdf-viewer.tar.gz" "$work/pdf-repeat.tar.gz"
 "$work/native/campusosctl" plugin sign "$work/input/pdf-viewer.tar.gz" --key-id owned08a-pdf --key-file "$work/signing-private.b64" --out "$work/input/pdf-viewer.release.json" >"$work/cli-sign.json"
 rm "$work/signing-private.b64"; trap - EXIT
 "$work/native/campusosctl" plugin verify "$work/input/pdf-viewer.tar.gz" --release "$work/input/pdf-viewer.release.json" --trust "$work/input/trust.json" >"$work/cli-verify.json"
 env -i PATH="$native_path" HOME="$work/home" TMPDIR="$work/tmp" GORACE=halt_on_error=1 "$work/native/server.test" -test.run '^TestV5ReadOnlyHostRejectsFileModeOnlyEvidence$' -test.v -test.count=1 >"$work/native/unit.log" 2>"$work/native/unit.stderr"
 go tool test2json -t <"$work/native/unit.log" >"$work/native/unit.jsonl"
 python3 scripts/v12-08a-readonly-pdf-report.py built "$work"
elif [[ "$mode" == target ]]; then
 python3 scripts/v12-08a-readonly-pdf-report.py source "$work"
 [[ -s "$work/production-build-freeze.json" ]]
 uname -srm >"$work/environment.txt"
 bwrap --version >>"$work/environment.txt"
 docker_local info --format '{{.OSType}} cgroup={{.CgroupVersion}}' >>"$work/environment.txt"
 { docker_local ps --all --filter=label=campusos.container.worker=v5 --format '{{.Names}}'; docker_local ps --all --filter=label=campusos.wasm.worker=v5 --format '{{.Names}}'; } | sort >"$work/workers-before.txt"
 owned="campusos-v12-08a-readonly-pdf-$(date +%s)-$$"
 cleanup(){ docker_local rm --force --volumes "$owned" >"$work/pg-cleanup.log" 2>&1; }
 trap cleanup EXIT
 password="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
 docker_local run --pull=never -d --name "$owned" --tmpfs /var/lib/postgresql/data -e POSTGRES_DB=campusos_v12_08a_readonly_pdf -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$password" -p 127.0.0.1::5432 postgres:16-alpine >"$work/pg-create.log"
 for attempt in $(seq 1 80); do if docker_local exec "$owned" pg_isready -U campusos -d campusos_v12_08a_readonly_pdf >/dev/null 2>&1; then break; fi; sleep .5; done
 migrate_local up >"$work/migration.log" 2>&1
 schema before
 docker_local exec "$owned" psql -U campusos -d campusos_v12_08a_readonly_pdf -At -c 'SHOW server_version_num' >"$work/pg-version.txt"
 port="$(docker_local port "$owned" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
 [[ "$port" =~ ^[0-9]+$ ]]
 # Native API, gateway, static SPA servers and Chrome inherit this mount
 # namespace. Only owned runtime/evidence and private temporary data can write.
 # No API or guest Docker container receives a daemon socket; this target also
 # masks the local socket from the native UI-only host.
 env -i PATH="$native_path" HOME="$work/home" TMPDIR="$work/tmp" GORACE=halt_on_error=1 CAMPUSOS_V5_READONLY_DATABASE_URL="postgres://campusos:$password@127.0.0.1:$port/campusos_v12_08a_readonly_pdf?sslmode=disable" CAMPUSOS_V5_READONLY_OUTPUT="$work" CAMPUSOS_V5_READONLY_SERVER_BINARY="$work/native/campusos-server" \
 bwrap --die-with-parent --ro-bind / / --proc /proc --dev /dev --tmpfs /tmp --bind "$work" "$work" --ro-bind "$work/native" "$work/native" --ro-bind "$work/input" "$work/input" --ro-bind /dev/null /run/docker.sock --cap-drop ALL --chdir "$root/internal/server" -- \
 "$work/native/server.test" -test.run '^TestPluginV5ReadOnlyHostActualServerBrowser$' -test.v -test.count=1 -test.timeout=1200s >"$work/native/actual.log" 2>"$work/native/actual.stderr"
 go tool test2json -t <"$work/native/actual.log" >"$work/native/actual.jsonl"
 schema after
 cmp "$work/schema-before.sql" "$work/schema-after.sql"
 migrate_local check >"$work/migration-check.log" 2>&1
 docker_local exec "$owned" psql -U campusos -d campusos_v12_08a_readonly_pdf -At -c 'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1' >"$work/schema-version.txt"
 { docker_local ps --all --filter=label=campusos.container.worker=v5 --format '{{.Names}}'; docker_local ps --all --filter=label=campusos.wasm.worker=v5 --format '{{.Names}}'; } | sort >"$work/workers-after.txt"
 cmp "$work/workers-before.txt" "$work/workers-after.txt"
 trap - EXIT; cleanup
 python3 scripts/v12-08a-readonly-pdf-report.py verify "$work"
elif [[ "$mode" == common ]]; then
 python3 scripts/v12-08a-readonly-pdf-report.py verify "$work"
 : >"$work/common-passed.txt"
 check full-go go test ./... -count=1
 check affected-race go test -race ./internal/server ./internal/plugin/... ./cmd/campusosctl -count=1
 check contracts make contracts-check
 check architecture make architecture-check
 check runner-syntax bash -n scripts/v12-08a-readonly-pdf-drill.sh
 check docs-links make docs-links
 check docs-build make docs-build
 check line-endings python3 scripts/check-line-endings.py --include-untracked
 check diff git diff --check
elif [[ "$mode" == documents ]]; then
 python3 scripts/v12-08a-readonly-pdf-report.py verify "$work"
 : >"$work/document-passed.txt"
 for name in docs-links docs-build line-endings diff; do
  case "$name" in docs-links) args=(make docs-links);; docs-build) args=(make docs-build);; line-endings) args=(python3 scripts/check-line-endings.py --include-untracked);; diff) args=(git diff --check);; esac
  "${args[@]}" >"$work/document-$name.log" 2>&1
  echo "$name" >>"$work/document-passed.txt"
 done
elif [[ "$mode" == final ]]; then
 python3 scripts/v12-08a-readonly-pdf-report.py seal "$work"
else exit 2; fi
