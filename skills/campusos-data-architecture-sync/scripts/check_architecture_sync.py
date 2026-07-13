#!/usr/bin/env python3
"""Check that the Admin data-architecture view covers repository schema tables."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path


CREATE_TABLE = re.compile(r"CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(", re.IGNORECASE)
FOREIGN_KEY = re.compile(
    r"FOREIGN\s+KEY\s*\((?P<columns>[^)]+)\)\s*REFERENCES\s+(?P<table>[A-Za-z_][A-Za-z0-9_]*)",
    re.IGNORECASE,
)


def read(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8")
    except OSError as exc:
        raise RuntimeError(f"cannot read {path}: {exc}") from exc


def block(source: str, start_marker: str, end_marker: str) -> str:
    start = source.find(start_marker)
    end = source.find(end_marker, start)
    if start < 0 or end < 0:
        raise RuntimeError(f"cannot find architecture-view block: {start_marker} ... {end_marker}")
    return source[start:end]


def migration_schema(root: Path) -> tuple[set[str], list[dict[str, str]], list[str]]:
    migrations = sorted((root / "migrations").glob("*.up.sql"))
    if not migrations:
        raise RuntimeError("no migrations/*.up.sql files found")

    tables: set[str] = set()
    foreign_keys: list[dict[str, str]] = []
    versions: list[str] = []
    for path in migrations:
        source = read(path)
        versions.append(path.name.split("_", 1)[0])
        tables.update(match.group(1) for match in CREATE_TABLE.finditer(source))
        for match in FOREIGN_KEY.finditer(source):
            foreign_keys.append({"file": path.name, "columns": match.group("columns"), "target": match.group("table")})
    return tables, foreign_keys, versions


def system_tables(root: Path) -> set[str]:
    migration_scripts = [root / "scripts/migrate.sh", root / "scripts/migrate.ps1"]
    sources = [read(path) for path in migration_scripts if path.exists()]
    return {"schema_migrations"} if "schema_migrations" in "\n".join(sources) else set()


def architecture_view(root: Path) -> tuple[str, set[str], list[tuple[str, str, str]]]:
    source = read(root / "admin/src/modules/architecture/pages/SystemArchitectureView.vue")
    tables_block = block(source, "const databaseTables", "const relations")
    relations_block = block(source, "const relations", "const storageRows")
    tables = set(re.findall(r"\bname:\s*'([A-Za-z_][A-Za-z0-9_]*)'", tables_block))
    relations = [
        (match.group("id"), match.group("source"), match.group("target"))
        for match in re.finditer(
            r"\bid:\s*'(?P<id>[^']+)'\s*,\s*source:\s*'(?P<source>[A-Za-z_][A-Za-z0-9_]*)'\s*,\s*target:\s*'(?P<target>[A-Za-z_][A-Za-z0-9_]*)'",
            relations_block,
        )
    ]
    return source, tables, relations


def report(root: Path) -> dict[str, object]:
    migration_tables, foreign_keys, versions = migration_schema(root)
    expected_tables = migration_tables | system_tables(root)
    view_source, view_tables, relations = architecture_view(root)
    relation_errors = [
        {"id": identifier, "source": source, "target": target}
        for identifier, source, target in relations
        if source not in expected_tables or target not in expected_tables
    ]
    connected = {name for _, source, target in relations for name in (source, target)}
    latest_version = versions[-1]
    advertised = re.search(r"迁移\s+000001\s+-\s+(\d{6})", view_source)
    return {
        "migration_count": len(versions),
        "migration_versions": versions,
        "schema_tables": sorted(expected_tables),
        "view_tables": sorted(view_tables),
        "missing_from_view": sorted(expected_tables - view_tables),
        "stale_in_view": sorted(view_tables - expected_tables),
        "unknown_relation_endpoints": relation_errors,
        "unconnected_tables": sorted(expected_tables - connected),
        "explicit_foreign_keys": foreign_keys,
        "latest_migration": latest_version,
        "advertised_latest_migration": advertised.group(1) if advertised else "",
        "migration_header_current": bool(advertised and advertised.group(1) == latest_version),
        "migration_timeline_current": f"version: '{latest_version}'" in view_source,
        "foreign_key_wording_current": not foreign_keys or "迁移中没有声明外键" not in view_source,
    }


def print_text(result: dict[str, object]) -> None:
    def line(label: str, values: object) -> None:
        rendered = ", ".join(values) if isinstance(values, list) and values else "none"
        print(f"{label}: {rendered}")

    print("CampusOS data architecture sync report")
    print(f"migrations: {result['migration_count']} ({', '.join(result['migration_versions'])})")
    print(f"schema tables: {len(result['schema_tables'])}")
    print(f"architecture-view tables: {len(result['view_tables'])}")
    line("missing from view", result["missing_from_view"])
    line("stale in view", result["stale_in_view"])
    line("unconnected tables (review only)", result["unconnected_tables"])
    print(
        "latest migration metadata: "
        f"repository={result['latest_migration']} view={result['advertised_latest_migration'] or 'missing'}"
    )
    if result["explicit_foreign_keys"]:
        print("explicit foreign keys: present; review the Admin page wording and relation labels")
    else:
        print("explicit foreign keys: none in migrations; logical-relation wording is appropriate")
    if result["unknown_relation_endpoints"]:
        print("unknown relation endpoints:")
        for relation in result["unknown_relation_endpoints"]:
            print(f"  - {relation['id']}: {relation['source']} -> {relation['target']}")


def main() -> int:
    parser = argparse.ArgumentParser(description="Check CampusOS migration and Admin architecture-view drift.")
    parser.add_argument("--root", default=".", help="CampusOS repository root (default: current directory)")
    parser.add_argument("--json", action="store_true", help="Print the report as JSON")
    args = parser.parse_args()
    try:
        result = report(Path(args.root).resolve())
    except RuntimeError as exc:
        print(f"architecture sync check failed: {exc}", file=sys.stderr)
        return 2

    if args.json:
        print(json.dumps(result, ensure_ascii=False, indent=2))
    else:
        print_text(result)

    if (
        result["missing_from_view"]
        or result["stale_in_view"]
        or result["unknown_relation_endpoints"]
        or not result["migration_header_current"]
        or not result["migration_timeline_current"]
        or not result["foreign_key_wording_current"]
    ):
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
