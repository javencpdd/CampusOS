#!/usr/bin/env bash
set -euo pipefail

export GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}"
VERSION="$(go run ./cmd/campusosctl version)"

cleanup() {
  rm -f examples/plugins/grpc-example/plugin
  rm -f examples/plugins/campus-welcome/plugin
  rm -f examples/plugins/schedule-helper/plugin
  rm -f examples/plugins/v2-managed-example/plugin
}
trap cleanup EXIT

echo "==> contracts"
go run ./cmd/campusos-contracts --check
make error-contract-check
make observability-check
make v13-reliability-observability-check
make v13-baseline-check
make v13-capacity-check
make appearance-delivery-check
make docker-deploy-check
make version-check

echo "==> documentation links"
make line-endings-check
python3 scripts/check-doc-links.py
make readme-check

echo "==> architecture boundaries"
make architecture-check
make reliability-check
make outbox-check
make failure-injection-check
make v12-failure-injection-check
make identity-email-check
make identity-challenge-check
make identity-registration-check
make identity-session-check
make identity-recovery-check
make email-delivery-check
make category-hierarchy-check
make structured-thread-check
make mutual-aid-check
make secondhand-check

echo "==> data and generated file governance"
make data-governance-check
make generated-files-check

echo "==> Go tests"
GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test ./... -count=1

echo "==> database"
./scripts/migrate.sh up
./scripts/database-check.sh all
make v12-migration-check
make v13-migration-check
python3 skills/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .

if [[ "${RUN_V13_CAPACITY_DRILL:-true}" == "true" ]]; then
  echo "==> isolated v13 capacity drill"
  make v13-capacity-drill
fi

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

echo "CampusOS ${VERSION} release check passed"
