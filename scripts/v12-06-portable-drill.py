#!/usr/bin/env python3
"""Native V12-06 acceptance. Cross-compilation/WSL cannot certify Windows."""
import argparse
import ctypes
import hashlib
import json
import ntpath
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time
import uuid
from datetime import datetime, timedelta, timezone

ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence'
CACHE = ROOT / '.cache/v12-06-runtime'
COMMON = ['evidence-validator', 'launcher-validation', 'full-go', 'affected-race', 'contracts', 'architecture', 'docker-dev', 'docker-deploy', 'readme', 'docs-links', 'docs-build', 'line-endings', 'diff']
FINAL = ['docs-links', 'docs-build', 'architecture', 'line-endings', 'diff']
DOCS = ['docs/进度/v1.2-dev/v1.2.127-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件Container执行沙箱合同.md', 'docs/architecture/配置与运行边界.md', 'docs-site/deployment/configuration.md']
DEPS = ['v12-05-runtime-exit-linux.json', 'v12-03c-exit-linux.json', 'v12-01b-exit-linux.json', 'v12-06-runtime-linux.json', 'v12-06-local-command-linux.json']
TESTS = {
    'capture': [('internal/plugin/v5_container_program_test.go', r'TestV5ContainerProgram\w+')],
    'metadata': [('internal/plugin/container/v5_image_test.go', r'TestV5ContainerImage\w+')],
    'coordinator': [('internal/plugin/v5_runtime_router_test.go', r'TestV5RuntimeRouter\w+')],
    'wire': [('internal/plugin/containerwire/wire_test.go', r'TestV5ContainerWire\w+')],
    'native': [('internal/plugin/v5_container_runtime_actual_test.go', 'TestV5ContainerNativeIsolation')],
    'actual': [('internal/plugin/v5_container_runtime_pg_actual_test.go', 'TestV5ContainerRuntimeActualPostgres'), ('internal/plugin/v5_container_server_actual_test.go', 'TestV5ContainerActualServer')],
    'local-command': [('internal/plugin/container/v5_local_command_test.go', r'TestV5ContainerLocal\w+')],
    'native-profile': [('internal/plugin/container/v5_native_profile_test.go', 'TestV5ContainerNativeProfile')],
    'shutdown': [('internal/plugin/v5_container_native_test.go', 'TestV5ContainerNativeShutdown')],
}
WASM_TESTS = [('internal/plugin/wasm/v5_native_test.go', r'TestV5NativeWorker\w+')]


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def save(path, value):
    Path(path).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8', newline='\n')


def load(path):
    return json.loads(Path(path).read_text(encoding='utf-8-sig'))


def snapshot():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).decode('utf-8').split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.ps1', '.json', '.yaml', '.yml', '.html', '.css', '.png', '.svg'}
    raw, canonical = {}, {}
    for name in sorted(set(names)):
        path = ROOT / name
        if not name or not path.is_file() or name.startswith('docs/项目计划书') or (path.suffix not in suffixes and name not in {'Makefile', '.env.example', 'deploy/docker/Dockerfile.wasm-worker'}):
            continue
        data = path.read_bytes()
        raw[name] = hashlib.sha256(data).hexdigest()
        # Git checks out PowerShell as CRLF. Only newline bytes are normalized
        # for the cross-OS comparison; actual target files retain their raw hash.
        canonical[name] = hashlib.sha256(data if path.suffix == '.png' else data.replace(b'\r\n', b'\n')).hexdigest()
    return {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT).decode().strip(), 'source_sha256': raw, 'canonical_source_sha256': canonical, 'dependencies': {str((EVIDENCE / n).relative_to(ROOT)).replace('\\', '/'): sha(EVIDENCE / n) for n in DEPS}}


def frozen_migration_head(frozen):
    versions = []
    for name in frozen['source_sha256']:
        match = re.fullmatch(r'migrations/(\d{6})_[^/]+\.up\.sql', name)
        if match:
            versions.append(int(match.group(1)))
    require(versions and sorted(versions) == list(range(1, len(versions) + 1)),
            'frozen migration sources must be a unique contiguous sequence')
    return len(versions)


def native_host(windows_required):
    actual = 'windows' if sys.platform == 'win32' else 'linux' if sys.platform.startswith('linux') else ''
    require(actual and (not windows_required or actual == 'windows'), 'requires native Windows Python; Linux/WSL/PowerShell-on-Linux cannot certify Windows')
    return actual


def system_root():
    if sys.platform != 'win32':
        return ''
    buffer = ctypes.create_unicode_buffer(32768)
    function = ctypes.WinDLL('kernel32', use_last_error=True).GetSystemWindowsDirectoryW
    function.argtypes = [ctypes.c_wchar_p, ctypes.c_uint]
    function.restype = ctypes.c_uint
    length = function(buffer, len(buffer))
    require(0 < length < len(buffer), 'native system Windows directory API failed')
    return buffer.value


def clean_environment(work, host):
    if host == 'windows':
        root = system_root()
        return {'SystemRoot': root, 'SystemDrive': ntpath.splitdrive(root)[0],
                'USERPROFILE': str(work / 'home'), 'TEMP': str(work / 'tmp'),
                'TMP': str(work / 'tmp'), 'GORACE': 'halt_on_error=1'}
    return {'PATH': '/usr/bin:/bin', 'HOME': str(work / 'home'), 'TMPDIR': str(work / 'tmp'), 'GORACE': 'halt_on_error=1'}


def run(args, log, *, env=None, cwd=ROOT, timeout=1200, stdin=None):
    # Do not echo argv: owned PG passwords are arguments to docker exec/run.
    with Path(log).open('wb') as output:
        try:
            result = subprocess.run([str(a) for a in args], cwd=cwd, env=env, input=stdin, stdout=output, stderr=subprocess.STDOUT, timeout=timeout)
        except (OSError, subprocess.TimeoutExpired) as error:
            raise RuntimeError(f'{Path(log).name}: command unavailable or timed out') from error
    require(result.returncode == 0, f'{Path(log).name}: command failed; inspect the owned log')
    return Path(log).read_text(encoding='utf-8-sig', errors='replace').strip()


def native(work, group, binary, pattern, package, env, go):
    log = work / f'native/{group}.log'
    stderr = work / f'native/{group}.stderr'
    with log.open('wb') as output, stderr.open('wb') as errors:
        try:
            result = subprocess.run([str(binary), '-test.run', pattern, '-test.v', '-test.count=1', '-test.timeout=240s'], cwd=ROOT / package, env=env, stdout=output, stderr=errors, timeout=260)
        except (OSError, subprocess.TimeoutExpired) as error:
            raise RuntimeError(f'{group}: native process unavailable or timed out') from error
    # Preserve output even on failure. No PASS report is written in that case.
    data = subprocess.run([go, 'tool', 'test2json', '-t'], input=log.read_bytes(), capture_output=True, cwd=ROOT, check=True)
    (work / f'native/{group}.jsonl').write_bytes(data.stdout)
    require(result.returncode == 0, f'{group}: native acceptance failed; inspect owned logs')


def test_evidence(work, group, contracts):
    expected = {name for source, pattern in contracts for name in re.findall(r'^func (' + pattern + r')\(', (ROOT / source).read_text(encoding='utf-8'), re.M)}
    rows = [json.loads(line) for line in (work / f'native/{group}.jsonl').read_text(encoding='utf-8').splitlines()]
    passed = {r['Test'] for r in rows if r.get('Action') == 'pass' and 'Test' in r}
    require(expected and not any(r.get('Action') in {'fail', 'skip'} for r in rows), f'{group}: failure/skip or missing test contract')
    require({n for n in passed if '/' not in n} == expected and any(r.get('Action') == 'pass' and 'Test' not in r for r in rows), f'{group}: incomplete native tests')
    require(not (work / f'native/{group}.stderr').read_bytes() and b'DATA RACE' not in (work / f'native/{group}.log').read_bytes(), f'{group}: stderr/race')
    return {'tests': sorted(expected), 'top_level': len(expected), 'pass_events': len(passed), 'fail': 0, 'skip': 0}


def child_evidence(work):
    paths = list((work / 'server').glob('server-*.log.proof.json'))
    require(len(paths) == 3, 'three actual server child proofs required')
    forced = 0
    for path in paths:
        proof = load(path)
        log = Path(str(path).removesuffix('.proof.json'))
        require(proof['exit_checked'] and proof['race_checked'] and not proof['race_detected'] and proof['secrets_absent'], 'server child evidence failed')
        require(sha(log) == proof['log_sha256'] and b'DATA RACE' not in log.read_bytes(), 'server log hash/race mismatch')
        require(proof['force'] or proof['exit_clean'], 'graceful child did not exit cleanly')
        forced += proof['force']
    require(forced == 1, 'exactly one forced server exit required')


def verify(work, host, cross_host=False):
    frozen, current = load(work / 'source-freeze.json'), snapshot()
    if cross_host:
        require(all(frozen[k] == current[k] for k in ['head', 'canonical_source_sha256', 'dependencies']), 'cross-host source/HEAD/dependency drift')
    else:
        require(frozen == current, 'source/HEAD/dependency drift: rerun in a fresh evidence directory')
    tests = dict(TESTS)
    if host == 'linux':
        tests['wasm-regression'] = WASM_TESTS
    target = {name: test_evidence(work, name, contracts) for name, contracts in tests.items()}
    profile = load(work / 'native-platform.json')
    require(profile['goos'] == host and profile['goarch'] == 'amd64', 'native OS/architecture profile mismatch')
    require(profile['host'] == ('npipe:////./pipe/docker_engine' if host == 'windows' else 'unix:///var/run/docker.sock'), 'nonlocal Docker profile')
    binaries = ['plugin.test', 'image.test', 'wire.test', 'server'] + (['wasm.test'] if host == 'linux' else [])
    for name in binaries:
        info = (work / f'native/{name}-build-info.txt').read_text(encoding='utf-8-sig')
        require('-race=true' in info and f'GOOS={host}' in info and 'GOARCH=amd64' in info, f'{name}: native race build required')
        binary = work / f'native/{name}{".exe" if host == "windows" else ""}'
        require(binary.read_bytes().startswith(b'MZ' if host == 'windows' else b'\x7fELF'), f'{name}: native executable header mismatch')
    require(int((work / 'schema-version.txt').read_text().strip()) == frozen_migration_head(frozen),
            'database migration head differs from frozen source')
    require('linux cgroup=2' in (work / 'environment.txt').read_text(encoding='utf-8-sig'), 'actual Docker Linux/cgroup2 required')
    digests = load(work / 'repo-digests.json')
    require(len(digests) == 1 and re.fullmatch(r'localhost/campusos-owned-v12-06-runtime@sha256:[0-9a-f]{64}', digests[0]), 'exact owned image RepoDigest required')
    require('Untagged: localhost/campusos-owned-v12-06-runtime:' in (work / 'image-cleanup.log').read_text(), 'owned runtime image cleanup missing')
    require('campusos-v12-06-runtime-' in (work / 'cleanup.log').read_text(), 'owned PG cleanup missing')
    if host == 'windows':
        require(load(work / 'cleanup-proof.json') == {'postgres_removed': True, 'runtime_image_removed': True, 'workers_remaining': []}, 'native Windows cleanup incomplete')
    else:
        for name in ['plugin.windows.test.exe', 'image.windows.test.exe', 'server.windows.exe']:
            require((work / f'native/{name}').read_bytes().startswith(b'MZ') and 'GOOS=windows' in (work / f'native/{name}-build-info.txt').read_text(), 'current-source Windows PE compilation missing; not native execution')
    child_evidence(work)
    return target


def build_env(work, host):
    env = dict(os.environ)
    env.update(GOCACHE=str(ROOT / '.cache/v12-g0/go-cache'), GOOS=host, GOARCH='amd64', CGO_ENABLED='1', GOFLAGS='-buildvcs=false')
    return env


def windows_docker_env(base, work):
    # Docker Desktop locates its bundled CLI plugins through ProgramFiles.
    # Preserve that non-secret system path without inheriting PATH, APPDATA,
    # credentials, proxy settings, or the caller's Docker configuration.
    program_files = Path(os.environ.get('ProgramFiles', ''))
    require(program_files.is_absolute() and program_files.is_dir(),
            'native Windows ProgramFiles is required for Docker Buildx discovery')
    return {**base, 'DOCKER_CONFIG': str(work / 'home'), 'ProgramFiles': str(program_files)}


def windows_migration_env(docker_env, profile, owned, password):
    # With PATHEXT absent, PowerShell resolves Docker Desktop's extensionless
    # Unix `docker` shim before docker.exe and silently loses `docker ps`.
    return {**docker_env,
            'PATH': str(Path(profile['binary']).parent) + os.pathsep + str(Path(system_root()) / 'System32'),
            'PATHEXT': '.COM;.EXE;.BAT;.CMD',
            'DOCKER_HOST': profile['host'], 'CAMPUSOS_SKIP_DOTENV': 'true',
            'PSQL_MODE': 'docker', 'POSTGRES_CONTAINER': owned,
            'DB_HOST': '127.0.0.1', 'DB_PORT': '5432', 'DB_USER': 'campusos',
            'DB_NAME': 'campusos_v12_06_container', 'DB_PASSWORD': password}


def windows_target(work, go):
    # Fail before native compilation or any Docker mutation when the official
    # Windows launcher/migration runtime is unavailable.
    pwsh = os.environ.get('CAMPUSOS_V12_06_PWSH') or shutil.which('pwsh.exe')
    require(pwsh and Path(pwsh).is_file(), 'PowerShell 7.2+ pwsh.exe is required for native Windows acceptance')
    version = run([pwsh, '-NoProfile', '-NonInteractive', '-Command', '$PSVersionTable.PSVersion.ToString()'],
                  work / 'powershell-version.txt')
    match = re.fullmatch(r'(\d+)\.(\d+)(?:\.\d+)?', version)
    require(match and (int(match.group(1)), int(match.group(2))) >= (7, 2),
            'PowerShell 7.2+ is required for native Windows acceptance')
    env = build_env(work, 'windows')
    require(run([go, 'env', 'GOHOSTOS'], work / 'go-host.txt', env=env) == 'windows', 'Go itself must be a native Windows toolchain')
    for name, package in [('image', './internal/plugin/container'), ('plugin', './internal/plugin'), ('wire', './internal/plugin/containerwire')]:
        run([go, 'test', '-c', '-race', '-o', work / f'native/{name}.test.exe', package], work / f'native/{name}-build.log', env=env)
    run([go, 'build', '-race', '-o', work / 'native/server.exe', './cmd/server'], work / 'native/server-build.log', env=env)
    for name in ['image.test', 'plugin.test', 'wire.test', 'server']:
        run([go, 'version', '-m', work / f'native/{name}.exe'], work / f'native/{name}-build-info.txt', env=env)
    base = clean_environment(work, 'windows')
    profile_env = {**base, 'CAMPUSOS_V5_CONTAINER_NATIVE_PROFILE': str(work / 'native-platform.json')}
    native(work, 'native-profile', work / 'native/image.test.exe', '^TestV5ContainerNativeProfile$', 'internal/plugin/container', profile_env, go)
    native(work, 'shutdown', work / 'native/plugin.test.exe', '^TestV5ContainerNativeShutdown$', 'internal/plugin', base, go)
    profile = load(work / 'native-platform.json')
    require(profile['goos'] == 'windows' and profile['goarch'] == 'amd64' and profile['system_root'] == system_root(), 'native OS API profile mismatch')
    docker = [profile['binary'], '--host=' + profile['host'], '--config=' + str(work / 'home')]
    docker_env = windows_docker_env(base, work)
    run(docker + ['buildx', 'version'], work / 'buildx-version.txt', env=docker_env)
    info = run(docker + ['info', '--format', '{{.OSType}} cgroup={{.CgroupVersion}} driver={{.CgroupDriver}}'], work / 'environment.txt', env=docker_env)
    require(info.startswith('linux cgroup=2 '), 'requires Docker Desktop Linux containers/cgroup v2')
    baseline_text = run(docker + ['ps', '--all', '--filter=label=campusos.container.worker=v5', '--format={{.Names}}'], work / 'baseline-workers.txt', env=docker_env)
    baseline_workers = set(baseline_text.splitlines())
    run(docker + ['image', 'inspect', 'postgres:16-alpine'], work / 'postgres-image.json', env=docker_env)
    image = work / 'image'
    image.mkdir()
    guest_env = {**env, 'GOOS': 'linux', 'CGO_ENABLED': '0'}
    run([go, 'build', '-o', image / 'fixture', './internal/plugin/containerfixture'], work / 'worker-build.log', env=guest_env)
    (image / 'Dockerfile').write_text('FROM scratch\nCOPY --chmod=0555 fixture /fixture\nUSER 65534:65534\nENTRYPOINT ["/fixture"]\n', encoding='utf-8', newline='\n')
    suffix = uuid.uuid4().hex
    tag = 'localhost/campusos-owned-v12-06-runtime:windows-' + suffix
    owned = 'campusos-v12-06-runtime-windows-' + suffix
    pg_created = image_created = False
    password = os.urandom(24).hex()
    cleanup = {'postgres_removed': False, 'runtime_image_removed': False, 'workers_remaining': None}
    try:
        # UUID-owned names are registered before creation so failure cleanup can
        # remove a partially-created object without touching a shared resource.
        image_created = True
        # The deliberately reduced process environment must still select
        # BuildKit: the owned scratch Dockerfile uses COPY --chmod.
        run(docker + ['build', '--pull=false', '--network=none', '--tag', tag, '--iidfile', str(work / 'image-id.txt'), str(image)], work / 'image-build.log', env={**docker_env, 'DOCKER_BUILDKIT': '1'})
        run(docker + ['image', 'inspect', '--format', '{{json .RepoDigests}}', tag], work / 'repo-digests.json', env=docker_env)
        digests = load(work / 'repo-digests.json')
        require(len(digests) == 1 and re.fullmatch(r'localhost/campusos-owned-v12-06-runtime@sha256:[0-9a-f]{64}', digests[0]), 'Docker local content store must expose the built RepoDigest; no registry fallback')
        pg_created = True
        run(docker + ['run', '--pull=never', '-d', '--name', owned, '--tmpfs', '/var/lib/postgresql/data', '-e', 'POSTGRES_DB=campusos_v12_06_container', '-e', 'POSTGRES_USER=campusos', '-e', 'POSTGRES_PASSWORD=' + password, '-p', '127.0.0.1::5432', 'postgres:16-alpine'], work / 'container-create.log', env=docker_env)
        for attempt in range(80):
            result = subprocess.run(docker + ['exec', owned, 'pg_isready', '-U', 'campusos', '-d', 'campusos_v12_06_container'], env=docker_env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
            if result.returncode == 0:
                break
            time.sleep(.5)
        else:
            raise RuntimeError('owned PostgreSQL readiness timed out')
        port_text = run(docker + ['port', owned, '5432/tcp'], work / 'postgres-port.txt', env=docker_env)
        match = re.fullmatch(r'127\.0\.0\.1:(\d+)', port_text)
        require(match, 'owned PostgreSQL must expose exactly one loopback ephemeral port')
        migration_env = windows_migration_env(docker_env, profile, owned, password)
        for action in ['up', 'check']:
            run([pwsh, '-NoProfile', '-NonInteractive', '-File', ROOT / 'scripts/migrate.ps1', action], work / f'migration-{action}.log', env=migration_env)
        test_env = {**base, 'CAMPUSOS_V5_CONTAINER_DATABASE_URL': f'postgres://campusos:{password}@127.0.0.1:{match[1]}/campusos_v12_06_container?sslmode=disable', 'CAMPUSOS_V5_CONTAINER_RUNTIME_REFERENCE': digests[0], 'CAMPUSOS_V5_CONTAINER_SERVER_BINARY': str(work / 'native/server.exe'), 'CAMPUSOS_V5_CONTAINER_SERVER_PROOF_DIR': str(work / 'server')}
        groups = [('local-command', 'image', '^TestV5ContainerLocal', 'internal/plugin/container'), ('metadata', 'image', '^TestV5ContainerImage', 'internal/plugin/container'), ('capture', 'plugin', '^TestV5ContainerProgram', 'internal/plugin'), ('coordinator', 'plugin', '^TestV5RuntimeRouter', 'internal/plugin'), ('wire', 'wire', '^TestV5ContainerWire', 'internal/plugin/containerwire'), ('native', 'plugin', '^TestV5ContainerNativeIsolation$', 'internal/plugin'), ('actual', 'plugin', '^TestV5Container(RuntimeActualPostgres|ActualServer)$', 'internal/plugin')]
        for name, binary, pattern, package in groups:
            native(work, name, work / f'native/{binary}.test.exe', pattern, package, test_env, go)
        run(docker + ['exec', owned, 'psql', '-U', 'campusos', '-d', 'campusos_v12_06_container', '-At', '-c', 'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1'], work / 'schema-version.txt', env=docker_env)
        # Test fixtures must reap their own plugin workers; the runner never
        # deletes every v5 container as a shortcut to passing this check.
        rows = run(docker + ['ps', '--all', '--filter=label=campusos.container.worker=v5', '--format={{.Names}}'], work / 'remaining-workers.txt', env=docker_env)
        final_workers = set(rows.splitlines())
        cleanup['workers_remaining'] = sorted(final_workers - baseline_workers)
        require(final_workers == baseline_workers, 'worker set changed after cleanup; preserve diagnostics without deleting shared workers')
    finally:
        # Attempt both owned cleanups even if one fails, then propagate failure.
        errors = []
        if pg_created:
            try:
                run(docker + ['rm', '--force', '--volumes', owned], work / 'cleanup.log', env=docker_env, timeout=30)
                cleanup['postgres_removed'] = True
            except RuntimeError as error:
                errors.append(str(error))
        if image_created:
            try:
                # A failed build may never have created the owned tag. Check
                # only that exact tag, so cleanup cannot hide the build error
                # or remove an unrelated image.
                found = run(docker + ['image', 'ls', '--filter', 'reference=' + tag, '--format', '{{.Repository}}:{{.Tag}}'], work / 'image-presence.log', env=docker_env, timeout=30)
                require(found in {'', tag}, 'unexpected owned image tag inventory')
                if found:
                    run(docker + ['image', 'rm', tag], work / 'image-cleanup.log', env=docker_env, timeout=30)
                else:
                    (work / 'image-cleanup.log').write_text('Owned image tag was never created.\n', encoding='utf-8', newline='\n')
                cleanup['runtime_image_removed'] = True
            except RuntimeError as error:
                errors.append(str(error))
        save(work / 'cleanup-proof.json', cleanup)
        # No password, DSN or native subprocess argv is copied to the report.
        for path in work.rglob('*.log'):
            data = path.read_bytes()
            if password.encode() in data:
                path.write_bytes(data.replace(password.encode(), b'[redacted]'))
                errors.append('credential found and redacted from owned log')
        require(not errors, '; '.join(errors))


def target(work, host):
    require(not work.exists(), 'fresh evidence directory required; completed commands are never replayed in place')
    work.mkdir(parents=True)
    for name in ['native', 'home', 'tmp']:
        (work / name).mkdir()
    save(work / 'source-freeze.json', snapshot())
    for name in DEPS[:3]:
        require(load(EVIDENCE / name)['stage_status'] == 'completed', f'prerequisite incomplete: {name}')
    go = shutil.which('go.exe' if host == 'windows' else 'go')
    require(go, 'native Go toolchain is required')
    if host == 'windows':
        windows_target(work, go)
    else:
        env = {**os.environ, 'CAMPUSOS_V5_CONTAINER_RUNTIME_DIR': str(work), 'CAMPUSOS_V5_CONTAINER_DIRECTED': 'true'}
        run(['/bin/bash', 'scripts/v12-06-runtime-drill.sh', 'target'], work / 'linux-target.log', env=env, timeout=1800)
        base = clean_environment(work, host)
        native(work, 'local-command', work / 'native/image.test', '^TestV5ContainerLocal', 'internal/plugin/container', base, go)
        native(work, 'native-profile', work / 'native/image.test', '^TestV5ContainerNativeProfile$', 'internal/plugin/container', {**base, 'CAMPUSOS_V5_CONTAINER_NATIVE_PROFILE': str(work / 'native-platform.json')}, go)
        native(work, 'shutdown', work / 'native/plugin.test', '^TestV5ContainerNativeShutdown$', 'internal/plugin', base, go)
        compile_env = {**build_env(work, 'windows'), 'CGO_ENABLED': '0'}
        for name, package in [('plugin.windows.test.exe', './internal/plugin'), ('image.windows.test.exe', './internal/plugin/container')]:
            run([go, 'test', '-c', '-o', work / f'native/{name}', package], work / f'native/{name}-build.log', env=compile_env)
        run([go, 'build', '-o', work / 'native/server.windows.exe', './cmd/server'], work / 'native/server.windows.exe-build.log', env=compile_env)
        for name in ['plugin.windows.test.exe', 'image.windows.test.exe', 'server.windows.exe']:
            run([go, 'version', '-m', work / f'native/{name}'], work / f'native/{name}-build-info.txt', env=compile_env)
    result = verify(work, host)
    print(json.dumps({'native_host': host, 'work': str(work), 'target': result}, ensure_ascii=False))


def validate_launcher(work, host):
    pwsh = os.environ.get('CAMPUSOS_V12_06_PWSH') or shutil.which('pwsh.exe' if host == 'windows' else 'pwsh')
    require(pwsh, 'PowerShell 7 parser required; set CAMPUSOS_V12_06_PWSH to a local portable pwsh executable')
    parser_env = {**os.environ, 'XDG_CACHE_HOME': str(work / 'tool-cache'), 'XDG_CONFIG_HOME': str(work / 'tool-config'), 'XDG_DATA_HOME': str(work / 'tool-data')}
    checker = work / 'parse-launcher.ps1'
    checker.write_text('param([string]$Source)\n$ErrorActionPreference = "Stop"\n$tokens = $null\n$errors = $null\n$null = [System.Management.Automation.Language.Parser]::ParseFile($Source, [ref]$tokens, [ref]$errors)\nif ($errors.Count -ne 0) { throw ($errors | Out-String) }\nWrite-Output "PowerShell launcher syntax passed"\n', encoding='utf-8', newline='\n')
    run([pwsh, '-NoProfile', '-NonInteractive', '-File', checker, ROOT / 'scripts/v12-06-windows-drill.ps1'], work / 'common-launcher-validation.log', env=parser_env)
    if host == 'linux':
        result = subprocess.run([pwsh, '-NoProfile', '-NonInteractive', '-File', ROOT / 'scripts/v12-06-windows-drill.ps1', '-Mode', 'verify', '-WorkDirectory', work], env=parser_env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=30)
        (work / 'launcher-linux-rejection.log').write_bytes(result.stdout)
        require(result.returncode != 0 and b'Requires native Windows' in result.stdout, 'PowerShell-on-Linux must reject native Windows acceptance')
    save(work / 'launcher-proof.json', {'syntax': 'passed', 'parser_host': host, 'linux_guard': 'passed' if host == 'linux' else 'not_applicable', 'windows_native_acceptance': False})


def gates(work, host, final=False):
    if host == 'windows':
        require(shutil.which('make.exe') or shutil.which('make'),
                'GNU make is required for the unmodified native Windows common gates')
    verify(work, host)
    env = build_env(work, host)
    commands = {'evidence-validator': [sys.executable, '-m', 'unittest', 'discover', '-s', 'scripts', '-p', 'test_v12_06_portable_drill.py', '-v'], 'full-go': ['go', 'test', './...', '-count=1'], 'affected-race': ['go', 'test', '-race', './internal/plugin/...', './internal/server', './pkg/config', '-count=1'], 'contracts': ['make', 'contracts-check'], 'architecture': ['make', 'architecture-check'], 'docker-dev': ['make', 'docker-dev-test'], 'docker-deploy': ['make', 'docker-deploy-check'], 'readme': ['make', 'readme-check'], 'docs-links': ['make', 'docs-links'], 'docs-build': ['make', 'docs-build'], 'line-endings': [sys.executable, 'scripts/check-line-endings.py', '--include-untracked'], 'diff': ['git', 'diff', '--check']}
    prefix = 'final-document' if final else 'common'
    passed = work / f'{prefix}-passed.txt'
    require(not passed.exists(), f'{prefix} already started; inspect evidence before rerunning')
    passed.write_text('', encoding='utf-8')
    for name in FINAL if final else COMMON:
        if name == 'launcher-validation':
            validate_launcher(work, host)
        else:
            run(commands[name], work / f'{prefix}-{name}.log', env=env)
        with passed.open('a', encoding='utf-8', newline='\n') as output:
            output.write(name + '\n')


def seal(work, host):
    tests = verify(work, host)
    for prefix, expected in [('common', COMMON), ('final-document', FINAL)]:
        require((work / f'{prefix}-passed.txt').read_text().splitlines() == expected, f'{prefix}: incomplete gates')
    current = snapshot()
    migration_head = frozen_migration_head(current)
    report = {'schema': 'campusos.v12-06-native-runtime/v1', 'generated_at': datetime.now(timezone(timedelta(hours=8))).isoformat(), 'stage': 'V12-06', 'stage_status': 'in_progress', 'native_host': host, 'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed_for_this_host_only', 'targets': {host: 'passed'}, 'target': tests, 'native_windows_execution': host == 'windows', 'actual_docker': True, 'actual_postgres': True, 'actual_server': True, 'transports': ['http/1.1', 'unary-grpc+json/http2'], 'common_gates': {n: 'passed' for n in COMMON}, 'final_document_gates': {n: 'passed' for n in FINAL}, 'migration': {'current': migration_head, 'next': migration_head + 1, 'changed': False}, **current, 'artifacts_sha256': {p.relative_to(ROOT).as_posix(): sha(p) for p in sorted(work.rglob('*')) if p.is_file()}, 'documents_sha256': {n: sha(ROOT / n) for n in DOCS}, 'remaining': 'Matching current-source native Linux and Windows reports plus raw artifacts are both required by exit mode.', 'commit_or_push': False, 'shared_database_reset': False}
    path = EVIDENCE / ('v12-06-runtime-windows.json' if host == 'windows' else 'v12-06-runtime-portable-linux.json')
    require(not path.exists(), 'report already sealed; preserve historical evidence')
    save(path, report)
    print('sealed ' + str(path.relative_to(ROOT)) + '; cross-platform stage exit is separate')


def stage_exit():
    current = snapshot()
    migration_head = frozen_migration_head(current)
    reports = {}
    for host, name in [('linux', 'v12-06-runtime-portable-linux.json'), ('windows', 'v12-06-runtime-windows.json')]:
        path = EVIDENCE / name
        require(path.is_file(), f'missing native {host} report; V12-06 stays in_progress')
        report = load(path)
        require(report['native_host'] == host and report['targets'] == {host: 'passed'} and report['automation'] == 'passed', f'{host}: native report incomplete')
        require(report['native_windows_execution'] == (host == 'windows'), f'{host}: native execution flag mismatch')
        require(report['migration'] == {'current': migration_head, 'next': migration_head + 1, 'changed': False}, f'{host}: migration source/report mismatch')
        require(report['canonical_source_sha256'] == current['canonical_source_sha256'] and report['dependencies'] == current['dependencies'], f'{host}: current source/dependencies mismatch')
        for key in ['artifacts_sha256', 'documents_sha256']:
            require(all((ROOT / name).is_file() and sha(ROOT / name) == value for name, value in report[key].items()), f'{host}: raw evidence/document hash mismatch')
        freezes = [ROOT / n for n in report['artifacts_sha256'] if n.endswith('/source-freeze.json')]
        require(len(freezes) == 1, f'{host}: missing unique raw source freeze')
        work = freezes[0].parent
        require(work.parent == CACHE and work.name.startswith('acceptance.'), f'{host}: evidence directory outside owned cache')
        require(verify(work, host, cross_host=True) == report['target'], f'{host}: raw native target mismatch')
        for key, expected in [('common_gates', COMMON), ('final_document_gates', FINAL)]:
            require(report[key] == {n: 'passed' for n in expected}, f'{host}: common/final gates incomplete')
        for prefix, expected in [('common', COMMON), ('final-document', FINAL)]:
            require((work / f'{prefix}-passed.txt').read_text().splitlines() == expected, f'{host}: raw gate proof incomplete')
        reports[host] = {'report': path.relative_to(ROOT).as_posix(), 'sha256': sha(path)}
    output = EVIDENCE / 'v12-06-exit.json'
    require(not output.exists(), 'stage exit already sealed')
    save(output, {'schema': 'campusos.v12-06-exit/v1', 'stage': 'V12-06', 'stage_status': 'completed', 'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed', 'native_reports': reports, **current})
    print('V12-06 exit verified; synchronize plan/progress documents against this report')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['target', 'verify', 'common', 'documents', 'seal', 'exit'])
    parser.add_argument('work', nargs='?')
    parser.add_argument('--native-windows', action='store_true')
    args = parser.parse_args()
    host = native_host(args.native_windows)
    if args.mode == 'exit':
        stage_exit()
        return
    work = Path(args.work).resolve() if args.work else CACHE / ('acceptance.portable-' + host + '-' + uuid.uuid4().hex)
    require(work.parent == CACHE and work.name.startswith('acceptance.'), 'requires repository-owned .cache/v12-06-runtime/acceptance.* directory')
    print(str(work), flush=True)
    if args.mode == 'target':
        target(work, host)
    elif args.mode == 'verify':
        print(json.dumps(verify(work, host)))
    elif args.mode in {'common', 'documents'}:
        gates(work, host, args.mode == 'documents')
    else:
        seal(work, host)


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
