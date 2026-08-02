# CampusOS Migration Safety Contract

> 更新时间：2026-08-03

## Source and immutability

1. Treat numbered files under `migrations/` as an append-only history once they may have run outside a disposable database.
2. Fix an applied migration with a new forward migration; do not silently rewrite old SQL to make a current database pass.
3. Keep every `.up.sql` paired with a data-preserving `.down.sql` where rollback is genuinely supported. State explicitly
   when a destructive rollback is intentionally unavailable.
4. Verify constraints, indexes, triggers, seeds, and backfills against current repository/service ownership before calling
   apparently similar statements redundant.

## Redundancy review

Classify a repeated statement before changing it:

| Class | Action |
| --- | --- |
| Intentional idempotency (`IF NOT EXISTS`, compatibility guard) | Keep unless it hides real drift. |
| Superseded schema followed by a later forward alteration | Keep historical files; document the final schema. |
| Duplicate active index/constraint with the same semantics | Remove only through a new forward migration after query/lock review. |
| Conflicting seed or trigger behavior | Add a corrective migration and regression drill. |
| Dead table/column with retained production data | Plan deprecation, export/retention, and a later explicit removal. |

## Required validation

Run the smallest applicable isolated checks first, then broader gates:

```bash
python skills/sources/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .
./scripts/database-check.sh all
make v12-migration-check
make v13-migration-check
```

For Windows, run Bash migration drills in Git Bash/WSL2 or the repository Docker environment. Never point destructive
drills at `deploy/docker/.env.dev.local` without confirming they create and remove only isolated test databases.

Record the tested PostgreSQL version, up/down/up sequence, data preservation result, and any lock/backfill risk in the
current progress document. Synchronize Admin architecture, `docs/architecture/`, public API docs, and operational Help when
the final schema or operator workflow changes.
