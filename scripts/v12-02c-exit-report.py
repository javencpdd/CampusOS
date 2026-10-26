#!/usr/bin/env python3
"""Verify current V12-02c evidence; never infer success from a file name."""
import argparse
import hashlib
import json
import re
import subprocess
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence'
REPORT = EVIDENCE / 'v12-02c-exit-linux.json'
COMMON = ['full-go', 'related-race', 'admin-components', 'sdk', 'contracts', 'architecture', 'database', 'docs-build', 'docs-links', 'line-endings', 'diff']
DEPS = ['v12-02a-exit-linux.json', 'v12-02d-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-02b-exit-linux.json', 'v12-02c-integration-access-linux.json']
DOCUMENTS = ['docs/进度/v1.2-dev/v1.2.112-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2Integration凭据与接入合同.md', 'docs/help/机器集成凭据操作说明.md', 'docs/help/README.md', 'docs/architecture/当前架构概览.md', 'docs-site/guide/architecture.md', 'skills/sources/campusos-project-onboarding/references/current-project-status.md', 'migrations/README.md']


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def read(path):
    return json.loads(Path(path).read_text())


def write(path, value):
    Path(path).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def sources():
    paths = subprocess.check_output(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.vue', '.js', '.mjs', '.cjs', '.sql', '.py', '.sh', '.ps1', '.json', '.yaml', '.yml', '.html', '.css', '.svg'}
    return {p: digest(ROOT / p) for p in sorted(set(paths)) if p and (ROOT / p).is_file() and (Path(p).suffix in suffixes or p == 'Makefile' or p.startswith('migrations/er/current/')) and not p.startswith('docs/项目计划书')}


def historical_head_inputs():
    prior = read(EVIDENCE / DEPS[-1])
    expected = {**prior['source_sha256'], **prior['contract_input_sha256']}
    paths = list(expected)
    attributes = subprocess.check_output(['git', 'check-attr', '-z', 'eol', '--stdin'], input=('\0'.join(paths)+'\0').encode(), cwd=ROOT).decode().split('\0')
    eol = {attributes[i]: attributes[i+2] for i in range(0,len(attributes)-1,3)}
    result = subprocess.run(['git', 'cat-file', '--batch'], input=''.join('HEAD:' + p + '\n' for p in paths).encode(), capture_output=True, check=True, cwd=ROOT).stdout
    offset = 0
    for path in paths:
        end = result.index(b'\n', offset)
        header = result[offset:end].split()
        assert len(header) == 3 and header[1] == b'blob', ('preflight input missing at original HEAD', path)
        size = int(header[2]); data = result[end + 1:end + 1 + size]; offset = end + 2 + size
        if eol.get(path)=='crlf':
            # The old evidence hashes checked-out bytes; Git stores LF and
            # .gitattributes explicitly restores CRLF for PowerShell files.
            data=data.replace(b'\r\n',b'\n').replace(b'\n',b'\r\n')
        assert hashlib.sha256(data).hexdigest() == expected[path], ('preflight HEAD differs from accepted111', path)
    return len(paths)


def freeze(work):
    work.mkdir(parents=True, exist_ok=True)
    dependency_hashes = {}
    for name in DEPS:
        path = EVIDENCE / name; data = read(path)
        assert data['implementation'] == data['automation'] == data['target_environment'] == 'passed', name
        assert data['stage_status'] == ('partial' if name == DEPS[-1] else 'completed'), name
        dependency_hashes[path.relative_to(ROOT).as_posix()] = digest(path)
    value = {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(), 'branch': subprocess.check_output(['git', 'branch', '--show-current'], cwd=ROOT, text=True).strip(), 'historical111_inputs_verified_at_original_head': historical_head_inputs(), 'source_sha256': sources(), 'dependencies': dependency_hashes}
    write(work / 'source-freeze.json', value)
    print('Frozen current sources:', len(value['source_sha256']), '; prior111 HEAD inputs:', value['historical111_inputs_verified_at_original_head'])


def verify(work):
    frozen = read(work / 'source-freeze.json')
    assert sources() == frozen['source_sha256'], 'source changed after target freeze; rerun affected acceptance'
    assert subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip() == frozen['head'], 'HEAD changed'
    for path, sha in frozen['dependencies'].items():
        assert digest(ROOT / path) == sha, path
    checks = {}; tops = 0; assertions = 0
    for name in ['port', 'policy', 'repository', 'service', 'handler', 'ingress']:
        expected = set((work / f'native/{name}-required.txt').read_text().splitlines())
        assert expected and all(n.startswith('TestIntegration') for n in expected), name
        events = [json.loads(line) for line in (work / f'native/{name}.jsonl').read_text().splitlines()]
        assert not any(e['Action'] in {'fail', 'skip'} for e in events), name
        passed = {e['Test'] for e in events if e['Action'] == 'pass' and 'Test' in e}
        assert {n for n in passed if '/' not in n} == expected, name
        text = (work / f'native/{name}.log').read_text() + (work / f'native/{name}.stderr').read_text()
        assert 'WARNING: DATA RACE' not in text and text.rstrip().endswith('PASS'), name
        checks[name] = {'status': 'passed', 'required_top_level': sorted(expected), 'top_level_count': len(expected), 'assertion_count': len(passed), 'skip': 0, 'fail': 0, 'race': False}
        tops += len(expected); assertions += len(passed)
    for version in ['v1', 'v2']:
        oracle = read(work / f'native/{version}-oracle.json')
        assert oracle['status'] == 'passed' and oracle['actual_native_go_decisions'] == 192 and oracle['matrix_combinations'] == 144, version
    browser = read(work / 'browser/browser-result.json'); runtime = read(work / 'browser/runtime-proof.json')
    assert browser['status'] == 'passed' and not browser['mocked_api'] and browser['actual_cmd_server'] and len(browser['checks']) >= 18 and browser['browser_errors'] == []
    assert runtime['actual_cmd_server'] and runtime['server_binary_sha256'] == digest(work / 'campusos-server')
    browser_events = [json.loads(line) for line in (work / 'target-browser.jsonl').read_text().splitlines()]
    assert not any(e['Action'] in {'skip', 'fail'} for e in browser_events)
    assert any(e['Action'] == 'pass' and e.get('Test') == 'TestIntegrationActualServerBrowserLifecycle' for e in browser_events)
    before = (work / 'legacy-before.json').read_bytes()
    assert len(json.loads(before)) == 100
    for phase in ['after-up', 'after-guard', 'after-down', 'after-reup']:
        assert (work / f'legacy-{phase}.json').read_bytes() == before, phase
    assert (work / 'machine-before-guard.json').read_bytes() == (work / 'machine-after-guard.json').read_bytes()
    assert 'integration identity data exists' in (work / 'ddl-nonempty-down-rejected.log').read_text()
    assert 'integration management references exist' in (work / 'ddl-references-down-rejected.log').read_text()
    consumed, fresh = map(int, (work / 'ddl-nonce-preservation.txt').read_text().split()); assert fresh > consumed
    assert (work / 'common-passed.txt').read_text().splitlines() == COMMON
    assert 160000 <= int((work / 'pg-version.txt').read_text()) < 170000
    raw_token = re.compile(r'cint1_[a-f0-9]{16}\.[A-Za-z0-9_-]{43}')
    artifacts = {}
    for path in sorted(work.rglob('*')):
        # Compiled test/server bytes are evidence too, not just their logs.
        if path.is_file() and (path.suffix=='.test' or path.name=='campusos-server'):
            artifacts[path.relative_to(ROOT).as_posix()]=digest(path)
            continue
        if path.is_file() and path.suffix in {'.log', '.stderr', '.json', '.jsonl', '.txt'} and path.name != 'source-freeze.json' and 'tmp' not in path.relative_to(work).parts:
            assert not raw_token.search(path.read_text()), ('raw credential in evidence', path.name)
            artifacts[path.relative_to(ROOT).as_posix()] = digest(path)
    return frozen, checks, tops, assertions, browser, runtime, artifacts, consumed, fresh


def report(work, final):
    frozen, checks, tops, assertions, browser, runtime, artifacts, consumed, fresh = verify(work)
    docs = {}
    if final:
        assert (work / 'final-docs-passed.txt').read_text().splitlines() == ['docs-links', 'docs-build', 'line-endings', 'diff', 'architecture', 'skill-bridges']
        for path in DOCUMENTS:
            assert (ROOT / path).is_file(), path
            docs[path] = digest(ROOT / path)
    value = {
        'schema': 'campusos.v12-02c-exit/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(timespec='seconds'),
        'stage': 'V12-02c', 'stage_status': 'completed' if final else 'verified_pending_documents',
        'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed', 'common_gates_status': 'passed', 'document_finalize': 'passed' if final else 'pending',
        'scope': 'Complete planned Integration identity and ingress foundation; Knowledge producers09a/full public Knowledge MCP11 and v5Dispatcher03c retain their actual stage ownership.',
        'current_migration': 25, 'next_migration': 26, 'business_tables': 102, 'executor_tables': 2,
        'worktree_context': {'head': frozen['head'], 'branch': frozen['branch'], 'initial_worktree': 'clean', 'commit_or_push': False, 'shared_database_reset': False, 'historical111_head_inputs_verified': frozen['historical111_inputs_verified_at_original_head']},
        'coverage': {'native_packages': 6, 'required_native_top_level': tops, 'native_passed_assertions': assertions, 'native_skip': 0, 'native_fail': 0, 'race': False, 'native_actual_go_decisions': 384, 'domain_matrix_combinations': 288, 'real_backend_browser_assertions': len(browser['checks']), 'legacy_tables_preserved': 100, 'common_gates': len(COMMON)},
        'native': checks, 'browser': browser, 'runtime': runtime,
        'migration': {'empty_down_up': 'passed', 'nonempty_rollback_guard': 'passed', 'runtime_permission_reference_guard': 'passed', 'legacy_full_rows_before_up_guard_down_reup_equal': True, 'nonce_consumed': consumed, 'nonce_after_reup': fresh},
        'common_gates': {name: 'passed' for name in COMMON}, 'source_sha256': frozen['source_sha256'], 'dependencies': frozen['dependencies'], 'artifacts_sha256': artifacts, 'documents_sha256': docs,
        'boundaries': ['Only dedicated Integration scheme; no User/Admin/Host token fallback.', 'Current exact principal/credential/endpoint/rule facts plus original Community public source ACL.', 'Machine and public source locks last through required typed audit/outbox commit; failure returns no bytes.', 'Only declared public-thread HTTP/MCP calls are registered. Knowledge collection/retrieval producers are not mocked or moved into02c.', 'Linux PG16 and actual Chrome only; Windows/WSL2 and whole-version Final remain later acceptance.']
    }
    write(REPORT, value)
    print('V12-02c:', value['stage_status'], '; native tops:', tops, '; native assertions:', assertions, '; browser:', len(browser['checks']))


def main():
    parser = argparse.ArgumentParser(); action = parser.add_mutually_exclusive_group(required=True)
    for name in ['freeze', 'build', 'finalize', 'verify']:
        action.add_argument('--' + name, type=Path)
    args = parser.parse_args(); mode = next(k for k, v in vars(args).items() if v is not None); work = getattr(args, mode).resolve()
    assert work.is_relative_to(ROOT / '.cache'), 'owned cache work directory required'
    if mode == 'freeze': freeze(work)
    elif mode == 'verify': verify(work); print('Current source and acceptance evidence verified')
    else: report(work, mode == 'finalize')


if __name__ == '__main__': main()
