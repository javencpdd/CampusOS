#!/usr/bin/env bash
set -euo pipefail

cleanup() {
  rm -f examples/plugins/grpc-example/plugin
}
trap cleanup EXIT

echo "==> contracts"
go run ./cmd/campusos-contracts --check

echo "==> documentation links"
python3 scripts/check-doc-links.py

echo "==> architecture boundaries"
make architecture-check

echo "==> Go tests"
GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test ./... -count=1

echo "==> database"
./scripts/migrate.sh up
./scripts/database-check.sh all
python3 skills/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .

echo "==> plugin templates"
go run ./cmd/campusosctl plugin dev examples/plugins/builtin-example --json
go run ./cmd/campusosctl plugin dev examples/plugins/grpc-example --json
go run ./cmd/campusosctl plugin dev examples/plugins/wasm-example --json

echo "==> frontend builds"
(cd web && pnpm lint && pnpm exec prettier --check src tests && pnpm build)
(cd admin && pnpm build)
(cd docs-site && pnpm build)
python3 scripts/check-frontend-bundles.py

echo "==> TypeScript SDK"
(cd sdk/typescript && pnpm build)

echo "==> scripts and worktree whitespace"
bash -n scripts/*.sh scripts/smoke/*.sh
git diff --check

if [[ "${RUN_RESTORE_DRILL:-true}" == "true" ]]; then
  echo "==> isolated restore drill"
  ./scripts/restore-drill.sh
fi

if [[ "${RUN_BROWSER_SMOKE:-true}" == "true" ]]; then
  echo "==> browser smoke"
  ./scripts/smoke/browser-smoke.sh
fi

echo "CampusOS v0.9 release check passed"
