#!/usr/bin/env python3
"""Exercise the actual native restore gate, output custody and real PG loss."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import signal
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
OWNED = ROOT / '.cache/release-restore-drill'


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def main(work):
    if work is None:
        OWNED.mkdir(parents=True, exist_ok=True)
        work = Path(tempfile.mkdtemp(prefix='acceptance-', dir=OWNED))
    else:
        work = work.resolve()
        if not work.is_relative_to(OWNED) or work == OWNED:
            raise RuntimeError('drill work must be new and below .cache/release-restore-drill')
        work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    nonce = secrets.token_hex(12)
    good = ROOT / '.cache/release-restore' / ('acceptance-' + nonce + '-positive')
    bad = ROOT / '.cache/release-restore' / ('acceptance-' + nonce + '-loss')
    env = os.environ | {'DB_NAME': 'forbidden_parent_db', 'POSTGRES_CONTAINER': 'forbidden-parent-container',
                        'DATABASE_DSN': 'postgres://forbidden.invalid/unused', 'CAMPUSOS_SKIP_DOTENV': 'false'}
    command = lambda path: ['make', 'release-restore-check', 'RELEASE_RESTORE_WORK=' + str(path)]
    commands = []

    def run(label, args, use_env=env):
        with (work / (label + '.log')).open('wb') as output:
            result = subprocess.run(args, cwd=ROOT, env=use_env, stdout=output, stderr=subprocess.STDOUT, timeout=900)
        commands.append({'name': label, 'args': args, 'exit_code': result.returncode})
        return result.returncode

    if run('positive', command(good)) != 0:
        raise RuntimeError('actual positive native restore gate failed')
    proof = json.loads((good / 'report.json').read_text())
    if proof['status'] != 'passed' or len(proof['checks']) != 7 or len(set(proof['epochs'])) != 2:
        raise RuntimeError('positive native restore proof incomplete')
    before = {p: sha(ROOT / p) for p in proof['artifact_sha256']}
    original_report = sha(good / 'report.json')
    if run('existing-work', command(good)) == 0 or 'FileExistsError' not in (work / 'existing-work.log').read_text():
        raise RuntimeError('existing evidence directory was not rejected')
    invalid = work / 'not-a-restore-gate-directory'
    if run('outside-work', ['python3', 'scripts/check-release-restore.py', '--work', str(invalid)]) == 0 or invalid.exists():
        raise RuntimeError('outside-scope output directory was not rejected')
    if sha(good / 'report.json') != original_report or any(sha(ROOT / p) != h for p, h in before.items()):
        raise RuntimeError('output rejection modified accepted evidence')
    with (work / 'database-loss.log').open('wb') as output:
        child = subprocess.Popen(command(bad), cwd=ROOT, env=env, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            end = time.monotonic() + 180
            while not (bad / 'scope.json').exists():
                if child.poll() is not None or time.monotonic() > end:
                    raise RuntimeError('actual failure target did not attest its scope')
                time.sleep(.05)
            scope = json.loads((bad / 'scope.json').read_text())
            name, owner = scope['container'], scope['owner_label']
            if not re.fullmatch(r'campusos-release-restore-[0-9a-f]{24}', name) or scope['seed_database'] != 'campusos_v12_08b_backup_seed':
                raise RuntimeError('failure injection refused unknown target')
            docker_env = {k: env[k] for k in ('PATH', 'HOME') if k in env}
            docker_env.update(DOCKER_HOST='unix:///var/run/docker.sock', DOCKER_CONFIG=str(bad / 'docker-config'))
            actual = subprocess.check_output(['docker', 'inspect', '--format={{.Id}}/{{index .Config.Labels "campusos.release-restore.owner"}}', name], env=docker_env).decode().strip()
            if actual != scope['container_id'] + '/' + owner or child.poll() is not None:
                raise RuntimeError('failure injection refused changed or completed target')
            if run('stop-owned-postgres', ['docker', 'stop', '--time=1', name], docker_env) != 0:
                raise RuntimeError('actual owned PG stop failed')
            status = child.wait(timeout=60)
            commands.append({'name': 'database-loss', 'args': command(bad), 'exit_code': status})
        finally:
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGINT)
                child.wait(timeout=60)
    cleanup = json.loads((bad / 'cleanup.json').read_text())
    failure = json.loads((bad / 'failure.json').read_text())
    failed = [c for c in json.loads((bad / 'commands.json').read_text()) if c['exit_code'] != 0 and not c['expected_failure']]
    if status == 0 or failure['status'] != 'failed' or not failed or (bad / 'report.json').exists():
        raise RuntimeError('real PG loss was masked as accepted recovery')
    if cleanup['errors'] or cleanup['credentials_persisted'] or not all(cleanup[k] for k in ('api_stopped', 'container_removed', 'ports_closed', 'dotenv_removed')):
        raise RuntimeError('actual failure leaked owned resources or credentials')
    for path, digest in proof['source_sha256'].items():
        if sha(ROOT / path) != digest:
            raise RuntimeError('execution inputs changed during the drill')
    artifacts = {str(p.relative_to(ROOT)): sha(p) for directory in (work, good, bad) for p in sorted(directory.rglob('*')) if p.is_file()}
    report = {'schema': 'campusos.release-restore-drill/v1', 'status': 'passed', 'positive_report': str((good / 'report.json').relative_to(ROOT)),
              'positive': proof, 'failure_work': str(bad.relative_to(ROOT)), 'failure_exit': status,
              'actual_failure_commands': failed, 'failure_cleanup': cleanup, 'existing_evidence_unchanged': True,
              'outside_scope_rejected': True, 'parent_DB_targets_ignored': True, 'no_mock_PostgreSQL_or_commands': True,
              'commands': commands, 'source_sha256': proof['source_sha256'], 'artifact_sha256': artifacts}
    (work / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'status': 'passed', 'positive_checks': len(proof['checks']), 'actual_PG_loss_exit': status,
                      'output_guards': 2, 'report': str(work / 'report.json')}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', type=Path)
    main(parser.parse_args().work)
