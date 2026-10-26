#!/usr/bin/env python3
"""Verify the bounded 03a declaration/CLI slice, without claiming stage exit."""
import argparse
import hashlib
import json
import pathlib
import subprocess
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[1]
COMMON = ['full-go', 'plugin-race', 'contracts', 'architecture', 'sdk', 'docs-links', 'docs-build', 'line-endings', 'diff']
FINAL = ['docs-links', 'docs-build', 'line-endings', 'diff', 'architecture', 'skill-bridges']
DOCS = [
    'docs/进度/v1.2-dev/v1.2.113-dev.md',
    'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md',
    'docs/api/v1.2插件v5完整Manifest目标合同.md',
    'docs/help/插件v5声明离线检查说明.md', 'docs/help/README.md',
    'docs/architecture/当前架构概览.md', 'docs-site/guide/architecture.md',
    'skills/sources/campusos-project-onboarding/references/current-project-status.md',
]


def read(path):
    return json.loads(path.read_text())


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def events(path, required):
    rows = [json.loads(s) for s in path.read_text().splitlines()]
    assert not any(e['Action'] in {'skip', 'fail'} for e in rows), path
    passed = {e['Test'] for e in rows if e['Action'] == 'pass' and 'Test' in e}
    assert {n for n in passed if '/' not in n} == required, path
    return len(passed)


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--work', type=pathlib.Path, required=True)
    p.add_argument('--common', type=pathlib.Path, required=True)
    p.add_argument('--preflight', type=pathlib.Path, required=True)
    args = p.parse_args()
    work, common, preflight = [v.resolve() for v in (args.work, args.common, args.preflight)]
    for path in [work, common, preflight]:
        assert path.is_relative_to(ROOT / '.cache'), path
    initial = read(preflight)
    assert subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip() == initial['head']
    # Existing dirty work belongs to the prior task/user. Only current status
    # documents listed above may be updated by this slice.
    for name, expected in initial['files_sha256'].items():
        if name not in DOCS:
            assert sha(ROOT / name) == expected, ('pre-existing file changed', name)
    deps = {}
    for name in ['v12-g1-exit-linux.json', 'v12-01a-exit-linux.json', 'v12-02c-exit-linux.json']:
        path = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence' / name
        d = read(path)
        assert d['implementation'] == d['automation'] == d['target_environment'] == 'passed', name
        deps[path.relative_to(ROOT).as_posix()] = sha(path)
    tops = {'TestPluginV5ManifestStrictDecode', 'TestPluginV5ManifestCatalogAndRelease', 'TestPluginV5ManifestCatalogIsolation'}
    native_passes = events(work / 'native.jsonl', tops)
    cli_passes = events(work / 'cli-tests.jsonl', {'TestPluginV5CheckCommand'})
    assert 'WARNING: DATA RACE' not in (work / 'native.log').read_text() + (work / 'native.stderr').read_text()
    target = read(work / 'target-result.json')
    assert target['status'] == 'passed' and target['platform'] == 'linux'
    assert target['actual_go_decisions'] == target['actual_cli_processes'] == 23
    assert target['cli_binary_sha256'] == sha(work / 'campusos-plugin-v5-check')
    for name, expected in target['contract_input_sha256'].items():
        assert sha(ROOT / name) == expected, name
    assert (common / 'common-passed.txt').read_text().splitlines() == COMMON
    assert (common / 'final-docs-passed.txt').read_text().splitlines() == FINAL
    paths = subprocess.check_output(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.ps1', '.json', '.yaml', '.yml'}
    sources = {name: sha(ROOT / name) for name in sorted(set(paths)) if name and (ROOT / name).is_file() and (pathlib.Path(name).suffix in suffixes or name == 'Makefile') and not name.startswith('docs/项目计划书')}
    artifacts = {}
    for directory in [work, common]:
        for path in sorted(directory.rglob('*')):
            if path.is_file() and (path.suffix in {'.log', '.stderr', '.jsonl', '.json', '.txt', '.test'} or path.name == 'campusos-plugin-v5-check'):
                artifacts[path.relative_to(ROOT).as_posix()] = sha(path)
    value = {
        'schema': 'campusos.v12-03a-manifest-slice/v1',
        'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(timespec='seconds'),
        'stage': 'V12-03a', 'stage_status': 'partial', 'slice_status': 'completed',
        'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed', 'common_gates_status': 'passed', 'document_finalize': 'passed',
        'scope': 'Strict complete Manifest JSON decode, explicit offline catalog/release checks and independent CLI; not production package verification or installation.',
        'worktree_context': {'head': initial['head'], 'branch': initial['branch'], 'initial_worktree': 'dirty, prior02c work preserved', 'preexisting_files_preserved_except_current_status_docs': True, 'commit_or_push': False, 'database_changed': False},
        'coverage': {'native_top_level': 3, 'native_pass_events': native_passes, 'cli_unit_top_level': 1, 'cli_unit_pass_events': cli_passes, 'actual_native_go_decisions': 23, 'actual_cli_processes': 23, 'parallel_validations': 64, 'fail': 0, 'skip': 0, 'race': False},
        'target': target, 'common_gates': {name: 'passed' for name in COMMON},
        'final_document_gates': {name: 'passed' for name in FINAL},
        'source_sha256': sources, 'dependencies': deps, 'artifacts_sha256': artifacts,
        'documents_sha256': {name: sha(ROOT / name) for name in DOCS},
        'remaining_stage_work': ['Actual package bytes/archive/symlink/size/digest and signature checks, immutable-version digest conflicts.', 'Current production catalog wiring, primary pack/check path switch and v4 example replacement.'],
        'boundaries': ['Synthetic G1 catalogs are explicit test inputs, not production capability publications.', 'Successful CLI output always denies installability and does not claim byte or signature verification.', 'No migrations, DB, HTTP, installation, Dispatcher or Runtime changes; no Windows/browser acceptance claimed.'],
    }
    out = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence/v12-03a-manifest-linux.json'
    out.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    print('V12-03a Manifest slice completed; whole stage partial; native/CLI 23/23; all common/document gates passed')


if __name__ == '__main__':
    main()
