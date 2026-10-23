#!/usr/bin/env python3
"""Seal the08a PDF/read-only production host slice; never the two-package exit."""
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
REPORT = EVIDENCE/'v12-08a-readonly-pdf-linux.json'
DEPS = ['v12-08a-author-linux.json', 'v12-07-lifecycle-exit-linux-v2.json', 'v12-06-windows-deferral.json']
DOCS = ['docs/进度/v1.2-dev/v1.2.133-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/help/插件v5包与签名操作说明.md', 'docs/architecture/当前架构概览.md', 'skills/sources/campusos-project-onboarding/references/current-project-status.md']
COMMON = ['full-go', 'affected-race', 'contracts', 'architecture', 'runner-syntax', 'docs-links', 'docs-build', 'line-endings', 'diff']
DOCUMENT = ['docs-links', 'docs-build', 'line-endings', 'diff']
TOP = 'TestPluginV5ReadOnlyHostActualServerBrowser'
UNIT = 'TestV5ReadOnlyHostRejectsFileModeOnlyEvidence'


def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as source:
        for chunk in iter(lambda: source.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def snapshot():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.ps1', '.json', '.yaml', '.yml', '.html', '.css', '.png', '.svg'}
    sources = {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (Path(n).suffix in suffixes or Path(n).name.startswith('Dockerfile') or n in {'Makefile', '.env.example'})}
    return {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': sources, 'dependencies': {str((EVIDENCE/n).relative_to(ROOT)): sha(EVIDENCE/n) for n in DEPS}}


def builds(work):
    files = [work/'native/campusos-server', work/'native/server.test', work/'native/campusosctl']
    for directory in ['web/dist', 'admin/dist', 'sdk/typescript/dist', 'plugins/campusos.pdf-viewer/dist']:
        found = sorted(p for p in (ROOT/directory).rglob('*') if p.is_file())
        assert found, directory
        files.extend(found)
    files.extend(sorted(p for p in (work/'input').rglob('*') if p.is_file()))
    return {str(p.relative_to(ROOT)): sha(p) for p in files}


def native(work, name, required):
    rows = [json.loads(line) for line in (work/f'native/{name}.jsonl').read_text().splitlines()]
    assert not any(row['Action'] in {'fail', 'skip'} for row in rows), name
    assert {row['Test'] for row in rows if row['Action'] == 'pass' and 'Test' in row} == {required}, name
    assert any(row['Action'] == 'pass' and 'Test' not in row for row in rows), name
    assert 'DATA RACE' not in (work/f'native/{name}.log').read_text(), name
    stderr = (work/f'native/{name}.stderr').read_text().splitlines()
    if name == 'actual':
        assert len(stderr) == 2 and all(re.fullmatch(r'\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} (default category ready|bootstrap administrator ready \(administrator created using configured bootstrap secret\); credentials were not logged)', line) for line in stderr)
    else:
        assert not stderr, name
    return {'top_level': 1, 'pass_events': 1, 'fail': 0, 'skip': 0, 'required': required}


def main():
    mode, raw = sys.argv[1:]
    work = Path(raw).resolve()
    assert work.parent == ROOT/'.cache/v12-08a-readonly-pdf' and work.name.startswith('acceptance.')
    frozen = work/'source-freeze.json'
    current = snapshot()
    if mode == 'freeze':
        assert not frozen.exists(), 'fresh evidence directory required'
        audit = ROOT/'.cache/v12-08a-readonly-pdf-preflight/entry-audit.json'
        before = json.loads(audit.read_text())
        assert all(not x['mismatches'] for x in before['checks'].values())
        assert before['report_sha256'] == sha(EVIDENCE/DEPS[0])
        assert json.loads((EVIDENCE/DEPS[1]).read_text())['stage_status'] == 'completed'
        shutil.copyfile(audit, work/'predecessor-audit.json')
        frozen.write_text(json.dumps(current, ensure_ascii=False, indent=2)+'\n')
        return
    assert json.loads(frozen.read_text()) == current, 'source/HEAD/dependency drift: build fresh target'
    if mode == 'source':
        return
    build_path = work/'production-build-freeze.json'
    if mode == 'built':
        assert not build_path.exists()
        build_path.write_text(json.dumps(builds(work), ensure_ascii=False, indent=2)+'\n')
        return
    build_sha256 = json.loads(build_path.read_text())
    assert build_sha256 == builds(work), 'server/test/CLI/production SPA/package build drift'
    tests = {'unit': native(work, 'unit', UNIT), 'actual': native(work, 'actual', TOP)}
    browser = json.loads((work/'browser/browser-result.json').read_text())
    server = json.loads((work/'browser/server-result.json').read_text())
    proof = json.loads((work/'readonly-host-proof.json').read_text())
    assert browser['status'] == server['status'] == proof['status'] == 'passed'
    assert not browser['mocked_api'] and browser['actual_cmd_server'] and not browser['browser_errors']
    assert len(browser['checks']) >= 50 and browser['pdf_presentations'] == 24 and not browser['diagnostic_tail_only']
    for required in ['actual Admin SPA refresh-cookie restoration', 'actual generic User UI', 'actual Admin operation query', 'elapsed real Grant expiry', 'expired own installation', 'late real range response', 'parent CSP blocks', 'no active plugin']:
        assert any(required in check for check in browser['checks']), required
    assert server['actual_pg_configuration'] and server['typed_grant_audits'] >= 3
    assert proof['actual_cmd_server'] and proof['before'] == proof['after'] and proof['restart_count'] == 0
    assert proof['readonly_errno'] == 'EROFS' and len(proof['protected_files']) == 11
    assert len(proof['live_root_protected_files']) == 10 and proof['before']['binary_mount_readonly'] and proof['after']['binary_mount_readonly']
    assert proof['new_source_file_rejected'] and proof['runtime_data_writable'] and proof['same_mount_namespace']
    assert proof['before']['binary_sha256'] == server['server_binary_sha256'] == sha(work/'native/campusos-server')
    assert not proof['headless_package_installed']
    assert not (work/'browser/browser-input.json').exists() and not (work/'signing-private.b64').exists()
    assert sha(work/'input/pdf-viewer.tar.gz') == sha(work/'pdf-repeat.tar.gz')
    verified = json.loads((work/'cli-verify.json').read_text())
    assert verified['signature_verified'] and not verified['installed'] and verified['runtime'] == 'none'
    assert (work/'schema-before.sql').read_bytes() == (work/'schema-after.sql').read_bytes()
    assert (work/'workers-before.txt').read_bytes() == (work/'workers-after.txt').read_bytes()
    assert int((work/'schema-version.txt').read_text().strip()) == 28
    assert 160000 <= int((work/'pg-version.txt').read_text()) < 170000
    assert 'campusos-v12-08a-readonly-pdf-' in (work/'pg-cleanup.log').read_text()
    assert 'Linux' in (work/'environment.txt').read_text()
    assert 'bubblewrap ' in (work/'environment.txt').read_text()
    assert (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 3
    for name in ['server-build-info.txt', 'test-build-info.txt']:
        info = (work/'native'/name).read_text()
        assert '-race=true' in info and 'GOOS=linux' in info and 'GOARCH=amd64' in info
    if mode == 'verify':
        print('verified08a read-only PDF slice:', len(browser['checks']), 'real browser checks,24 PDF presentations,no restart')
        return
    assert mode == 'seal'
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    assert (work/'document-passed.txt').read_text().splitlines() == DOCUMENT
    excluded = {'home', 'tmp'}
    artifacts = {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file() and not excluded.intersection(p.relative_to(work).parts)}
    report = {'schema': 'campusos.v12-08a-readonly-pdf-slice/v1', 'generated_at': datetime.now(timezone(timedelta(hours=8))).isoformat(), 'stage': 'V12-08a', 'stage_status': 'in_progress', 'slice': 'signed_pdf_on_readonly_production_api_and_spa', 'slice_status': 'completed', 'implementation': 'passed_for_slice', 'automation': 'passed_for_slice', 'target_environment': 'passed_linux_native_readonly_cmd_server_actual_browser_pg16', 'windows': 'deferred_by_user', 'target': tests, 'browser': browser, 'server': server, 'readonly_host': proof, 'actual_cli_signed_pdf_archive': True, 'headless_same_host_exit': 'not_run_in_this_slice', 'containerized_host_image_exit': 'not_run_in_this_slice', 'schema_before_sha256': sha(work/'schema-before.sql'), 'schema_after_sha256': sha(work/'schema-after.sql'), 'common_gates': {n: 'passed' for n in COMMON}, 'document_gates': {n: 'passed' for n in DOCUMENT}, **current, 'production_build_sha256': build_sha256, 'artifacts_sha256': artifacts, 'documents_sha256': {n: sha(ROOT/n) for n in DOCS}, 'remaining_for_stage': ['PDF and headless author package on the same read-only host with typed contract consumer and two independent headless instances; full server/SPA/image/schema fingerprints and no restart', 'AI/RAG production consumer belongs to14c; Windows remains deferred by user'], 'schema_changed': False, 'migration': {'current': 28, 'next': 29}, 'shared_database_reset': False, 'commit_or_push': False}
    assert not REPORT.exists(), 'do not overwrite historical evidence'
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed08a read-only PDF slice; full stage remains in_progress')


if __name__ == '__main__':
    main()
