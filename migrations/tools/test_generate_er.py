"""CampusOS migration ER 生成器的解析与产物一致性测试。"""

from pathlib import Path
from tempfile import TemporaryDirectory
import unittest

from migrations.tools import generate_er


class GenerateERTest(unittest.TestCase):
    def test_parses_cardinality_nullability_and_referential_actions(self) -> None:
        sql = """
        CREATE TABLE public.parents (
            id bigint PRIMARY KEY
        );
        CREATE TABLE public.children (
            id bigint PRIMARY KEY,
            required_parent_id bigint NOT NULL UNIQUE,
            optional_parent_id bigint
        );
        ALTER TABLE ONLY public.children
          ADD CONSTRAINT fk_required FOREIGN KEY (required_parent_id)
          REFERENCES public.parents(id) ON DELETE CASCADE ON UPDATE RESTRICT;
        ALTER TABLE ONLY public.children
          ADD CONSTRAINT fk_optional FOREIGN KEY (optional_parent_id)
          REFERENCES public.parents(id) ON DELETE SET NULL;
        ALTER TABLE public.children
          ADD COLUMN added_parent_id bigint,
          ADD CONSTRAINT fk_added FOREIGN KEY (added_parent_id)
          REFERENCES public.parents(id) ON DELETE RESTRICT;
        """
        tables = generate_er.parse_schema(sql)
        child = tables["children"]
        required, optional, added = child.foreign_keys

        self.assertTrue(generate_er.fk_is_unique(child, required))
        self.assertFalse(generate_er.fk_nullable(child, required))
        self.assertEqual(required.on_delete, "CASCADE")
        self.assertEqual(required.on_update, "RESTRICT")
        self.assertFalse(generate_er.fk_is_unique(child, optional))
        self.assertTrue(generate_er.fk_nullable(child, optional))
        self.assertEqual(optional.on_delete, "SET NULL")
        self.assertIsNotNone(child.column("added_parent_id"))
        self.assertEqual(added.child_columns, ["added_parent_id"])
        self.assertTrue(generate_er.fk_nullable(child, added))

    def test_partial_unique_index_does_not_imply_global_one_to_one(self) -> None:
        sql = """
        CREATE TABLE parents (id bigint PRIMARY KEY);
        CREATE TABLE children (id bigint PRIMARY KEY, parent_id bigint);
        ALTER TABLE children ADD CONSTRAINT fk_child_parent
          FOREIGN KEY (parent_id) REFERENCES parents(id);
        CREATE UNIQUE INDEX uk_child_parent_active ON children(parent_id)
          WHERE parent_id IS NOT NULL;
        """
        tables = generate_er.parse_schema(sql)
        child = tables["children"]
        self.assertFalse(generate_er.fk_is_unique(child, child.foreign_keys[0]))

    def test_forward_migration_drop_and_replace_foreign_key(self) -> None:
        sql = """
        CREATE TABLE parents (id bigint PRIMARY KEY);
        CREATE TABLE children (id bigint PRIMARY KEY, parent_id bigint);
        ALTER TABLE children ADD CONSTRAINT fk_child_parent
          FOREIGN KEY (parent_id) REFERENCES parents(id) ON DELETE CASCADE;
        ALTER TABLE children DROP CONSTRAINT IF EXISTS fk_child_parent;
        """
        child = generate_er.parse_schema(sql)["children"]
        self.assertEqual(child.foreign_keys, [])

        replaced = sql + """
        ALTER TABLE children ADD CONSTRAINT fk_child_parent
          FOREIGN KEY (parent_id) REFERENCES parents(id) ON DELETE SET NULL;
        """
        child = generate_er.parse_schema(replaced)["children"]
        self.assertEqual(len(child.foreign_keys), 1)
        self.assertEqual(child.foreign_keys[0].on_delete, "SET NULL")

    def test_current_migrations_generate_three_consistent_artifacts(self) -> None:
        root = Path(__file__).resolve().parents[2]
        migrations = root / "migrations"
        files = generate_er.migration_files(migrations)
        tables = generate_er.parse_schema(generate_er.read_sql_files(migrations))
        fingerprint = generate_er.schema_fingerprint(migrations, files)

        self.assertGreater(len(tables), 0)
        self.assertGreater(sum(len(table.foreign_keys) for table in tables.values()), 0)
        for table in tables.values():
            for foreign_key in table.foreign_keys:
                self.assertIn(foreign_key.parent_table, tables)
                self.assertIsNotNone(table.column(foreign_key.child_columns[0]))

        with TemporaryDirectory() as temporary:
            out = Path(temporary)
            png_name = "CampusOS数据库ER图.png"
            svg_name = "CampusOS数据库ER图.svg"
            markdown_name = "CampusOS数据库实体关系说明.md"
            layout = generate_er.build_layout(tables)
            generate_er.render_svg(tables, layout, out / svg_name, fingerprint)
            generate_er.render_png(tables, layout, out / png_name, fingerprint)
            (out / markdown_name).write_text(
                generate_er.relationship_markdown(
                    tables,
                    migration_paths=files,
                    migrations_root=migrations,
                    fingerprint=fingerprint,
                    png_name=png_name,
                    svg_name=svg_name,
                ),
                encoding="utf-8",
            )

            self.assertEqual(
                generate_er.validate_outputs(
                    out, tables, fingerprint, png_name, svg_name, markdown_name
                ),
                [],
            )


if __name__ == "__main__":
    unittest.main()
