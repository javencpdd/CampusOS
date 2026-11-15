#!/usr/bin/env python3
"""Generate public schema facts for the Admin Chen view, without a database."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "migrations/tools"))
import generate_er  # noqa: E402


def schema_snapshot(root: Path) -> dict:
    tables = generate_er.parse_schema(generate_er.read_sql_files(root / "migrations"))
    metadata = []
    for launcher in ("migrate.sh", "migrate.ps1"):
        source = (root / "scripts" / launcher).read_text(encoding="utf-8")
        statements = re.findall(
            r"CREATE TABLE IF NOT EXISTS public\.schema_migration(?:s|_locks)\s*\(.*?\n\s*\);",
            source, re.S,
        )
        system = generate_er.parse_schema("\n".join(statements))
        if set(system) != {"schema_migrations", "schema_migration_locks"}:
            raise ValueError(f"cannot resolve system metadata from {launcher}")
        metadata.append({name: (t.primary_key, [c.name for c in t.columns]) for name, t in system.items()})
        if launcher == "migrate.sh":
            tables.update(system)
    if metadata[0] != metadata[1]:
        raise ValueError("migration launchers disagree about system metadata")
    result = {}
    for name, table in sorted(tables.items()):
        if not table.primary_key:
            raise ValueError(f"entity has no identifier: {name}")
        result[name] = {
            "primaryKey": table.primary_key,
            "columns": [c.name for c in table.columns],
            "multivalued": [c.name for c in table.columns if c.type_sql.endswith("[]")],
            "foreignKeys": [{
                "name": fk.name,
                "columns": fk.child_columns,
                "parent": fk.parent_table,
                "parentColumns": fk.parent_columns,
                "required": all(c in table.primary_key or table.column(c) is not None and not table.column(c).nullable for c in fk.child_columns),
                "unique": any(set(key).issubset(fk.child_columns) for key in [table.primary_key, *table.unique_sets] if key),
            } for fk in table.foreign_keys],
        }
    fingerprint = hashlib.sha256(json.dumps(result, sort_keys=True).encode()).hexdigest()
    return {"version": 1, "fingerprint": fingerprint, "tables": result}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    target = ROOT / "admin/src/modules/architecture/schema.generated.json"
    snapshot = schema_snapshot(ROOT)
    content = json.dumps(snapshot, ensure_ascii=False, indent=2) + "\n"
    if args.check:
        if not target.exists() or target.read_text(encoding="utf-8") != content:
            print("Admin Chen schema facts are stale; run python3 scripts/generate-architecture-schema.py", file=sys.stderr)
            return 1
    else:
        target.write_text(content, encoding="utf-8", newline="\n")
    print(f"Admin Chen schema facts: {len(snapshot['tables'])} entities, migration and system metadata aligned")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
