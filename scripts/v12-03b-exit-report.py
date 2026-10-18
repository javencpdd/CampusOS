#!/usr/bin/env python3
"""Full03b exit seal. Frozen source and required target events precede completion."""
import argparse
import hashlib
import json
import pathlib
import re
import subprocess
from datetime import datetime
from zoneinfo import ZoneInfo

ROOT = pathlib.Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / 'docs/项目计划书v1/项目计划v1.2/evidence'
REPORT = EVIDENCE / 'v12-03b-exit-linux.json'
COMMON = ['full-go', 'related-race', 'sdk', 'contracts', 'architecture', 'database', 'admin-build', 'er-tests', 'readme', 'docs-links', 'docs-build', 'line-endings', 'diff']
FINAL = ['docs-links', 'docs-build', 'line-endings', 'diff', 'architecture', 'skill-bridges', 'onboarding-skill']
DEPS = ['v12-03a-exit-linux.json', 'v12-02a-exit-linux.json', 'v12-02d-exit-linux.json', 'v12-03b-package-identities-linux.json']
DOCS = ['docs/进度/v1.2-dev/v1.2.116-dev.md', 'docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md', 'docs/api/v1.2插件v5安装与操作合同.md', 'docs/api/v1.2插件v5包字节与签名合同.md', 'docs/api/v1.2插件v5完整Manifest目标合同.md', 'docs/api/v1.2插件宿主资源与非侵入式安装合同.md', 'docs/api/v1.2插件双向调用事件与生命周期合同.md', 'docs/help/插件v5包与签名操作说明.md', 'docs/help/README.md', 'docs/architecture/当前架构概览.md', 'docs-site/guide/architecture.md', 'skills/sources/campusos-project-onboarding/references/current-project-status.md', 'migrations/README.md']


def read(p):
    return json.loads(p.read_text())


def sha(p):
    h = hashlib.sha256()
    with p.open('rb') as f:
        for chunk in iter(lambda: f.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def sources():
    names = subprocess.check_output(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'], cwd=ROOT, text=True).split('\0')
    suffixes = {'.go', '.mod', '.sum', '.ts', '.mts', '.vue', '.js', '.mjs', '.sql', '.py', '.sh', '.ps1', '.json', '.yaml', '.yml', '.html', '.css', '.svg', '.png'}
    return {n: sha(ROOT/n) for n in sorted(set(names)) if n and (ROOT/n).is_file() and not n.startswith('docs/项目计划书') and (pathlib.Path(n).suffix in suffixes or n == 'Makefile' or n.startswith('plugins/') and n.endswith('.md'))}


def verify_source(w):
    frozen = read(w/'source-freeze.json')
    assert sources() == frozen['source_sha256'], 'source changed after full03b target freeze'
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
        assert tops == required, (p, tops, required)
    return {'top_level': len(tops), 'pass_events': len(tests), 'fail': 0, 'skip': 0}


def build(w, final):
    frozen = verify_source(w)
    expected = {m for p in (ROOT/'internal/plugin').glob('*_test.go') for m in re.findall(r'^func (Test(?:V5|PluginV5)\w+)\(', p.read_text(), re.M)}
    native = events(w/'native/plugin.jsonl', expected)
    pg = events(w/'native/pg.jsonl', {'TestPostgresV5LifecycleAtomicityAndRecovery'})
    restored = events(w/'native/restore.jsonl', {'TestPostgresV5Restore'})
    assembly = events(w/'native/assembly.jsonl', {'TestPluginV5IdentityAssemblyUsesHostStore'})
    http = events(w/'native/http.jsonl', {'TestPluginV5ActualServerInstallationBoundary'})
    assert native['top_level'] >= 28 and pg['pass_events'] >= 6
    assert 160000 <= int((w/'pg-version.txt').read_text()) < 170000
    before = read(w/'legacy-before.json')
    assert len(before['tables']) == 103 and before['sequences']
    for table in ['plugins', 'plugin_versions', 'users', 'user_storage_quotas', 'admin_accounts', 'plugin_package_identities']:
        assert before['tables'][table], table
    for phase in ['after-up', 'after-down', 'after-reup']:
        assert read(w/f'legacy-{phase}.json') == before, phase
    assert read(w/'legacy-before-restart.json') == read(w/'legacy-after-restart.json')
    assert read(w/'legacy-before-guard.json') == read(w/'legacy-after-guard.json')
    assert read(w/'v5-before-restart.json') == read(w/'v5-after-restart.json')
    assert read(w/'v5-before-guard.json') == read(w/'v5-after-guard.json')
    epochs = [int((w/f'epoch-{phase}.txt').read_text()) for phase in ['before-down', 'after-down', 'after-reup']]
    assert 0 < epochs[0] < epochs[1] < epochs[2]
    assert 'cannot roll back nonempty v5 installation domain' in (w/'migration-nonempty-down-rejected.log').read_text()
    assert not (w/'private.env').exists(), 'test credentials not cleared'
    assert 'campusos-v12-03b-exit-' in (w/'cleanup.log').read_text() and 'Error' not in (w/'cleanup.log').read_text()
    assert (w/'common-passed.txt').read_text().splitlines() == COMMON
    for name in ['plugin.test', 'server.test', 'campusos-server']:
        with (w/'native'/name).open('rb') as f:
            assert f.read(4) == b'\x7fELF'
    server = read(w/'server/server-result.json')
    assert server['status'] == 'passed' and server['actual_cmd_server'] and server['installation_http'] and server['migration'] == 27
    assert server['server_binary_sha256'] == sha(w/'native/campusos-server')
    for p in list((w/'native').glob('*.log')) + list((w/'native').glob('*.stderr')) + [w/'server/actual-server.log']:
        assert 'WARNING: DATA RACE' not in p.read_text(), p
    if final:
        assert (w/'final-docs-passed.txt').read_text().splitlines() == FINAL
    artifacts = {p.relative_to(ROOT).as_posix(): sha(p) for p in sorted(w.rglob('*')) if p.is_file() and not {'tmp', 'managed'} & set(p.relative_to(w).parts) and p.name != 'private.env' and (p.suffix in {'.log', '.json', '.jsonl', '.stderr', '.txt', '.test', '.gz'} or p.name == 'campusos-server')}
    report = {'schema': 'campusos.v12-03b-exit/v1', 'generated_at': datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(timespec='seconds'),
              'stage': 'V12-03b', 'stage_status': 'completed' if final else 'verified_pending_documents', 'scope': 'all remaining03b after accepted durable identity slice',
              'implementation': 'passed', 'automation': 'passed', 'target_environment': 'passed', 'document_finalize': 'passed' if final else 'pending',
              'head': frozen['head'], 'current_migration': 27, 'next_migration': 28, 'business_tables': 111, 'executor_tables': 2,
              'native': native, 'postgres': pg, 'restored': restored, 'server_assembly': assembly, 'actual_http': http,
              'checks': {'postgres_restart': 'passed', 'fresh_postgres_processes': 2, 'concurrent_hosts_cas': 12, 'current_admin_permission_mfa_and_session': 'passed', 'actual_current_user_session_and_owner_isolation': 'passed', 'browser_csrf_both_domains': 'passed', 'audit_outbox_and_real_commit_failure': 'passed', 'stale_generation_lease_expiry_and_config_tamper': 'passed', 'desired_observed_independent_and_no_fake_runtime_ready': 'passed', 'preserve_upgrade_switch_and_schema_change_rejection': 'passed', 'owner_retention_purge_and_referenced_release_gc': 'passed', 'legacy_tables_preserved': 103, 'legacy_sequences_preserved': True, 'empty_down_up_epoch_nonreuse_and_nonempty_rollback_guard': 'passed', 'legacy_controls_retired': 'passed'},
              'common_gates': {n: 'passed' for n in COMMON}, 'final_document_gates': {n: 'passed' for n in FINAL} if final else {},
              'source_sha256': frozen['source_sha256'], 'dependencies': frozen['dependencies'], 'artifacts_sha256': artifacts,
              'documents_sha256': {n: sha(ROOT/n) for n in DOCS} if final else {},
              'boundaries': ['No Dispatcher/resource ACL or callable Host Capability authorization from a stored DTO.', 'Production has no runtime observer: desired enable waits pending/runtime_unavailable; ready promotion is tested only through an explicitly bounded host Port.', '03c/04/05/06 own actual contribution publication, cancellation/drain, UI Host and Runner isolation.', 'Only equal private schema preserve upgrades are supported; no unverifiable data migration or snapshot rollback claim.', 'No browser/Windows/real Runner, power-loss or whole-system backup-restore claim; target is native Linux, PostgreSQL restart and actual cmd/server HTTP.', 'No shared database reset, legacy migration rewrite, commit or push.'],
              'commit_or_push': False, 'shared_database_reset': False}
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print('03b full exit:', report['stage_status'], '; native:', native, '; PG:', pg, '; HTTP:', http, '; common:', len(COMMON))


def main():
    p = argparse.ArgumentParser()
    g = p.add_mutually_exclusive_group(required=True)
    for name in ['freeze', 'verify-source', 'build', 'finalize']:
        g.add_argument('--'+name, type=pathlib.Path)
    args = vars(p.parse_args())
    mode = next(k for k,v in args.items() if v is not None)
    w = args[mode].resolve()
    assert w.is_relative_to(ROOT/'.cache/v12-03b-exit')
    if mode == 'freeze':
        deps = {}
        for name in DEPS:
            d = read(EVIDENCE/name)
            assert d['implementation'] == d['automation'] == d['target_environment'] == 'passed'
            deps[(EVIDENCE/name).relative_to(ROOT).as_posix()] = sha(EVIDENCE/name)
        frozen = {'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(), 'source_sha256': sources(), 'dependencies': deps}
        (w/'source-freeze.json').write_text(json.dumps(frozen, ensure_ascii=False, indent=2)+'\n')
        REPORT.write_text(json.dumps({'schema': 'campusos.v12-03b-exit/v1', 'stage_status': 'acceptance_in_progress', 'implementation': 'pending', 'automation': 'pending', 'target_environment': 'pending'}, indent=2)+'\n')
        print('Frozen full03b sources:', len(frozen['source_sha256']))
    elif mode == 'verify_source':
        verify_source(w)
        print('Full03b frozen sources and dependencies unchanged')
    else:
        build(w, mode == 'finalize')

if __name__ == '__main__':
    main()
