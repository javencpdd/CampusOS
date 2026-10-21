#!/usr/bin/env python3
"""V12-09b first slice: normal resident Outbox worker to metadata-only tasks.

Owned Linux PG16/native production-module target; no test callback or manual
worker pump, no index executor, HTTP/browser, backfill or reconciliation claim.
Only fresh owned cache roots and random tmpfs Docker resources are mutated.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import time

REPO = Path(__file__).resolve().parent.parent
PRIOR = REPO / 'docs/项目计划书v1/项目计划v1.2/evidence/v12-09a-exit-linux.json'
PRIOR_SHA = '279d10395db6a7bfbd74a6fb6bee694ecc19b32a90dabf39e5da546da84a49b9'
DATABASE = 'campusos_v12_09b_task'
TABLES = ['knowledge_index_tasks']


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def source_map():
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=REPO).decode().split('\0')
    return {n: sha(REPO / n) for n in sorted(set(names)) if n and (REPO / n).is_file()
            and not n.endswith('.md') and not n.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}


def normalize(raw):
    return '\n'.join(line for line in raw.splitlines() if not line.startswith(('\\restrict ', '\\unrestrict ')))


def main(work):
    assert os.uname().sysname == 'Linux'
    assert sha(PRIOR) == PRIOR_SHA, 'predecessor evidence changed'
    assert work.is_relative_to(REPO / '.cache/v12-09b-task'), 'owned work root required'
    work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    runtime = work / 'runtime'
    runtime.mkdir(mode=0o700)
    binary_dir = work / 'native'
    binary_dir.mkdir()
    frozen = source_map()
    (work / 'source-freeze.json').write_text(json.dumps(frozen, ensure_ascii=False, indent=2) + '\n')
    env = {n: os.environ[n] for n in ('PATH', 'HOME') if n in os.environ}
    env.update(GOCACHE=str(REPO / '.cache/v12-g0/go-cache'), GOTOOLCHAIN='auto', GOFLAGS='-buildvcs=false', GOMAXPROCS='2', CAMPUSOS_SKIP_DOTENV='true', TMPDIR='/tmp')
    password = secrets.token_hex(24)
    container = 'campusos-v12-09b-task-' + secrets.token_hex(6)
    started = False
    commands = []

    def run(name, args, extra=None, expected_success=True, input_bytes=None):
        begin = time.monotonic()
        with (work / (name + '.log')).open('wb') as out:
            result = subprocess.run(args, cwd=REPO, env=env | (extra or {}), input=input_bytes, stdout=out, stderr=subprocess.STDOUT)
        commands.append({'name': name, 'exit_code': result.returncode, 'expected_success': expected_success, 'seconds': round(time.monotonic() - begin, 3)})
        assert (result.returncode == 0) == expected_success, name + ' failed; inspect private log'
        return (work / (name + '.log')).read_text()

    def mandatory(log, name):
        assert '--- PASS: ' + name + ' ' in log and '--- SKIP' not in log and '--- FAIL' not in log and 'DATA RACE' not in log

    def psql(db=DATABASE):
        return ['docker', 'exec', '-i', container, 'psql', '-U', 'campusos', '-d', db, '-XAt', '-v', 'ON_ERROR_STOP=1']

    def snapshot(name, tables, db=DATABASE):
        queries = []
        for table in tables:
            assert table.replace('_', '').isalnum()
            where = ''
            queries.append(f"SELECT '{table}',coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]') FROM public.{table} t{where};")
        return run(name, psql(db), input_bytes=('\n'.join(queries) + '\n').encode())

    try:
        for package, name in [('./internal/modules/features/knowledge', 'knowledge.test'), ('./internal/platform/reliability', 'reliability.test'), ('./internal/server', 'server.test')]:
            run('build-' + name, ['go', 'test', '-race', '-c', '-o', str(binary_dir / name), package])
        binaries = {str(f.relative_to(REPO)): sha(f) for f in binary_dir.iterdir()}
        unit = run('native-knowledge-unit', [str(binary_dir / 'knowledge.test'), '-test.v'])
        assert 'PASS\n' in unit and '--- FAIL' not in unit and '--- SKIP' not in unit and 'DATA RACE' not in unit
        rich_unit = run('native-reliability-unit', [str(binary_dir / 'reliability.test'), '-test.v', '-test.run=^Test(Worker|Consumer|StoppedWorker|FinishAttempt|CompleteFailure|StaleLease)'])
        assert 'PASS\n' in rich_unit and '--- FAIL' not in rich_unit and '--- SKIP' not in rich_unit and 'DATA RACE' not in rich_unit
        run('pg-start', ['docker', 'run', '--rm', '--pull=never', '-d', '--name', container, '--tmpfs', '/var/lib/postgresql/data:rw,size=512m',
                         '-e', 'POSTGRES_USER=campusos', '-e', 'POSTGRES_DB=' + DATABASE, '-e', 'POSTGRES_PASSWORD', '-p', '127.0.0.1::5432', 'postgres:16-alpine'], {'POSTGRES_PASSWORD': password})
        started = True
        for _ in range(120):
            if subprocess.run(['docker', 'exec', container, 'pg_isready', '-h', '127.0.0.1', '-U', 'campusos', '-d', DATABASE], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                break
            time.sleep(.25)
        else:
            raise AssertionError('owned PostgreSQL startup deadline')
        port = int(run('pg-port', ['docker', 'port', container, '5432/tcp']).strip().rsplit(':', 1)[1])
        pg_env = {'PSQL_MODE': 'docker', 'POSTGRES_CONTAINER': container, 'DB_USER': 'campusos', 'DB_NAME': DATABASE, 'DB_PASSWORD': password}
        previous = work / 'migrations-1-30'
        previous.mkdir()
        for path in (REPO / 'migrations').glob('*.sql'):
            if int(path.name.split('_', 1)[0]) < 31:
                shutil.copyfile(path, previous / path.name)
        run('migration-1-30-up', ['bash', 'scripts/migrate.sh', 'up'], pg_env | {'MIGRATIONS_DIR': str(previous)})
        old_tables = run('old-tables', psql() + ['-c', "SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename NOT IN ('schema_migrations','schema_migration_locks') ORDER BY tablename"]).splitlines()
        assert len(old_tables) == 118
        original = snapshot('old-118-before', old_tables)
        run('migration-31-up', ['bash', 'scripts/migrate.sh', 'up'], pg_env)
        assert original == snapshot('old-118-after-up', old_tables), 'old rows changed by migration'
        run('migration-31-empty-down', ['bash', 'scripts/migrate.sh', 'down'], pg_env)
        assert original == snapshot('old-118-after-down', old_tables)
        run('migration-31-reup', ['bash', 'scripts/migrate.sh', 'up'], pg_env)
        assert original == snapshot('old-118-after-reup', old_tables)
        run('migration-check', ['bash', 'scripts/migrate.sh', 'check'], pg_env)
        run('migration-status', ['bash', 'scripts/migrate.sh', 'status'], pg_env)
        run('database-entry', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        version = run('pg-version', psql() + ['-c', 'SHOW server_version']).strip()
        dump = ['docker', 'exec', container, 'pg_dump', '-U', 'campusos', '-d', DATABASE, '--schema-only', '--no-owner', '--no-privileges']
        before = run('schema-before', dump)
        # PostgreSQL deparses some pre-existing varchar ARRAY casts differently
        # after SQL replay. Compare the full restored DDL with an independent
        # schema-only roundtrip of the pre-target DDL; remove no constraints.
        schema_baseline_db = DATABASE + '_schema_baseline'
        run('schema-baseline-create', psql('postgres') + ['-c', 'CREATE DATABASE ' + schema_baseline_db])
        run('schema-baseline-restore', psql(schema_baseline_db) + ['--single-transaction'], input_bytes=before.encode())
        baseline_schema = run('schema-roundtrip-baseline', ['docker', 'exec', container, 'pg_dump', '-U', 'campusos', '-d', schema_baseline_db, '--schema-only', '--no-owner', '--no-privileges'])
        target_env = {
            'CAMPUSOS_KNOWLEDGE_INDEX_DSN': f'postgres://campusos:{password}@127.0.0.1:{port}/{DATABASE}?sslmode=disable',
            'CAMPUSOS_KNOWLEDGE_INDEX_ROOT': str(runtime), 'DATABASE_DSN': f'postgres://campusos:{password}@127.0.0.1:{port}/{DATABASE}?sslmode=disable',
            'CAMPUSOS_ENV': 'test', 'REDIS_ENABLED': 'false', 'NATS_URL': 'nats://127.0.0.1:1', 'HOST_API_ENABLED': 'false',
            'JWT_SECRET': secrets.token_hex(32), 'AUTH_PASSWORD_HASH_ENABLED': 'true', 'AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN': 'false',
            'AUTH_BOOTSTRAP_ADMIN_SECRET': secrets.token_hex(24), 'EMAIL_PROVIDER': 'fake', 'AI_ENABLED': 'false',
            'AUTH_MFA_ACTIVE_KEY_ID': 'owned09b', 'AUTH_MFA_ENCRYPTION_KEYS': 'owned09b:' + secrets.token_hex(32),
            'PLUGIN_DATA_DIR': str(runtime / 'data/plugin_data'), 'GIN_MODE': 'release', 'OBSERVABILITY_PROMETHEUS_ENABLED': 'false',
        }
        actual = run('native-postgres-target', [str(binary_dir / 'server.test'), '-test.v', '-test.run=^TestKnowledgeIndexRequestsResidentPostgres$', '-test.timeout=180s'], target_env)
        mandatory(actual, 'TestKnowledgeIndexRequestsResidentPostgres')
        proof = json.loads((runtime / 'target-proof.json').read_text())
        assert proof['status'] == 'passed' and proof['actual_server_composition'] and proof['actual_postgresql'] and proof['check_count'] >= 10 and proof['normal_resident_worker'] and not proof['test_callback_or_process_once']
        run('database-exit', ['bash', 'scripts/database-check.sh', 'all'], pg_env)
        data = snapshot('knowledge-before-refused-down', TABLES)
        run('migration-31-nonempty-down-denied', ['bash', 'scripts/migrate.sh', 'down'], pg_env, expected_success=False)
        assert data == snapshot('knowledge-after-refused-down', TABLES)
        run('migration-check-after-refusal', ['bash', 'scripts/migrate.sh', 'check'], pg_env)
        after = run('schema-after', dump)
        assert normalize(before) == normalize(after), 'whole schema changed by target'
        # Database-only dump/restore proves the new immutable request metadata
        # round trip. Full instance quarantine/credential reset remains08b's
        # separate CLI contract and is not claimed by this SQL-only check.
        restored = DATABASE + '_restored'
        run('restore-db-create', psql('postgres') + ['-c', 'CREATE DATABASE ' + restored])
        full = run('database-dump', ['docker', 'exec', container, 'pg_dump', '-U', 'campusos', '-d', DATABASE, '--no-owner', '--no-privileges'])
        run('database-restore', psql(restored) + ['--single-transaction'], input_bytes=full.encode())
        assert data == snapshot('knowledge-after-database-restore', TABLES, db=restored)
        restored_schema = run('schema-restored', ['docker', 'exec', container, 'pg_dump', '-U', 'campusos', '-d', restored, '--schema-only', '--no-owner', '--no-privileges'])
        assert normalize(baseline_schema) == normalize(restored_schema), 'whole restored schema differs from independent roundtrip baseline'
        run('restored-database-check', ['bash', 'scripts/database-check.sh', 'all'], pg_env | {'DB_NAME': restored})
        assert source_map() == frozen, 'source changed during target'
        assert all(sha(REPO / name) == h for name, h in binaries.items()), 'native ELF changed'
        report = {'schema': 'campusos.v12-09b-index-request-slice/v1', 'status': 'passed', 'stage': 'V12-09b', 'stage_status': 'in_progress', 'slice_status': 'completed_current_linux_scope',
                  'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed_linux_native_production_modules_resident_worker_pg16',
                  'proof': proof, 'postgres_version': version, 'source_sha256': frozen, 'binary_sha256': binaries,
                  'dependency': {str(PRIOR.relative_to(REPO)): PRIOR_SHA}, 'schema_before_after_sha256': hashlib.sha256(normalize(before).encode()).hexdigest(),
                  'schema_roundtrip_baseline_sha256': hashlib.sha256(normalize(baseline_schema).encode()).hexdigest(),
                  'schema_restored_sha256': hashlib.sha256(normalize(restored_schema).encode()).hexdigest(),
                  'new_tables': TABLES, 'old_business_tables_preserved': len(old_tables), 'schema_tables': 121,
                  'knowledge_database_roundtrip_sha256': hashlib.sha256(data.encode()).hexdigest(),
                  'commands': commands, 'windows': 'deferred_by_user_not_run', 'commit_or_push': False,
                  'scope_limits': ['only Knowledge-owned collection and official change events become immutable pending metadata requests', 'no index executor or lease/status transition, Community event subscription or projection', 'no durable pagination backfill, source revision projection, authoritative full reconciliation or retention-gap closure yet', 'no HTTP/browser/UI change or retrieval/private documents/AI/RAG/plugin/MCP/Integration egress grant', 'SQL database roundtrip is not full instance CLI restore']}
        (work / 'target.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        print(json.dumps({'status': 'passed', 'work': str(work.relative_to(REPO)), 'native_checks': proof['check_count'], 'stage_status': 'in_progress'}), flush=True)
    finally:
        if started:
            run('pg-server', ['docker', 'logs', container])
            run('pg-cleanup', ['docker', 'stop', container])
        (work / 'cleanup.json').write_text(json.dumps({'owned_postgres': container, 'commands': commands[-2:] if started else [], 'credentials_persisted': False}, indent=2) + '\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', required=True)
    args = parser.parse_args()
    main(Path(args.work).resolve())
