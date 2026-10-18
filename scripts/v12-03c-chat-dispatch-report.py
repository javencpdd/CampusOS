#!/usr/bin/env python3
"""Evidence for bounded prompt-only Chat dispatch, never a full03c exit."""
import hashlib
import json
import pathlib
import subprocess
import sys
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORT = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence/v12-03c-chat-dispatch-linux.json'
COMMON = ['full-go', 'related-race', 'contracts', 'architecture', 'docs-links', 'docs-build', 'line-endings', 'diff']
DOCS = ['docs/进度/v1.2-dev/v1.2.118-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件双向调用事件与生命周期合同.md', 'docs/architecture/当前架构概览.md', 'docs-site/guide/architecture.md', 'skills/sources/campusos-project-onboarding/references/current-project-status.md']


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def sources():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.json', '.yaml', '.yml'}
    return {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n == 'Makefile')}


def dependencies():
    return {str(p.relative_to(ROOT)): sha(p) for n in ['v12-03c-directory-linux.json', 'v12-03b-exit-linux.json', 'v12-02a-exit-linux.json', 'v12-01b-exit-linux.json'] for p in [REPORT.parent/n]}


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
    assert work.parent == ROOT/'.cache/v12-03c-chat' and work.name.startswith('acceptance.')
    frozen = {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': sources(), 'dependencies': dependencies()}
    snapshot = work/'source-freeze.json'
    if mode == 'freeze':
        snapshot.write_text(json.dumps(frozen, ensure_ascii=False, indent=2)+'\n')
        return
    assert json.loads(snapshot.read_text()) == frozen, 'source/dependency changed after target freeze'
    if mode == 'verify':
        return
    assert mode == 'seal'
    native = events(work/'native/plugin.jsonl', {'TestV5ContributionTypedLookup', 'TestV5ContributionConflictAndExplicitSelection', 'TestV5ContributionLiveLifecycle', 'TestV5ContributionPaginationAndBudget', 'TestV5ChatDispatchPromptOnly', 'TestV5ChatDispatchStrictBoundaries', 'TestV5ChatDispatchResponseValidation', 'TestV5ChatDispatchCurrentFactsBeforeAndAfter', 'TestV5ChatDispatchDeadlineAndConcurrency'})
    assembly = events(work/'native/assembly.jsonl', {'TestV5ContributionDirectoryAssembly', 'TestV5ChatDispatcherAssemblyHasNoFakeRunner'})
    pg = events(work/'native/pg.jsonl', {'TestPostgresV5ChatDispatch'})
    reload = events(work/'native/reload.jsonl', {'TestPostgresV5ChatDispatchRestart'})
    assert 'Linux' in (work/'environment.txt').read_text()
    assert (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 2
    assert 160000 <= int((work/'pg-version.txt').read_text()) < 170000
    assert 'campusos-v12-03c-chat-' in (work/'cleanup.log').read_text() and 'Error' not in (work/'cleanup.log').read_text()
    assert (work/'postgres-restart.log').read_text().strip()
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    report = {'schema': 'campusos.v12-03c-chat/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(),
              'stage': 'V12-03c', 'stage_status': 'in_progress', 'slice': 'prompt-only host-to-contribution ai.chat Dispatcher with compiled contract transport',
              'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed',
              'native': native, 'server_assembly': assembly, 'postgres': pg, 'postgres_restart_reload': reload,
              'common_gates': {n: 'passed' for n in COMMON}, **frozen,
              'artifacts_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()},
              'documents_sha256': {n: sha(ROOT/n) for n in DOCS},
              'boundaries': ['Only a current interactive User may forward an explicit prompt with context_class=none; no host resource reads.',
                             'A compiled consumer binds its Feature reader/ID and exact owner/release/generation transport. Neither comes from request fields.',
                             'Production has no consumer or transport binding and fails runtime_unavailable; built-in AI/RAG stays independent.',
                             'Native/PG target uses a bounded compiled transport plus bounded host-ready observation, not a real Wasm/Container Runner.',
                             'Context deadlines and bounded output reject late data. Transport must honor context; no hard preemption/cross-process isolation claim.',
                             'Concurrency is process-local: current grant per instance and a deployment total ceiling of8, not a cluster quota.',
                             'Plugin-to-Host credentials/capabilities, preview/knowledge invocation, managed publication, lifecycle drain/cancel and events remain pending.',
                             'No browser/Windows/whole-system restore, shared database reset, migration rewrite, commit or push.'],
              'commit_or_push': False, 'shared_database_reset': False}
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed03c prompt-only Chat slice:', native, assembly, pg, reload)


if __name__ == '__main__':
    main()
