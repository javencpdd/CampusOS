#!/usr/bin/env bash
# Owned PG16 target for the Integration phase. Never uses the developer DB.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
state="${CAMPUSOS_INTEGRATION_DRILL_DIR:-$root/.cache/v12-02c-completion}"
mkdir -p "$state"
mkdir -p "$state/tmp"
export TMPDIR="$state/tmp"
mode="${1:-prepare}"
if [[ "$mode" == all ]]; then
  if [[ -z "${CAMPUSOS_INTEGRATION_DRILL_DIR:-}" ]]; then state="$(mktemp -d "$root/.cache/v12-02c-completion/acceptance.XXXXXX")"; fi
  export CAMPUSOS_INTEGRATION_DRILL_DIR="$state"
  echo "Integration phase acceptance: $state"
  python3 scripts/v12-02c-exit-report.py --freeze "$state"
  bash "$0" prepare
  trap 'bash "$0" stop' EXIT
  bash "$0" migrations
  bash "$0" native
  GOCACHE="$root/.cache/v12-g0/go-cache" go build -o "$state/campusos-server" ./cmd/server >"$state/server-build.log" 2>&1
  pnpm --dir admin build >"$state/admin-build.log" 2>&1
  bash "$0" browser
  bash "$0" common
  python3 scripts/v12-02c-exit-report.py --build "$state"
  echo "Implementation, target and common checks passed: $state"
  exit 0
fi
if [[ "$mode" == prepare ]]; then
  [[ ! -f "$state/private.env" ]] || { echo 'target already prepared'; exit 1; }
  target="campusos-v12-02c-$(date +%s)-$$"
  secret="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
  docker run --rm -d --name "$target" --tmpfs /var/lib/postgresql/data:rw,size=512m \
    -v "$root:$root:ro" -w "$root" -e POSTGRES_DB=campusos_v12_02c -e POSTGRES_USER=campusos \
    -e POSTGRES_PASSWORD="$secret" -p 127.0.0.1::5432 postgres:16-alpine >/dev/null
  trap 'docker stop "$target" >/dev/null 2>&1 || true' ERR
  for attempt in $(seq 1 80); do
    if docker exec -e PGPASSWORD="$secret" "$target" psql -h 127.0.0.1 -U campusos -d campusos_v12_02c -qAtc 'SELECT 1' >/dev/null 2>&1; then break; fi
    sleep 0.5
  done
  port="$(docker port "$target" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
  umask 077
  printf 'export DRILL_DB_CONTAINER=%q\nexport PGPASSWORD=%q\nexport DB_PASSWORD=%q\nexport DB_HOST=127.0.0.1\nexport DB_PORT=5432\nexport DB_USER=campusos\nexport DB_NAME=campusos_v12_02c\nexport CAMPUSOS_INTEGRATION_TEST_DATABASE_URL=%q\n' "$target" "$secret" "$secret" "postgres://campusos:$secret@127.0.0.1:$port/campusos_v12_02c?sslmode=disable" >"$state/private.env"
  mkdir -p "$state/bin"
  cat >"$state/bin/psql" <<'SHIM'
#!/usr/bin/env bash
exec docker exec -i -e PGPASSWORD "$DRILL_DB_CONTAINER" psql "$@"
SHIM
  chmod +x "$state/bin/psql"
  source "$state/private.env"
  export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host PATH="$state/bin:$PATH"
  bash scripts/migrate.sh up >"$state/migrations-up.log" 2>&1
  bash scripts/migrate.sh check >"$state/migrations-check.log" 2>&1
  docker exec -e PGPASSWORD "$target" psql -h 127.0.0.1 -U campusos -d "$DB_NAME" -qAtc 'SHOW server_version_num' >"$state/pg-version.txt"
  echo 'isolated PG16 prepared; private connection retained only in ignored cache'
elif [[ "$mode" == migrations ]]; then
  source "$state/private.env"
  export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host PATH="$state/bin:$PATH"
  docker exec -e PGPASSWORD "$DRILL_DB_CONTAINER" psql -h 127.0.0.1 -U campusos -d postgres -v ON_ERROR_STOP=1 -c 'DROP DATABASE IF EXISTS campusos_v12_02c_ddl WITH (FORCE)' -c 'CREATE DATABASE campusos_v12_02c_ddl' >"$state/ddl-create.log"
  export DB_NAME=campusos_v12_02c_ddl
  sql() { docker exec -i -e PGPASSWORD "$DRILL_DB_CONTAINER" psql -h 127.0.0.1 -U campusos -d "$DB_NAME" -qAtv ON_ERROR_STOP=1 "$@"; }
  migrate() { bash scripts/migrate.sh "$1" >"$state/ddl-$2.log" 2>&1; }
  snap() { sql -f scripts/fixtures/v12-02c-legacy-snapshot.sql >"$state/legacy-$1.json"; }
  migrate up initial-up
  migrate down empty-down
  [[ "$(sql -c 'SELECT max(version) FROM schema_migrations')" =~ ^0*24$ ]]
  sql -f scripts/fixtures/v12-02b-storage-quota-facts-legacy.sql >"$state/ddl-fixture.log"
  snap before
  migrate up legacy-up
  snap after-up
  cmp "$state/legacy-before.json" "$state/legacy-after-up.json"
  sql -c "INSERT INTO admin_roles(id,name) VALUES('integration_ddl_role','DDL authority'); INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES('integration_ddl_role','identity.integration.manage','ddl');" >"$state/ddl-reference-create.log"
  if bash scripts/migrate.sh down >"$state/ddl-references-down-rejected.log" 2>&1; then echo 'referenced permission rollback unexpectedly succeeded' >&2; exit 1; fi
  [[ "$(sql -c "SELECT count(*) FROM admin_role_permissions WHERE role_id='integration_ddl_role'")" == 1 ]]
  sql -c "DELETE FROM admin_role_permissions WHERE role_id='integration_ddl_role'; DELETE FROM admin_roles WHERE id='integration_ddl_role';" >"$state/ddl-reference-clean.log"
  sql -c "INSERT INTO integration_principals(id,name,status,created_by_admin_id) VALUES('ip_ddl','DDL machine','active','adm.quota.legacy'); INSERT INTO integration_credentials(id,principal_id,prefix,digest,purpose,status,rules,not_before_ms,expires_at_ms,created_by_admin_id) VALUES('dddddddddddddddd','ip_ddl','cint1_dddddddddddddddd',decode(repeat('22',32),'hex'),'integration_api','active','[]',1,9007199254740991,'adm.quota.legacy');" >"$state/ddl-machine-create.log"
  machine_snapshot="SELECT jsonb_build_object('principal',(SELECT to_jsonb(p) FROM integration_principals p),'credential',(SELECT to_jsonb(c) FROM integration_credentials c))::text"
  sql -c "$machine_snapshot" >"$state/machine-before-guard.json"
  if bash scripts/migrate.sh down >"$state/ddl-nonempty-down-rejected.log" 2>&1; then echo 'nonempty rollback unexpectedly succeeded' >&2; exit 1; fi
  [[ "$(sql -c 'SELECT max(version) FROM schema_migrations')" =~ ^0*25$ ]]
  sql -c "$machine_snapshot" >"$state/machine-after-guard.json"
  cmp "$state/machine-before-guard.json" "$state/machine-after-guard.json"
  snap after-guard
  cmp "$state/legacy-before.json" "$state/legacy-after-guard.json"
  consumed="$(sql -c 'SELECT last_value FROM integration_facts_version_seq')"
  sql -c "DELETE FROM integration_credentials; DELETE FROM integration_principals;" >"$state/ddl-owned-fixture-clean.log"
  migrate down preservation-down
  snap after-down
  cmp "$state/legacy-before.json" "$state/legacy-after-down.json"
  migrate up preservation-up
  snap after-reup
  cmp "$state/legacy-before.json" "$state/legacy-after-reup.json"
  sql -c "INSERT INTO integration_principals(id,name,status,created_by_admin_id) VALUES('ip_ddl','DDL machine','active','adm.quota.legacy')" >"$state/ddl-fresh-create.log"
  fresh="$(sql -c "SELECT facts_version FROM integration_principals WHERE id='ip_ddl'")"
  (( fresh > consumed ))
  printf '%s %s\n' "$consumed" "$fresh" >"$state/ddl-nonce-preservation.txt"
  migrate check final-check
  echo '25 empty down/up, nonempty rollback guard, all100 legacy tables and nonreuse passed'
elif [[ "$mode" == native ]]; then
  source "$state/private.env"
  export GOCACHE="$root/.cache/v12-g0/go-cache"
  mkdir -p "$state/native"
  for entry in \
    port:internal/modules/core/identity/port \
    policy:internal/modules/core/identity/policy \
    repository:internal/modules/core/identity/repository \
    service:internal/modules/core/identity/service \
    handler:internal/modules/core/identity/handler \
    ingress:internal/modules/features/integration; do
    name="${entry%%:*}"; package="${entry#*:}"
    go test -c -race -o "$state/native/$name.test" "./$package" >"$state/native/$name-build.log" 2>&1
    "$state/native/$name.test" -test.list '^TestIntegration' >"$state/native/$name-required.txt"
    env -i PATH="$PATH" TMPDIR="$state/tmp" CAMPUSOS_INTEGRATION_TEST_DATABASE_URL="$CAMPUSOS_INTEGRATION_TEST_DATABASE_URL" \
      CAMPUSOS_INTEGRATION_ACCESS_OUTPUT="$state/native/v1-decisions.json" CAMPUSOS_INTEGRATION_RUNTIME_OUTPUT="$state/native/v2-decisions.json" \
      "$state/native/$name.test" -test.run '^TestIntegration' -test.v -test.count=1 >"$state/native/$name.log" 2>"$state/native/$name.stderr"
    go tool test2json -p "github.com/campusos/CampusOS/$package" -t <"$state/native/$name.log" >"$state/native/$name.jsonl"
  done
  node scripts/check-v12-integration-access-contract.mjs --output "$state/native/v1-decisions.json" --report "$state/native/v1-oracle.json" >"$state/native/v1-oracle.log"
  node scripts/check-v12-integration-runtime-contract.mjs --output "$state/native/v2-decisions.json" --report "$state/native/v2-oracle.json" >"$state/native/v2-oracle.log"
elif [[ "$mode" == browser ]]; then
  source "$state/private.env"
  export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host PATH="$state/bin:$PATH"
  # Only this owned disposable browser database is recreated on reruns.
  docker exec -e PGPASSWORD "$DRILL_DB_CONTAINER" psql -h 127.0.0.1 -U campusos -d postgres -v ON_ERROR_STOP=1 -c 'DROP DATABASE IF EXISTS campusos_v12_02c_browser WITH (FORCE)' -c 'CREATE DATABASE campusos_v12_02c_browser' >"$state/browser-database-create.log" 2>&1
  export DB_NAME=campusos_v12_02c_browser
  bash scripts/migrate.sh up >"$state/browser-migrations-up.log" 2>&1
  bash scripts/migrate.sh check >"$state/browser-migrations-check.log" 2>&1
  export CAMPUSOS_INTEGRATION_BROWSER_DATABASE_URL="${CAMPUSOS_INTEGRATION_TEST_DATABASE_URL/\/campusos_v12_02c?/\/campusos_v12_02c_browser?}"
  export CAMPUSOS_INTEGRATION_SERVER_BINARY="$state/campusos-server" CAMPUSOS_INTEGRATION_BROWSER_OUTPUT="$state/browser" GOCACHE="$root/.cache/v12-g0/go-cache"
  go test -json ./internal/server -run '^TestIntegrationActualServerBrowserLifecycle$' -count=1 >"$state/target-browser.jsonl" 2>"$state/target-browser.stderr"
elif [[ "$mode" == test ]]; then
  source "$state/private.env"
  export GOCACHE="$root/.cache/v12-g0/go-cache"
  go test -race -json ./internal/modules/core/identity/repository ./internal/modules/core/identity/service ./internal/modules/core/identity/handler ./internal/modules/features/integration -run 'Integration' -count=1 >"$state/target-go.jsonl" 2>"$state/target-go.stderr"
elif [[ "$mode" == common ]]; then
  source "$state/private.env"
  export GOCACHE="$root/.cache/v12-g0/go-cache" CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host POSTGRES_CONTAINER="$DRILL_DB_CONTAINER" PATH="$state/bin:$PATH"
  : >"$state/common-passed.txt"
  check() { local name="$1"; shift; echo "Integration common: $name"; if "$@" >"$state/common-$name.log" 2>&1; then echo "$name" >>"$state/common-passed.txt"; else tail -n 60 "$state/common-$name.log" >&2; return 1; fi; }
  check full-go go test ./... -count=1
  check related-race go test -race ./internal/modules/core/identity/... ./internal/modules/core/community/... ./internal/modules/features/integration ./internal/platform/reliability ./internal/platform/transaction ./internal/platform/route ./internal/projectaudit ./internal/transport/httpapi ./pkg/middleware ./pkg/apperror -count=1
  check admin-components pnpm --dir admin test:component --maxWorkers=1 --minWorkers=1
  check sdk pnpm --dir sdk/typescript build
  check contracts make contracts-check
  check architecture make architecture-check
  check database make database-check
  check docs-build make docs-build
  check docs-links make docs-links
  check line-endings python3 scripts/check-line-endings.py --include-untracked
  check diff git diff --check
elif [[ "$mode" == final ]]; then
  export GOCACHE="$root/.cache/v12-g0/go-cache"
  : >"$state/final-docs-passed.txt"
  check() { local name="$1"; shift; if "$@" >"$state/final-$name.log" 2>&1; then echo "$name" >>"$state/final-docs-passed.txt"; else tail -n 60 "$state/final-$name.log" >&2; return 1; fi; }
  check docs-links make docs-links
  check docs-build make docs-build
  check line-endings python3 scripts/check-line-endings.py --include-untracked
  check diff git diff --check
  check architecture make architecture-check
  check skill-bridges python3 skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --check
  python3 scripts/v12-02c-exit-report.py --finalize "$state"
elif [[ "$mode" == stop ]]; then
  source "$state/private.env"
  docker stop "$DRILL_DB_CONTAINER" >/dev/null
  rm "$state/private.env"
else
  echo 'usage: v12-02c-exit-drill.sh all|prepare|migrations|native|test|browser|common|final|stop' >&2; exit 2
fi
