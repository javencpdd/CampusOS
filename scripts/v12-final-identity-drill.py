#!/usr/bin/env python3
"""Final current identity/resource/Integration matrix on owned PG16 and built UIs.
Historical migration-preservation reports stay immutable; this target reexecutes
all current assertions, adds Integration lifecycle/browser, and binds full DDL.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import time

ROOT = Path(__file__).resolve().parent.parent
LABEL = 'campusos.final-identity.owner'

def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

def normal(raw):
    return '\n'.join(x for x in raw.splitlines() if not x.startswith(('\\restrict ', '\\unrestrict ')))

def main(work):
    assert os.uname().sysname == 'Linux'
    assert work.is_relative_to(ROOT / '.cache/v12-final-identity') and work != ROOT / '.cache/v12-final-identity'
    work.mkdir(parents=True, exist_ok=False)
    work.chmod(0o700)
    for name in ['docker-config', 'native']:
        (work / name).mkdir(mode=0o700)
    spec = importlib.util.spec_from_file_location('restore', ROOT / 'scripts/check-release-restore.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    frozen = module.sources()
    (work / 'source-freeze.json').write_text(json.dumps(frozen, ensure_ascii=False, indent=2) + '\n')
    frontend = {str(p.relative_to(ROOT)): sha(p) for base in ['web/dist', 'admin/dist'] for p in (ROOT / base).rglob('*') if p.is_file()}
    assert frontend and (ROOT/'web/dist/index.html').is_file() and (ROOT/'admin/dist/index.html').is_file()
    env = {k: os.environ[k] for k in ('PATH', 'HOME') if k in os.environ}
    env.update(GOCACHE='/tmp/campusos-go-cache', GOFLAGS='-buildvcs=false', CAMPUSOS_SKIP_DOTENV='true',
               DOCKER_HOST='unix:///var/run/docker.sock', DOCKER_CONFIG=str(work/'docker-config'))
    nonce = secrets.token_hex(12)
    container = 'campusos-final-identity-' + nonce
    password = secrets.token_hex(24)
    commands, checks, cleanup = [], {}, {'errors': []}
    attempted, cid, port = False, None, None
    suffixes = ['space_files','author_write','richtext_read','asset_read_empty','admin_spa','identity','public','list','owner','owner_list','document_read','typed_read']
    databases = ['campusos_v12_02b_'+s for s in suffixes] + ['campusos_v12_02c','campusos_v12_02c_browser']
    def run(name, args, extra=None, data=None):
        started=time.monotonic()
        result=subprocess.run([str(a) for a in args],cwd=ROOT,env=env|(extra or {}),input=data,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        raw=result.stdout.replace(password.encode(),b'[redacted]')
        (work/(name+'.log')).write_bytes(raw)
        commands.append({'name':name,'exit_code':result.returncode,'seconds':round(time.monotonic()-started,3),'args':[str(a) for a in args]})
        assert result.returncode==0,name+' failed; inspect owned log'
        return raw.decode()
    def sql(db, statement):
        return run('sql-'+str(len(commands)),['docker','exec','-i',container,'psql','-U','campusos','-d',db,'-XAtv','ON_ERROR_STOP=1'],data=statement.encode())
    def dump(db, phase):
        return normal(run(phase+'-'+db,['docker','exec',container,'pg_dump','-U','campusos','-d',db,'--schema-only','--no-owner','--no-privileges']))
    try:
        run('test-plan',['python3','scripts/v12-02b-exit-plan.py',work/'test-plan.json'],{'CAMPUSOS_FINAL_ACCEPTANCE':'true'})
        plan=json.loads((work/'test-plan.json').read_text())
        run('build-server',['go','build','-o',work/'native/server','./cmd/server'])
        binary={str((work/'native/server').relative_to(ROOT)):sha(work/'native/server')}
        image=run('pg-image',['docker','image','inspect','postgres:16-alpine','--format={{.Id}}']).strip()
        attempted=True
        cid=run('pg-start',['docker','run','--rm','--pull=never','-d','--name',container,'--label',LABEL+'='+nonce,'--tmpfs','/var/lib/postgresql/data:rw,size=2g','--cpus=2','--memory=2g','--memory-swap=2g','--pids-limit=256','-e','POSTGRES_USER=campusos','-e','POSTGRES_DB='+databases[0],'-e','POSTGRES_PASSWORD','-p','127.0.0.1::5432',image],{'POSTGRES_PASSWORD':password}).strip()
        for _ in range(120):
            if subprocess.run(['docker','exec',container,'pg_isready','-h','127.0.0.1','-U','campusos'],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
            time.sleep(.25)
        else:raise AssertionError('owned PG readiness deadline')
        scope=json.loads(run('scope',['docker','inspect',container,'--format={{json .}}']))
        assert scope['Id']==cid and scope['Config']['Labels'].get(LABEL)==nonce
        assert scope['HostConfig']['PortBindings']['5432/tcp'][0]['HostIp']=='127.0.0.1'
        assert not scope['Mounts'] or all(m['Type']=='tmpfs' for m in scope['Mounts'])
        assert sql('postgres','SHOW server_version_num;').startswith('16')
        port=int(run('pg-port',['docker','port',container,'5432/tcp']).strip().rsplit(':',1)[1])
        pg={'PSQL_MODE':'docker','POSTGRES_CONTAINER':container,'DB_NAME':databases[0],'DB_USER':'campusos','DB_PASSWORD':password}
        run('migrate-up',['bash','scripts/migrate.sh','up'],pg)
        assert sql(databases[0],"SELECT count(*) FROM pg_tables WHERE schemaname='public';SELECT max(version)::int FROM schema_migrations;SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='public'::regnamespace;").splitlines()==['132','35','143']
        for db in databases[1:]:sql('postgres','CREATE DATABASE '+db+' TEMPLATE '+databases[0]+';')
        schemas={db:dump(db,'before') for db in databases}
        run('legacy-author-fixture',['docker','exec','-i',container,'psql','-U','campusos','-d','campusos_v12_02b_author_write','-v','ON_ERROR_STOP=1'],data=(ROOT/'scripts/fixtures/v12-02b-business-actor-legacy.sql').read_bytes())
        dsn=lambda db:f'postgres://campusos:{password}@127.0.0.1:{port}/{db}?sslmode=disable'
        targets={
            'CAMPUSOS_SPACE_FILES_TEST_DATABASE_URL':'space_files','CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL':'author_write',
            'CAMPUSOS_RICHTEXT_TEST_DATABASE_URL':'richtext_read','CAMPUSOS_ASSET_EMPTY_TEST_DATABASE_URL':'asset_read_empty',
            'CAMPUSOS_IDENTITY_TEST_DATABASE_URL':'identity','CAMPUSOS_DOCUMENT_TEST_DATABASE_URL':'document_read',
            'CAMPUSOS_TYPED_FEATURE_TEST_DATABASE_URL':'typed_read','CAMPUSOS_ADMIN_SPA_TEST_DATABASE_URL':'admin_spa'}
        extra={k:dsn('campusos_v12_02b_'+v) for k,v in targets.items()}
        extra.update(CAMPUSOS_INTEGRATION_TEST_DATABASE_URL=dsn('campusos_v12_02c'),CAMPUSOS_INTEGRATION_BROWSER_DATABASE_URL=dsn('campusos_v12_02c_browser'),
            CAMPUSOS_INTEGRATION_SERVER_BINARY=str(work/'native/server'),CAMPUSOS_INTEGRATION_BROWSER_OUTPUT=str(work/'integration-browser'),
            CAMPUSOS_ADMIN_SPA_SERVER_BINARY=str(work/'native/server'),CAMPUSOS_ADMIN_SPA_BROWSER_OUTPUT=str(work/'browser'),CAMPUSOS_THREAD_OWNER_POLICY_OUTPUT=str(work/'owner-policy.jsonl'))
        for shard in plan['shards']:
            current=extra if shard['name']=='main' else {'CAMPUSOS_COMMUNITY_TEST_DATABASE_URL':dsn(shard['database'])}
            pattern='^('+'|'.join(shard['tests'])+')$'
            raw=run('target-'+shard['name'],['go','test','-json','-race','-p','1','-count=1','-run',pattern,*shard['packages']],current)
            rows=[json.loads(x) for x in raw.splitlines() if x.startswith('{')]
            assert not any(x.get('Action') in ('fail','skip') or 'DATA RACE' in x.get('Output','') for x in rows)
            passed=[x['Test'] for x in rows if x.get('Action')=='pass' and x.get('Test') and '/' not in x['Test']]
            assert sorted(passed)==shard['tests'],'current required test inventory differs'
            checks.update({n:shard['name'] for n in passed})
        assert sorted(checks)==plan['required_tests']
        run('owner-oracle',['node','scripts/check-v12-thread-owner-read-contract.mjs','--go-output',work/'owner-policy.jsonl','--report',work/'owner-policy.json'])
        for db in databases:
            assert dump(db,'after')==schemas[db],db+' whole Schema changed'
        run('database-after',['bash','scripts/database-check.sh','all'],pg)
        assert module.sources()==frozen and all(sha(ROOT/n)==h for n,h in (binary|frontend).items())
        for name in ['browser','integration-browser']:
            proof=json.loads((work/name/'browser-result.json').read_text())
            assert proof['status']=='passed'
    finally:
        if attempted:
            try:
                current=subprocess.run(['docker','inspect',container,'--format={{json .}}'],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
                if current.returncode==0:
                    obj=json.loads(current.stdout);assert obj['Config']['Labels'].get(LABEL)==nonce and (not cid or obj['Id']==cid)
                    run('pg-stop',['docker','rm','-f','-v',container])
                run('daemon-after',['docker','info','--format={{.ServerVersion}}'])
                missing=subprocess.run(['docker','inspect',container],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
                cleanup['owned_container_removed']=missing.returncode==1 and b'no such object' in missing.stderr.lower()
                with socket.socket() as s:s.settimeout(.5);cleanup['owned_port_closed']=port is None or s.connect_ex(('127.0.0.1',port))!=0
            except Exception as error:cleanup['errors'].append(str(error))
        (work/'commands.json').write_text(json.dumps(commands,indent=2)+'\n')
        for p in work.rglob('*'):
            if p.is_file() and p.suffix in ('.json','.jsonl','.log','.txt'):p.write_bytes(p.read_bytes().replace(password.encode(),b'[redacted]'))
        cleanup['database_password_persisted']=any(password.encode() in p.read_bytes() for p in work.rglob('*') if p.is_file())
        (work/'cleanup.json').write_text(json.dumps(cleanup,indent=2)+'\n')
        assert not cleanup['errors'] and not cleanup['database_password_persisted']
        if attempted:assert cleanup['owned_container_removed'] and cleanup['owned_port_closed']
    report={'schema':'campusos.final-identity-matrix/v1','status':'passed','implementation':'passed','automation':'passed','target_environment':'Linux_PG16_normal_cmd_server_actual_Admin_User_browsers',
        'checks':checks,'required_count':len(checks),'source_sha256':frozen,'binary_sha256':binary,'frontend_sha256':frontend,'whole_schema_sha256':{db:hashlib.sha256(s.encode()).hexdigest() for db,s in schemas.items()},
        'databases':databases,'public_tables':132,'foreign_keys':143,'migration':35,'commands':commands,'cleanup':cleanup,'windows':'deferred_not_run','commit_or_push':False}
    (work/'target.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps({'status':'passed','checks':len(checks),'work':str(work.relative_to(ROOT))}))

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--work',type=Path,required=True);main(parser.parse_args().work.resolve())
