#!/usr/bin/env python3
"""Freeze the platform CLI contract slice; keep native Windows unverified."""
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
REPORT = ROOT/'docs/项目计划书v1/项目计划v1.2/evidence/v12-06-local-command-linux.json'
PREVIOUS = REPORT.parent/'v12-06-runtime-linux.json'
COMMON = ['full-go', 'affected-race', 'architecture', 'docker-dev', 'docker-deploy', 'docs-links', 'docs-build', 'line-endings', 'diff']
FINAL = ['docs-links', 'docs-build', 'architecture', 'line-endings', 'diff']
DOCS = ['docs/进度/v1.2-dev/v1.2.126-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件Container执行沙箱合同.md', 'docs/architecture/当前架构概览.md', 'docs/architecture/配置与运行边界.md', 'docs-site/deployment/configuration.md']
TESTS = {
    'contract': ('internal/plugin/container/v5_local_command_test.go', r'TestV5ContainerLocal\w+'),
    'metadata': ('internal/plugin/container/v5_image_test.go', r'TestV5ContainerImage\w+'),
    'capture': ('internal/plugin/v5_container_program_test.go', r'TestV5ContainerProgram\w+'),
    'image': ('internal/plugin/v5_container_image_actual_test.go', r'TestV5ContainerImageActualDocker'),
    'execution': ('internal/plugin/v5_container_runtime_actual_test.go', r'TestV5ContainerNativeIsolation'),
}


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def snapshot():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.ps1', '.json', '.yaml', '.yml', '.html', '.css', '.png', '.svg'}
    source = {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n in {'Makefile', '.env.example', 'deploy/docker/Dockerfile.wasm-worker'})}
    return {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': source, 'dependencies': {str(PREVIOUS.relative_to(ROOT)): sha(PREVIOUS)}}


def native(work, name):
    source, pattern = TESTS[name]
    expected = set(re.findall(r'^func ('+pattern+r')\(', (ROOT/source).read_text(), re.M))
    events = [json.loads(line) for line in (work/f'native/{name}.jsonl').read_text().splitlines()]
    passed = {e['Test'] for e in events if e['Action'] == 'pass' and 'Test' in e}
    assert expected and not any(e['Action'] in {'fail', 'skip'} for e in events), name
    assert {t for t in passed if '/' not in t} == expected, name
    assert any(e['Action'] == 'pass' and 'Test' not in e for e in events), name
    assert not (work/f'native/{name}.stderr').read_text() and 'DATA RACE' not in (work/f'native/{name}.log').read_text()
    return {'tests': sorted(expected), 'top_level': len(expected), 'pass_events': len(passed), 'fail': 0, 'skip': 0}


def main():
    mode, raw = sys.argv[1:]
    work = pathlib.Path(raw).resolve()
    assert work.parent == ROOT/'.cache/v12-06-local-command' and work.name.startswith('acceptance.')
    current = snapshot()
    freeze = work/'source-freeze.json'
    if mode == 'freeze':
        assert not freeze.exists()
        preflight = ROOT/'.cache/v12-06-windows-preflight/predecessor.json'
        before = json.loads(preflight.read_text())
        assert before['report_sha256'] == sha(PREVIOUS)
        assert before['states']['targets']['linux'] == 'passed'
        assert all(not c['mismatches'] for c in before['checks'].values())
        shutil.copyfile(preflight, work/'predecessor-preflight.json')
        freeze.write_text(json.dumps(current, ensure_ascii=False, indent=2)+'\n')
        return
    assert json.loads(freeze.read_text()) == current, 'source/HEAD/predecessor drift; use a new evidence directory'
    target = {name: native(work, name) for name in TESTS}
    for binary in ['container', 'plugin']:
        info = (work/f'native/{binary}-build-info.txt').read_text()
        assert '-race=true' in info and 'GOOS=linux' in info
    assert (work/'native/binaries.txt').read_text().count('ELF 64-bit') == 3
    for binary in ['container-windows.test.exe', 'server-windows.exe']:
        assert 'GOOS=windows' in (work/f'native/{binary}-build-info.txt').read_text()
    assert (work/'native/windows-compile-only.txt').read_text().count('PE32+') == 2
    assert 'linux cgroup=2' in (work/'environment.txt').read_text()
    proof = json.loads((work/'image-proof.json').read_text())
    assert proof['reference'] in json.loads((work/'repo-digests.json').read_text())
    assert proof['config_id'] == (work/'config-id.txt').read_text().strip()
    assert not proof['registry_pull'] and not proof['plugin_started']  # Image admission proof only; execution is separate.
    assert 'Untagged: localhost/campusos-owned-v12-06-runtime:local-command-' in (work/'image-cleanup.log').read_text()
    if mode == 'verify':
        print('verified CLI contract and actual Linux regression:', json.dumps(target))
        return
    assert mode == 'seal'
    assert (work/'common-passed.txt').read_text().splitlines() == COMMON
    assert (work/'final-document-passed.txt').read_text().splitlines() == FINAL
    report = {
        'schema': 'campusos.v12-06-local-command/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(),
        'stage': 'V12-06', 'stage_status': 'in_progress', 'slice': 'system-resolved-local-container-cli-contract',
        'implementation': 'passed', 'automation': 'passed', 'target_environment': 'partial',
        'targets': {'linux_regression': 'passed', 'native_windows': 'not_run'},
        'target': target, 'windows_cross_compile': {'test_binary': 'passed', 'server_binary': 'passed', 'native_execution': False},
        'os_folder_reader_on_linux': 'contract_oracle_only', 'actual_docker': True, 'actual_container_execution': True,
        'common_gates': {n: 'passed' for n in COMMON}, 'final_document_gates': {n: 'passed' for n in FINAL}, **current,
        'artifacts_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(work.rglob('*')) if p.is_file()},
        'documents_sha256': {n: sha(ROOT/n) for n in DOCS},
        'migration': {'current': 28, 'next': 29, 'changed': False},
        'boundaries': [
            'Windows paths come from native KnownFolder/SystemWindowsDirectory APIs; injected readers exercise the pure contract only.',
            'No C-drive, inherited ProgramFiles/SystemRoot/PATH, Docker context or proxy fallback; local daemon, owned empty CLI config and bounded cancellation remain.',
            'Linux actual signed image admission and HTTP/gRPC cgroup/directory/network/cancel/OOM regression are current-source evidence.',
            'The unmodified predecessor preserves PG/current-authority/server lifecycle acceptance. No fresh PG/migration drill is claimed for this CLI-only slice.',
            'Windows PE compilation does not prove Windows native API or Docker Desktop execution. Stage remains in_progress; no V12-07 work.',
            'No new dependency version, schema, public endpoint, shared-data reset, commit or push.',
        ], 'commit_or_push': False,
    }
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('sealed local-command slice; native Windows target remains not_run')


if __name__ == '__main__':
    main()
