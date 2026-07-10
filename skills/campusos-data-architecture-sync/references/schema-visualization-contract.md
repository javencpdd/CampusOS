# Schema Visualization Contract

## Source Order

1. `migrations/*.up.sql` defines application PostgreSQL tables and explicit foreign keys.
2. `scripts/migrate.sh` and `scripts/migrate.ps1` define the system-managed `schema_migrations` table.
3. `admin/src/views/SystemArchitectureView.vue` presents a reviewed, Chinese-language explanation of those tables and data directories.
4. Service models and repository code decide whether an ID-like field is a real business relationship.

The checker verifies that each schema table is represented in `databaseTables` and that every relation endpoint exists. It does not generate titles, purposes, card domains, cardinality, or relationships automatically because those are business documentation decisions.

## Review Rules

- A new `CREATE TABLE` requires a `databaseTables` entry and migration timeline update.
- An `ALTER TABLE` requires a UI review when it changes a key field, ownership, data retention, or displayed purpose.
- Add a relation only when an explicit FK, a documented ID/name relation, or the owning service proves it. Polymorphic IDs need a clear label.
- If a migration adds explicit foreign keys, revise the Admin warning so it distinguishes FK-enforced edges from logical edges.
- A path change under `data/`, `.campusos/logs/`, or a plugin storage configuration requires a `storageRows` and backup-boundary review.

## Checker Outcomes

| Output | Meaning | Required action |
| --- | --- | --- |
| `missing from view` | Migration/system table is absent from the Admin view. | Add an entry before merging. |
| `stale in view` | View refers to a table no longer defined by repository schema. | Remove it or document why it remains. |
| `unknown relation endpoints` | A relation card points to a non-schema table. | Correct the relation. |
| `unconnected tables` | Informational; a table may be standalone, polymorphic, or intentionally omitted from the relation lanes. | Review its detail card, not necessarily add an edge. |
