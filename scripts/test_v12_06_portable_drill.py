#!/usr/bin/env python3
"""Negative evidence-validator oracles, not native target acceptance."""
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('drill', Path(__file__).with_name('v12-06-portable-drill.py'))
drill = importlib.util.module_from_spec(spec)
spec.loader.exec_module(drill)


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.work = Path(self.directory.name)
        (self.work / 'native').mkdir()
        (self.work / 'server').mkdir()
        (self.work / 'contract.go').write_text('func TestRequired(t *testing.T) {}\n')
        self.contracts = [('contract.go', 'TestRequired')]
        self.root = patch.object(drill, 'ROOT', self.work)
        self.root.start()
        self.addCleanup(self.root.stop)
        self.events([{'Action': 'pass', 'Test': 'TestRequired'}, {'Action': 'pass'}])

    def events(self, rows):
        (self.work / 'native/target.jsonl').write_text(''.join(json.dumps(r) + '\n' for r in rows))
        (self.work / 'native/target.log').write_text('PASS\n')
        (self.work / 'native/target.stderr').write_text('')

    def verify(self):
        return drill.test_evidence(self.work, 'target', self.contracts)

    def children(self):
        for n in range(3):
            log = self.work / f'server/server-{n}.log'
            log.write_text('ordinary actual server log\n')
            drill.save(str(log) + '.proof.json', {'force': n == 1, 'exit_clean': n != 1, 'exit_checked': True, 'race_checked': True, 'race_detected': False, 'secrets_absent': True, 'log_sha256': drill.sha(log)})

    def test_complete_contract(self):
        self.assertEqual(self.verify()['top_level'], 1)

    def test_skip_cannot_certify_target(self):
        self.events([{'Action': 'skip', 'Test': 'TestRequired'}, {'Action': 'pass'}])
        with self.assertRaisesRegex(RuntimeError, 'failure/skip'):
            self.verify()

    def test_failed_child_cannot_hide_behind_package_pass(self):
        self.events([{'Action': 'fail', 'Test': 'TestRequired/subcase'}, {'Action': 'pass', 'Test': 'TestRequired'}, {'Action': 'pass'}])
        with self.assertRaisesRegex(RuntimeError, 'failure/skip'):
            self.verify()

    def test_missing_required_top_level(self):
        self.events([{'Action': 'pass', 'Test': 'TestOther'}, {'Action': 'pass'}])
        with self.assertRaisesRegex(RuntimeError, 'incomplete native tests'):
            self.verify()

    def test_package_exit_required(self):
        self.events([{'Action': 'pass', 'Test': 'TestRequired'}])
        with self.assertRaisesRegex(RuntimeError, 'incomplete native tests'):
            self.verify()

    def test_stderr_and_race_rejected(self):
        for path, value in [('target.stderr', 'native failure'), ('target.log', 'WARNING: DATA RACE')]:
            self.events([{'Action': 'pass', 'Test': 'TestRequired'}, {'Action': 'pass'}])
            (self.work / 'native' / path).write_text(value)
            with self.assertRaisesRegex(RuntimeError, 'stderr/race'):
                self.verify()

    def test_real_child_proof_contract(self):
        self.children()
        drill.child_evidence(self.work)

    def test_forced_kill_is_not_graceful_exit(self):
        self.children()
        path = self.work / 'server/server-0.log.proof.json'
        proof = drill.load(path)
        proof['exit_clean'] = False
        drill.save(path, proof)
        with self.assertRaisesRegex(RuntimeError, 'did not exit cleanly'):
            drill.child_evidence(self.work)

    def test_child_log_tamper_and_secret_rejected(self):
        self.children()
        (self.work / 'server/server-0.log').write_text('changed after proof\n')
        with self.assertRaisesRegex(RuntimeError, 'hash/race'):
            drill.child_evidence(self.work)
        self.children()
        path = self.work / 'server/server-0.log.proof.json'
        proof = drill.load(path)
        proof['secrets_absent'] = False
        drill.save(path, proof)
        with self.assertRaisesRegex(RuntimeError, 'child evidence failed'):
            drill.child_evidence(self.work)

    def test_linux_cannot_be_windows(self):
        with patch.object(drill.sys, 'platform', 'linux'):
            with self.assertRaisesRegex(RuntimeError, 'native Windows'):
                drill.native_host(True)

    def test_migration_head_is_bound_to_frozen_current_sources(self):
        sources = {f'migrations/{version:06d}_fixture.up.sql': 'digest' for version in range(1, 36)}
        self.assertEqual(drill.frozen_migration_head({'source_sha256': sources}), 35)
        sources.pop('migrations/000034_fixture.up.sql')
        with self.assertRaisesRegex(RuntimeError, 'contiguous'):
            drill.frozen_migration_head({'source_sha256': sources})
        sources['migrations/000034_fixture.up.sql'] = 'digest'
        sources['migrations/000035_duplicate.up.sql'] = 'digest'
        with self.assertRaisesRegex(RuntimeError, 'unique contiguous'):
            drill.frozen_migration_head({'source_sha256': sources})

    def test_windows_preflight_requires_pwsh_before_build_or_docker(self):
        with patch.dict(os.environ, {'CAMPUSOS_V12_06_PWSH': ''}), patch.object(drill.shutil, 'which', return_value=None):
            with self.assertRaisesRegex(RuntimeError, 'PowerShell 7.2'):
                drill.windows_target(self.work, 'go.exe')
        fake_pwsh = self.work / 'pwsh.exe'
        fake_pwsh.write_bytes(b'fixture')
        with patch.dict(os.environ, {'CAMPUSOS_V12_06_PWSH': str(fake_pwsh)}), patch.object(drill, 'run', return_value='5.1.0') as run:
            with self.assertRaisesRegex(RuntimeError, 'PowerShell 7.2'):
                drill.windows_target(self.work, 'go.exe')
            self.assertEqual(run.call_count, 1)

    def test_windows_docker_env_keeps_buildx_path_without_user_config(self):
        base = {'SystemRoot': 'C:\\Windows', 'USERPROFILE': str(self.work)}
        with patch.dict(os.environ, {'ProgramFiles': str(self.work)}):
            result = drill.windows_docker_env(base, self.work)
        self.assertEqual(result['ProgramFiles'], str(self.work))
        self.assertEqual(result['DOCKER_CONFIG'], str(self.work / 'home'))
        self.assertNotIn('APPDATA', result)
        self.assertNotIn('PATH', result)
        with patch.dict(os.environ, {'ProgramFiles': ''}):
            with self.assertRaisesRegex(RuntimeError, 'ProgramFiles'):
                drill.windows_docker_env(base, self.work)

    def test_windows_clean_env_resolves_system_drive_for_native_tools(self):
        with patch.object(drill, 'system_root', return_value=r'C:\Windows'):
            env = drill.clean_environment(self.work, 'windows')
        self.assertEqual(env['SystemDrive'], 'C:')
        self.assertEqual(env['SystemRoot'], r'C:\Windows')
        self.assertNotIn('APPDATA', env)

    def test_windows_migration_uses_docker_executable_not_unix_shim(self):
        profile = {'binary': r'C:\Program Files\Docker\resources\bin\docker.exe',
                   'host': 'npipe:////./pipe/docker_engine'}
        with patch.object(drill, 'system_root', return_value=r'C:\Windows'):
            env = drill.windows_migration_env({'DOCKER_CONFIG': str(self.work)}, profile,
                                              'owned-postgres', 'fixture-secret')
        self.assertEqual(env['PATHEXT'], '.COM;.EXE;.BAT;.CMD')
        self.assertEqual(env['POSTGRES_CONTAINER'], 'owned-postgres')
        self.assertEqual(env['DOCKER_HOST'], profile['host'])
        self.assertNotIn('APPDATA', env)

    def test_windows_common_gates_require_make_before_evidence_replay(self):
        with patch.object(drill.shutil, 'which', return_value=None), patch.object(drill, 'verify') as verify:
            with self.assertRaisesRegex(RuntimeError, 'GNU make'):
                drill.gates(self.work, 'windows')
            verify.assert_not_called()

    def test_stage_exit_requires_both_native_reports(self):
        frozen = {'source_sha256': {'migrations/000001_fixture.up.sql': 'digest'}}
        with patch.object(drill, 'snapshot', return_value=frozen), patch.object(drill, 'EVIDENCE', self.work):
            with self.assertRaisesRegex(RuntimeError, 'missing native linux report'):
                drill.stage_exit()


if __name__ == '__main__':
    unittest.main()
