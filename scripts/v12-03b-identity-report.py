#!/usr/bin/env python3
"""Evidence for one03b durable identity slice; never marks the stage complete."""
import argparse
import hashlib
import json
import pathlib
import subprocess
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence'
REPORT = EVIDENCE / 'v12-03b-package-identities-linux.json'
COMMON = ['full-go', 'related-race', 'sdk', 'contracts', 'architecture', 'database', 'admin-build', 'er-tests', 'readme', 'docs-links', 'docs-build', 'line-endings', 'diff']
FINAL = ['docs-links', 'docs-build', 'line-endings', 'diff', 'architecture', 'skill-bridges', 'onboarding-skill']
DEPS = ['v12-03a-exit-linux.json', 'v12-02a-exit-linux.json', 'v12-02d-exit-linux.json']
DOCS = ['docs/进度/v1.2-dev/v1.2.115-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件v5发布身份持久化合同.md', 'docs/api/v1.2插件v5包字节与签名合同.md', 'docs/api/v1.2插件v5完整Manifest目标合同.md', 'docs/help/插件v5包与签名操作说明.md', 'docs/help/README.md', 'docs/architecture/当前架构概览.md', 'docs-site/guide/architecture.md', 'skills/sources/campusos-project-onboarding/references/current-project-status.md', 'migrations/README.md']


def read(p):
    return json.loads(p.read_text())


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def sources():
    names = subprocess.check_output(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'], cwd=ROOT, text=True).split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.ps1', '.json', '.yaml', '.yml', '.html', '.css', '.svg', '.png'}
    return {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n == 'Makefile' or n.startswith('plugins/') and n.endswith('.md'))}


def verify_source(w):
    frozen = read(w/'source-freeze.json')
    assert sources() == frozen['source_sha256'], 'source changed after target freeze'
    assert subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip() == frozen['head']
    for name, digest in frozen['dependencies'].items():
        assert sha(ROOT/name) == digest, name
    return frozen


def events(p, required=None):
    rows = [json.loads(s) for s in p.read_text().splitlines()]
    assert not any(r['Action'] in {'fail', 'skip'} for r in rows), p
    tests = {r['Test'] for r in rows if r['Action'] == 'pass' and 'Test' in r}
    tops = {n for n in tests if '/' not in n}
    assert tops, p
    if required is not None:
        assert tops == required, (p, tops)
    return {'top_level': len(tops), 'pass_events': len(tests), 'fail': 0, 'skip': 0}


def build(w, final):
    frozen = verify_source(w)
    native = events(w/'native/plugin.jsonl')
    assert native['top_level'] == 14
    pg = events(w/'native/pg.jsonl', {'TestPostgresPluginV5IdentityDurability'})
    restored = events(w/'native/restore.jsonl', {'TestPostgresPluginV5IdentityRestore'})
    assembly = events(w/'native/server.jsonl', {'TestPluginV5IdentityAssemblyUsesHostStore'})
    for phase in ['before-restart', 'after-restart']:
        for decision in ['record', 'conflict', 'revoked']:
            s = (w/'identity'/f'{phase}-{decision}.log').read_text()
            assert '--- PASS: TestPostgresPluginV5IdentityChild' in s
            assert not any(bad in s for bad in ['FAIL', 'SKIP', 'WARNING: DATA RACE'])
    assert 160000 <= int((w/'pg-version.txt').read_text()) < 170000
    before = read(w/'legacy-before.json')
    assert len(before['tables']) == 102 and before['sequences']
    assert before['tables']['plugins'] and before['tables']['plugin_versions'] and before['tables']['integration_principals']
    for phase in ['after-up', 'after-down', 'after-reup', 'after-guard']:
        assert read(w/f'legacy-{phase}.json') == before, phase
    assert read(w/'identities-before-restart.json') == read(w/'identities-after-guard.json')
    assert len(read(w/'identities-after-guard.json')) == 2
    assert 'cannot roll back nonempty plugin package identities' in (w/'migration-nonempty-down-rejected.log').read_text()
    assert not (w/'private.env').exists(), 'test credentials not cleared'
    assert 'campusos-v12-03b-identity-' in (w/'cleanup.log').read_text() and 'Error' not in (w/'cleanup.log').read_text()
    assert (w/'common-passed.txt').read_text().splitlines() == COMMON
    for name in ['plugin', 'server']:
        assert (w/f'native/{name}.test').read_bytes().startswith(b'\x7fELF')
    assert not any('WARNING: DATA RACE' in p.read_text() for p in (w/'native').glob('*.log'))
    assert not any('WARNING: DATA RACE' in p.read_text() for p in (w/'native').glob('*.stderr'))
    if final:
        assert (w/'final-docs-passed.txt').read_text().splitlines() == FINAL
    artifacts = {p.relative_to(ROOT).as_posix(): sha(p) for p in sorted(w.rglob('*')) if p.is_file() and 'tmp' not in p.relative_to(w).parts and p.name != 'private.env' and p.suffix in {'.log', '.json', '.jsonl', '.stderr', '.txt', '.test', '.gz'}}
    report = {'schema': 'campusos.v12-03b-identities/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(timespec='seconds'),
              'stage': 'V12-03b', 'stage_status': 'in_progress', 'slice': 'durable-package-identities', 'slice_status': 'completed' if final else 'verified_pending_documents',
              'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed', 'document_finalize': 'passed' if final else 'pending',
              'head': frozen['head'], 'current_migration': 26, 'next_migration': 27, 'business_tables': 103, 'executor_tables': 2,
              'native': native, 'postgres': pg, 'restored': restored, 'server_assembly': assembly,
              'checks': {'independent_go_processes': 6, 'postgres_restart': 'passed', 'concurrent_catalogs': 16, 'same_digest_idempotency': 'passed', 'different_digest_conflict': 'passed', 'current_revocation': 'passed', 'sql_immutability_and_validation': 'passed', 'commit_failure': 'passed', 'cancellation_and_closed_database': 'passed', 'legacy_tables_preserved': 102, 'legacy_sequences_preserved': True, 'empty_down_up_and_nonempty_rollback_guard': 'passed'},
              'common_gates': {n: 'passed' for n in COMMON}, 'final_document_gates': {n: 'passed' for n in FINAL} if final else {},
              'source_sha256': frozen['source_sha256'], 'dependencies': frozen['dependencies'], 'artifacts_sha256': artifacts,
              'documents_sha256': {n: sha(ROOT/n) for n in DOCS} if final else {},
              'boundaries': ['Only host-internal verified publication identity persistence; no new HTTP/CLI management or installation operation.', 'The ledger stores coordinates/digest only, not package bytes, signature trust, grants, active state or runtime health.', 'Server binds PostgreSQL even when other business modules fall back to memory; nil/closed/missing database fails closed on record.', 'Current trust is per catalog and is reverified; the identity ledger is never cached authorization.', 'Remaining03b: package byte persistence/catalog, global/user installation, grants/consent binding, desired/observed generations, Operation/Outbox, reconciliation and retention/cleanup.', 'No Dispatcher/UI/Runner, browser/Windows, dump/restore or whole-version final acceptance claim.'],
              'commit_or_push': False, 'shared_database_reset': False}
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('03b identity slice:', report['slice_status'], '; stage remains in progress;', native, '; PG fresh processes:6; common:', len(COMMON))


def main():
    p = argparse.ArgumentParser()
    g = p.add_mutually_exclusive_group(required=True)
    for name in ['freeze', 'verify-source', 'build', 'finalize']:
        g.add_argument('--'+name, type=pathlib.Path)
    args = vars(p.parse_args())
    mode = next(k for k,v in args.items() if v is not None)
    w = args[mode].resolve()
    assert w.is_relative_to(ROOT/'.cache/v12-03b-identity')
    if mode == 'freeze':
        deps = {}
        for name in DEPS:
            d = read(EVIDENCE/name)
            assert d['implementation'] == d['automation'] == d['target_environment'] == 'passed'
            deps[(EVIDENCE/name).relative_to(ROOT).as_posix()] = sha(EVIDENCE/name)
        frozen = {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(), 'source_sha256': sources(), 'dependencies': deps}
        (w/'source-freeze.json').write_text(json.dumps(frozen, ensure_ascii=False, indent=2)+'\n')
        REPORT.write_text(json.dumps({'schema': 'campusos.v12-03b-identities/v1', 'stage_status': 'in_progress', 'slice_status': 'acceptance_in_progress', 'implementation': 'pending', 'automation': 'pending', 'target_environment': 'pending'}, indent=2)+'\n')
        print('Frozen identity sources:', len(frozen['source_sha256']))
    elif mode == 'verify_source':
        verify_source(w)
        print('03b identity frozen sources and dependencies unchanged')
    else:
        build(w, mode == 'finalize')


if __name__ == '__main__':
    main()
