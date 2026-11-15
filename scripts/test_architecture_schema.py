from __future__ import annotations

import importlib.util
import json
from pathlib import Path
from tempfile import TemporaryDirectory
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("architecture_schema", ROOT / "scripts/generate-architecture-schema.py")
generator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generator)


class ArchitectureSchemaFactsTest(unittest.TestCase):
    def test_current_metadata_identifiers_and_one_to_one(self):
        snapshot = generator.schema_snapshot(ROOT)
        self.assertEqual(len(snapshot["tables"]), 132)
        for table in snapshot["tables"].values():
            self.assertTrue(table["primaryKey"])
            self.assertTrue(set(table["primaryKey"]).issubset(table["columns"]))
        self.assertTrue(snapshot["tables"]["ai_model_budgets"]["foreignKeys"][0]["unique"])
        self.assertEqual(snapshot["tables"]["threads"]["multivalued"], ["tags"])
        self.assertEqual(snapshot["tables"]["schema_migrations"]["primaryKey"], ["version"])

    def test_constraint_facts_do_not_leak_defaults_or_confuse_optional_participation(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "migrations").mkdir()
            (root / "scripts").mkdir()
            sql = """
            CREATE TABLE owners(a integer, b integer, PRIMARY KEY(a,b));
            CREATE TABLE children(
                a integer, b integer NOT NULL, ordinal integer,
                hidden text DEFAULT 'never-copy-this-default',
                PRIMARY KEY(ordinal,a), UNIQUE (b,a),
                FOREIGN KEY(a,b) REFERENCES owners(a,b)
            );
            CREATE TABLE optional_children(
                id integer PRIMARY KEY, a integer, b integer,
                FOREIGN KEY(a,b) REFERENCES owners(a,b)
            );
            """
            (root / "migrations/000001_fixture.up.sql").write_text(sql)
            system = """
            CREATE TABLE IF NOT EXISTS public.schema_migrations (
                version text PRIMARY KEY
            );
            CREATE TABLE IF NOT EXISTS public.schema_migration_locks (
                id integer PRIMARY KEY
            );
            """
            for name in ("migrate.sh", "migrate.ps1"):
                (root / "scripts" / name).write_text(system)
            snapshot = generator.schema_snapshot(root)
            required = snapshot["tables"]["children"]["foreignKeys"][0]
            self.assertTrue(required["required"])
            self.assertTrue(required["unique"])
            self.assertFalse(snapshot["tables"]["optional_children"]["foreignKeys"][0]["required"])
            self.assertNotIn("never-copy-this-default", json.dumps(snapshot))
            (root / "scripts/migrate.ps1").write_text(system.replace("version text", "other text"))
            with self.assertRaises(ValueError):
                generator.schema_snapshot(root)


if __name__ == "__main__":
    unittest.main()
