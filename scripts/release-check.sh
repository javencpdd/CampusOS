#!/usr/bin/env bash
set -euo pipefail

export GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}"
VERSION="$(go run ./cmd/campusosctl version)"

echo "==> contracts"
go run ./cmd/campusos-contracts --check
make error-contract-check
make observability-check
make v13-reliability-observability-check
make v13-baseline-check
make v14-baseline-check
make v14-storage-check
make v14-schedule-check
make v14-documents-check
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
python3 scripts/check-release-legacy.py

echo "==> data and generated file governance"
make data-governance-check
make generated-files-check

echo "==> Go tests"
GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test ./... -count=1

echo "==> database"
make release-database-check
python3 skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .

if [[ "${RUN_V13_CAPACITY_DRILL:-true}" == "true" ]]; then
  echo "==> owned current-identity bounded capacity reads"
  make release-capacity-check
fi

echo "==> current v5 author packages (not runtime installation)"
go test ./modules -count=1
make plugin-v5-release-check

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
  echo "==> owned current native official restore"
  make release-restore-check
  echo "==> owned current Docker official restore"
  make release-restore-docker-check
  echo "==> isolated restore drill"
  ./scripts/restore-drill.sh
fi

if [[ "${RUN_BROWSER_SMOKE:-true}" == "true" ]]; then
  echo "==> browser smoke"
  make release-browser-check
fi

echo "CampusOS ${VERSION} release check passed"
