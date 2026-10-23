#!/usr/bin/env python3
"""Seal only the first05 execution slice; never declare a production Runner ready."""
import hashlib
import json
import pathlib
import re
import subprocess
import sys
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORT = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence/v12-05-wasm-sandbox-linux.json'
COMMON = ['full-go', 'affected-race', 'contracts', 'architecture', 'readme', 'docs-links', 'docs-build', 'line-endings', 'diff']
FINAL_DOCUMENTS = ['docs-links', 'docs-build', 'architecture', 'line-endings', 'diff']
DOCS = ['docs/进度/v1.2-dev/v1.2.122-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件Wasm执行沙箱合同.md']


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def snapshot():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.json', '.yaml', '.yml', '.html', '.css', '.png', '.svg'}
    sources = {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n == 'Makefile')}
    dependencies = {str(p.relative_to(ROOT)): sha(p) for p in [REPORT.parent/n for n in ['v12-03c-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-04-ui-exit-linux.json']]}
    return {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': sources, 'dependencies': dependencies}


def native(work, name, source, prefix):
    expected = set(re.findall(r'^func (Test'+prefix+r'\w+)\(', (ROOT/source).read_text(), re.M))
    rows = [json.loads(line) for line in (work/f'native/{name}.jsonl').read_text().splitlines()]
    assert expected and not any(row['Action'] in {'fail', 'skip'} for row in rows), name
    passed = {row['Test'] for row in rows if row['Action'] == 'pass' and 'Test' in row}
    assert {test for test in passed if '/' not in test} == expected, name
    assert any(row['Action'] == 'pass' and 'Test' not in row for row in rows), name
    assert not (work/f'native/{name}.stderr').read_text(), name
    assert 'DATA RACE' not in (work/f'native/{name}.log').read_text(), name
    info = (work/f'native/{name}-build-info.txt').read_text()
    assert '-race=true' in info and 'GOOS=linux' in info, name
    return {'top_level': len(expected), 'pass_events': len(passed), 'fail': 0, 'skip': 0, 'tests': sorted(expected)}


def main():
    if len(sys.argv) != 3:
        raise SystemExit('usage: v12-05-wasm-sandbox-report.py freeze|verify|seal OWNED_DIR')
    mode, work = sys.argv[1], pathlib.Path(sys.argv[2]).resolve()
    assert work.parent == ROOT/'.cache/v12-05-wasm-sandbox' and work.name.startswith('acceptance.')
    freeze = work/'source-freeze.json'
    current = snapshot()
    if mode == 'freeze':
        assert not freeze.exists(), 'use a new owned directory for every target execution'
        preflight = ROOT/'.cache/v12-05-wasm-sandbox/preflight.json'
        before = json.loads(preflight.read_text())
        assert before['head'] == current['head'] and not before['status']
        assert all(not check['mismatches'] for check in before['checks'].values())
        assert before['predecessor_report_sha256'] == current['dependencies'][before['predecessor_report']]
        for name in ['v12-03c-exit-linux.json', 'v12-04-ui-exit-linux.json']:
            report = json.loads((REPORT.parent/name).read_text())
            assert report['stage_status'] == 'completed'
            assert all(report[key] == 'passed' for key in ['implementation', 'automation', 'target_environment'])
        (work/'preflight.json').write_bytes(preflight.read_bytes())
        freeze.write_text(json.dumps(current, ensure_ascii=False, indent=2)+'\n')
        return
    assert json.loads(freeze.read_text()) == current, 'HEAD, source or accepted dependency changed after target freeze'
    capture = native(work, 'capture', 'internal/plugin/v5_wasm_program_test.go', 'V5WasmProgram')
    sandbox = native(work, 'sandbox', 'internal/plugin/wasm/v5_sandbox_test.go', 'V5Sandbox')
    assert 'Linux' in (work/'environment.txt').read_text()
    assert (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 2
    if mode == 'verify':
        print('verified native Linux/race/env -i:', capture['top_level'], '+', sandbox['top_level'], 'top-level tests')
        return
    assert mode == 'seal'
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    assert (work/'final-document-passed.txt').read_text().splitlines() == FINAL_DOCUMENTS
    report = {
        'schema': 'campusos.v12-05-wasm-sandbox/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(),
        'stage': 'V12-05', 'stage_status': 'in_progress', 'slice': 'signed-immutable-wasm-json-sandbox',
        'slice_status': 'completed', 'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed',
        'native_capture': capture, 'native_wasm': sandbox, 'actual_wazero': True, 'mock_runtime': False,
        'native_launch': {'clean_environment': True, 'environment_keys': ['PATH', 'HOME', 'TMPDIR', 'GORACE'], 'guest_host_imports': []},
        'common_gates': {name: 'passed' for name in COMMON},
        'final_document_gates': {name: 'passed' for name in FINAL_DOCUMENTS},
        'migration': {'current': 28, 'next': 29, 'changed': False}, **current,
        'artifacts_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()},
        'documents_sha256': {name: sha(ROOT/name) for name in DOCS},
        'boundaries': [
            'This first05 slice executes actual signed Wasm bytes; it is not a production managed Runtime Adapter or lifecycle/publication acceptance.',
            'Linear guest memory, per-sandbox concurrency, per-call deadline/cancellation and finite JSON IO are enforced. Native compiler/RSS limits, CPU shares, aggregate/cluster capacity and hard process isolation remain later05 work.',
            'All import kinds are refused; no WASI, filesystem, environment, stdio, network or Host API module is bound. Approved typed Host imports/Broker remain later05 work.',
            'Captured package trust/resource facts are snapshots. Current trust/Grant/Consent/config/generation checks, publication, withdrawal and startup reconciliation remain the lifecycle coordinator; production enable stays pending/runtime_unavailable.',
            'Native Linux race binaries run under env -i with owned temporary files; no PostgreSQL, actual HTTP/browser/Windows or Container claim for this execution-only slice.',
            'Historical dependency reports are unchanged. No migrations, shared database reset, commit or push.',
        ],
        'commit_or_push': False, 'shared_database_reset': False,
    }
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed first05 sandbox slice; full V12-05 remains in_progress')


if __name__ == '__main__':
    main()
