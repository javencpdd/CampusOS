#!/usr/bin/env bash
set -euo pipefail

migration="migrations/000001_v1_schema_baseline.up.sql"
for table in platform_outbox outbox_consumer_receipts platform_outbox_attempts platform_command_audits platform_worker_leases; do
  grep -Eq "CREATE TABLE public\.${table}" "$migration"
done
for token in "lease_generation" "schema_version" "uk_platform_outbox_idempotency"; do
  grep -Fq "$token" "$migration"
done
grep -Fq "FOR UPDATE SKIP LOCKED" internal/platform/reliability/pg_store.go
grep -Fq "attempts < max_attempts" internal/platform/reliability/pg_store.go
grep -Fq "system:outbox-finalize" internal/platform/reliability/worker.go
grep -Fq "'failed'" "$migration"

if grep -Eq '_ = w\.store\.(Complete|Retry|DeadLetter|FinishAttempt|RecordConsumerReceipt)' internal/platform/reliability/worker.go; then
  echo "outbox check failed: critical worker persistence errors must not be discarded" >&2
  exit 1
fi

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test ./internal/platform/reliability -count=1
echo "outbox check passed: schema, max-attempt convergence, claim fencing, receipts, finalization, and observable transitions are covered"
