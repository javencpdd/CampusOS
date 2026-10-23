#!/usr/bin/env python3
"""Seal the first06 image admission slice without claiming a Container Runtime."""
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
REPORT = ROOT/'docs/项目计划书v1/项目计划v1.2/evidence/v12-06-container-image-linux.json'
DEPS = ['v12-05-runtime-exit-linux.json', 'v12-03c-exit-linux.json', 'v12-01b-exit-linux.json']
COMMON = ['full-go', 'affected-race', 'contracts', 'architecture', 'docker-dev', 'docker-deploy', 'readme', 'docs-links', 'docs-build', 'line-endings', 'diff']
FINAL = ['docs-links', 'docs-build', 'architecture', 'line-endings', 'diff']
DOCS = ['docs/进度/v1.2-dev/v1.2.124-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件Container镜像准入合同.md', 'docs/architecture/当前架构概览.md', 'docs-site/plugins/manifest.md']
TESTS = {
    'capture': ('internal/plugin/v5_container_program_test.go', r'TestV5ContainerProgram\w+'),
    'metadata': ('internal/plugin/container/v5_image_test.go', r'TestV5ContainerImage\w+'),
    'actual': ('internal/plugin/v5_container_image_actual_test.go', r'TestV5ContainerImageActualDocker'),
}


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def snapshot():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.json', '.yaml', '.yml', '.html', '.css', '.png', '.svg'}
    sources = {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n in {'Makefile', '.env.example', 'deploy/docker/Dockerfile.wasm-worker'})}
    return {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': sources, 'dependencies': {str((REPORT.parent/n).relative_to(ROOT)): sha(REPORT.parent/n) for n in DEPS}}


def native(work, name):
    source, pattern = TESTS[name]
    expected = set(re.findall(r'^func ('+pattern+r')\(', (ROOT/source).read_text(), re.M))
    rows = [json.loads(line) for line in (work/f'native/{name}.jsonl').read_text().splitlines()]
    passed = {r['Test'] for r in rows if r['Action'] == 'pass' and 'Test' in r}
    assert expected and not any(r['Action'] in {'fail', 'skip'} for r in rows), name
    assert {t for t in passed if '/' not in t} == expected, name
    assert any(r['Action'] == 'pass' and 'Test' not in r for r in rows), name
    assert not (work/f'native/{name}.stderr').read_text()
    assert 'DATA RACE' not in (work/f'native/{name}.log').read_text()
    return {'tests': sorted(expected), 'top_level': len(expected), 'pass_events': len(passed), 'fail': 0, 'skip': 0}


def main():
    mode, raw = sys.argv[1:]
    work = pathlib.Path(raw).resolve()
    assert work.parent == ROOT/'.cache/v12-06-container-image' and work.name.startswith('acceptance.')
    current = snapshot()
    freeze = work/'source-freeze.json'
    if mode == 'freeze':
        assert not freeze.exists(), 'each target execution requires a fresh owned directory'
        preflight = ROOT/'.cache/v12-06-preflight/predecessor.json'
        before = json.loads(preflight.read_text())
        assert before['head'] == current['head'] and before['stage_status'] == 'completed'
        assert all(s == 'passed' for s in before['states'].values())
        assert all(not check['mismatches'] for check in before['checks'].values())
        assert before['report_sha256'] == current['dependencies'][str((REPORT.parent/DEPS[0]).relative_to(ROOT))]
        for name in DEPS:
            assert json.loads((REPORT.parent/name).read_text())['stage_status'] == 'completed'
        shutil.copyfile(preflight, work/'predecessor-preflight.json')
        freeze.write_text(json.dumps(current, ensure_ascii=False, indent=2)+'\n')
        return
    assert json.loads(freeze.read_text()) == current, 'source/HEAD/dependency drift; use a new target directory'
    target = {name: native(work, name) for name in TESTS}
    for binary in ['plugin', 'image']:
        info = (work/f'native/{binary}-build-info.txt').read_text()
        assert '-race=true' in info and 'GOOS=linux' in info
    assert (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 2
    proof = json.loads((work/'actual-image-proof.json').read_text())
    assert proof['reference'] in json.loads((work/'repo-digests.json').read_text())
    assert proof['config_id'] == (work/'config-id.txt').read_text().strip()
    assert proof['registry_pull'] is False and proof['plugin_started'] is False
    assert re.fullmatch(r'sha256:[0-9a-f]{64}', proof['config_id'])
    assert (work/'containers-before.txt').read_bytes() == (work/'containers-after.txt').read_bytes()
    assert 'Untagged: localhost/campusos-owned-v12-06-image:' in (work/'image-cleanup.log').read_text()
    if mode == 'verify':
        print('verified first06 Linux/race/env-i image admission:', json.dumps(target))
        return
    assert mode == 'seal'
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    assert (work/'final-document-passed.txt').read_text().splitlines() == FINAL
    report = {
        'schema': 'campusos.v12-06-container-image/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(),
        'stage': 'V12-06', 'stage_status': 'in_progress', 'slice': 'signed-container-local-image-admission', 'slice_status': 'completed',
        'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed',
        'target': target, 'actual_docker_image_inspect': True, 'actual_signed_package': True,
        'runtime_adapter': False, 'plugin_process_started': False, 'registry_pull': False,
        'native_launch': {'clean_environment': True, 'race_test_binaries': 2, 'os': 'Linux', 'image': proof},
        'common_gates': {n: 'passed' for n in COMMON}, 'final_document_gates': {n: 'passed' for n in FINAL},
        'migration': {'current': 28, 'next': 29, 'changed': False}, **current,
        'artifacts_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()},
        'documents_sha256': {n: sha(ROOT/n) for n in DOCS},
        'boundaries': [
            'Capture and local immutable OCI mapping are snapshots, not current installation/Grant/Consent or readiness/publication proof.',
            'Only local Linux daemon image metadata is inspected; host CLI uses an empty owned configuration and bounded output/time. No remote context/registry pull, plugin container/process/endpoint/credential/Host call is created.',
            'The signed manifest RepoDigest must exactly match local RepoDigests; platform configuration ID is a distinct digest namespace. Local daemon content-addressed storage is trusted; no independent OCI blob/cosign verifier claim.',
            'Container runtime creation, CPU/RSS/network/filesystem isolation, finite HTTP/gRPC transport, current authority/lifecycle/publication and Windows/Linux execution acceptance remain later V12-06 work.',
            'No migration/shared database reset/commit/push. Historical V12-05/03c/01b reports are unchanged.',
        ], 'commit_or_push': False, 'shared_database_reset': False,
    }
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed first06 image admission slice; full V12-06 remains in_progress')


if __name__ == '__main__':
    main()
