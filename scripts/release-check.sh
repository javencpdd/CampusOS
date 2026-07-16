#!/usr/bin/env bash
set -euo pipefail

cleanup() {
  rm -f examples/plugins/grpc-example/plugin
  rm -f examples/plugins/campus-welcome/plugin
  rm -f examples/plugins/schedule-helper/plugin
  rm -f examples/plugins/v2-managed-example/plugin
}
trap cleanup EXIT

echo "==> contracts"
go run ./cmd/campusos-contracts --check
make version-check

echo "==> documentation links"
python3 scripts/check-doc-links.py
make readme-check

echo "==> architecture boundaries"
make architecture-check

echo "==> data and generated file governance"
make data-governance-check
make generated-files-check

echo "==> Go tests"
GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test ./... -count=1

echo "==> database"
./scripts/migrate.sh up
./scripts/database-check.sh all
python3 skills/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .

echo "==> plugin templates"
go test ./modules -count=1
go run ./cmd/campusosctl plugin dev examples/plugins/campus-welcome --json
(cd examples/plugins/campus-welcome && go test ./... -count=1)
go run ./cmd/campusosctl plugin dev examples/plugins/grpc-example --json
go run ./cmd/campusosctl plugin dev examples/plugins/schedule-helper --json
(cd examples/plugins/schedule-helper && go test ./... -count=1)
go run ./cmd/campusosctl plugin dev examples/plugins/v2-managed-example --json
(cd examples/plugins/v2-managed-example && go test ./... -count=1)
go run ./cmd/campusosctl plugin dev examples/plugins/wasm-example --json

echo "==> resource packages"
for resource in \
  data/resources/homepage-packs/campus-hero \
  data/resources/space-style-packs/clean-blog \
  data/resources/space-style-packs/kinetic-journal \
  data/resources/themes/aurora-campus \
  data/resources/themes/campus-canvas; do
  go run ./cmd/campusosctl resource inspect "$resource" >/dev/null
done

echo "==> frontend builds"
(cd web && pnpm lint && pnpm exec prettier --check src tests && pnpm build)
(cd admin && pnpm test:component && pnpm build)
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

echo "CampusOS v0.10 release check passed"
