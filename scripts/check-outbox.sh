#!/usr/bin/env bash
set -euo pipefail

migration="migrations/000027_v11_reliable_commands_and_outbox.up.sql"
for table in platform_outbox outbox_consumer_receipts platform_outbox_attempts platform_command_audits platform_worker_leases; do
  grep -Eq "CREATE TABLE IF NOT EXISTS ${table}" "$migration"
done
for token in "lease_generation" "schema_version" "uk_platform_outbox_idempotency"; do
  grep -Fq "$token" "$migration"
done
grep -Fq "FOR UPDATE SKIP LOCKED" internal/platform/reliability/pg_store.go

GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test ./internal/platform/reliability -count=1
echo "outbox check passed: schema, claim fencing, idempotency, receipts, and attempts are covered"
