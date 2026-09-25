#!/usr/bin/env python3
"""Generate CampusOS ER diagrams directly from PostgreSQL migration SQL.

Usage (run from repository root):
    python tools/db/generate_er.py --migrations migrations --out docs/database/er

Outputs:
    campusos_er_full.dot / .svg       Full field-level ER diagram
    campusos_er_overview.dot / .svg   Table-level overview
    relations.md                      1:1, 1:N, M:N relationship report
    schema.json                       Parsed machine-readable schema
    domains/*.svg                     Detailed diagrams by business domain

No Python third-party package is required. SVG rendering uses the Graphviz `dot`
command if installed; DOT files are always generated.
"""
from __future__ import annotations

import argparse
import html
import json
import re
import shutil
import subprocess
import sys
from collections import defaultdict
from dataclasses import dataclass, field, asdict
from pathlib import Path
from typing import Iterable, Optional


# ---------- Model ----------

@dataclass
class Column:
    name: str
    type_sql: str
    nullable: bool = True
    default: Optional[str] = None
    pk: bool = False
    fk: bool = False
    unique: bool = False


@dataclass
class ForeignKey:
    name: str
    child_table: str
    child_columns: list[str]
    parent_table: str
    parent_columns: list[str]
    on_delete: str = ""
    on_update: str = ""


@dataclass
class Table:
    name: str
    columns: list[Column] = field(default_factory=list)
    primary_key: list[str] = field(default_factory=list)
    unique_sets: list[list[str]] = field(default_factory=list)
    foreign_keys: list[ForeignKey] = field(default_factory=list)

    def column(self, name: str) -> Optional[Column]:
        return next((c for c in self.columns if c.name == name), None)


# ---------- SQL parsing ----------

IDENT = r'(?:"(?:[^"]|"")*"|[A-Za-z_][A-Za-z0-9_$]*)'
TABLE_REF = rf'(?:(?P<schema>{IDENT})\.)?(?P<table>{IDENT})'


def unquote_ident(value: str) -> str:
    value = value.strip()
    if value.startswith('"') and value.endswith('"'):
        return value[1:-1].replace('""', '"')
    return value


def normalize_table_ref(value: str) -> str:
    parts = [unquote_ident(x) for x in value.strip().split('.')]
    return parts[-1]


def read_sql_files(migrations: Path, sql_mode: str = 'up', include_down: bool = False) -> str:
    """Read schema-producing SQL recursively.

    sql_mode='up'  -> **/*.up.sql (recommended for migration repositories)
    sql_mode='all' -> **/*.sql, excluding *.down.sql unless --include-down is used
    """
    pattern = '*.up.sql' if sql_mode == 'up' else '*.sql'
    files = sorted(p for p in migrations.rglob(pattern) if p.is_file())
    if sql_mode == 'all' and not include_down:
        files = [p for p in files if not p.name.lower().endswith('.down.sql')]
    if not files:
        detail = '*.up.sql' if sql_mode == 'up' else '*.sql'
        raise SystemExit(f"No {detail} files found under {migrations}")
    chunks = []
    for path in files:
        chunks.append(f"\n-- SOURCE: {path.as_posix()}\n")
        try:
            chunks.append(path.read_text(encoding='utf-8-sig'))
        except UnicodeDecodeError as exc:
            raise SystemExit(f"SQL file is not UTF-8: {path} ({exc})") from exc
    return '\n'.join(chunks)


def strip_sql_comments(sql: str) -> str:
    result: list[str] = []
    i = 0
    while i < len(sql):
        ch = sql[i]
        if ch == "'":
            end = skip_single_quoted(sql, i)
            result.append(sql[i:end])
            i = end
            continue
        if ch == '"':
            end = skip_double_quoted(sql, i)
            result.append(sql[i:end])
            i = end
            continue
        if ch == '$':
            end = skip_dollar_quoted(sql, i)
            if end != i:
                result.append(sql[i:end])
                i = end
                continue
        if ch == '-' and i + 1 < len(sql) and sql[i + 1] == '-':
            end = sql.find('\n', i + 2)
            result.append('\n')
            i = len(sql) if end < 0 else end + 1
            continue
        if ch == '/' and i + 1 < len(sql) and sql[i + 1] == '*':
            end = sql.find('*/', i + 2)
            if end < 0:
                result.append('\n' * sql[i:].count('\n'))
                break
            result.append('\n' * sql[i:end + 2].count('\n'))
            i = end + 2
            continue
        result.append(ch)
        i += 1
    return ''.join(result)


def skip_single_quoted(sql: str, i: int) -> int:
    i += 1
    while i < len(sql):
        if sql[i] == "'":
            if i + 1 < len(sql) and sql[i + 1] == "'":
                i += 2
                continue
            return i + 1
        i += 1
    return i


def skip_double_quoted(sql: str, i: int) -> int:
    i += 1
    while i < len(sql):
        if sql[i] == '"':
            if i + 1 < len(sql) and sql[i + 1] == '"':
                i += 2
                continue
            return i + 1
        i += 1
    return i


def skip_dollar_quoted(sql: str, i: int) -> int:
    m = re.match(r'\$[A-Za-z0-9_]*\$', sql[i:])
    if not m:
        return i
    tag = m.group(0)
    end = sql.find(tag, i + len(tag))
    return len(sql) if end < 0 else end + len(tag)


def find_matching_paren(sql: str, open_pos: int) -> int:
    depth = 0
    i = open_pos
    while i < len(sql):
        ch = sql[i]
        if ch == "'":
            i = skip_single_quoted(sql, i)
            continue
        if ch == '"':
            i = skip_double_quoted(sql, i)
            continue
        if ch == '$':
            j = skip_dollar_quoted(sql, i)
            if j != i:
                i = j
                continue
        if ch == '-' and i + 1 < len(sql) and sql[i + 1] == '-':
            eol = sql.find('\n', i + 2)
            i = len(sql) if eol < 0 else eol + 1
            continue
        if ch == '/' and i + 1 < len(sql) and sql[i + 1] == '*':
            end = sql.find('*/', i + 2)
            i = len(sql) if end < 0 else end + 2
            continue
        if ch == '(':
            depth += 1
        elif ch == ')':
            depth -= 1
            if depth == 0:
                return i
        i += 1
    raise ValueError("Unbalanced parentheses while parsing CREATE TABLE")


def split_top_level(text: str, delimiter: str = ',') -> list[str]:
    parts, start, depth, i = [], 0, 0, 0
    while i < len(text):
        ch = text[i]
        if ch == "'":
            i = skip_single_quoted(text, i)
            continue
        if ch == '"':
            i = skip_double_quoted(text, i)
            continue
        if ch == '$':
            j = skip_dollar_quoted(text, i)
            if j != i:
                i = j
                continue
        if ch == '(':
            depth += 1
        elif ch == ')':
            depth -= 1
        elif ch == delimiter and depth == 0:
            parts.append(text[start:i].strip())
            start = i + 1
        i += 1
    tail = text[start:].strip()
    if tail:
        parts.append(tail)
    return parts


def find_create_tables(sql: str) -> Iterable[tuple[str, str]]:
    pat = re.compile(rf'\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?({IDENT}(?:\s*\.\s*{IDENT})?)\s*\(', re.I)
    for m in pat.finditer(sql):
        table_name = normalize_table_ref(re.sub(r'\s+', '', m.group(1)))
        open_pos = sql.find('(', m.start())
        close_pos = find_matching_paren(sql, open_pos)
        yield table_name, sql[open_pos + 1:close_pos]


CLAUSE_WORDS = {
    'NOT', 'NULL', 'DEFAULT', 'CONSTRAINT', 'CHECK', 'REFERENCES', 'PRIMARY',
    'UNIQUE', 'COLLATE', 'GENERATED', 'IDENTITY'
}


def first_identifier(item: str) -> tuple[str, str]:
    item = item.lstrip()
    if item.startswith('"'):
        i = skip_double_quoted(item, 0)
        return unquote_ident(item[:i]), item[i:].lstrip()
    m = re.match(r'([A-Za-z_][A-Za-z0-9_$]*)\s+(.*)', item, re.S)
    if not m:
        raise ValueError(f"Cannot parse column definition: {item[:100]}")
    return m.group(1), m.group(2)


def parse_type_and_tail(rest: str) -> tuple[str, str]:
    depth, i = 0, 0
    while i < len(rest):
        ch = rest[i]
        if ch == "'":
            i = skip_single_quoted(rest, i)
            continue
        if ch == '"':
            i = skip_double_quoted(rest, i)
            continue
        if ch == '(':
            depth += 1
        elif ch == ')':
            depth -= 1
        elif ch.isspace() and depth == 0:
            j = i
            while j < len(rest) and rest[j].isspace():
                j += 1
            m = re.match(r'([A-Za-z_]+)', rest[j:])
            if m and m.group(1).upper() in CLAUSE_WORDS:
                return rest[:i].strip(), rest[j:].strip()
        i += 1
    return rest.strip(), ''


def parse_column(item: str) -> Optional[Column]:
    upper = item.lstrip().upper()
    if upper.startswith(('CONSTRAINT ', 'PRIMARY KEY', 'UNIQUE ', 'FOREIGN KEY', 'CHECK ', 'EXCLUDE ')):
        return None
    name, rest = first_identifier(item)
    type_sql, tail = parse_type_and_tail(rest)
    if not type_sql:
        return None
    nullable = not bool(re.search(r'\bNOT\s+NULL\b', tail, re.I))
    default = None
    m = re.search(r'\bDEFAULT\s+(.+?)(?=\s+(?:NOT\s+NULL|NULL|CONSTRAINT|CHECK|REFERENCES|PRIMARY\s+KEY|UNIQUE)\b|$)', tail, re.I | re.S)
    if m:
        default = re.sub(r'\s+', ' ', m.group(1).strip())
    return Column(
        name=name,
        type_sql=re.sub(r'\s+', ' ', type_sql),
        nullable=nullable,
        default=default,
        pk=bool(re.search(r'\bPRIMARY\s+KEY\b', tail, re.I)),
        unique=bool(re.search(r'\bUNIQUE\b', tail, re.I)),
    )


def parse_column_list(value: str) -> list[str]:
    return [unquote_ident(x.strip()) for x in split_top_level(value) if x.strip()]


def parse_inline_constraints(table: Table, body_items: list[str]) -> None:
    for item in body_items:
        text = re.sub(r'\s+', ' ', item.strip())
        m = re.search(r'\bPRIMARY\s+KEY\s*\((.*?)\)', text, re.I | re.S)
        if m:
            table.primary_key = parse_column_list(m.group(1))
        m = re.search(r'\bUNIQUE\s*\((.*?)\)', text, re.I | re.S)
        if m:
            cols = parse_column_list(m.group(1))
            if cols not in table.unique_sets:
                table.unique_sets.append(cols)
        m = re.search(
            rf'(?:CONSTRAINT\s+({IDENT})\s+)?FOREIGN\s+KEY\s*\((.*?)\)\s+REFERENCES\s+({IDENT}(?:\.{IDENT})?)\s*\((.*?)\)(.*)$',
            text, re.I | re.S)
        if m:
            tail = m.group(5)
            table.foreign_keys.append(ForeignKey(
                name=unquote_ident(m.group(1)) if m.group(1) else f"fk_{table.name}_{len(table.foreign_keys)+1}",
                child_table=table.name,
                child_columns=parse_column_list(m.group(2)),
                parent_table=normalize_table_ref(m.group(3)),
                parent_columns=parse_column_list(m.group(4)),
                on_delete=(re.search(r'\bON\s+DELETE\s+(CASCADE|RESTRICT|SET\s+NULL|SET\s+DEFAULT|NO\s+ACTION)', tail, re.I) or [None, ''])[1].upper(),
                on_update=(re.search(r'\bON\s+UPDATE\s+(CASCADE|RESTRICT|SET\s+NULL|SET\s+DEFAULT|NO\s+ACTION)', tail, re.I) or [None, ''])[1].upper(),
            ))


def parse_alter_constraints(sql: str, tables: dict[str, Table]) -> None:
    # PostgreSQL pg_dump emits most constraints as ALTER TABLE ... ADD CONSTRAINT.
    pat = re.compile(
        rf'ALTER\s+TABLE(?:\s+ONLY)?\s+({IDENT}(?:\.{IDENT})?)\s+ADD\s+CONSTRAINT\s+({IDENT})\s+(.+?);',
        re.I | re.S
    )
    for m in pat.finditer(sql):
        table_name = normalize_table_ref(m.group(1))
        if table_name not in tables:
            continue
        cname = unquote_ident(m.group(2))
        expr = re.sub(r'\s+', ' ', m.group(3).strip())
        table = tables[table_name]

        pk = re.match(r'PRIMARY\s+KEY\s*\((.*?)\)', expr, re.I | re.S)
        if pk:
            table.primary_key = parse_column_list(pk.group(1))
            continue

        uq = re.match(r'UNIQUE\s*\((.*?)\)', expr, re.I | re.S)
        if uq:
            cols = parse_column_list(uq.group(1))
            if cols not in table.unique_sets:
                table.unique_sets.append(cols)
            continue

        fk = re.match(
            rf'FOREIGN\s+KEY\s*\((.*?)\)\s+REFERENCES\s+({IDENT}(?:\.{IDENT})?)\s*\((.*?)\)(.*)$',
            expr, re.I | re.S
        )
        if fk:
            tail = fk.group(4)
            delete_m = re.search(r'\bON\s+DELETE\s+(CASCADE|RESTRICT|SET\s+NULL|SET\s+DEFAULT|NO\s+ACTION)', tail, re.I)
            update_m = re.search(r'\bON\s+UPDATE\s+(CASCADE|RESTRICT|SET\s+NULL|SET\s+DEFAULT|NO\s+ACTION)', tail, re.I)
            table.foreign_keys.append(ForeignKey(
                name=cname,
                child_table=table_name,
                child_columns=parse_column_list(fk.group(1)),
                parent_table=normalize_table_ref(fk.group(2)),
                parent_columns=parse_column_list(fk.group(3)),
                on_delete=delete_m.group(1).upper() if delete_m else '',
                on_update=update_m.group(1).upper() if update_m else '',
            ))


def parse_unique_indexes(sql: str, tables: dict[str, Table]) -> None:
    # Only unconditional UNIQUE indexes imply global uniqueness and therefore 1:1 cardinality.
    pat = re.compile(
        rf'CREATE\s+UNIQUE\s+INDEX\s+{IDENT}\s+ON\s+({IDENT}(?:\.{IDENT})?)\s+(?:USING\s+\w+\s+)?\((.*?)\)(.*?);',
        re.I | re.S
    )
    for m in pat.finditer(sql):
        table_name = normalize_table_ref(m.group(1))
        if table_name not in tables:
            continue
        tail = m.group(3)
        if re.search(r'\bWHERE\b', tail, re.I):
            continue
        raw_cols = split_top_level(m.group(2))
        # Ignore expression indexes; only plain identifiers are safe for cardinality inference.
        cols = []
        safe = True
        for raw in raw_cols:
            raw = raw.strip()
            if re.fullmatch(IDENT, raw):
                cols.append(unquote_ident(raw))
            else:
                safe = False
                break
        if safe and cols and cols not in tables[table_name].unique_sets:
            tables[table_name].unique_sets.append(cols)


def parse_schema(sql: str) -> dict[str, Table]:
    sql = strip_sql_comments(sql)
    tables: dict[str, Table] = {}
    for table_name, body in find_create_tables(sql):
        body_items = split_top_level(body)
        table = Table(name=table_name)
        for item in body_items:
            col = parse_column(item)
            if col:
                table.columns.append(col)
                if col.pk:
                    table.primary_key.append(col.name)
                if col.unique:
                    table.unique_sets.append([col.name])
        parse_inline_constraints(table, body_items)
        tables[table_name] = table

    parse_alter_constraints(sql, tables)
    parse_unique_indexes(sql, tables)

    # Decorate columns with PK/FK/UK flags.
    for table in tables.values():
        pk = set(table.primary_key)
        uk_cols = {c for s in table.unique_sets for c in s if len(s) == 1}
        fk_cols = {c for fk in table.foreign_keys for c in fk.child_columns}
        for col in table.columns:
            col.pk = col.name in pk
            col.fk = col.name in fk_cols
            col.unique = col.unique or col.name in uk_cols
    return tables


# ---------- Business-domain grouping ----------

DOMAIN_ORDER = [
    'Identity & Access',
    'Community & Content',
    'Academic & Schedule',
    'User Space & Documents',
    'Storage',
    'Plugin Ecosystem & Authorization',
    'Platform Reliability & Integration',
    'Migration Metadata',
    'Other',
]

DOMAIN_COLORS = {
    'Identity & Access': '#DCEBFF',
    'Community & Content': '#E3F4E8',
    'Academic & Schedule': '#FFF0D8',
    'User Space & Documents': '#EEE5FF',
    'Storage': '#DFF5F2',
    'Plugin Ecosystem & Authorization': '#FFE1E7',
    'Platform Reliability & Integration': '#ECEFF3',
    'Migration Metadata': '#F5F0D8',
    'Other': '#F3F3F3',
}


def classify_table(name: str) -> str:
    if name in {'schema_migrations', 'schema_migration_locks'}:
        return 'Migration Metadata'
    if name.startswith('plugin_') or name == 'plugins':
        return 'Plugin Ecosystem & Authorization'
    if name.startswith(('storage_', 'user_storage_')) or name == 'storage_objects':
        return 'Storage'
    if name.startswith(('personal_document', 'user_space')):
        return 'User Space & Documents'
    if name.startswith(('academic_term', 'user_schedule')):
        return 'Academic & Schedule'
    if name.startswith(('identity_', 'mfa_', 'email_', 'auth_')) or name in {
        'users', 'accounts', 'sessions', 'roles', 'user_roles', 'role_permissions',
        'permission_definitions', 'api_keys', 'authorization_audits'
    }:
        return 'Identity & Access'
    if name.startswith(('category_', 'richtext_', 'mutual_aid', 'secondhand')) or name in {
        'categories', 'threads', 'posts', 'likes', 'notifications'
    }:
        return 'Community & Content'
    if name.startswith(('webhook_', 'message_', 'mcp_', 'outbox_', 'job_', 'ai_')) or name in {
        'audit_logs', 'configurations', 'builtin_feature_states', 'feature_states',
        'system_settings', 'operation_logs'
    }:
        return 'Platform Reliability & Integration'
    return 'Other'


# ---------- Cardinality & M:N inference ----------

def fk_is_unique(table: Table, fk: ForeignKey) -> bool:
    cols = tuple(fk.child_columns)
    if tuple(table.primary_key) == cols:
        return True
    return any(tuple(s) == cols for s in table.unique_sets)


def fk_nullable(table: Table, fk: ForeignKey) -> bool:
    cols = [table.column(c) for c in fk.child_columns]
    return any(c is None or c.nullable for c in cols)


def infer_m2m(tables: dict[str, Table]) -> list[dict]:
    """Infer constraint-backed many-to-many relations via association tables.

    A pair is considered strong M:N when the union of the two FK column sets is
    exactly (or is contained by) a composite PK/UNIQUE set of the association table.
    """
    relations = []
    seen = set()
    for assoc in tables.values():
        fks = [fk for fk in assoc.foreign_keys if fk.parent_table in tables]
        if len(fks) < 2:
            continue
        unique_sets = [set(assoc.primary_key)] if assoc.primary_key else []
        unique_sets += [set(x) for x in assoc.unique_sets]
        for i in range(len(fks)):
            for j in range(i + 1, len(fks)):
                a, b = fks[i], fks[j]
                if a.parent_table == b.parent_table:
                    continue
                union = set(a.child_columns) | set(b.child_columns)
                strong = any(union and union.issubset(u) and len(u) <= len(union) + 1 for u in unique_sets)
                # Explicit naming heuristic for canonical association entities.
                hinted = assoc.name in {'user_roles', 'role_permissions'} or bool(
                    re.search(r'(?:_roles|_permissions|_tags|_members|_links|_bindings)$', assoc.name)
                )
                if not (strong or hinted):
                    continue
                key = tuple(sorted((a.parent_table, b.parent_table)) + [assoc.name])
                if key in seen:
                    continue
                seen.add(key)
                relations.append({
                    'left': a.parent_table,
                    'right': b.parent_table,
                    'via': assoc.name,
                    'strength': 'constraint-backed' if strong else 'association-entity inferred',
                })
    return relations


# ---------- Graphviz rendering ----------

def esc(value: object) -> str:
    return html.escape(str(value), quote=True)


def node_id(name: str) -> str:
    return 'n_' + re.sub(r'[^A-Za-z0-9_]', '_', name)


def port_id(name: str) -> str:
    return 'p_' + re.sub(r'[^A-Za-z0-9_]', '_', name)


def display_type(type_sql: str) -> str:
    replacements = {
        'character varying': 'varchar',
        'timestamp with time zone': 'timestamptz',
        'timestamp without time zone': 'timestamp',
        'double precision': 'float8',
    }
    out = type_sql
    for a, b in replacements.items():
        out = re.sub(re.escape(a), b, out, flags=re.I)
    return out


def table_label(table: Table, color: str, show_fields: bool, show_defaults: bool) -> str:
    rows = [
        f'<TR><TD BGCOLOR="{color}" COLSPAN="3"><B>{esc(table.name)}</B></TD></TR>'
    ]
    if show_fields:
        for col in table.columns:
            badges = []
            if col.pk: badges.append('PK')
            if col.fk: badges.append('FK')
            if col.unique: badges.append('UQ')
            if not col.nullable: badges.append('NN')
            badge = ' '.join(badges)
            typ = display_type(col.type_sql)
            if show_defaults and col.default:
                d = col.default if len(col.default) <= 36 else col.default[:33] + '...'
                typ += f' = {d}'
            badge_html = f'<FONT POINT-SIZE="8">{esc(badge)}</FONT>' if badge else ''
            rows.append(
                '<TR>'
                f'<TD ALIGN="LEFT">{badge_html}</TD>'
                f'<TD PORT="{port_id(col.name)}" ALIGN="LEFT"><FONT POINT-SIZE="9">{esc(col.name)}</FONT></TD>'
                f'<TD ALIGN="LEFT"><FONT POINT-SIZE="8">{esc(typ)}</FONT></TD>'
                '</TR>'
            )
    return '<<TABLE BORDER="0" CELLBORDER="1" CELLSPACING="0" CELLPADDING="3">' + ''.join(rows) + '</TABLE>>'


def build_dot(
    tables: dict[str, Table],
    *,
    title: str,
    show_fields: bool,
    show_defaults: bool,
    logical_m2m: bool,
    only_domain: Optional[str] = None,
) -> str:
    included = {n for n in tables if only_domain is None or classify_table(n) == only_domain}
    # For domain diagrams, include referenced external parents as compact stubs.
    external = set()
    if only_domain:
        for name in included:
            for fk in tables[name].foreign_keys:
                if fk.parent_table in tables and fk.parent_table not in included:
                    external.add(fk.parent_table)

    lines = [
        'digraph CampusOS_ER {',
        '  graph [rankdir=LR, bgcolor="white", pad="0.25", nodesep="0.35", ranksep="1.0",',
        '         splines=polyline, overlap=false, compound=true, concentrate=false,',
        f'         labelloc="t", label="{esc(title)}", fontsize=20, fontname="Arial"];',
        '  node [shape=plain, fontname="Arial"];',
        '  edge [fontname="Arial", fontsize=8, color="#667085", arrowsize=0.7];',
    ]

    by_domain: dict[str, list[str]] = defaultdict(list)
    for name in included:
        by_domain[classify_table(name)].append(name)

    for domain in DOMAIN_ORDER:
        names = sorted(by_domain.get(domain, []))
        if not names:
            continue
        color = DOMAIN_COLORS[domain]
        cluster_id = re.sub(r'[^A-Za-z0-9_]', '_', domain)
        lines += [
            f'  subgraph cluster_{cluster_id} {{',
            f'    label="{esc(domain)}"; color="{color}"; penwidth=2; style="rounded";',
        ]
        for name in names:
            label = table_label(tables[name], color, show_fields, show_defaults)
            lines.append(f'    {node_id(name)} [label={label}, tooltip="{esc(name)}"];')
        lines.append('  }')

    if external:
        lines += ['  subgraph cluster_external {', '    label="External references"; color="#DDDDDD"; style="dashed,rounded";']
        for name in sorted(external):
            label = table_label(tables[name], '#F7F7F7', False, False)
            lines.append(f'    {node_id(name)} [label={label}, tooltip="{esc(name)}"];')
        lines.append('  }')

    visible = included | external
    for child_name in sorted(included):
        child = tables[child_name]
        for fk in child.foreign_keys:
            if fk.parent_table not in visible:
                continue
            parent = tables.get(fk.parent_table)
            if not parent:
                continue
            one_to_one = fk_is_unique(child, fk)
            optional = fk_nullable(child, fk)
            cardinality = ('0..1' if optional else '1') if one_to_one else ('0..N' if optional else 'N')
            label = f"1 : {cardinality}"
            if fk.on_delete:
                label += f" · DEL {fk.on_delete}"

            if show_fields and fk.child_columns and fk.parent_columns:
                child_ep = f'{node_id(child_name)}:{port_id(fk.child_columns[0])}:w'
                parent_ep = f'{node_id(fk.parent_table)}:{port_id(fk.parent_columns[0])}:e'
            else:
                child_ep = node_id(child_name)
                parent_ep = node_id(fk.parent_table)

            # parent -> child, crow on child for 1:N; tee on both for 1:1
            arrowhead = 'tee' if one_to_one else 'crow'
            lines.append(
                f'  {parent_ep} -> {child_ep} '
                f'[dir=both, arrowtail=tee, arrowhead={arrowhead}, label="{esc(label)}", '
                f'tooltip="{esc(fk.name)}: {esc(child_name)}({esc(", ".join(fk.child_columns))}) → '
                f'{esc(fk.parent_table)}({esc(", ".join(fk.parent_columns))})"];'
            )

    if logical_m2m and only_domain is None:
        for rel in infer_m2m(tables):
            if rel['left'] not in included or rel['right'] not in included:
                continue
            lines.append(
                f'  {node_id(rel["left"])} -> {node_id(rel["right"])} '
                f'[dir=both, arrowtail=crow, arrowhead=crow, style=dashed, color="#9B51E0", '
                f'constraint=false, label="M:N via {esc(rel["via"])}", tooltip="{esc(rel["strength"])}"];'
            )

    lines.append('}')
    return '\n'.join(lines)


def render_dot(dot_path: Path, fmt: str = 'svg') -> Optional[Path]:
    dot_bin = shutil.which('dot')
    if not dot_bin:
        return None
    out_path = dot_path.with_suffix('.' + fmt)
    subprocess.run(
        [dot_bin, f'-T{fmt}', str(dot_path), '-o', str(out_path)],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    return out_path


# ---------- Reports ----------

def schema_to_json(tables: dict[str, Table]) -> dict:
    return {
        'table_count': len(tables),
        'foreign_key_count': sum(len(t.foreign_keys) for t in tables.values()),
        'tables': {name: asdict(t) for name, t in sorted(tables.items())},
        'logical_many_to_many': infer_m2m(tables),
    }


def relationship_markdown(tables: dict[str, Table]) -> str:
    one_to_one, one_to_many = [], []
    for child in sorted(tables.values(), key=lambda t: t.name):
        for fk in child.foreign_keys:
            if fk.parent_table not in tables:
                continue
            item = {
                'parent': fk.parent_table,
                'child': child.name,
                'fk': fk.name,
                'child_cols': ', '.join(fk.child_columns),
                'parent_cols': ', '.join(fk.parent_columns),
                'optional': fk_nullable(child, fk),
                'delete': fk.on_delete or 'NO ACTION',
            }
            (one_to_one if fk_is_unique(child, fk) else one_to_many).append(item)

    lines = [
        '# CampusOS Database Relationship Report', '',
        f'- Tables: **{len(tables)}**',
        f'- Foreign keys: **{sum(len(t.foreign_keys) for t in tables.values())}**',
        f'- Inferred 1:1 relations: **{len(one_to_one)}**',
        f'- Physical 1:N relations: **{len(one_to_many)}**',
        f'- Inferred logical M:N relations: **{len(infer_m2m(tables))}**', '',
        '> Cardinalities are inferred from actual FK + PK/UNIQUE constraints. Nullable FKs are shown as optional on the child side.', '',
        '## 1:1 / 1:0..1', '',
        '| Parent | Child | FK | Columns | Cardinality | ON DELETE |',
        '|---|---|---|---|---|---|',
    ]
    for x in one_to_one:
        card = '1 : 0..1' if x['optional'] else '1 : 1'
        lines.append(f"| `{x['parent']}` | `{x['child']}` | `{x['fk']}` | `{x['child_cols']}` → `{x['parent_cols']}` | {card} | {x['delete']} |")

    lines += ['', '## 1:N / 1:0..N', '',
              '| Parent | Child | FK | Columns | Cardinality | ON DELETE |',
              '|---|---|---|---|---|---|']
    for x in one_to_many:
        card = '1 : 0..N' if x['optional'] else '1 : N'
        lines.append(f"| `{x['parent']}` | `{x['child']}` | `{x['fk']}` | `{x['child_cols']}` → `{x['parent_cols']}` | {card} | {x['delete']} |")

    lines += ['', '## Logical M:N (through association entities)', '',
              '| Entity A | Entity B | Association table | Basis |',
              '|---|---|---|---|']
    for x in infer_m2m(tables):
        lines.append(f"| `{x['left']}` | `{x['right']}` | `{x['via']}` | {x['strength']} |")

    lines += ['', '## Business domains', '']
    grouped = defaultdict(list)
    for name in sorted(tables):
        grouped[classify_table(name)].append(name)
    for domain in DOMAIN_ORDER:
        if grouped[domain]:
            lines.append(f"### {domain}")
            lines.append(', '.join(f'`{x}`' for x in grouped[domain]))
            lines.append('')

    lines += [
        '## Reading the diagram', '',
        '- **PK**: primary-key field; **FK**: foreign-key field; **UQ**: globally unique field; **NN**: NOT NULL.',
        '- Solid gray edge: physical database FK.',
        '- Purple dashed edge: inferred logical many-to-many relation, while the physical FK edges to the association entity remain visible.',
        '- `DEL CASCADE / RESTRICT / SET NULL ...` on edges is taken from migration DDL.',
        '- Cross-domain relations remain visible in the full diagram. Per-domain SVGs add compact external-reference stubs when necessary.',
    ]
    return '\n'.join(lines) + '\n'


# ---------- CLI ----------

def main() -> int:
    ap = argparse.ArgumentParser(description='Generate CampusOS ER diagrams from PostgreSQL migration SQL.')
    ap.add_argument('--migrations', type=Path, default=Path('migrations'), help='Migration directory (default: migrations)')
    ap.add_argument('--out', type=Path, default=Path('docs/database/er'), help='Output directory')
    ap.add_argument('--format', choices=['svg', 'pdf', 'png'], default='svg', help='Graphviz render format (default: svg)')
    ap.add_argument('--sql-mode', choices=['up', 'all'], default='up',
                    help="SQL discovery mode: 'up' scans **/*.up.sql; 'all' scans **/*.sql (default: up)")
    ap.add_argument('--include-down', action='store_true',
                    help='With --sql-mode all, also read *.down.sql files (normally not recommended)')
    ap.add_argument('--show-defaults', action='store_true', help='Show column defaults in field-level diagrams')
    ap.add_argument('--no-logical-m2m', action='store_true', help='Do not draw inferred dashed M:N edges')
    args = ap.parse_args()

    sql = read_sql_files(args.migrations, sql_mode=args.sql_mode, include_down=args.include_down)
    tables = parse_schema(sql)
    if not tables:
        raise SystemExit('No CREATE TABLE statements parsed.')

    args.out.mkdir(parents=True, exist_ok=True)
    domains_out = args.out / 'domains'
    domains_out.mkdir(parents=True, exist_ok=True)

    schema_json = schema_to_json(tables)
    (args.out / 'schema.json').write_text(json.dumps(schema_json, ensure_ascii=False, indent=2), encoding='utf-8')
    (args.out / 'relations.md').write_text(relationship_markdown(tables), encoding='utf-8')

    jobs = [
        ('campusos_er_full', True, 'CampusOS v1 — Full Database ER Diagram'),
        ('campusos_er_overview', False, 'CampusOS v1 — Database Domain Overview'),
    ]
    rendered = []
    for basename, show_fields, title in jobs:
        dot = build_dot(
            tables,
            title=title,
            show_fields=show_fields,
            show_defaults=args.show_defaults,
            logical_m2m=not args.no_logical_m2m,
        )
        dot_path = args.out / f'{basename}.dot'
        dot_path.write_text(dot, encoding='utf-8')
        out = render_dot(dot_path, args.format)
        if out:
            rendered.append(out)

    for domain in DOMAIN_ORDER:
        domain_tables = [t for t in tables if classify_table(t) == domain]
        if not domain_tables:
            continue
        slug = re.sub(r'[^A-Za-z0-9]+', '_', domain).strip('_').lower()
        dot = build_dot(
            tables,
            title=f'CampusOS v1 — {domain}',
            show_fields=True,
            show_defaults=args.show_defaults,
            logical_m2m=False,
            only_domain=domain,
        )
        dot_path = domains_out / f'{slug}.dot'
        dot_path.write_text(dot, encoding='utf-8')
        out = render_dot(dot_path, args.format)
        if out:
            rendered.append(out)

    fk_count = sum(len(t.foreign_keys) for t in tables.values())
    print(f'Parsed tables: {len(tables)}')
    print(f'Parsed foreign keys: {fk_count}')
    print(f'Logical M:N relations: {len(infer_m2m(tables))}')
    print(f'Output: {args.out.resolve()}')
    if shutil.which('dot'):
        print(f'Rendered {len(rendered)} {args.format.upper()} files with Graphviz.')
    else:
        print('Graphviz `dot` was not found; DOT files were generated. Install Graphviz and rerun to render SVG/PDF/PNG.')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
