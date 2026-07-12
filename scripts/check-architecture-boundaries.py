#!/usr/bin/env python3
"""Reject new CampusOS module-boundary violations while allowing documented legacy debt."""
from __future__ import annotations
import json,re,sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
allow=json.loads((ROOT/'config/architecture-boundary-allowlist.json').read_text())
imports=re.compile(r'"github\.com/campusos/CampusOS/(internal/[^"\s]+)"')
violations=[]
for path in sorted((ROOT/'internal').rglob('*.go')):
    if path.name.endswith('_test.go'): continue
    rel=path.relative_to(ROOT).as_posix(); text=path.read_text()
    owner=rel.split('/')[1] if rel.startswith('internal/') else ''
    for target in imports.findall(text):
        parts=target.split('/'); target_owner=parts[1] if len(parts)>1 else ''
        if target.endswith('/repository') and target_owner!=owner and rel not in allow['cross_repository']:
            violations.append(f'{rel}: cross_repository -> {target}')
    if owner in {'richtext','schedule'} and 'internal/space' in text:
        violations.append(f'{rel}: feature depends on personal-space internals')
for path in sorted((ROOT/'data/plugins').rglob('*.go')):
    if 'github.com/campusos/CampusOS/internal/' in path.read_text(): violations.append(f'{path.relative_to(ROOT)}: external plugin imports internal/*')
for path in sorted((ROOT/'internal/agentcontract').rglob('*.go')):
    text=path.read_text();
    for forbidden in ('pkg/database','pkg/auth','internal/core/identity/repository'):
        if forbidden in text: violations.append(f'{path.relative_to(ROOT)}: Agent contract imports {forbidden}')
if violations:
    print('CampusOS architecture boundary check failed:',file=sys.stderr)
    for item in violations: print(f'- {item}',file=sys.stderr)
    raise SystemExit(1)
print(f'CampusOS architecture boundary check passed: {len(allow["cross_repository"])} documented legacy exceptions')
