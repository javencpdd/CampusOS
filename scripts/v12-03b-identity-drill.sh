#!/usr/bin/env bash
# One03b slice: durable publication identities, not installation/operation.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-all}"
if [[ "$mode" == all ]]; then
  mkdir -p .cache/v12-03b-identity
  work="$(mktemp -d "$root/.cache/v12-03b-identity/acceptance.XXXXXX")"
  export CAMPUSOS_V5_IDENTITY_DRILL_DIR="$work"
  echo "03b identity acceptance: $work"
  python3 scripts/v12-03b-identity-report.py --freeze "$work"
  bash "$0" target
  python3 scripts/v12-03b-identity-report.py --build "$work"
  exit 0
fi
work="${CAMPUSOS_V5_IDENTITY_DRILL_DIR:?set owned acceptance directory}"
export GOCACHE="$root/.cache/v12-g0/go-cache" TMPDIR="$work/tmp"
mkdir -p "$TMPDIR" "$work/native"
if [[ "$mode" == target ]]; then
  go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/plugin-build.log" 2>&1
  (cd internal/plugin; env -i PATH="$PATH" TMPDIR="$TMPDIR" "$work/native/plugin.test" -test.run '^TestPluginV5' -test.v -test.count=1) >"$work/native/plugin.log" 2>"$work/native/plugin.stderr"
  go tool test2json -p github.com/campusos/CampusOS/internal/plugin -t <"$work/native/plugin.log" >"$work/native/plugin.jsonl"
  go test -c -race -o "$work/native/server.test" ./internal/server >"$work/native/server-build.log" 2>&1
  (cd internal/server; env -i PATH="$PATH" TMPDIR="$TMPDIR" "$work/native/server.test" -test.run '^TestPluginV5IdentityAssembly' -test.v -test.count=1) >"$work/native/server.log" 2>"$work/native/server.stderr"
  go tool test2json -p github.com/campusos/CampusOS/internal/server -t <"$work/native/server.log" >"$work/native/server.jsonl"
  # An owned anonymous Docker volume survives this test's database restart;
  # docker rm -v removes it on every exit. Never use the developer database.
  DRILL_DB_CONTAINER="campusos-v12-03b-identity-$(date +%s)-$$"
  export DRILL_DB_CONTAINER
  cleanup(){ docker rm -f -v "$DRILL_DB_CONTAINER" >"$work/cleanup.log" 2>&1; rm -f "$work/private.env"; }
  trap cleanup EXIT
  secret="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
  docker run -d --name "$DRILL_DB_CONTAINER" -v "$root:$root:ro" -w "$root" \
    -e POSTGRES_DB=campusos_v12_03b_identities -e POSTGRES_USER=campusos -e POSTGRES_PASSWORD="$secret" \
    -p 127.0.0.1::5432 postgres:16-alpine >"$work/container-create.log"
  for attempt in $(seq 1 80); do
    if docker exec -e PGPASSWORD="$secret" "$DRILL_DB_CONTAINER" psql -h 127.0.0.1 -U campusos -d campusos_v12_03b_identities -qAtc 'SELECT 1' >/dev/null 2>&1; then break; fi
    sleep 0.5
  done
  port="$(docker port "$DRILL_DB_CONTAINER" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
  mkdir -p "$work/bin"
  umask 077
  printf 'export PGPASSWORD=%q\nexport DB_PASSWORD=%q\nexport DB_HOST=127.0.0.1\nexport DB_PORT=5432\nexport DB_USER=campusos\nexport DB_NAME=campusos_v12_03b_identities\nexport CAMPUSOS_V5_IDENTITY_DATABASE_URL=%q\n' "$secret" "$secret" "postgres://campusos:$secret@127.0.0.1:$port/campusos_v12_03b_identities?sslmode=disable" >"$work/private.env"
  cat >"$work/bin/psql" <<'SHIM'
#!/usr/bin/env bash
exec docker exec -i -e PGPASSWORD "$DRILL_DB_CONTAINER" psql "$@"
SHIM
  chmod +x "$work/bin/psql"
  source "$work/private.env"
  unset secret
  export CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host POSTGRES_CONTAINER="$DRILL_DB_CONTAINER" PATH="$work/bin:$PATH"
  export CAMPUSOS_V5_IDENTITY_OUTPUT="$work/identity"
  sql(){ docker exec -i -e PGPASSWORD "$DRILL_DB_CONTAINER" psql -h 127.0.0.1 -U campusos -d "$DB_NAME" -qAtv ON_ERROR_STOP=1 "$@"; }
  migrate(){ bash scripts/migrate.sh "$1" >"$work/migration-$2.log" 2>&1; }
  snap(){ sql -f scripts/fixtures/v12-03b-identity-legacy-snapshot.sql >"$work/legacy-$1.json"; }
  sql -c 'SHOW server_version_num' >"$work/pg-version.txt"
  migrate up empty-up
  migrate down empty-down
  [[ "$(sql -c 'SELECT max(version)::integer FROM schema_migrations')" == 25 ]]
  sql -f scripts/fixtures/v12-02b-storage-quota-facts-legacy.sql >"$work/legacy-seed.log"
  sql -c "INSERT INTO plugins(id,name,display_name,version,runtime,manifest) VALUES(260001,'legacy-identity-kept','Historical plugin','1.0.0','grpc','{\"historical\":true}'); INSERT INTO plugin_versions(id,plugin_id,version,package_digest,manifest_api_version,host_api_version,permission_fingerprint,manifest) VALUES(260002,260001,'1.0.0',repeat('a',64),'campusos.plugin/v1','v1',repeat('b',64),'{\"historical\":true}'); INSERT INTO integration_principals(id,name,status,created_by_admin_id) VALUES('ip_identity_legacy','Historical machine','active','adm.quota.legacy');" >>"$work/legacy-seed.log"
  snap before
  migrate up preservation-up
  snap after-up
  cmp "$work/legacy-before.json" "$work/legacy-after-up.json"
  migrate down preservation-down
  snap after-down
  cmp "$work/legacy-before.json" "$work/legacy-after-down.json"
  migrate up preservation-reup
  snap after-reup
  cmp "$work/legacy-before.json" "$work/legacy-after-reup.json"
  (cd internal/plugin; env -i PATH="$PATH" TMPDIR="$TMPDIR" CAMPUSOS_V5_IDENTITY_DATABASE_URL="$CAMPUSOS_V5_IDENTITY_DATABASE_URL" CAMPUSOS_V5_IDENTITY_OUTPUT="$CAMPUSOS_V5_IDENTITY_OUTPUT" "$work/native/plugin.test" -test.run '^TestPostgresPluginV5IdentityDurability$' -test.v -test.count=1) >"$work/native/pg.log" 2>"$work/native/pg.stderr"
  go tool test2json -p github.com/campusos/CampusOS/internal/plugin -t <"$work/native/pg.log" >"$work/native/pg.jsonl"
  sql -c 'SELECT jsonb_agg(to_jsonb(p) ORDER BY publisher,plugin_key,version) FROM plugin_package_identities p' >"$work/identities-before-restart.json"
  docker restart "$DRILL_DB_CONTAINER" >"$work/postgres-restart.log"
  # Docker may allocate a different ephemeral host port after restart. The
  # in-container readiness probe alone does not validate that old mapping.
  port="$(docker port "$DRILL_DB_CONTAINER" 5432/tcp | awk -F: '/127\.0\.0\.1/{print $NF;exit}')"
  export CAMPUSOS_V5_IDENTITY_DATABASE_URL="postgres://campusos:$DB_PASSWORD@127.0.0.1:$port/campusos_v12_03b_identities?sslmode=disable"
  for attempt in $(seq 1 80); do
    if sql -c 'SELECT 1' >/dev/null 2>&1; then break; fi
    sleep 0.5
  done
  (cd internal/plugin; env -i PATH="$PATH" TMPDIR="$TMPDIR" CAMPUSOS_V5_IDENTITY_DATABASE_URL="$CAMPUSOS_V5_IDENTITY_DATABASE_URL" CAMPUSOS_V5_IDENTITY_OUTPUT="$CAMPUSOS_V5_IDENTITY_OUTPUT" "$work/native/plugin.test" -test.run '^TestPostgresPluginV5IdentityRestore$' -test.v -test.count=1) >"$work/native/restore.log" 2>"$work/native/restore.stderr"
  go tool test2json -p github.com/campusos/CampusOS/internal/plugin -t <"$work/native/restore.log" >"$work/native/restore.jsonl"
  if bash scripts/migrate.sh down >"$work/migration-nonempty-down-rejected.log" 2>&1; then echo 'nonempty identity rollback unexpectedly allowed' >&2; exit 1; fi
  [[ "$(sql -c 'SELECT max(version)::integer FROM schema_migrations')" == 26 ]]
  sql -c 'SELECT jsonb_agg(to_jsonb(p) ORDER BY publisher,plugin_key,version) FROM plugin_package_identities p' >"$work/identities-after-guard.json"
  cmp "$work/identities-before-restart.json" "$work/identities-after-guard.json"
  snap after-guard
  cmp "$work/legacy-before.json" "$work/legacy-after-guard.json"
  migrate check final-check
  unset CAMPUSOS_V5_IDENTITY_DATABASE_URL CAMPUSOS_V5_IDENTITY_OUTPUT
  bash "$0" common
elif [[ "$mode" == common ]]; then
  python3 scripts/v12-03b-identity-report.py --verify-source "$work"
  : >"$work/common-passed.txt"
  check(){ local name="$1";shift; echo "03b identity common: $name"; if "$@" >"$work/common-$name.log" 2>&1; then echo "$name" >>"$work/common-passed.txt"; else tail -n 50 "$work/common-$name.log" >&2;return 1;fi; }
  check full-go go test ./... -count=1
  check related-race go test -race ./internal/plugin/... ./internal/server -count=1
  check sdk pnpm --dir sdk/typescript build
  check contracts make contracts-check
  check architecture make architecture-check
  check database make database-check
  check admin-build pnpm --dir admin build
  check er-tests python3 -m unittest migrations.tools.test_generate_er
  check readme make readme-check
  check docs-links make docs-links
  check docs-build make docs-build
  check line-endings python3 scripts/check-line-endings.py --include-untracked
  check diff git diff --check
elif [[ "$mode" == final ]]; then
  : >"$work/final-docs-passed.txt"
  check(){ local name="$1";shift;if "$@" >"$work/final-$name.log" 2>&1;then echo "$name" >>"$work/final-docs-passed.txt";else tail -n 50 "$work/final-$name.log" >&2;return 1;fi; }
  check docs-links make docs-links
  check docs-build make docs-build
  check line-endings python3 scripts/check-line-endings.py --include-untracked
  check diff git diff --check
  check architecture make architecture-check
  check skill-bridges python3 skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --check
  check onboarding-skill python3 "${CODEX_HOME:-$HOME/.codex}/skills/.system/skill-creator/scripts/quick_validate.py" skills/sources/campusos-project-onboarding
  python3 scripts/v12-03b-identity-report.py --finalize "$work"
else
  echo 'usage: v12-03b-identity-drill.sh all|target|common|final' >&2;exit 2
fi
