#!/usr/bin/env python3
"""Assemble and finalize current-source V12-02b evidence without reusing old PASS flags."""
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

work = Path(sys.argv[1]).resolve()
finalize = len(sys.argv) > 2 and sys.argv[2] == '--finalize'
base = Path('docs/项目计划书v1/项目计划v1.2/evidence')
exit_path = base / 'v12-02b-exit-linux.json'
spa_path = base / 'v12-02b-admin-spa-linux.json'
sha = lambda p: hashlib.sha256(Path(p).read_bytes()).hexdigest()
plan = json.loads((work/'test-plan.json').read_text())
freeze = json.loads((work/'source-freeze.json').read_text())
continuation = None
accepted_freeze = freeze
if (work/'continuation-proof.json').exists():
    continuation = json.loads((work/'continuation-proof.json').read_text())
    prior = Path(continuation['predecessor_work'])
    assert (prior/'.exit_code').read_text().strip() == '1'
    assert sha(prior/'source-freeze.json') == continuation['predecessor_freeze_sha256']
    assert sha(prior/'architecture.log') == continuation['predecessor_failure_sha256']
    accepted_freeze = json.loads((prior/'source-freeze.json').read_text())
    added = 'admin/src/modules/identity/navigation.ts'
    assert set(freeze) == set(accepted_freeze) | {added}
    changed = {p for p in accepted_freeze if accepted_freeze[p] != freeze[p]}
    assert changed == {'admin/src/router/index.ts', 'scripts/v12-02b-exit-report.py', 'scripts/v12-02b-exit-plan.py'}
    old_router = prior/'facade-refactor/router.before.ts.txt'
    assert sha(old_router) == accepted_freeze['admin/src/router/index.ts']
    guard = old_router.read_text().split('export const adminAuthGuard: NavigationGuard =', 1)[1].split('\n\nrouter.beforeEach', 1)[0]
    assert Path(added).read_text().split('export const adminAuthGuard: NavigationGuard =', 1)[1].rstrip() == guard.rstrip()
    for item in continuation['carried_checks']:
        assert sha(item['original_log']) == item['sha256']
        assert sha(work/(item['check']+'.log')) == item['sha256']
for path, digest in freeze.items():
    assert sha(path) == digest, ('frozen current source changed', path)
# Catch new/deleted implementation files as well as changed bytes.
paths = subprocess.check_output(['git','ls-files','--cached','--others','--exclude-standard'], text=True).splitlines()
extensions={'.go','.ts','.mts','.vue','.mjs','.py','.sh','.ps1','.sql','.yaml','.json','.mod','.sum','.toml','.lock','.css','.html'}
prefixes=('internal/','cmd/','pkg/','admin/src/','admin/tests/','web/src/','web/tests/','migrations/','modules/','scripts/','sdk/','packages/','schemas/','contracts/','skills/sources/')
configs={'Makefile','go.mod','go.sum','admin/package.json','admin/pnpm-lock.yaml','admin/vite.config.ts','admin/vitest.config.ts','admin/tsconfig.json','web/package.json','web/pnpm-lock.yaml','web/vite.config.ts','web/vitest.config.ts','web/tsconfig.json'}
current={p for p in paths if Path(p).is_file()and(p in configs or p.startswith(prefixes)and Path(p).suffix in extensions)}
assert current == set(freeze), ('current source inventory changed', current ^ set(freeze))
for report in plan['historical_reports']:
    assert sha(report['path']) == report['sha256'], report['path']
logs={}
for state in sorted((work/'check-state').glob('*.exit_code')):
    assert state.read_text().strip()=='0', state
    name=state.name.removesuffix('.exit_code');log=work/(name+'.log');assert log.exists()
    logs[name]={'status':'passed','exit_code':0,'local_log':str(log),'log_sha256':sha(log)}
required_common=['web-component','web-format','web-build','admin-component','admin-build','admin-event-browser','server-build','owner-policy-oracle','composition','windows-space-compile','windows-community-compile','go-all','migration-parser','architecture-sync','contracts','architecture','docs-build','docs-links','line-endings','diff','preservation']
assert set(required_common)<=logs.keys()
checks={}
for shard in plan['shards']:
    name='target-'+shard['name'];assert name in logs
    rows=[]
    for line in Path(logs[name]['local_log']).read_text().splitlines():
        if line.startswith('{'):
            rows.append(json.loads(line))
    assert not [r for r in rows if r.get('Action')in['fail','skip']], ('target failed/skipped',name)
    assert not any('WARNING: DATA RACE' in r.get('Output','')for r in rows)
    passed=[r for r in rows if r.get('Action')=='pass'and r.get('Test')and '/'not in r['Test']]
    assert sorted(r['Test']for r in passed)==shard['tests'], ('target differs from complete current plan',name)
    for row in passed:
        assert row['Test']not in checks
        checks[row['Test']]={'status':'passed','package':row['Package'],'log':name}
assert sorted(checks)==plan['required_tests'] and len(checks)==plan['required_count']
if continuation:
    assert {'facade-targeted', 'facade-format', 'target-browser-refactor'} <= logs.keys()
    rows = [json.loads(line) for line in Path(logs['target-browser-refactor']['local_log']).read_text().splitlines() if line.startswith('{')]
    assert not [row for row in rows if row.get('Action') in ['fail', 'skip']]
    assert not any('WARNING: DATA RACE' in row.get('Output', '') for row in rows)
    passed = [row for row in rows if row.get('Action') == 'pass' and row.get('Test') and '/' not in row['Test']]
    assert len(passed) == 1 and passed[0]['Test'] == 'TestAdminSPABrowserActualServerCurrentIdentityAndLatePrivateResponse'
    checks[passed[0]['Test']] = {'status':'passed', 'package':passed[0]['Package'], 'log':'target-browser-refactor'}
preservation=json.loads((work/'preservation/preservation.json').read_text())
assert preservation['status']=='passed'and preservation['current']==24
assert [r['migration']for r in preservation['migrations']]==[f'{n:06d}'for n in range(16,25)]
for row in preservation['migrations']:
    assert len({item['sha256']for item in row['snapshots']})==1
    for item in row['snapshots']:assert sha(item['path'])==item['sha256']
    if row['highwater']:
        a,b,c,d=row['highwater'];assert a<b<=c<d
oracle=json.loads((work/'owner-policy.json').read_text());assert oracle['passed']and oracle['native_go_decisions']==216
browser=json.loads((work/'browser/browser-result.json').read_text())
runtime=json.loads((work/'browser/runtime-proof.json').read_text())
assert browser['status']=='passed'and not browser['mocked_api']and len(browser['checks'])>=17
assert runtime['actual_cmd_server']and runtime['actual_pg_pin_and_typed_audit']and runtime['server_binary_sha256']==sha(work/'campusos-server')
assets={}
for folder in ['admin/dist','web/dist']:
    assets.update({str(p):sha(p)for p in Path(folder).rglob('*')if p.is_file()})
assert assets
artifacts={str(p):sha(p)for p in [work/'test-plan.json',work/'source-freeze.json',work/'preservation/preservation.json',work/'browser/browser-result.json',work/'browser/runtime-proof.json',work/'browser/browser.log',work/'browser/actual-server.log',work/'campusos-server',work/'owner-policy.jsonl',work/'owner-policy.json']}
if continuation:
    artifacts[str(work/'continuation-proof.json')] = sha(work/'continuation-proof.json')
reuse_proof=None
if (work/'reused-checks.json').exists():
    reuse_proof=json.loads((work/'reused-checks.json').read_text())
    assert sha(reuse_proof['original_freeze'])==reuse_proof['original_freeze_sha256']
    assert sha(reuse_proof['original_failed_target'])==reuse_proof['original_failed_target_sha256']
    original=json.loads(Path(reuse_proof['original_freeze']).read_text());assert original.keys()==accepted_freeze.keys()
    assert {p for p in original if original[p]!=accepted_freeze[p]}<=set(reuse_proof['orchestration_changes'])
    for item in reuse_proof['checks']:assert sha(Path(item['source_work'])/(item['check']+'.log'))==item['source_log_sha256']
    artifacts[str(work/'reused-checks.json')]=sha(work/'reused-checks.json')
real_pg=[name for name in checks if 'PostgreSQL' in name or 'Postgres' in name]
common={
 'generated_at':datetime.now(timezone.utc).isoformat(),'stage':'V12-02b','stage_status':'pending_final_documents','implementation':'passed','automation':'passed','target_environment':'passed','common_gates_status':'passed','document_finalize':'pending',
 'environment':{'os':'Linux','postgres_version_num':int((work/'pg-version.txt').read_text()),'database_count':13,'database_scope':'owned disposable PostgreSQL16 loopback tmpfs; independent identity cleanup, current Admin SPA and four incompatible Community fixture environments separated','shared_database_reset':False,'browser':browser['browser'],'actual_cmd_server':True,'mocked_api':False,'windows':'two packages cross-compiled only; no Windows runtime claim'},
 'source_sha256':freeze,'reused_check_proof':reuse_proof,'continuation_proof':continuation,'native_owner_policy_oracle':oracle,'logs':logs,'artifacts_sha256':artifacts,'built_frontend_sha256':assets,'historical_inputs':plan['historical_reports'],'historical_input_semantics':'Historical report bytes identify assertions and responsibility only. Every required current Go assertion was executed with unchanged Go/test/DDL inputs. A narrowly verified frontend guard extraction reruns all Admin components, build, UI-only browser and actual backend browser; its proof preserves the prior failed boundary gate and every carried log. Old source hashes or unverified PASS flags are not treated as current execution.',
 'migration_preservation':preservation,'browser':browser,'worktree_context':{'preflight_head':(work/'head.txt').read_text().strip(),'agent_git_mutation':False,'frozen_current_source_files':len(freeze)},
 'boundaries':['User/Admin resource paths and the scoped responsibility inventory only; no new Integration/v5 Dispatcher/Knowledge implementation.','Existing catalogue/configuration/recipient notification endpoints retain their own baseline guards; no private original-body authority is inferred from metadata or previews.','All target top-level and sub-tests required here have no FAIL/SKIP/race. Common Native runs may skip environment-dependent tests; these skips are not counted as target acceptance.','HTTP output already emitted cannot be retracted; authorization rechecks occur at the documented command/read boundaries.'],
}
spa_names=[n for n in checks if n.startswith(('TestAdminSPA','TestAdminMFA','TestAdminSession','TestPostgreSQLAdminMFA','TestPostgreSQLAdminSession'))or n=='TestAdminDomainHTTPBootstrapLoginAndCrossDomainRejection']
spa=dict(common,schema='campusos.v12-02b-admin-spa-acceptance/v1',slice_status='completed',scope='Current independent Admin browser identity, refresh/CSRF, self MFA/logout, current SPA state/guards and actual server browser; does not replace management permissions.',checks={n:checks[n]for n in spa_names})
exit_report=dict(common,schema='campusos.v12-02b-exit-acceptance/v1',scope='Complete V12-02b current-source responsibility inventory and historical assertion union; nine current DDL preservation paths, actual backend browser and impact-based common gates.',checks=checks,coverage={'historical_reports':len(plan['historical_reports']),'historical_test_names':plan['historical_test_count'],'required_current_tests':len(checks),'new_or_changed_tests':len(plan['new_or_changed_tests']),'real_pg_named_tests':len(real_pg),'current_pg_named_test_names':real_pg,'target_shards':len(plan['shards']),'migration_paths':9,'actual_browser_checks':len(browser['checks'])})
documents=['docs/进度/v1.2-dev/v1.2.109-dev.md','docs/进度/v1.2-dev/v1.2.110-dev.md','docs/api/v1.2独立管理员SPA当前身份与会话合同.md','docs/api/v1.2-02b阶段出口责任清单.json','docs/api/v1.2业务授权接入与阶段验收.md','docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md','docs/architecture/当前架构概览.md','docs-site/guide/architecture.md','skills/sources/campusos-project-onboarding/references/current-project-status.md','docs/api/http-routes-v0.6.json','docs/api/HTTP路由与授权矩阵-v0.6.md','docs/api/openapi-v0.6-current.yaml']
if finalize:
    finals=sorted((work/'final-docs').glob('*.exit_code'));assert len(finals)==7
    final_logs={}
    for state in finals:
        assert state.read_text().strip()=='0',state
        log=state.with_name(state.name.removesuffix('.exit_code')+'.log');assert log.exists()
        final_logs[log.stem]={'status':'passed','exit_code':0,'local_log':str(log),'log_sha256':sha(log)}
    for report in [spa,exit_report]:
        report['documents_sha256']={p:sha(p)for p in documents}
        report['logs'].update(final_logs)
        report['stage_status']='completed';report['document_finalize']='passed';report['next_eligible_stage']='V12-02c; not started'
    assert (work/'.exit_code').read_text().strip()=='0'
spa_path.write_text(json.dumps(spa,ensure_ascii=False,indent=2)+'\n')
exit_report['dependencies']={str(spa_path):sha(spa_path)}
exit_path.write_text(json.dumps(exit_report,ensure_ascii=False,indent=2)+'\n')
print('Verified',len(checks),'required current tests,',len(freeze),'frozen source files,',len(browser['checks']),'real browser checks;', 'phase completed'if finalize else 'final document checks pending')
