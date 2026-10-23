#!/usr/bin/env python3
"""Frozen evidence for the complete current Linux V12-07 lifecycle contract."""
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
REPORT = EVIDENCE / 'v12-07-lifecycle-exit-linux-v2.json'
COMMON = ['full-go', 'affected-race', 'contracts', 'architecture', 'admin-build', 'web-directed', 'admin-directed', 'web-format', 'web-build', 'docs-links', 'docs-build', 'line-endings', 'diff']
DOCUMENT = ['docs-links', 'docs-build', 'line-endings', 'diff']
DOCS = ['docs/进度/v1.2-dev/v1.2.131-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件v5安装与操作合同.md', 'docs/architecture/当前架构概览.md', 'docs/help/插件v5包与签名操作说明.md', 'skills/sources/campusos-project-onboarding/references/current-project-status.md']
DEPS = ['v12-06-windows-deferral.json', 'v12-06-runtime-portable-linux.json', 'v12-04-ui-exit-linux.json', 'v12-05-runtime-exit-linux.json', 'v12-07-upgrade-cancel-linux.json', 'v12-07-lifecycle-exit-linux.json']
TESTS = {'unit': {'TestV5CheckpointRestoreAndAuthorizationFence', 'TestV5CheckpointTamperingAndOwnerBoundaries', 'TestV5CheckpointObserveAtomicity', 'TestV5UIOnlyUpgradeActivatesAssetsWithoutRunner', 'TestV5UpgradeStopPathsDoNotPromoteCandidate', 'TestV5UpgradeStoppedRequiresExplicitReadiness', 'TestV5VersionOrdering', 'TestV5UpgradeDisableCancelsPendingVersion', 'TestV5UpgradePrepareSwitchAndVerifiableRollback'}, 'actual': {'TestV5LifecycleActualPostgres', 'TestV5UIOnlyLifecycleActualPostgres', 'TestV5UpgradeCancelActualPostgres'}}


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
        for leaf in ['', '/signed-assets-without-backend-readiness', '/version-cutover-fences-old-ui-leases', '/ui-rollback-rechecks-current-authority-and-owner-snapshots', '/ui-owner-purge-and-unreferenced-package-cleanup']:
            assert 'TestV5UIOnlyLifecycleActualPostgres' + leaf in passed, leaf
        for variant in ['container-http', 'container-grpc', 'wasm']:
            prefix = 'TestV5LifecycleActualPostgres/' + variant
            for leaf in ['', '/expanded-review-and-two-owner-instances', '/candidate-failure-isolation-and-explicit-recovery', '/ready-cutover-and-verified-rollback', '/repeated-reload-event-dedup-and-uninstall']:
                assert prefix + leaf in passed, prefix + leaf
        for transport in ['http', 'grpc']:
            prefix = 'TestV5UpgradeCancelActualPostgres/' + transport
            for path in ['', '/disable-cancels-real-ready-candidate-before-observe', '/version-configurations-retained-and-current-grant-required']:
                assert prefix + path in passed, prefix + path
    return {'tests': sorted(TESTS[name]), 'top_level': len(TESTS[name]), 'pass_events': len(passed), 'fail': 0, 'skip': 0}


def main():
    mode, raw = sys.argv[1:]
    work = Path(raw).resolve()
    assert work.parent == ROOT/'.cache/v12-07-lifecycle' and work.name.startswith('acceptance.')
    current = snapshot()
    frozen = work/'source-freeze.json'
    if mode == 'freeze':
        assert not frozen.exists(), 'fresh owned evidence directory required'
        before = ROOT/'.cache/v12-07-ui-completion/entry-audit.json'
        audit = json.loads(before.read_text())
        assert all(not item['mismatches'] for item in audit['checks'].values())
        assert audit['windows'] == 'deferred_by_user'
        assert audit['sha256'] == sha(EVIDENCE/DEPS[-1])
        assert json.loads((EVIDENCE/DEPS[-1]).read_text())['stage_status'] == 'completed'
        shutil.copyfile(before, work/'predecessor-audit.json')
        frozen.write_text(json.dumps(current, ensure_ascii=False, indent=2)+'\n')
        return
    assert json.loads(frozen.read_text()) == current, 'source/HEAD/dependency drift; use fresh evidence directory'
    if mode == 'source':
        print('source/HEAD/dependencies still match frozen build')
        return
    target = {n: native(work, n) for n in TESTS}
    info = (work/'native/plugin-build-info.txt').read_text()
    assert '-race=true' in info and 'GOOS=linux' in info and 'GOARCH=amd64' in info
    assert (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 3
    assert 'linux cgroup=2' in (work/'environment.txt').read_text()
    assert int((work/'schema-version.txt').read_text().strip()) == 28
    assert (work/'workers-before.txt').read_bytes() == (work/'workers-after.txt').read_bytes()
    digests = json.loads((work/'repo-digests.json').read_text())
    assert len(digests) == 1 and re.fullmatch(r'localhost/campusos-owned-v12-07-upgrade@sha256:[0-9a-f]{64}', digests[0])
    assert 'Untagged: localhost/campusos-owned-v12-07-upgrade:' in (work/'image-cleanup.log').read_text()
    assert 'campusos-v12-07-upgrade-' in (work/'pg-cleanup.log').read_text()
    assert 'Untagged: localhost/campusos-owned-v12-07-lifecycle-wasm:' in (work/'wasm-image-cleanup.log').read_text()
    if mode == 'verify':
        print('verified upgrade cancellation targets', json.dumps(target))
        return
    assert mode == 'seal'
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    assert (work/'document-passed.txt').read_text().splitlines() == DOCUMENT
    report = {'schema': 'campusos.v12-07-lifecycle-exit/v1', 'generated_at': datetime.now(timezone(timedelta(hours=8))).isoformat(), 'stage': 'V12-07', 'stage_status': 'completed', 'scope': 'current_linux_wasm_container_and_ui_only_preserve_lifecycle', 'slice_status': 'stage_exit', 'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed_for_linux_scope', 'windows': 'deferred_by_user', 'target': target, 'actual_postgres': True, 'actual_container': True, 'actual_wasm': True, 'actual_ui_only_http_assets': True, 'ui_only_backend_ready_fabricated': False, 'supersedes_overall_exit': 'v12-07-lifecycle-exit-linux.json', 'transports': ['http/1.1', 'unary-grpc+json/http2'], 'common_gates': {n: 'passed' for n in COMMON}, 'document_gates': {n: 'passed' for n in DOCUMENT}, **current, 'artifacts_sha256': {p.relative_to(ROOT).as_posix(): sha(p) for p in sorted(work.rglob('*')) if p.is_file()}, 'documents_sha256': {n: sha(ROOT/n) for n in DOCS}, 'remaining': ['Windows related acceptance remains explicitly deferred by the user.'], 'unsupported_rejected': ['migrate/reset or changed private schema', 'rollback forbidden by current release', 'missing/tampered/cross-owner/oversized checkpoint'], 'host_private_api': 'not_published; private file bytes are host-owned fixture oracles, not unrestricted guest mounts', 'schema_changed': False, 'migration': {'current': 28, 'next': 29}, 'shared_database_reset': False, 'commit_or_push': False}
    assert not REPORT.exists(), 'historical report must not be overwritten'
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed current Linux V12-07 lifecycle exit; Windows remains deferred')


if __name__ == '__main__':
    main()
