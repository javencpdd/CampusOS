#!/usr/bin/env python3
"""Seal actual Linux Container evidence; never infer Windows acceptance."""
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import sys
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORT = ROOT/'docs/项目计划书v1/项目计划v1.2/evidence/v12-06-runtime-linux.json'
DEPS = ['v12-05-runtime-exit-linux.json', 'v12-03c-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-06-container-image-linux.json']
COMMON = ['full-go', 'affected-race', 'contracts', 'architecture', 'docker-dev', 'docker-deploy', 'readme', 'docs-links', 'docs-build', 'line-endings', 'diff']
FINAL = ['docs-links', 'docs-build', 'architecture', 'line-endings', 'diff']
DOCS = ['docs/进度/v1.2-dev/v1.2.125-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件Container执行沙箱合同.md', 'docs/api/v1.2插件Container镜像准入合同.md', 'docs/architecture/当前架构概览.md', 'docs/architecture/配置与运行边界.md', 'docs/help/插件v5包与签名操作说明.md', 'docs-site/deployment/configuration.md', 'docs-site/plugins/manifest.md', 'docs-site/plugins/overview.md']
TESTS = {
    'capture': [('internal/plugin/v5_container_program_test.go', r'TestV5ContainerProgram\w+')],
    'metadata': [('internal/plugin/container/v5_image_test.go', r'TestV5ContainerImage\w+')],
    'coordinator': [('internal/plugin/v5_runtime_router_test.go', r'TestV5RuntimeRouter\w+')],
    'wire': [('internal/plugin/containerwire/wire_test.go', r'TestV5ContainerWire\w+')],
    'native': [('internal/plugin/v5_container_runtime_actual_test.go', r'TestV5ContainerNativeIsolation')],
    'wasm-regression': [('internal/plugin/wasm/v5_native_test.go', r'TestV5NativeWorker\w+')],
    'actual': [('internal/plugin/v5_container_runtime_pg_actual_test.go', r'TestV5ContainerRuntimeActualPostgres'), ('internal/plugin/v5_container_server_actual_test.go', r'TestV5ContainerActualServer')],
}


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def snapshot():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.ps1', '.json', '.yaml', '.yml', '.html', '.css', '.png', '.svg'}
    sources = {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n in {'Makefile', '.env.example', 'deploy/docker/Dockerfile.wasm-worker'})}
    return {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': sources, 'dependencies': {str((REPORT.parent/n).relative_to(ROOT)): sha(REPORT.parent/n) for n in DEPS}}


def native(work, name):
    expected = {test for source, pattern in TESTS[name] for test in re.findall(r'^func ('+pattern+r')\(', (ROOT/source).read_text(), re.M)}
    rows = [json.loads(line) for line in (work/f'native/{name}.jsonl').read_text().splitlines()]
    passed = {r['Test'] for r in rows if r['Action'] == 'pass' and 'Test' in r}
    assert expected and not any(r['Action'] in {'fail', 'skip'} for r in rows), name
    assert {t for t in passed if '/' not in t} == expected, (name, expected, passed)
    assert any(r['Action'] == 'pass' and 'Test' not in r for r in rows), name
    assert not (work/f'native/{name}.stderr').read_text(), name
    assert 'DATA RACE' not in (work/f'native/{name}.log').read_text(), name
    return {'tests': sorted(expected), 'top_level': len(expected), 'pass_events': len(passed), 'fail': 0, 'skip': 0}


def main():
    mode, raw = sys.argv[1:]
    work = pathlib.Path(raw).resolve()
    assert work.parent == ROOT/'.cache/v12-06-runtime' and work.name.startswith('acceptance.')
    current = snapshot()
    freeze = work/'source-freeze.json'
    if mode == 'freeze':
        assert not freeze.exists(), 'fresh owned directory required'
        before = ROOT/'.cache/v12-06-completion/preflight.json'
        preflight = json.loads(before.read_text())
        assert preflight['slice_status'] == 'completed'
        assert all(not c['mismatches'] for c in preflight['checks'].values())
        assert preflight['report_sha256'] == sha(REPORT.parent/DEPS[3])
        for name in DEPS[:3]:
            assert json.loads((REPORT.parent/name).read_text())['stage_status'] == 'completed'
        shutil.copyfile(before, work/'predecessor-preflight.json')
        freeze.write_text(json.dumps(current, ensure_ascii=False, indent=2)+'\n')
        return
    assert json.loads(freeze.read_text()) == current, 'source/HEAD/dependency drift: rerun in new owned directory'
    target = {name: native(work, name) for name in TESTS}
    for binary in ['plugin.test', 'image.test', 'wire.test', 'wasm.test', 'server']:
        info = (work/f'native/{binary}-build-info.txt').read_text()
        assert '-race=true' in info and 'GOOS=linux' in info
    assert (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 7
    assert 'linux cgroup=2' in (work/'environment.txt').read_text()
    assert int((work/'schema-version.txt').read_text().strip()) == 28
    digests = json.loads((work/'repo-digests.json').read_text())
    assert len(digests) == 1 and re.fullmatch(r'localhost/campusos-owned-v12-06-runtime@sha256:[0-9a-f]{64}', digests[0])
    assert 'Untagged: localhost/campusos-owned-v12-06-runtime:' in (work/'image-cleanup.log').read_text()
    assert 'campusos-v12-06-runtime-' in (work/'cleanup.log').read_text()
    # Retain and inspect the real child server logs, including forced exits.
    proofs = list((work/'server').glob('server-*.log.proof.json'))
    assert len(proofs) == 3
    forced = 0
    for path in proofs:
        p = json.loads(path.read_text())
        log = pathlib.Path(str(path).removesuffix('.proof.json'))
        assert p['exit_checked'] and p['race_checked'] and not p['race_detected'] and p['secrets_absent']
        assert sha(log) == p['log_sha256'] and 'DATA RACE' not in log.read_text()
        assert p['force'] or p['exit_clean']
        forced += p['force']
    assert forced == 1
    if mode == 'verify':
        print('verified actual Linux Container targets:', json.dumps(target))
        return
    assert mode == 'seal'
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    assert (work/'final-document-passed.txt').read_text().splitlines() == FINAL
    report = {
        'schema': 'campusos.v12-06-container-runtime/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(),
        'stage': 'V12-06', 'stage_status': 'in_progress', 'slice': 'linux-container-runtime-lifecycle', 'slice_status': 'completed',
        'implementation': 'passed', 'automation': 'passed', 'target_environment': 'partial',
        'targets': {'linux': 'passed', 'windows': 'not_run'}, 'target': target,
        'runtime_adapter': True, 'plugin_process_started': True, 'actual_postgres': True, 'actual_server': True,
        'transports': ['http/1.1', 'unary-grpc+json/http2'], 'registry_pull': False,
        'native_launch': {'clean_environment': True, 'race_test_binaries': 4, 'race_server': True, 'static_linux_guest': True, 'os': 'Linux', 'repo_digest': digests[0]},
        'common_gates': {n: 'passed' for n in COMMON}, 'final_document_gates': {n: 'passed' for n in FINAL},
        'migration': {'current': 28, 'next': 29, 'changed': False}, **current,
        'artifacts_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()},
        'documents_sha256': {n: sha(ROOT/n) for n in DOCS},
        'remaining': ['Actual native Windows host + Docker Desktop Linux containers acceptance; no Windows target was identified in this session.'],
        'boundaries': [
            'Independent immutable historical first06 and full05 reports preserved; current06 source/dependency/artifact hashes are new.',
            'Local daemon content-addressed storage trusted; no registry pull, independent OCI layer/cosign verification or arbitrary endpoint.',
            'Untrusted guest executes only inside bounded network-none/nonroot/readonly/cgroup2 isolation; no host mounts, application credentials, database or socket.',
            'Host credentials stay in the parent; published finite Host capabilities/current authority checks only. Unpublished Secret/Egress denied.',
            'Wasm and Container share one elected host and aggregate reservation pool; not a distributed HA scheduler.',
            'Actual Linux acceptance cannot substitute for native Windows. V12-06 remains in_progress and V12-07 is not started.',
            'Author SDK remains08a; built-in AI/RAG consumer binding remains14c; no migration/shared database reset/commit/push.',
        ], 'commit_or_push': False, 'shared_database_reset': False,
    }
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed Linux Container runtime evidence; stage remains in_progress pending Windows')


if __name__ == '__main__':
    main()
