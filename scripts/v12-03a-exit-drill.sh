#!/usr/bin/env bash
# Full03a package/CLI exit; every database/container is created and owned here.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-all}"
if [[ "$mode" == all ]]; then
  mkdir -p .cache/v12-03a-exit
  work="$(mktemp -d "$root/.cache/v12-03a-exit/acceptance.XXXXXX")"
  export CAMPUSOS_V5_EXIT_DIR="$work"
  echo "V12-03a exit: $work"
  bash scripts/build-v5-examples.sh >"$work/examples-build.log" 2>&1
  GOCACHE="$root/.cache/v12-g0/go-cache" go run ./cmd/campusos-contracts --write >"$work/contracts-write.log" 2>&1
  python3 scripts/v12-03a-exit-report.py --freeze "$work"
  bash "$0" target
  bash "$0" common
  python3 scripts/v12-03a-exit-report.py --build "$work"
  exit 0
fi
work="${CAMPUSOS_V5_EXIT_DIR:?set CAMPUSOS_V5_EXIT_DIR to the owned acceptance directory}"
export GOCACHE="$root/.cache/v12-g0/go-cache" TMPDIR="$work/tmp"
mkdir -p "$TMPDIR"
if [[ "$mode" == target ]]; then
  mkdir -p "$work/native" "$work/server"
  go test -c -race -o "$work/native/plugin.test" ./internal/plugin >"$work/native/build.log" 2>&1
  (cd internal/plugin; env -i PATH="$PATH" TMPDIR="$TMPDIR" CAMPUSOS_PLUGIN_V5_MANIFEST_OUTPUT="$work/native/decisions.json" "$work/native/plugin.test" -test.run '^TestPluginV5' -test.count=1 -test.v) >"$work/native/plugin.log" 2>"$work/native/plugin.stderr"
  go tool test2json -p github.com/campusos/CampusOS/internal/plugin -t <"$work/native/plugin.log" >"$work/native/plugin.jsonl"
  go test -race -json ./cmd/campusosctl ./cmd/campusos-plugin-v5-check -run '^TestPlugin|^TestRemovedPlugin|^TestVersion' -count=1 >"$work/native/cli.jsonl" 2>"$work/native/cli.stderr"
  go test -race -json ./sdk/go -count=1 >"$work/native/sdk.jsonl" 2>"$work/native/sdk.stderr"
  go build -buildvcs=false -o "$work/campusosctl" ./cmd/campusosctl
  go build -buildvcs=false -o "$work/manifest-check" ./cmd/campusos-plugin-v5-check
  env -i PATH="$PATH" TMPDIR="$TMPDIR" node scripts/check-v12-plugin-v5-native-manifest.mjs --native "$work/native/decisions.json" --cli "$work/manifest-check" --work "$work/wire" >"$work/native/oracle.log" 2>&1
  env -i PATH="$PATH" TMPDIR="$TMPDIR" node scripts/v12-03a-package-cli.mjs "$work/campusosctl" "$work/cli" >"$work/cli-driver.log" 2>&1
  go build -buildvcs=false -o "$work/campusos-server" ./cmd/server >"$work/server/build.log" 2>&1
  CAMPUSOS_INTEGRATION_DRILL_DIR="$work/pg" bash scripts/v12-02c-exit-drill.sh prepare >"$work/pg-prepare.log" 2>&1
  trap 'CAMPUSOS_INTEGRATION_DRILL_DIR="$work/pg" bash scripts/v12-02c-exit-drill.sh stop' EXIT
  source "$work/pg/private.env"
  docker exec -e PGPASSWORD "$DRILL_DB_CONTAINER" psql -h 127.0.0.1 -U campusos -d postgres -v ON_ERROR_STOP=1 -c 'CREATE DATABASE campusos_v12_03a_target' >"$work/server/database-create.log"
  export DB_NAME=campusos_v12_03a_target CAMPUSOS_SKIP_DOTENV=true PSQL_MODE=host PATH="$work/pg/bin:$PATH"
  bash scripts/migrate.sh up >"$work/server/migrations.log" 2>&1
  bash scripts/migrate.sh check >"$work/server/checksum.log" 2>&1
  export CAMPUSOS_V5_SERVER_DATABASE_URL="${CAMPUSOS_INTEGRATION_TEST_DATABASE_URL/\/campusos_v12_02c?/\/campusos_v12_03a_target?}"
  export CAMPUSOS_V5_SERVER_BINARY="$work/campusos-server" CAMPUSOS_V5_SERVER_OUTPUT="$work/server"
  go test -race -json ./internal/server -run '^TestPluginV5ActualServerIgnoresRetiredPackages$' -count=1 >"$work/server/target.jsonl" 2>"$work/server/target.stderr"
elif [[ "$mode" == common ]]; then
  python3 scripts/v12-03a-exit-report.py --verify-source "$work"
  touch "$work/common-passed.txt"
  check(){ local name="$1";shift;if rg -q -x "$name" "$work/common-passed.txt" && [[ -s "$work/common-$name.log" ]];then echo "03a retained passed gate: $name";return;fi;echo "03a common: $name";if "$@" >"$work/common-$name.log" 2>&1;then echo "$name" >>"$work/common-passed.txt";else tail -n 50 "$work/common-$name.log" >&2;return 1;fi; }
  check full-go go test ./... -count=1
  check related-race go test -race ./internal/plugin/... ./internal/server ./cmd/campusosctl ./cmd/campusos-plugin-v5-check ./sdk/go -count=1
  check sdk pnpm --dir sdk/typescript build
  check contracts make contracts-check
  check architecture make architecture-check
  check web-build pnpm --dir web build
  check admin-build pnpm --dir admin build
  check web-components pnpm --dir web test:component --maxWorkers=1 --minWorkers=1
  check admin-components pnpm --dir admin test:component --maxWorkers=1 --minWorkers=1
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
  python3 scripts/v12-03a-exit-report.py --finalize "$work"
else
  echo 'usage: v12-03a-exit-drill.sh all|target|common|final' >&2;exit 2
fi
