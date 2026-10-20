#!/usr/bin/env python3
"""Evidence for the first08a author SDK slice, never a full stage exit."""
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
from datetime import datetime, timezone, timedelta

ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT/'docs/项目计划书v1/项目计划v1.2/evidence'
REPORT = EVIDENCE/'v12-08a-author-linux.json'
DEPS = ['v12-07-lifecycle-exit-linux-v2.json', 'v12-06-windows-deferral.json']
DOCS = ['docs/进度/v1.2-dev/v1.2.132-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/help/插件v5包与签名操作说明.md', 'docs/architecture/当前架构概览.md', 'skills/sources/campusos-project-onboarding/references/current-project-status.md', 'sdk/go/README.md', 'plugins/campusos.chat-echo/README.md']
COMMON = ['full-go', 'affected-race', 'contracts', 'architecture', 'author-syntax', 'docs-links', 'docs-build', 'line-endings', 'diff']
DOCUMENT = ['docs-links', 'docs-build', 'line-endings', 'diff']
REQUIRED_UNIT = {'TestServeV5ContainerChatHTTPConfigurationIsolation', 'TestServeV5ContainerChatRejectsInvalidProtocol', 'TestServeV5ContainerChatRejectsInvalidTypedRequests', 'TestServeV5ContainerChatBoundsAndConfiguration', 'TestServeV5ContainerChatHandlerFailureClosed', 'TestServeV5ContainerChatDeadlineAndPipeCancellation', 'TestEchoUsesOrdinaryOwnerConfiguration'}
TOP = 'TestV5AuthorContainerActualPostgres'
LEAVES = ['signed-import-and-independent-current-approval', 'two-owner-real-typed-chat-and-configuration-isolation', 'relative-deadline-and-current-configuration-fences', 'reload-revocation-and-owner-uninstall']


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def snapshot():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.ps1', '.json', '.yaml', '.yml', '.html', '.css', '.png', '.svg'}
    sources = {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (Path(n).suffix in suffixes or Path(n).name.startswith('Dockerfile') or n in {'Makefile', '.env.example'})}
    return {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': sources, 'dependencies': {str((EVIDENCE/n).relative_to(ROOT)): sha(EVIDENCE/n) for n in DEPS}}


def native(work, name):
    rows = [json.loads(line) for line in (work/f'native/{name}.jsonl').read_text().splitlines()]
    passed = {r['Test'] for r in rows if r['Action'] == 'pass' and 'Test' in r}
    assert not any(r['Action'] in {'fail', 'skip'} for r in rows), name
    assert any(r['Action'] == 'pass' and 'Test' not in r for r in rows), name
    assert not (work/f'native/{name}.stderr').read_bytes(), name
    assert 'DATA RACE' not in (work/f'native/{name}.jsonl').read_text(), name
    if name == 'unit':
        assert REQUIRED_UNIT <= passed
    else:
        assert {TOP, *[TOP+'/'+leaf for leaf in LEAVES]} == passed
    return {'top_level': len([n for n in passed if '/' not in n]), 'pass_events': len(passed), 'fail': 0, 'skip': 0, 'required': sorted(REQUIRED_UNIT) if name == 'unit' else [TOP]}


def main():
    mode, raw = sys.argv[1:]
    work = Path(raw).resolve()
    assert work.parent == ROOT/'.cache/v12-08a-author' and work.name.startswith('acceptance.')
    current = snapshot()
    frozen = work/'source-freeze.json'
    if mode == 'freeze':
        assert not frozen.exists(), 'fresh evidence directory required'
        audit = ROOT/'.cache/v12-08a-author-preflight/entry-audit.json'
        before = json.loads(audit.read_text())
        assert all(not x['mismatches'] for x in before['checks'].values())
        assert before['report_sha256'] == sha(EVIDENCE/DEPS[0])
        assert json.loads((EVIDENCE/DEPS[0]).read_text())['stage_status'] == 'completed'
        shutil.copyfile(audit, work/'predecessor-audit.json')
        frozen.write_text(json.dumps(current, ensure_ascii=False, indent=2)+'\n')
        return
    assert json.loads(frozen.read_text()) == current, 'source/HEAD/dependency drift: build fresh target'
    if mode == 'source':
        print('current source/HEAD/dependencies match author build')
        return
    tests = {name: native(work, name) for name in ['unit', 'actual']}
    author = json.loads((work/'author-build/author-build.json').read_text())
    assert author['offline'] and not author['host_module_used'] and not author['image_built'] and not author['plugin_executed']
    for name, digest in author['files_sha256'].items():
        assert sha(work/'author-build'/name) == digest, name
    assert (work/'author-build/nonstandard-imports.txt').read_text().splitlines() == ['github.com/campusos/CampusOS/sdk/go', 'example.invalid/campusos-chat-echo']
    for p in (work/'author-build/sdk').glob('*.go'):
        assert sha(p) == sha(ROOT/'sdk/go'/p.name), p.name
    assert sha(work/'author-build/author/main.go') == sha(ROOT/'plugins/campusos.chat-echo/main.go')
    info = (work/'native/test-build-info.txt').read_text()
    assert '-race=true' in info and 'GOOS=linux' in info and 'GOARCH=amd64' in info
    assert (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 3
    assert 'linux cgroup=2' in (work/'environment.txt').read_text()
    assert (work/'schema-before.sql').read_bytes() == (work/'schema-after.sql').read_bytes()
    assert (work/'workers-before.txt').read_bytes() == (work/'workers-after.txt').read_bytes()
    refs = json.loads((work/'repo-digests.json').read_text())
    assert len(refs) == 1 and re.fullmatch(r'localhost/campusos-owned-v12-08a-author@sha256:[0-9a-f]{64}', refs[0])
    verified = json.loads((work/'cli-verify.json').read_text())
    assert verified['signature_verified'] and not verified['installed'] and verified['runtime'] == 'container'
    assert sha(work/'echo.tar.gz') == sha(work/'echo-repeat.tar.gz')
    assert not (work/'signing-private.b64').exists()
    proof = json.loads((work/'native-host-proof.json').read_text())
    assert proof['workers_after'] == 0 and proof['schema_columns_before'] == proof['schema_columns_after']
    assert proof['binary_sha256'] == sha(work/'native/plugin.test')
    assert 'campusos-v12-08a-author-' in (work/'pg-cleanup.log').read_text()
    assert 'Untagged: localhost/campusos-owned-v12-08a-author:' in (work/'image-cleanup.log').read_text()
    if mode == 'verify':
        print('verified first08a author slice', json.dumps(tests))
        return
    assert mode == 'seal'
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    assert (work/'document-passed.txt').read_text().splitlines() == DOCUMENT
    excluded = {'go-cache', 'home', 'tmp', 'gopath', 'gomodcache'}
    artifacts = {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file() and not excluded.intersection(p.relative_to(work).parts)}
    report = {'schema': 'campusos.v12-08a-author-slice/v1', 'generated_at': datetime.now(timezone(timedelta(hours=8))).isoformat(), 'stage': 'V12-08a', 'stage_status': 'in_progress', 'slice': 'independent_headless_container_chat_author_sdk', 'slice_status': 'completed', 'implementation': 'passed_for_slice', 'automation': 'passed_for_slice', 'target_environment': 'linux_native_actual_container_and_postgres', 'windows': 'deferred_by_user', 'target': tests, 'sdk_standard_library_only': True, 'author_build_offline_independent_module': True, 'actual_cli_signed_archive': True, 'actual_postgres': True, 'actual_container_http': True, 'production_api_readonly_deployment_acceptance': 'not_run_in_this_slice', 'pdf_two_package_exit': 'not_run_in_this_slice', 'native_host_pid_and_binary_unchanged': True, 'schema_before_sha256': sha(work/'schema-before.sql'), 'schema_after_sha256': sha(work/'schema-after.sql'), 'common_gates': {n: 'passed' for n in COMMON}, 'document_gates': {n: 'passed' for n in DOCUMENT}, **current, 'artifacts_sha256': artifacts, 'documents_sha256': {n: sha(ROOT/n) for n in DOCS}, 'remaining_for_stage': ['PDF plus headless package installation on the same already-built read-only production host, full host/SPA/image/schema fingerprints and no-restart proof', 'remaining author SDK/CLI/tutorial exit checks for the two-package scenario; AI/RAG actual use belongs to14c'], 'schema_changed': False, 'migration': {'current': 28, 'next': 29}, 'shared_database_reset': False, 'commit_or_push': False}
    assert not REPORT.exists(), 'do not overwrite a historical report'
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed first08a author slice; stage remains in_progress')


if __name__ == '__main__':
    main()
