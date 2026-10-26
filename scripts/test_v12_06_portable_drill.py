#!/usr/bin/env python3
"""Negative evidence-validator oracles, not native target acceptance."""
import importlib.util
import json
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

    def test_stage_exit_requires_both_native_reports(self):
        with patch.object(drill, 'snapshot', return_value={}), patch.object(drill, 'EVIDENCE', self.work):
            with self.assertRaisesRegex(RuntimeError, 'missing native linux report'):
                drill.stage_exit()


if __name__ == '__main__':
    unittest.main()
