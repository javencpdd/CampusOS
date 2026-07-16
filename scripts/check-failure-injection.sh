#!/usr/bin/env bash
set -euo pipefail

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test \
  ./internal/platform/transaction \
  ./internal/platform/reliability \
  ./internal/modules/core/identity/service \
  ./internal/modules/core/community/service \
  ./internal/plugin \
  -count=1 \
  -run 'Test(MemoryRestoresSnapshotsOnPanic|ExecuteRollsBackMemoryStateWhenRequiredAuditFails|ReplayCommandRollsBackWhenRequiredAuditFails|StaleLeaseCannotCompleteAfterRecovery|ConsumerReceiptSkipsRepeatedExternalDelivery|RegisterRollsBackUserWhenAccountOrRequiredAuditFails|GovernanceCommandRollsBackWhenRequiredEvidenceFails|GovernanceDenyAuditSurvivesReliableTransactionRollback|ManagerImportPackageRestoresPriorVersionAfterCatalogFailure)'
echo "failure-injection check passed: transaction rollback, required audit rollback, lease fencing, consumer receipts, governance evidence, registration rollback, and package replacement recovery are covered"
