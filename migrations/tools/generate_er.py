#!/usr/bin/env python3
"""从 CampusOS PostgreSQL migration 生成一致的 ER 图与中文关系说明。

在仓库根目录运行：
    python migrations/tools/generate_er.py

默认输出：
    migrations/er/current/CampusOS数据库ER图.png
    migrations/er/current/CampusOS数据库实体关系说明.md

解析器只读取 ``*.up.sql``，不会连接或修改数据库。PNG 渲染使用 Pillow，
图像和 Markdown 由同一个内存模型生成，并写入相同的 Schema SHA-256 指纹。
"""
from __future__ import annotations

import argparse
import hashlib
import html
import re
import struct
import sys
from collections import defaultdict
from dataclasses import dataclass, field
from pathlib import Path
from tempfile import TemporaryDirectory
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


def migration_files(migrations: Path) -> list[Path]:
    files = sorted(p for p in migrations.rglob('*.up.sql') if p.is_file())
    if not files:
        raise SystemExit(f"No *.up.sql files found under {migrations}")
    return files


def read_sql_files(migrations: Path) -> str:
    files = migration_files(migrations)
    chunks = []
    for path in files:
        chunks.append(f"\n-- SOURCE: {path.as_posix()}\n")
        chunks.append(path.read_text(encoding='utf-8'))
    return '\n'.join(chunks)


def schema_fingerprint(migrations: Path, files: list[Path]) -> str:
    """Return a path-and-content fingerprint stable across operating systems."""
    digest = hashlib.sha256()
    for path in files:
        digest.update(path.relative_to(migrations).as_posix().encode('utf-8'))
        digest.update(b'\0')
        digest.update(path.read_bytes())
        digest.update(b'\0')
    return digest.hexdigest()


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


def parse_column_foreign_key(table: Table, column: Column, item: str) -> None:
    """Parse PostgreSQL column-level ``col TYPE REFERENCES parent(id)``."""
    match = re.search(
        rf'(?:CONSTRAINT\s+({IDENT})\s+)?REFERENCES\s+({IDENT}(?:\.{IDENT})?)\s*\((.*?)\)(.*)$',
        item,
        re.I | re.S,
    )
    if not match:
        return
    tail = match.group(4)
    delete_match = re.search(
        r'\bON\s+DELETE\s+(CASCADE|RESTRICT|SET\s+NULL|SET\s+DEFAULT|NO\s+ACTION)',
        tail,
        re.I,
    )
    update_match = re.search(
        r'\bON\s+UPDATE\s+(CASCADE|RESTRICT|SET\s+NULL|SET\s+DEFAULT|NO\s+ACTION)',
        tail,
        re.I,
    )
    table.foreign_keys.append(
        ForeignKey(
            name=(
                unquote_ident(match.group(1))
                if match.group(1)
                else f'{table.name}_{column.name}_fkey'
            ),
            child_table=table.name,
            child_columns=[column.name],
            parent_table=normalize_table_ref(match.group(2)),
            parent_columns=parse_column_list(match.group(3)),
            on_delete=delete_match.group(1).upper() if delete_match else '',
            on_update=update_match.group(1).upper() if update_match else '',
        )
    )


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
    # PostgreSQL permits multiple comma-separated ALTER actions in one statement,
    # for example ADD COLUMN followed by ADD CONSTRAINT.
    statement_pattern = re.compile(
        rf'ALTER\s+TABLE(?:\s+ONLY)?\s+({IDENT}(?:\.{IDENT})?)\s+(.+?);',
        re.I | re.S,
    )
    for statement in statement_pattern.finditer(sql):
        table_name = normalize_table_ref(statement.group(1))
        if table_name not in tables:
            continue
        table = tables[table_name]
        for action in split_top_level(statement.group(2)):
            drop_constraint = re.match(
                rf'DROP\s+CONSTRAINT\s+(?:IF\s+EXISTS\s+)?({IDENT})(?:\s+(?:CASCADE|RESTRICT))?$',
                action.strip(),
                re.I | re.S,
            )
            if drop_constraint:
                cname = unquote_ident(drop_constraint.group(1))
                table.foreign_keys = [fk for fk in table.foreign_keys if fk.name != cname]
                continue
            column_action = re.match(r'ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?(.+)$', action.strip(), re.I | re.S)
            if column_action:
                column = parse_column(column_action.group(1))
                if column and table.column(column.name) is None:
                    table.columns.append(column)
                    parse_column_foreign_key(table, column, column_action.group(1))
                    if column.pk:
                        table.primary_key.append(column.name)
                    if column.unique:
                        table.unique_sets.append([column.name])
                continue
            constraint = re.match(
                rf'ADD\s+CONSTRAINT\s+({IDENT})\s+(.+)$', action.strip(), re.I | re.S
            )
            if not constraint:
                continue
            cname = unquote_ident(constraint.group(1))
            expr = re.sub(r'\s+', ' ', constraint.group(2).strip())

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
                expr,
                re.I | re.S,
            )
            if fk:
                tail = fk.group(4)
                delete_m = re.search(r'\bON\s+DELETE\s+(CASCADE|RESTRICT|SET\s+NULL|SET\s+DEFAULT|NO\s+ACTION)', tail, re.I)
                update_m = re.search(r'\bON\s+UPDATE\s+(CASCADE|RESTRICT|SET\s+NULL|SET\s+DEFAULT|NO\s+ACTION)', tail, re.I)
                # PostgreSQL cannot keep two constraints with the same name. Replacing
                # an existing item also makes the generated model deterministic for a
                # DROP CONSTRAINT + ADD CONSTRAINT forward migration.
                table.foreign_keys = [item for item in table.foreign_keys if item.name != cname]
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
    tables: dict[str, Table] = {}
    for table_name, body in find_create_tables(sql):
        body_items = split_top_level(body)
        table = Table(name=table_name)
        for item in body_items:
            col = parse_column(item)
            if col:
                table.columns.append(col)
                parse_column_foreign_key(table, col, item)
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

DOMAIN_LABELS = {
    'Identity & Access': '身份与访问控制',
    'Community & Content': '社区与内容',
    'Academic & Schedule': '学期与日程',
    'User Space & Documents': '个人空间与文档',
    'Storage': '统一存储',
    'Plugin Ecosystem & Authorization': '插件生态与授权',
    'Platform Reliability & Integration': '平台可靠性与集成',
    'Migration Metadata': '迁移元数据',
    'Other': '其他平台数据',
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


# ---------- Dependency-free SVG + Pillow PNG rendering ----------

CARD_WIDTH = 570
LANE_GAP = 48
CARD_GAP = 16
PAGE_MARGIN = 64
TITLE_HEIGHT = 112
DOMAIN_HEADER_HEIGHT = 46
ROW_HEIGHT = 25
CARD_HEADER_HEIGHT = 40


def diagram_columns(table: Table, limit: int = 7) -> tuple[list[Column], int]:
    """Select relationship-bearing fields for the overview without hiding entities."""
    selected = [column for column in table.columns if column.pk or column.fk]
    for column in table.columns:
        if len(selected) >= limit:
            break
        if column.unique and column not in selected:
            selected.append(column)
    if not selected:
        selected = table.columns[:2]
    selected = sorted(selected[:limit], key=lambda column: table.columns.index(column))
    return selected, max(0, len(table.columns) - len(selected))


def build_layout(tables: dict[str, Table]) -> dict:
    domains = [domain for domain in DOMAIN_ORDER if any(classify_table(name) == domain for name in tables)]
    positions: dict[str, dict] = {}
    max_bottom = TITLE_HEIGHT
    for lane_index, domain in enumerate(domains):
        x = PAGE_MARGIN + lane_index * (CARD_WIDTH + LANE_GAP)
        y = TITLE_HEIGHT + DOMAIN_HEADER_HEIGHT
        for table_name in sorted(name for name in tables if classify_table(name) == domain):
            rows, omitted = diagram_columns(tables[table_name])
            row_count = len(rows) + (1 if omitted else 0)
            height = CARD_HEADER_HEIGHT + 12 + row_count * ROW_HEIGHT
            positions[table_name] = {
                'x': x,
                'y': y,
                'width': CARD_WIDTH,
                'height': height,
                'rows': rows,
                'omitted': omitted,
                'domain': domain,
            }
            y += height + CARD_GAP
        max_bottom = max(max_bottom, y)
    width = PAGE_MARGIN * 2 + len(domains) * CARD_WIDTH + max(0, len(domains) - 1) * LANE_GAP
    height = max_bottom + 120
    return {'domains': domains, 'positions': positions, 'width': width, 'height': height}


def edge_points(parent_box: dict, child_box: dict, bend: int = 0) -> list[tuple[float, float]]:
    """Return a cubic Bezier polyline routed behind table cards."""
    if parent_box['x'] < child_box['x']:
        start = (parent_box['x'] + parent_box['width'], parent_box['y'] + parent_box['height'] / 2)
        end = (child_box['x'], child_box['y'] + child_box['height'] / 2)
        control_x = (start[0] + end[0]) / 2
        c1, c2 = (control_x, start[1]), (control_x, end[1])
    elif parent_box['x'] > child_box['x']:
        start = (parent_box['x'], parent_box['y'] + parent_box['height'] / 2)
        end = (child_box['x'] + child_box['width'], child_box['y'] + child_box['height'] / 2)
        control_x = (start[0] + end[0]) / 2
        c1, c2 = (control_x, start[1]), (control_x, end[1])
    else:
        route_x = parent_box['x'] + parent_box['width'] + 22 + (bend % 5) * 8
        start = (parent_box['x'] + parent_box['width'], parent_box['y'] + parent_box['height'] / 2)
        end = (child_box['x'] + child_box['width'], child_box['y'] + child_box['height'] / 2)
        c1, c2 = (route_x, start[1]), (route_x, end[1])

    points = []
    for step in range(25):
        t = step / 24
        mt = 1 - t
        x = mt ** 3 * start[0] + 3 * mt ** 2 * t * c1[0] + 3 * mt * t ** 2 * c2[0] + t ** 3 * end[0]
        y = mt ** 3 * start[1] + 3 * mt ** 2 * t * c1[1] + 3 * mt * t ** 2 * c2[1] + t ** 3 * end[1]
        points.append((x, y))
    return points


def column_badges(column: Column) -> str:
    badges = []
    if column.pk:
        badges.append('PK')
    if column.fk:
        badges.append('FK')
    if column.unique:
        badges.append('UQ')
    if not column.nullable:
        badges.append('NN')
    return '/'.join(badges) or '—'


def render_svg(tables: dict[str, Table], layout: dict, target: Path, fingerprint: str) -> None:
    width, height = layout['width'], layout['height']
    positions = layout['positions']
    lines = [
        '<?xml version="1.0" encoding="UTF-8"?>',
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}">',
        f'<metadata>campusos_schema_sha256={fingerprint};tables={len(tables)};foreign_keys={sum(len(t.foreign_keys) for t in tables.values())}</metadata>',
        '<defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 Z" fill="#667085"/></marker></defs>',
        '<rect width="100%" height="100%" fill="#f8fafc"/>',
        '<g font-family="Segoe UI, Microsoft YaHei, Noto Sans CJK SC, sans-serif">',
        f'<text x="{PAGE_MARGIN}" y="48" font-size="30" font-weight="700" fill="#101828">CampusOS 数据库实体关系图</text>',
        f'<text x="{PAGE_MARGIN}" y="80" font-size="17" fill="#475467">{len(tables)} 张实体表 · {sum(len(t.foreign_keys) for t in tables.values())} 条物理外键 · 字段行优先展示 PK/FK/UQ</text>',
    ]

    for index, child in enumerate(sorted(tables.values(), key=lambda table: table.name)):
        for fk in child.foreign_keys:
            if fk.parent_table not in positions:
                continue
            points = edge_points(positions[fk.parent_table], positions[child.name], index)
            path = ' '.join((f'M {points[0][0]:.1f},{points[0][1]:.1f}',) + tuple(f'L {x:.1f},{y:.1f}' for x, y in points[1:]))
            card = '父 1 : 子 0..1' if fk_is_unique(child, fk) else '父 1 : 子 0..N'
            child_requirement = '子表可选引用父表' if fk_nullable(child, fk) else '子表必须引用父表'
            tooltip = f'{fk.name}: {child.name}({", ".join(fk.child_columns)}) → {fk.parent_table}({", ".join(fk.parent_columns)}), {card}, {child_requirement}, ON DELETE {fk.on_delete or "NO ACTION"}'
            lines.append(f'<path d="{path}" fill="none" stroke="#98a2b3" stroke-width="1.4" opacity="0.72" marker-end="url(#arrow)"><title>{esc(tooltip)}</title></path>')

    for relation in infer_m2m(tables):
        if relation['left'] not in positions or relation['right'] not in positions:
            continue
        points = edge_points(positions[relation['left']], positions[relation['right']])
        path = ' '.join((f'M {points[0][0]:.1f},{points[0][1]:.1f}',) + tuple(f'L {x:.1f},{y:.1f}' for x, y in points[1:]))
        lines.append(f'<path d="{path}" fill="none" stroke="#9b51e0" stroke-width="2" stroke-dasharray="8 7" opacity="0.55"><title>M:N，经由 {esc(relation["via"])}</title></path>')

    for lane_index, domain in enumerate(layout['domains']):
        x = PAGE_MARGIN + lane_index * (CARD_WIDTH + LANE_GAP)
        lines.append(f'<rect x="{x}" y="{TITLE_HEIGHT}" width="{CARD_WIDTH}" height="36" rx="9" fill="{DOMAIN_COLORS[domain]}" stroke="#d0d5dd"/>')
        lines.append(f'<text x="{x + 15}" y="{TITLE_HEIGHT + 25}" font-size="17" font-weight="700" fill="#344054">{esc(DOMAIN_LABELS[domain])}</text>')

    for table_name, box in positions.items():
        x, y, width, height = box['x'], box['y'], box['width'], box['height']
        color = DOMAIN_COLORS[box['domain']]
        lines.append(f'<rect x="{x}" y="{y}" width="{width}" height="{height}" rx="8" fill="white" stroke="#98a2b3" stroke-width="1.2"/>')
        lines.append(f'<rect x="{x}" y="{y}" width="{width}" height="{CARD_HEADER_HEIGHT}" rx="8" fill="{color}"/>')
        lines.append(f'<text x="{x + 13}" y="{y + 27}" font-size="16" font-weight="700" fill="#101828">{esc(table_name)}</text>')
        table = tables[table_name]
        lines.append(f'<text x="{x + width - 12}" y="{y + 26}" text-anchor="end" font-size="12" fill="#475467">{len(table.columns)} 字段 / {len(table.foreign_keys)} 外键</text>')
        row_y = y + CARD_HEADER_HEIGHT + 23
        for column in box['rows']:
            lines.append(f'<text x="{x + 13}" y="{row_y}" font-size="12" font-weight="700" fill="#175cd3">{esc(column_badges(column))}</text>')
            lines.append(f'<text x="{x + 93}" y="{row_y}" font-size="13" fill="#344054">{esc(column.name)}</text>')
            lines.append(f'<text x="{x + width - 12}" y="{row_y}" text-anchor="end" font-size="12" fill="#667085">{esc(display_type(column.type_sql))}</text>')
            row_y += ROW_HEIGHT
        if box['omitted']:
            lines.append(f'<text x="{x + 93}" y="{row_y}" font-size="12" fill="#98a2b3">… 其余 {box["omitted"]} 个字段详见中文说明</text>')

    footer_y = height - 55
    lines.append(f'<text x="{PAGE_MARGIN}" y="{footer_y}" font-size="14" fill="#475467">灰色实线：物理外键　紫色虚线：推断的逻辑多对多　箭头指向引用方（子表）　指纹：{fingerprint[:16]}</text>')
    lines += ['</g>', '</svg>']
    # Path.write_text uses the host default newline translation on Windows.
    # ER SVG and Markdown are checksum-protected repository artifacts, so
    # write LF explicitly to keep generated bytes identical on every platform.
    with target.open('w', encoding='utf-8', newline='\n') as handle:
        handle.write('\n'.join(lines) + '\n')


def resolve_font(size: int, bold: bool = False):
    try:
        from PIL import ImageFont
    except ImportError as exc:
        raise SystemExit('生成 PNG 需要 Pillow：python -m pip install Pillow') from exc
    candidates = [
        Path('C:/Windows/Fonts/simhei.ttf'),
        Path('C:/Windows/Fonts/msyh.ttc'),
        Path('/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc'),
        Path('/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc'),
        Path('/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf'),
    ]
    for candidate in candidates:
        if candidate.exists():
            return ImageFont.truetype(str(candidate), size=size)
    return ImageFont.load_default()


def render_png(tables: dict[str, Table], layout: dict, target: Path, fingerprint: str) -> None:
    try:
        from PIL import Image, ImageDraw, PngImagePlugin
    except ImportError as exc:
        raise SystemExit('生成 PNG 需要 Pillow：python -m pip install Pillow') from exc

    image = Image.new('RGB', (layout['width'], layout['height']), '#f8fafc')
    draw = ImageDraw.Draw(image)
    font_title = resolve_font(30, True)
    font_domain = resolve_font(17, True)
    font_table = resolve_font(16, True)
    font_body = resolve_font(12)
    font_body_bold = resolve_font(12, True)
    positions = layout['positions']

    draw.text((PAGE_MARGIN, 22), 'CampusOS 数据库实体关系图', fill='#101828', font=font_title)
    draw.text((PAGE_MARGIN, 65), f'{len(tables)} 张实体表 · {sum(len(t.foreign_keys) for t in tables.values())} 条物理外键 · 字段行优先展示 PK/FK/UQ', fill='#475467', font=font_domain)

    for index, child in enumerate(sorted(tables.values(), key=lambda table: table.name)):
        for fk in child.foreign_keys:
            if fk.parent_table in positions:
                points = edge_points(positions[fk.parent_table], positions[child.name], index)
                draw.line(points, fill='#98a2b3', width=2, joint='curve')
                end, before = points[-1], points[-3]
                dx, dy = end[0] - before[0], end[1] - before[1]
                scale = max((dx * dx + dy * dy) ** 0.5, 1)
                ux, uy = dx / scale, dy / scale
                px, py = -uy, ux
                arrow = [end, (end[0] - 12 * ux + 5 * px, end[1] - 12 * uy + 5 * py), (end[0] - 12 * ux - 5 * px, end[1] - 12 * uy - 5 * py)]
                draw.polygon(arrow, fill='#667085')

    for relation in infer_m2m(tables):
        if relation['left'] in positions and relation['right'] in positions:
            points = edge_points(positions[relation['left']], positions[relation['right']])
            for index in range(0, len(points) - 1, 3):
                draw.line(points[index:min(index + 2, len(points))], fill='#9b51e0', width=3)

    for lane_index, domain in enumerate(layout['domains']):
        x = PAGE_MARGIN + lane_index * (CARD_WIDTH + LANE_GAP)
        draw.rounded_rectangle((x, TITLE_HEIGHT, x + CARD_WIDTH, TITLE_HEIGHT + 36), radius=9, fill=DOMAIN_COLORS[domain], outline='#d0d5dd')
        draw.text((x + 15, TITLE_HEIGHT + 8), DOMAIN_LABELS[domain], fill='#344054', font=font_domain)

    for table_name, box in positions.items():
        x, y, width, height = box['x'], box['y'], box['width'], box['height']
        draw.rounded_rectangle((x, y, x + width, y + height), radius=8, fill='white', outline='#98a2b3', width=2)
        draw.rounded_rectangle((x, y, x + width, y + CARD_HEADER_HEIGHT), radius=8, fill=DOMAIN_COLORS[box['domain']], outline='#98a2b3')
        draw.text((x + 13, y + 10), table_name, fill='#101828', font=font_table)
        table = tables[table_name]
        summary = f'{len(table.columns)} 字段 / {len(table.foreign_keys)} 外键'
        summary_width = draw.textbbox((0, 0), summary, font=font_body)[2]
        draw.text((x + width - summary_width - 12, y + 12), summary, fill='#475467', font=font_body)
        row_y = y + CARD_HEADER_HEIGHT + 8
        for column in box['rows']:
            draw.text((x + 13, row_y), column_badges(column), fill='#175cd3', font=font_body_bold)
            draw.text((x + 93, row_y), column.name, fill='#344054', font=font_body)
            type_text = display_type(column.type_sql)
            type_width = draw.textbbox((0, 0), type_text, font=font_body)[2]
            draw.text((x + width - type_width - 12, row_y), type_text, fill='#667085', font=font_body)
            row_y += ROW_HEIGHT
        if box['omitted']:
            draw.text((x + 93, row_y), f'… 其余 {box["omitted"]} 个字段详见中文说明', fill='#98a2b3', font=font_body)

    draw.text((PAGE_MARGIN, layout['height'] - 62), f'灰色实线：物理外键　紫色虚线：推断的逻辑多对多　箭头指向引用方（子表）　指纹：{fingerprint[:16]}', fill='#475467', font=font_body)
    metadata = PngImagePlugin.PngInfo()
    metadata.add_text('CampusOS-Schema-SHA256', fingerprint)
    metadata.add_text('CampusOS-Table-Count', str(len(tables)))
    metadata.add_text('CampusOS-Foreign-Key-Count', str(sum(len(table.foreign_keys) for table in tables.values())))
    image.save(target, format='PNG', optimize=True, dpi=(144, 144), pnginfo=metadata)


# ---------- Reports ----------

def md_cell(value: object) -> str:
    return str(value).replace('|', '\\|').replace('\n', ' ')


def relationship_markdown(
    tables: dict[str, Table],
    *,
    migration_paths: list[Path],
    migrations_root: Path,
    fingerprint: str,
    png_name: str,
    svg_name: str,
) -> str:
    one_to_one, one_to_many = [], []
    incoming: dict[str, list[ForeignKey]] = defaultdict(list)
    for child in sorted(tables.values(), key=lambda t: t.name):
        for fk in child.foreign_keys:
            if fk.parent_table not in tables:
                continue
            incoming[fk.parent_table].append(fk)
            item = {
                'parent': fk.parent_table,
                'child': child.name,
                'fk': fk.name,
                'child_cols': ', '.join(fk.child_columns),
                'parent_cols': ', '.join(fk.parent_columns),
                'optional': fk_nullable(child, fk),
                'delete': fk.on_delete or 'NO ACTION',
                'update': fk.on_update or 'NO ACTION',
            }
            (one_to_one if fk_is_unique(child, fk) else one_to_many).append(item)

    lines = [
        '# CampusOS 数据库实体关系说明', '',
        f'<!-- campusos-er:schema_sha256={fingerprint};tables={len(tables)};foreign_keys={sum(len(t.foreign_keys) for t in tables.values())} -->',
        '> 本文档由 `migrations/tools/generate_er.py` 从 migration UP 文件自动生成，请勿手工维护生成区。', '',
        f'![CampusOS 数据库 ER 图](./{png_name})', '',
        f'- 可缩放版本：[打开 SVG ER 图](./{svg_name})',
        f'- 实体表：**{len(tables)}**',
        f'- 物理外键：**{sum(len(t.foreign_keys) for t in tables.values())}**',
        f'- 一对一/可选一对一关系：**{len(one_to_one)}**',
        f'- 一对多关系：**{len(one_to_many)}**',
        f'- 推断的逻辑多对多关系：**{len(infer_m2m(tables))}**',
        f'- Schema 指纹：`{fingerprint}`', '',
        '## 1. 生成范围与判定规则', '',
        '工具只读取 `*.up.sql`，不连接数据库，也不会执行 migration。当前输入文件：', '',
    ]
    lines.extend(f'- `{path.relative_to(migrations_root).as_posix()}`' for path in migration_paths)
    lines += [
        '',
        '- **PK**：主键；**FK**：外键；**UQ**：全局唯一；**NN**：非空。',
        '- 一对一仅在外键列集合同时构成主键或非部分唯一约束时判定。',
        '- “子表可选”表示外键列允许为空；父记录对应的子记录数量仍可能为零。',
        '- `ON DELETE`、`ON UPDATE` 来自 migration DDL；未显式声明时按 PostgreSQL `NO ACTION` 展示。',
        '- 逻辑多对多是经关联实体推断的业务阅读视图，物理约束仍以两条外键为准。', '',
        '## 2. 实体总览', '',
        '| 业务域 | 实体表 | 字段数 | 主键 | 出站外键 | 入站外键 |',
        '| --- | --- | ---: | --- | ---: | ---: |',
    ]
    for domain in DOMAIN_ORDER:
        for table_name in sorted(name for name in tables if classify_table(name) == domain):
            table = tables[table_name]
            primary = ', '.join(table.primary_key) or '—'
            lines.append(f'| {DOMAIN_LABELS[domain]} | `{table_name}` | {len(table.columns)} | `{primary}` | {len(table.foreign_keys)} | {len(incoming[table_name])} |')

    lines += ['', '## 3. 一对一与可选一对一', '',
              '| 父表 | 子表 | 外键约束 | 字段映射 | 关联类型 | 子表引用 | ON DELETE | ON UPDATE |',
              '| --- | --- | --- | --- | --- | --- | --- | --- |']
    for x in one_to_one:
        child_requirement = '可选（0..1 个父记录）' if x['optional'] else '必选（恰好 1 个父记录）'
        lines.append(f"| `{x['parent']}` | `{x['child']}` | `{x['fk']}` | `{x['child_cols']}` → `{x['parent_cols']}` | 父 1 : 子 0..1 | {child_requirement} | `{x['delete']}` | `{x['update']}` |")

    if not one_to_one:
        lines.append('| — | — | — | — | 当前没有满足唯一约束的一对一关系 | — | — | — |')

    lines += ['', '## 4. 一对多', '',
              '| 父表 | 子表 | 外键约束 | 字段映射 | 关联类型 | 子表引用 | ON DELETE | ON UPDATE |',
              '| --- | --- | --- | --- | --- | --- | --- | --- |']
    for x in one_to_many:
        child_requirement = '可选（0..1 个父记录）' if x['optional'] else '必选（恰好 1 个父记录）'
        lines.append(f"| `{x['parent']}` | `{x['child']}` | `{x['fk']}` | `{x['child_cols']}` → `{x['parent_cols']}` | 父 1 : 子 0..N | {child_requirement} | `{x['delete']}` | `{x['update']}` |")

    lines += ['', '## 5. 逻辑多对多', '',
              '| 实体 A | 实体 B | 关联实体 | 判定依据 |',
              '| --- | --- | --- | --- |']
    for x in infer_m2m(tables):
        basis = '关联列受主键/唯一约束共同约束' if x['strength'] == 'constraint-backed' else '按关联实体命名与双外键推断（非唯一性保证）'
        lines.append(f"| `{x['left']}` | `{x['right']}` | `{x['via']}` | {basis} |")

    lines += ['', '## 6. 各实体字段与约束', '']
    for domain in DOMAIN_ORDER:
        names = sorted(name for name in tables if classify_table(name) == domain)
        if not names:
            continue
        lines += [f'### 6.{DOMAIN_ORDER.index(domain) + 1} {DOMAIN_LABELS[domain]}', '']
        for table_name in names:
            table = tables[table_name]
            unique_text = '；'.join('(' + ', '.join(columns) + ')' for columns in table.unique_sets) or '无全局唯一列集'
            lines += [f'#### `{table_name}`', '',
                      f'- 主键：`{", ".join(table.primary_key) or "无"}`',
                      f'- 唯一列集：{unique_text}',
                      f'- 出站外键：{len(table.foreign_keys)}；入站外键：{len(incoming[table_name])}', '',
                      '| 字段 | 数据类型 | 标记 | 可空 | 默认值 |',
                      '| --- | --- | --- | --- | --- |']
            for column in table.columns:
                default = md_cell(column.default) if column.default is not None else '—'
                lines.append(f'| `{column.name}` | `{md_cell(display_type(column.type_sql))}` | {column_badges(column)} | {"是" if column.nullable else "否"} | `{default}` |')
            if table.foreign_keys:
                lines += ['', '外键明细：', '']
                for fk in table.foreign_keys:
                    lines.append(f'- `{fk.name}`：`{table.name}({", ".join(fk.child_columns)})` → `{fk.parent_table}({", ".join(fk.parent_columns)})`；ON DELETE `{fk.on_delete or "NO ACTION"}`；ON UPDATE `{fk.on_update or "NO ACTION"}`。')
            lines.append('')

    lines += ['## 7. 一致性与更新方式', '',
              'PNG、SVG 与本文档使用同一解析结果和 Schema 指纹。migration 变化后运行：', '',
              '```bash', 'python migrations/tools/generate_er.py',
              'python migrations/tools/generate_er.py --check', '```', '']
    return '\n'.join(lines) + '\n'


def validate_outputs(
    out: Path,
    tables: dict[str, Table],
    fingerprint: str,
    png_name: str,
    svg_name: str,
    markdown_name: str,
    *,
    expected_markdown: Optional[str] = None,
    expected_svg: Optional[str] = None,
) -> list[str]:
    errors = []
    marker = f'campusos-er:schema_sha256={fingerprint};tables={len(tables)};foreign_keys={sum(len(table.foreign_keys) for table in tables.values())}'
    markdown_path, svg_path, png_path = out / markdown_name, out / svg_name, out / png_name
    if not markdown_path.exists() or marker not in markdown_path.read_text(encoding='utf-8'):
        errors.append(f'Markdown 缺失或已漂移：{markdown_path}')
    elif expected_markdown is not None and markdown_path.read_text(encoding='utf-8') != expected_markdown:
        errors.append(f'Markdown 内容不是当前解析器的生成结果：{markdown_path}')
    if not svg_path.exists() or f'campusos_schema_sha256={fingerprint}' not in svg_path.read_text(encoding='utf-8'):
        errors.append(f'SVG 缺失或已漂移：{svg_path}')
    elif expected_svg is not None and svg_path.read_text(encoding='utf-8') != expected_svg:
        errors.append(f'SVG 内容不是当前解析器的生成结果：{svg_path}')
    if not png_path.exists():
        errors.append(f'PNG 缺失：{png_path}')
    else:
        try:
            metadata = read_png_text_metadata(png_path)
            if metadata.get('CampusOS-Schema-SHA256') != fingerprint:
                errors.append(f'PNG Schema 指纹已漂移：{png_path}')
        except OSError as exc:
            errors.append(f'无法校验 PNG：{exc}')
    return errors


def read_png_text_metadata(path: Path) -> dict[str, str]:
    """Read uncompressed PNG tEXt chunks without requiring Pillow in CI."""
    data = path.read_bytes()
    if not data.startswith(b'\x89PNG\r\n\x1a\n'):
        raise OSError('文件不是有效 PNG')
    metadata: dict[str, str] = {}
    offset = 8
    while offset + 12 <= len(data):
        length = struct.unpack('>I', data[offset:offset + 4])[0]
        chunk_type = data[offset + 4:offset + 8]
        chunk_data = data[offset + 8:offset + 8 + length]
        offset += 12 + length
        if chunk_type == b'tEXt' and b'\0' in chunk_data:
            key, value = chunk_data.split(b'\0', 1)
            metadata[key.decode('latin-1')] = value.decode('latin-1')
        if chunk_type == b'IEND':
            break
    return metadata


# ---------- CLI ----------

def main() -> int:
    ap = argparse.ArgumentParser(description='从 CampusOS PostgreSQL migration 生成 PNG、SVG 与中文 ER 关系说明。')
    ap.add_argument('--migrations', type=Path, default=Path('migrations'), help='migration 目录（默认：migrations）')
    ap.add_argument('--out', type=Path, default=Path('migrations/er/current'), help='输出目录')
    ap.add_argument('--check', action='store_true', help='只检查已有三份产物是否与 migration 一致')
    args = ap.parse_args()

    files = migration_files(args.migrations)
    sql = read_sql_files(args.migrations)
    tables = parse_schema(sql)
    if not tables:
        raise SystemExit('没有解析到 CREATE TABLE。')

    fingerprint = schema_fingerprint(args.migrations, files)
    png_name = 'CampusOS数据库ER图.png'
    svg_name = 'CampusOS数据库ER图.svg'
    markdown_name = 'CampusOS数据库实体关系说明.md'

    if args.check:
        expected_markdown = relationship_markdown(
            tables,
            migration_paths=files,
            migrations_root=args.migrations,
            fingerprint=fingerprint,
            png_name=png_name,
            svg_name=svg_name,
        )
        with TemporaryDirectory() as temporary:
            expected_svg_path = Path(temporary) / svg_name
            render_svg(tables, build_layout(tables), expected_svg_path, fingerprint)
            expected_svg = expected_svg_path.read_text(encoding='utf-8')
        errors = validate_outputs(
            args.out,
            tables,
            fingerprint,
            png_name,
            svg_name,
            markdown_name,
            expected_markdown=expected_markdown,
            expected_svg=expected_svg,
        )
        if errors:
            print('\n'.join(f'错误：{error}' for error in errors), file=sys.stderr)
            return 1
        print(f'ER 产物一致性检查通过：{len(tables)} 张表，{sum(len(table.foreign_keys) for table in tables.values())} 条外键。')
        return 0

    args.out.mkdir(parents=True, exist_ok=True)
    layout = build_layout(tables)
    render_svg(tables, layout, args.out / svg_name, fingerprint)
    render_png(tables, layout, args.out / png_name, fingerprint)
    markdown = relationship_markdown(
        tables,
        migration_paths=files,
        migrations_root=args.migrations,
        fingerprint=fingerprint,
        png_name=png_name,
        svg_name=svg_name,
    )
    with (args.out / markdown_name).open('w', encoding='utf-8', newline='\n') as handle:
        handle.write(markdown)

    fk_count = sum(len(t.foreign_keys) for t in tables.values())
    print(f'已解析实体表：{len(tables)}')
    print(f'已解析物理外键：{fk_count}')
    print(f'逻辑多对多关系：{len(infer_m2m(tables))}')
    print(f'Schema 指纹：{fingerprint}')
    print(f'已生成：{(args.out / png_name).resolve()}')
    print(f'已生成：{(args.out / svg_name).resolve()}')
    print(f'已生成：{(args.out / markdown_name).resolve()}')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
