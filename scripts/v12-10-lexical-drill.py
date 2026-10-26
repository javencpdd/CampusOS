#!/usr/bin/env python3
"""V12-10 public lexical Port slice: owned Linux PG16 and normal Server graph.
No HTTP/browser, external Provider, full Chinese-quality or capacity claim.
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
PRIOR = REPO / 'docs/项目计划书v1/项目计划v1.2/evidence/v12-09b-exit-linux.json'
PRIOR_SHA = '4319f292e646c129af75ad4bbf87e3fc075a690d38197fb6733a9159062636f6'
DATABASE = 'campusos_v12_10_lexical'
TABLES = ['knowledge_index_tasks', 'knowledge_index_backfill_checkpoints', 'knowledge_index_jobs', 'knowledge_index_documents']


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
    assert sha(PRIOR) == PRIOR_SHA
    assert work.is_relative_to(REPO / '.cache/v12-10-lexical')
    work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    runtime = work / 'runtime'
    runtime.mkdir(mode=0o700)
    native = work / 'native'
    native.mkdir()
    frozen = source_map()
    (work / 'source-freeze.json').write_text(json.dumps(frozen, ensure_ascii=False, indent=2) + '\n')
    env = {k: os.environ[k] for k in ('PATH','HOME') if k in os.environ}
    env.update(GOCACHE=str(REPO / '.cache/v12-g0/go-cache'), GOMAXPROCS='2', GOTOOLCHAIN='auto', GOFLAGS='-buildvcs=false', CAMPUSOS_SKIP_DOTENV='true')
    password = secrets.token_hex(24)
    container = 'campusos-v12-10-lexical-' + secrets.token_hex(6)
    commands = []
    started = False
    def run(name,args,extra=None,input_bytes=None):
        begin=time.monotonic()
        with (work/(name+'.log')).open('wb') as output:
            result=subprocess.run(args,cwd=REPO,env=env|(extra or {}),input=input_bytes,stdout=output,stderr=subprocess.STDOUT)
        commands.append({'name':name,'exit_code':result.returncode,'seconds':round(time.monotonic()-begin,3)})
        assert result.returncode==0,name+' failed; inspect private log'
        return (work/(name+'.log')).read_text()
    def psql(db=DATABASE):
        return ['docker','exec','-i',container,'psql','-U','campusos','-d',db,'-XAt','-v','ON_ERROR_STOP=1']
    def mandatory(log,name):
        assert '--- PASS: '+name+' ' in log and not any(s in log for s in ('--- FAIL','--- SKIP','DATA RACE'))
    try:
        for package,name in [('./internal/modules/features/knowledge','knowledge.test'),('./internal/server','server.test')]:
            run('build-'+name,['go','test','-race','-c','-o',str(native/name),package])
        binaries={str(p.relative_to(REPO)):sha(p) for p in native.iterdir()}
        unit=run('native-knowledge-unit',[str(native/'knowledge.test'),'-test.v'])
        assert 'PASS\n' in unit and not any(s in unit for s in ('--- FAIL','--- SKIP','DATA RACE'))
        run('pg-start',['docker','run','--rm','--pull=never','-d','--name',container,'--tmpfs','/var/lib/postgresql/data:rw,size=512m','-e','POSTGRES_USER=campusos','-e','POSTGRES_DB='+DATABASE,'-e','POSTGRES_PASSWORD','-p','127.0.0.1::5432','postgres:16-alpine'],{'POSTGRES_PASSWORD':password})
        started=True
        for _ in range(120):
            if subprocess.run(['docker','exec',container,'pg_isready','-U','campusos','-d',DATABASE],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
            time.sleep(.25)
        else:raise AssertionError('owned PG startup deadline')
        port=int(run('pg-port',['docker','port',container,'5432/tcp']).strip().rsplit(':',1)[1])
        pg_env={'PSQL_MODE':'docker','POSTGRES_CONTAINER':container,'DB_USER':'campusos','DB_NAME':DATABASE,'DB_PASSWORD':password}
        run('migration-up',['bash','scripts/migrate.sh','up'],pg_env)
        run('migration-check',['bash','scripts/migrate.sh','check'],pg_env)
        run('migration-status',['bash','scripts/migrate.sh','status'],pg_env)
        run('database-entry',['bash','scripts/database-check.sh','all'],pg_env)
        version=run('pg-version',psql()+['-c','SHOW server_version']).strip()
        dump=['docker','exec',container,'pg_dump','-U','campusos','-d',DATABASE,'--schema-only','--no-owner','--no-privileges']
        before=run('schema-before',dump)
        target_env={
            'CAMPUSOS_KNOWLEDGE_RETRIEVAL_DSN':f'postgres://campusos:{password}@127.0.0.1:{port}/{DATABASE}?sslmode=disable',
            'CAMPUSOS_KNOWLEDGE_RETRIEVAL_ROOT':str(runtime),'DATABASE_DSN':f'postgres://campusos:{password}@127.0.0.1:{port}/{DATABASE}?sslmode=disable',
            'CAMPUSOS_ENV':'test','REDIS_ENABLED':'false','NATS_URL':'nats://127.0.0.1:1','HOST_API_ENABLED':'false',
            'JWT_SECRET':secrets.token_hex(32),'AUTH_PASSWORD_HASH_ENABLED':'true','AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN':'false',
            'AUTH_BOOTSTRAP_ADMIN_SECRET':secrets.token_hex(24),'EMAIL_PROVIDER':'fake','AI_ENABLED':'false',
            'AUTH_MFA_ACTIVE_KEY_ID':'owned10','AUTH_MFA_ENCRYPTION_KEYS':'owned10:'+secrets.token_hex(32),
            'PLUGIN_DATA_DIR':str(runtime/'data/plugin_data'),'GIN_MODE':'release','OBSERVABILITY_PROMETHEUS_ENABLED':'false',
        }
        actual=run('native-postgres-target',[str(native/'server.test'),'-test.v','-test.run=^TestKnowledgeRetrievalPublicLexicalPostgres$','-test.timeout=180s'],target_env)
        mandatory(actual,'TestKnowledgeRetrievalPublicLexicalPostgres')
        proof=json.loads((runtime/'target-proof.json').read_text())
        assert proof['status']=='passed' and proof['check_count']==7 and proof['actual_server_composition'] and proof['actual_postgresql'] and not proof['fake_provider_or_manual_page']
        regression_db='campusos_v12_09b_exit'
        run('execution-regression-db',psql('postgres')+['-c','CREATE DATABASE '+regression_db])
        run('execution-regression-migrations',['bash','scripts/migrate.sh','up'],pg_env|{'DB_NAME':regression_db})
        legacy_root=REPO/'.cache/v12-09b-exit'/('retrieval-regression-'+container.rsplit('-',1)[1])
        legacy_root.mkdir(mode=0o700,exist_ok=False)
        legacy_env=target_env|{
            'CAMPUSOS_KNOWLEDGE_EXECUTION_DSN':f'postgres://campusos:{password}@127.0.0.1:{port}/{regression_db}?sslmode=disable',
            'CAMPUSOS_KNOWLEDGE_EXECUTION_ROOT':str(legacy_root),'DATABASE_DSN':f'postgres://campusos:{password}@127.0.0.1:{port}/{regression_db}?sslmode=disable',
            'PLUGIN_DATA_DIR':str(legacy_root/'data/plugin_data'),
        }
        regression=run('native-index-execution-regression',[str(native/'server.test'),'-test.v','-test.run=^TestKnowledgeIndexExecutionResidentPostgres$','-test.timeout=180s'],legacy_env)
        mandatory(regression,'TestKnowledgeIndexExecutionResidentPostgres')
        regression_proof=json.loads((legacy_root/'target-proof.json').read_text())
        assert regression_proof['status']=='passed' and regression_proof['check_count']==12
        (work/'execution-regression-proof.json').write_text(json.dumps(regression_proof,ensure_ascii=False,indent=2)+'\n')
        run('database-exit',['bash','scripts/database-check.sh','all'],pg_env)
        after=run('schema-after',dump)
        assert normalize(before)==normalize(after),'whole schema changed'
        assert source_map()==frozen,'source changed during target'
        assert all(sha(REPO/n)==h for n,h in binaries.items()),'native ELF changed'
        report={'schema':'campusos.v12-10-lexical-slice/v1','status':'passed','stage':'V12-10','stage_status':'in_progress','slice_status':'completed_current_linux_scope',
                'implementation':'passed','automation':'passed','target_environment':'passed_linux_native_production_modules_pg16',
                'proof':proof,'index_execution_regression':regression_proof,'postgres_version':version,'source_sha256':frozen,'binary_sha256':binaries,
                'dependency':{str(PRIOR.relative_to(REPO)):PRIOR_SHA},'schema_before_after_sha256':hashlib.sha256(normalize(before).encode()).hexdigest(),
                'commands':commands,'schema_tables':124,'migration_version':33,'new_migrations':[], 'commit_or_push':False,'windows':'deferred_by_user_not_run',
                'scope_limits':['one anonymous-public collection internal typed Port, built-in in-process lexical Provider only','literal Chinese/Latin keyword smoke and raw query timings only; no full Chinese question-quality or target capacity acceptance','no HTTP/browser/UI/plugin/MCP/Integration ingress, external Provider or AI/egress grant','stage10 remains incomplete; no11/12/13/14 work']}
        (work/'target.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
        print(json.dumps({'status':'passed','work':str(work.relative_to(REPO)),'checks':proof['check_count'],'stage_status':'in_progress'}),flush=True)
    finally:
        if started:
            run('pg-server',['docker','logs',container])
            run('pg-cleanup',['docker','stop',container])
        (work/'cleanup.json').write_text(json.dumps({'owned_postgres':container,'credentials_persisted':False,'commands':commands[-2:] if started else []},indent=2)+'\n')

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work',required=True)
    main(Path(parser.parse_args().work).resolve())
