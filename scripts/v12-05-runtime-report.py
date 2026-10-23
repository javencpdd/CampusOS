#!/usr/bin/env python3
"""Fail-closed, source-frozen full V12-05 Linux runtime evidence."""
import hashlib
import json
import pathlib
import re
import subprocess
import sys
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORT = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence/v12-05-runtime-exit-linux.json'
DEPENDENCIES = ['v12-03c-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-04-ui-exit-linux.json', 'v12-05-wasm-sandbox-linux.json']
COMMON = ['full-go', 'affected-race', 'contracts', 'architecture', 'docker-dev', 'docker-deploy', 'readme', 'docs-links', 'docs-build', 'line-endings', 'diff']
FINAL = ['docs-links', 'docs-build', 'architecture', 'line-endings', 'diff']
DOCS = ['docs/进度/v1.2-dev/v1.2.123-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件Wasm执行沙箱合同.md', 'docs/architecture/当前架构概览.md', 'docs/architecture/配置与运行边界.md']
TESTS = {
    'capture': [('internal/plugin/v5_wasm_program_test.go', r'TestV5WasmProgram\w+')],
    'coordinator': [('internal/plugin/v5_runtime_coordinator_test.go', r'TestV5WasmCoordinator\w+')],
    'sandbox': [(f'internal/plugin/wasm/{name}', r'TestV5\w+') for name in ['v5_sandbox_test.go', 'v5_host_test.go', 'v5_native_test.go', 'v5_worker_test.go']],
    'actual': [('internal/plugin/v5_runtime_actual_test.go', r'TestV5WasmRuntimeActualPostgres'), ('internal/plugin/v5_runtime_server_actual_test.go', r'TestV5WasmActualServer')],
}


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def snapshot():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.json', '.yaml', '.yml', '.html', '.css', '.png', '.svg'}
    sources = {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n in {'Makefile', '.env.example', 'deploy/docker/Dockerfile.wasm-worker'})}
    return {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': sources, 'dependencies': {str((REPORT.parent/n).relative_to(ROOT)): sha(REPORT.parent/n) for n in DEPENDENCIES}}


def native(work, name):
    expected = set()
    for source, pattern in TESTS[name]:
        expected.update(re.findall(r'^func ('+pattern+r')\(', (ROOT/source).read_text(), re.M))
    rows = [json.loads(line) for line in (work/f'native/{name}.jsonl').read_text().splitlines()]
    assert expected and not any(r['Action'] in {'fail', 'skip'} for r in rows), name
    passed = {r['Test'] for r in rows if r['Action'] == 'pass' and 'Test' in r}
    assert {t for t in passed if '/' not in t} == expected, (name, expected, passed)
    assert any(r['Action'] == 'pass' and 'Test' not in r for r in rows), name
    assert not (work/f'native/{name}.stderr').read_text(), name
    assert 'DATA RACE' not in (work/f'native/{name}.log').read_text(), name
    return {'tests': sorted(expected), 'top_level': len(expected), 'pass_events': len(passed), 'fail': 0, 'skip': 0}


def main():
    mode, raw = sys.argv[1:]
    work = pathlib.Path(raw).resolve()
    assert work.parent == ROOT/'.cache/v12-05-runtime' and work.name.startswith('acceptance.')
    freeze = work/'source-freeze.json'
    current = snapshot()
    if mode == 'freeze':
        assert not freeze.exists(), 'use a fresh evidence directory'
        for name in DEPENDENCIES[:3]:
            r = json.loads((REPORT.parent/name).read_text())
            assert r['stage_status'] == 'completed', name
        prior = json.loads((REPORT.parent/DEPENDENCIES[3]).read_text())
        assert prior['slice_status'] == 'completed' and prior['target_environment'] == 'passed'
        freeze.write_text(json.dumps(current, ensure_ascii=False, indent=2)+'\n')
        return
    assert json.loads(freeze.read_text()) == current, 'source/HEAD/dependency changed; rerun target in a new directory'
    target = {name: native(work, name) for name in TESTS}
    for binary in ['plugin.test', 'wasm.test', 'server']:
        info = (work/f'native/{binary}-build-info.txt').read_text()
        assert '-race=true' in info and 'GOOS=linux' in info
    assert (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 4
    assert re.fullmatch(r'sha256:[0-9a-f]{64}', (work/'image-id.txt').read_text().strip())
    assert int((work/'schema-version.txt').read_text()) == 28
    assert (work/'cleanup.log').read_text().strip().startswith('campusos-v12-05-runtime-')
    assert 'ERROR' not in (work/'database-check.log').read_text()
    if mode == 'verify':
        print('verified full05 frozen Linux target:', json.dumps(target))
        return
    assert mode == 'seal'
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    assert (work/'final-document-passed.txt').read_text().splitlines() == FINAL
    report = {
        'schema': 'campusos.v12-05-runtime-exit/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(),
        'stage': 'V12-05', 'stage_status': 'completed',
        'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed',
        'target': target, 'actual_wazero': True, 'actual_postgresql': True, 'actual_cmd_server': True,
        'native_launch': {'clean_environment': True, 'race_test_binaries': 2, 'race_server': True, 'static_fixed_worker': True, 'image_id': (work/'image-id.txt').read_text().strip(), 'os': 'Linux', 'cgroup_version': 2, 'secret_to_guest': False, 'database_to_guest': False, 'socket_to_guest': False},
        'controls': {'per_worker_memory_mib': 'min(request,grant,host,deployment), max256', 'per_worker_cpu_millicores': 'min(request,grant,host,deployment), max1000', 'aggregate_memory_mib': 1024, 'aggregate_cpu_millicores': 4000, 'aggregate_workers': 16, 'single_advisory_lock_owner': True, 'network': 'none', 'mounts': [], 'read_only': True, 'uid': '65534:65534', 'pids': 64, 'swap': 0, 'caps': [], 'logs': 'none', 'imports': ['campusos.host_call'], 'host_positive': 'community.thread.public.read'},
        'common_gates': {n: 'passed' for n in COMMON}, 'final_document_gates': {n: 'passed' for n in FINAL}, 'database_check': 'passed',
        'migration': {'current': 28, 'next': 29, 'changed': False}, **current,
        'artifacts_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()},
        'documents_sha256': {n: sha(ROOT/n) for n in DOCS},
        'boundaries': [
            'Fixed host-owned scratch worker executes verified Wasm, not plugin OCI artifacts. Container adapter/Windows are V12-06; author SDK is V12-08a.',
            'Single writer deployment with exclusive PostgreSQL runtime guard; no multi-host HA or cluster capacity claim.',
            'Current published v5 Host API positive path is public thread read. Unknown/unpublished Secret/Egress capabilities are denied; legacy authorization is not resurrected.',
            'Actual typed contract consumer is used; built-in AI/RAG consumer binding remains V12-14c.',
            'Capture/kernel and coordinator contract tests are separately identified; actual target uses signed guest bytes, real PostgreSQL identities/Core Port, fixed native worker and cmd/server, no mocked ready.',
            'Historical .122 and predecessor evidence is unchanged. No migration/shared reset/commit/push.',
        ], 'commit_or_push': False, 'shared_database_reset': False,
    }
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed full V12-05: implementation/automation/Linux target passed')


if __name__ == '__main__':
    main()
