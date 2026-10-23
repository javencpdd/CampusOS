#!/usr/bin/env python3
"""Seal one04 text/boolean system-config UI slice, never mark whole04 complete."""
import hashlib
import json
import pathlib
import re
import subprocess
import sys
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence'
COMMON = ['admin-components', 'admin-build', 'full-go', 'contracts', 'architecture', 'readme', 'docs-links', 'docs-build', 'line-endings', 'diff']
FINAL = ['docs-links', 'docs-build', 'architecture', 'line-endings', 'diff']
DOCS = ['docs/进度/v1.2-dev/v1.2.120-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件配置Schema与Secret分流合同.md', 'docs/help/插件v5包与签名操作说明.md', 'docs-site/plugins/manifest.md']

def sha(p):
    h = hashlib.sha256()
    with p.open('rb') as f:
        for chunk in iter(lambda: f.read(1 << 20), b''): h.update(chunk)
    return h.hexdigest()

def sources():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.json', '.yaml', '.yml', '.html', '.css', '.png', '.svg'}
    return {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n == 'Makefile')}

def snapshot():
    return {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': sources(), 'dependencies': {str(p.relative_to(ROOT)): sha(p) for p in [EVIDENCE/n for n in ['v12-03c-exit-linux.json', 'v12-01b-exit-linux.json']]}}

def main():
    mode, work = sys.argv[1], pathlib.Path(sys.argv[2]).resolve()
    assert work.parent == ROOT/'.cache/v12-04-config' and work.name.startswith('acceptance.')
    freeze = work/'source-freeze.json'
    current = snapshot()
    if mode == 'freeze':
        assert not freeze.exists()
        freeze.write_text(json.dumps(current, ensure_ascii=False, indent=2)+'\n'); return
    assert json.loads(freeze.read_text()) == current, 'source, HEAD or accepted dependency changed after freeze'
    if mode == 'verify': return
    assert mode == 'seal'
    rows = [json.loads(line) for line in (work/'native/target.jsonl').read_text().splitlines()]
    assert not any(row['Action'] in {'fail', 'skip'} for row in rows)
    assert {row['Test'] for row in rows if row['Action'] == 'pass' and 'Test' in row} == {'TestPluginV5ConfigActualServerBrowser'}
    assert any(row['Action'] == 'pass' and 'Test' not in row for row in rows)
    # SeedAdmin's two expected startup records contain no credentials. Keep
    # them verbatim; unexpected diagnostics still prevent sealing.
    stderr = (work/'native/target.stderr').read_text().splitlines()
    assert len(stderr) == 2 and all(re.fullmatch(r'\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} (default category ready|bootstrap administrator ready \(administrator created using configured bootstrap secret\); credentials were not logged)', line) for line in stderr)
    assert 'DATA RACE' not in (work/'native/target.log').read_text()
    browser = json.loads((work/'browser/browser-result.json').read_text())
    server = json.loads((work/'browser/server-result.json').read_text())
    assert browser['status'] == server['status'] == 'passed' and browser['mocked_api'] is False and browser['actual_cmd_server'] is True
    assert not browser['browser_errors'] and len(browser['checks']) >= 17 and server['exact_typed_audits'] == 2 and server['actual_pg_configuration'] is True
    assert server['server_binary_sha256'] == sha(work/'native/campusos-server')
    assert 'Linux' in (work/'environment.txt').read_text() and (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 2
    assert 160000 <= int((work/'pg-version.txt').read_text()) < 170000
    assert 'campusos-v12-04-config-' in (work/'cleanup.log').read_text() and 'Error' not in (work/'cleanup.log').read_text()
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    assert (work/'final-passed.txt').read_text().splitlines() == FINAL
    report = {'schema': 'campusos.v12-04-config-ui/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(), 'stage': 'V12-04', 'stage_status': 'in_progress', 'slice': 'Admin v5 system text/boolean configuration form', 'slice_status': 'completed', 'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed', 'native': {'top_level': 1, 'fail': 0, 'skip': 0}, 'browser': browser, 'server': server, 'migration': {'current': 28, 'next': 29, 'changed': False}, 'common_gates': {n: 'passed' for n in COMMON}, 'final_document_gates': {n: 'passed' for n in FINAL}, **current, 'build_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted((ROOT/'admin/dist').rglob('*')) if p.is_file()}, 'artifacts_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()}, 'documents_sha256': {n: sha(ROOT/n) for n in DOCS}, 'boundaries': ['Only existing v5 Admin read/configure APIs, immutable Manifest and current identity/permission/MFA/CSRF/CAS facts authorize writes.', 'Only system/both text and boolean ordinary fields and same-type enums; unsupported numeric fields, selectors or existing Secret refs close the editor. No raw Secret input or implicit reference clearing.', 'Saving config is not runtime readiness; production has no fake runner or ready observation.', 'Full numeric/selector/Secret form, separate approval panels, User config and isolated UI Host/Bridge/PDF remain V12-04. Actual Wasm/Container runners remain05/06.', 'No new migration, shared database reset, commit or push.'], 'commit_or_push': False, 'shared_database_reset': False}
    (EVIDENCE/'v12-04-config-ui-linux.json').write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed04 config slice:', len(browser['checks']), 'actual browser checks; whole04 remains in progress')

if __name__ == '__main__': main()
