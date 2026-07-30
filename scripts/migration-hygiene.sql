\set ON_ERROR_STOP on

CREATE TEMP VIEW campusos_index_hygiene_catalog AS
SELECT
    index_definition.indrelid,
    index_definition.indexrelid,
    table_class.relname AS table_name,
    index_class.relname AS index_name,
    index_definition.indisunique,
    index_definition.indisprimary,
    index_definition.indnkeyatts,
    index_definition.indkey::text AS keys,
    index_definition.indclass::text AS classes,
    index_definition.indcollation::text AS collations,
    index_definition.indoption::text AS options,
    COALESCE(pg_get_expr(index_definition.indexprs, index_definition.indrelid), '') AS expressions,
    COALESCE(pg_get_expr(index_definition.indpred, index_definition.indrelid), '') AS predicate,
    access_method.amname
FROM pg_index index_definition
INNER JOIN pg_class table_class ON table_class.oid = index_definition.indrelid
INNER JOIN pg_class index_class ON index_class.oid = index_definition.indexrelid
INNER JOIN pg_namespace table_namespace ON table_namespace.oid = table_class.relnamespace
INNER JOIN pg_am access_method ON access_method.oid = index_class.relam
WHERE table_namespace.nspname = 'public';

CREATE TEMP VIEW campusos_constraint_hygiene_catalog AS
SELECT
    constraint_definition.oid,
    constraint_definition.conrelid,
    table_class.relname AS table_name,
    constraint_definition.conname,
    constraint_definition.contype,
    constraint_definition.conkey::text AS keys,
    constraint_definition.confrelid,
    constraint_definition.confkey::text AS referenced_keys,
    pg_get_constraintdef(constraint_definition.oid) AS definition
FROM pg_constraint constraint_definition
INNER JOIN pg_class table_class ON table_class.oid = constraint_definition.conrelid
INNER JOIN pg_namespace table_namespace ON table_namespace.oid = table_class.relnamespace
WHERE table_namespace.nspname = 'public';

CREATE TEMP TABLE campusos_schema_hygiene_issues (
    issue_type  TEXT NOT NULL,
    table_name  TEXT NOT NULL,
    object_name TEXT NOT NULL,
    covered_by  TEXT NOT NULL
);

INSERT INTO campusos_schema_hygiene_issues (issue_type, table_name, object_name, covered_by)
SELECT
    'duplicate_index',
    shorter.table_name,
    shorter.index_name,
    longer.index_name
FROM campusos_index_hygiene_catalog shorter
INNER JOIN campusos_index_hygiene_catalog longer
    ON longer.indrelid = shorter.indrelid
   AND longer.indexrelid > shorter.indexrelid
   AND longer.indisunique = shorter.indisunique
   AND longer.indisprimary = shorter.indisprimary
   AND longer.indnkeyatts = shorter.indnkeyatts
   AND longer.keys = shorter.keys
   AND longer.classes = shorter.classes
   AND longer.collations = shorter.collations
   AND longer.options = shorter.options
   AND longer.expressions = shorter.expressions
   AND longer.predicate = shorter.predicate
   AND longer.amname = shorter.amname;

INSERT INTO campusos_schema_hygiene_issues (issue_type, table_name, object_name, covered_by)
SELECT
    'redundant_btree_coverage',
    shorter.table_name,
    shorter.index_name,
    longer.index_name
FROM campusos_index_hygiene_catalog shorter
INNER JOIN campusos_index_hygiene_catalog longer
    ON longer.indrelid = shorter.indrelid
   AND longer.indexrelid <> shorter.indexrelid
   AND longer.amname = 'btree'
   AND shorter.amname = 'btree'
   AND longer.predicate = shorter.predicate
   AND longer.expressions = shorter.expressions
   AND (
       (
           longer.keys LIKE shorter.keys || ' %'
           AND longer.classes LIKE shorter.classes || ' %'
           AND longer.collations LIKE shorter.collations || ' %'
           AND longer.options LIKE shorter.options || ' %'
       )
       OR (
           longer.indisunique
           AND longer.keys = shorter.keys
           AND longer.classes = shorter.classes
           AND longer.collations = shorter.collations
           AND longer.options = shorter.options
       )
   )
WHERE NOT shorter.indisunique
  AND NOT shorter.indisprimary;

INSERT INTO campusos_schema_hygiene_issues (issue_type, table_name, object_name, covered_by)
SELECT
    'duplicate_constraint',
    first_constraint.table_name,
    first_constraint.conname,
    second_constraint.conname
FROM campusos_constraint_hygiene_catalog first_constraint
INNER JOIN campusos_constraint_hygiene_catalog second_constraint
   ON second_constraint.conrelid = first_constraint.conrelid
   AND second_constraint.oid > first_constraint.oid
   AND second_constraint.contype = first_constraint.contype
   AND second_constraint.keys IS NOT DISTINCT FROM first_constraint.keys
   AND second_constraint.confrelid = first_constraint.confrelid
   AND second_constraint.referenced_keys IS NOT DISTINCT FROM first_constraint.referenced_keys
   AND second_constraint.definition = first_constraint.definition;

SELECT jsonb_pretty(jsonb_build_object(
    'schema_hygiene', 'v0.13-index-v1',
    'database', current_database(),
    'checked_at', now(),
    'issues', COALESCE(
        jsonb_agg(to_jsonb(campusos_schema_hygiene_issues)
            ORDER BY issue_type, table_name, object_name, covered_by),
        '[]'::jsonb
    ),
    'total_issues', count(*)
)) AS migration_hygiene
FROM campusos_schema_hygiene_issues;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM campusos_schema_hygiene_issues) THEN
        RAISE EXCEPTION 'migration hygiene failed; duplicate or redundant schema objects remain';
    END IF;
END $$;
