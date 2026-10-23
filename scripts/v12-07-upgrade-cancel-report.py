#!/usr/bin/env python3
"""Evidence for one upgrade cancellation path, not full V12-07 acceptance."""
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
from datetime import datetime, timedelta, timezone

ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence'
REPORT = EVIDENCE / 'v12-07-upgrade-cancel-linux.json'
COMMON = ['full-go', 'affected-race', 'contracts', 'architecture', 'docs-links', 'docs-build', 'line-endings', 'diff']
DOCUMENT = ['docs-links', 'docs-build', 'line-endings', 'diff']
DOCS = ['docs/进度/v1.2-dev/v1.2.130-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件v5安装与操作合同.md']
DEPS = ['v12-06-windows-deferral.json', 'v12-06-runtime-portable-linux.json', 'v12-04-ui-exit-linux.json', 'v12-05-runtime-exit-linux.json']
TESTS = {'unit': {'TestV5UpgradeDisableCancelsPendingVersion', 'TestV5UpgradePrepareSwitchAndVerifiableRollback'}, 'actual': {'TestV5UpgradeCancelActualPostgres'}}


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def snapshot():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.ps1', '.json', '.yaml', '.yml', '.html', '.css', '.png', '.svg'}
    sources = {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (Path(n).suffix in suffixes or n in {'Makefile', '.env.example', 'deploy/docker/Dockerfile.wasm-worker'})}
    return {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': sources, 'dependencies': {str((EVIDENCE/n).relative_to(ROOT)): sha(EVIDENCE/n) for n in DEPS}}


def native(work, name):
    rows = [json.loads(line) for line in (work/f'native/{name}.jsonl').read_text().splitlines()]
    passed = {r['Test'] for r in rows if r['Action'] == 'pass' and 'Test' in r}
    assert not any(r['Action'] in {'fail', 'skip'} for r in rows), name
    assert {n for n in passed if '/' not in n} == TESTS[name], name
    assert any(r['Action'] == 'pass' and 'Test' not in r for r in rows), name
    assert not (work/f'native/{name}.stderr').read_bytes() and 'DATA RACE' not in (work/f'native/{name}.log').read_text(), name
    if name == 'actual':
        for transport in ['http', 'grpc']:
            prefix = 'TestV5UpgradeCancelActualPostgres/' + transport
            for path in ['', '/disable-cancels-real-ready-candidate-before-observe', '/version-configurations-retained-and-current-grant-required']:
                assert prefix + path in passed, prefix + path
    return {'tests': sorted(TESTS[name]), 'top_level': len(TESTS[name]), 'pass_events': len(passed), 'fail': 0, 'skip': 0}


def main():
    mode, raw = sys.argv[1:]
    work = Path(raw).resolve()
    assert work.parent == ROOT/'.cache/v12-07-cancel' and work.name.startswith('acceptance.')
    current = snapshot()
    frozen = work/'source-freeze.json'
    if mode == 'freeze':
        assert not frozen.exists(), 'fresh owned evidence directory required'
        before = ROOT/'.cache/v12-07-resume/entry-audit.json'
        audit = json.loads(before.read_text())
        assert all(not item['mismatches'] for item in audit['checks'].values())
        assert audit['states'] == {'linux_scope': 'completed', 'windows_scope': 'deferred', 'windows_native_acceptance': 'not_run', 'linux_followup_may_proceed': True}
        assert audit['deferral_report_sha256'] == sha(EVIDENCE/DEPS[0])
        assert all(json.loads((EVIDENCE/n).read_text())['stage_status'] == 'completed' for n in DEPS[2:])
        shutil.copyfile(before, work/'predecessor-audit.json')
        shutil.copyfile(ROOT/'.cache/v12-07-resume/dependency-audit.json', work/'dependency-audit.json')
        frozen.write_text(json.dumps(current, ensure_ascii=False, indent=2)+'\n')
        return
    assert json.loads(frozen.read_text()) == current, 'source/HEAD/dependency drift; use fresh evidence directory'
    if mode == 'source':
        print('source/HEAD/dependencies still match frozen build')
        return
    target = {n: native(work, n) for n in TESTS}
    info = (work/'native/plugin-build-info.txt').read_text()
    assert '-race=true' in info and 'GOOS=linux' in info and 'GOARCH=amd64' in info
    assert (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 2
    assert 'linux cgroup=2' in (work/'environment.txt').read_text()
    assert int((work/'schema-version.txt').read_text().strip()) == 28
    assert (work/'workers-before.txt').read_bytes() == (work/'workers-after.txt').read_bytes()
    digests = json.loads((work/'repo-digests.json').read_text())
    assert len(digests) == 1 and re.fullmatch(r'localhost/campusos-owned-v12-07-upgrade@sha256:[0-9a-f]{64}', digests[0])
    assert 'Untagged: localhost/campusos-owned-v12-07-upgrade:' in (work/'image-cleanup.log').read_text()
    assert 'campusos-v12-07-upgrade-' in (work/'pg-cleanup.log').read_text()
    if mode == 'verify':
        print('verified upgrade cancellation targets', json.dumps(target))
        return
    assert mode == 'seal'
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    assert (work/'document-passed.txt').read_text().splitlines() == DOCUMENT
    report = {'schema': 'campusos.v12-07-upgrade-cancel/v1', 'generated_at': datetime.now(timezone(timedelta(hours=8))).isoformat(), 'stage': 'V12-07', 'stage_status': 'in_progress', 'slice': 'disable-cancels-uncommitted-upgrade', 'slice_status': 'completed', 'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed_for_linux_slice', 'windows': 'deferred_by_user', 'target': target, 'actual_postgres': True, 'actual_container': True, 'transports': ['http/1.1', 'unary-grpc+json/http2'], 'common_gates': {n: 'passed' for n in COMMON}, 'document_gates': {n: 'passed' for n in DOCUMENT}, **current, 'artifacts_sha256': {p.relative_to(ROOT).as_posix(): sha(p) for p in sorted(work.rglob('*')) if p.is_file()}, 'documents_sha256': {n: sha(ROOT/n) for n in DOCS}, 'remaining': ['Full upgrade cutover/re-review/rollback and uninstall lifecycle acceptance.', 'Configuration/private data snapshots and verified migration/recovery paths.', 'Windows work remains deferred; this report is not a cross-platform stage exit.'], 'schema_changed': False, 'migration': {'current': 28, 'next': 29}, 'shared_database_reset': False, 'commit_or_push': False}
    assert not REPORT.exists(), 'historical report must not be overwritten'
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed one Linux upgrade cancellation slice; V12-07 remains in_progress')


if __name__ == '__main__':
    main()
