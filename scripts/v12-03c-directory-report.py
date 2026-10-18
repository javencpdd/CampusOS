#!/usr/bin/env python3
"""Evidence for the first03c directory slice, never a full03c exit."""
import hashlib
import json
import pathlib
import subprocess
import sys
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORT = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence/v12-03c-directory-linux.json'
COMMON = ['full-go', 'related-race', 'contracts', 'architecture', 'docs-links', 'docs-build', 'line-endings', 'diff']
DOCS = ['docs/进度/v1.2-dev/v1.2.117-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件双向调用事件与生命周期合同.md', 'docs/architecture/当前架构概览.md', 'docs-site/guide/architecture.md', 'skills/sources/campusos-project-onboarding/references/current-project-status.md']


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def sources():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.json', '.yaml', '.yml'}
    return {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n == 'Makefile')}


def dependencies():
    return {str(p.relative_to(ROOT)): sha(p) for s in ['03b', '02a', '01b'] for p in [REPORT.parent/f'v12-{s}-exit-linux.json']}


def events(p, expected):
    rows = [json.loads(s) for s in p.read_text().splitlines()]
    assert not any(r['Action'] in {'fail', 'skip'} for r in rows), p
    passed = {r['Test'] for r in rows if r['Action'] == 'pass' and 'Test' in r}
    tops = {n for n in passed if '/' not in n}
    assert tops == expected, (p, tops)
    assert any(r['Action'] == 'pass' and 'Test' not in r for r in rows), p
    return {'top_level': len(tops), 'pass_events': len(passed), 'fail': 0, 'skip': 0}


def main():
    mode, work = sys.argv[1], pathlib.Path(sys.argv[2]).resolve()
    assert work.parent == ROOT/'.cache/v12-03c-directory' and work.name.startswith('acceptance.')
    frozen = {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': sources(), 'dependencies': dependencies()}
    snapshot = work/'source-freeze.json'
    if mode == 'freeze':
        snapshot.write_text(json.dumps(frozen, ensure_ascii=False, indent=2)+'\n')
        return
    assert json.loads(snapshot.read_text()) == frozen, 'source/dependency changed after target freeze'
    if mode == 'verify':
        return
    assert mode == 'seal'
    native = events(work/'native/plugin.jsonl', {'TestV5ContributionTypedLookup', 'TestV5ContributionConflictAndExplicitSelection', 'TestV5ContributionLiveLifecycle', 'TestV5ContributionPaginationAndBudget'})
    assembly = events(work/'native/assembly.jsonl', {'TestV5ContributionDirectoryAssembly'})
    pg = events(work/'native/pg.jsonl', {'TestPostgresV5ContributionDirectory'})
    reload = events(work/'native/reload.jsonl', {'TestPostgresV5ContributionReload'})
    assert 'Linux' in (work/'environment.txt').read_text()
    assert (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 2
    assert 160000 <= int((work/'pg-version.txt').read_text()) < 170000
    assert 'campusos-v12-03c-directory-' in (work/'cleanup.log').read_text() and 'Error' not in (work/'cleanup.log').read_text()
    assert (work/'postgres-restart.log').read_text().strip()
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    report = {'schema': 'campusos.v12-03c-directory/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(),
              'stage': 'V12-03c', 'stage_status': 'in_progress', 'slice': 'host-only typed contribution lookup and selection',
              'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed',
              'native': native, 'server_assembly': assembly, 'postgres': pg, 'postgres_restart_reload': reload,
              'common_gates': {n: 'passed' for n in COMMON}, **frozen,
              'artifacts_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()},
              'documents_sha256': {n: sha(ROOT/n) for n in DOCS},
              'boundaries': ['Metadata lookup is not runtime publication or execution authorization.',
                             'No Dispatcher, feature/resource ACL invocation, Host API credential, cancellation/drain or event delivery yet.',
                             'Ready states in tests use a bounded compiled host observation only; production has no runner.',
                             'Host owner comes from a trusted adapter, not a plugin or HTTP field. No new HTTP routes.',
                             'Discovery is bounded to20 pages of50 families and fails closed on exhaustion.',
                             'No built-in AI/RAG dependency, shared database reset, migration rewrite, commit or push.'],
              'commit_or_push': False, 'shared_database_reset': False}
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed first03c directory slice:', native, assembly, pg, reload)


if __name__ == '__main__':
    main()
