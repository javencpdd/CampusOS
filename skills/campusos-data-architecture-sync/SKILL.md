---
name: campusos-data-architecture-sync
description: Keep CampusOS migrations, system-managed schema tables, local data-directory boundaries, and the Admin data-architecture view synchronized. Use when a CampusOS migration, `scripts/migrate.*`, persistent storage path, plugin data layout, or `admin/src/views/SystemArchitectureView.vue` changes; also use when auditing or refreshing the Admin `/architecture` page.
---

# CampusOS Data Architecture Sync

## Overview

Maintain the Admin `/architecture` page as a source-controlled schema view. It is not a live PostgreSQL inspector: it documents the schema represented by the repository and intentionally does not expose records, secrets, or local file contents.

## Workflow

1. Run the consistency checker before editing:

   ```bash
   python3 skills/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
   ```

2. Read the changed migration and the matching data model or service. Then update `admin/src/views/SystemArchitectureView.vue`:
   - Add each persistent PostgreSQL table to `databaseTables`, including a Chinese title, purpose, key fields, migration source, and a conservative relationship note.
   - Add a relation only when a field, explicit foreign key, or service-level contract establishes a real dependency. Label a non-FK relationship as logical.
   - Update `storageRows` when persistent paths, ownership, quotas, or backup boundaries change.
   - Update `migrations` when a migration adds/changes schema, system data, or seed data.

3. Keep the security boundary intact:
   - Do not claim the view is real-time unless it queries a protected live schema API.
   - Do not put table rows, file contents, `.env`, secrets, or unrestricted filesystem access into the Admin page.
   - Treat `schema_migrations` as a system-managed table created by `scripts/migrate.sh` and `scripts/migrate.ps1`, not by an application migration.

4. Run the checker again. Resolve errors before continuing. Read [schema visualization contract](references/schema-visualization-contract.md) when the report needs interpretation.

5. Validate the touched area:

   ```bash
   cd admin && pnpm build
   GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
   git diff --check
   ```

6. Update `docs/architecture/当前架构概览.md`, the current progress document, and project onboarding status when the user-visible architecture view changes.

## Live Database Requests

When a request requires live row counts, index sizes, or migration status, design a separate administrator-only read-only operations API first. Authenticate it with existing admin permissions, return a narrow allowlist of metadata, avoid SQL input, and document the retention and disclosure boundary. Do not turn this static schema skill into a generic database browser.

## Resources

- `scripts/check_architecture_sync.py`: compares repository migrations and migration scripts with the tables and relation endpoints declared by `admin/src/modules/architecture/pages/SystemArchitectureView.vue`. It exits nonzero on drift.
- `references/schema-visualization-contract.md`: explains the checker, source-of-truth ordering, and what requires a manual relationship review.
