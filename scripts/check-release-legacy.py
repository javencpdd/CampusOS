#!/usr/bin/env python3
"""Run all retained identity/business gates on a fresh owned local PG16.
The historical entry points reset only their frozen1-3 baseline database inside
this invocation's container. Never inherit an operator DB, DSN or Docker target.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
OWNED = ROOT / '.cache/release-legacy'
GATES = ['identity-email-check', 'identity-challenge-check', 'identity-registration-check', 'identity-session-check',
         'identity-recovery-check', 'email-delivery-check', 'category-hierarchy-check', 'structured-thread-check', 'mutual-aid-check', 'secondhand-check']
LABEL = 'campusos.release-legacy.owner'

def main(work):
    assert os.uname().sysname == 'Linux'
    if work is None:
        OWNED.mkdir(parents=True,exist_ok=True)
        work=Path(tempfile.mkdtemp(prefix='acceptance-',dir=OWNED))
    else:
        work=work.resolve()
        assert work.is_relative_to(OWNED) and work!=OWNED
        work.mkdir(parents=True,exist_ok=False)
    work.chmod(0o700)
    (work/'docker-config').mkdir(mode=0o700)
    spec=importlib.util.spec_from_file_location('restore',ROOT/'scripts/check-release-restore.py')
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    frozen=module.sources()
    (work/'source-freeze.json').write_text(json.dumps(frozen,ensure_ascii=False,indent=2)+'\n')
    nonce=secrets.token_hex(12);name='campusos-release-legacy-'+nonce;password=secrets.token_hex(24)
    env={k:os.environ[k] for k in ('HOME','PATH') if k in os.environ}
    env.update(GOCACHE='/tmp/campusos-go-cache',GOFLAGS='-buildvcs=false',DOCKER_HOST='unix:///var/run/docker.sock',DOCKER_CONFIG=str(work/'docker-config'),
               CAMPUSOS_SKIP_DOTENV='true',PSQL_MODE='docker',POSTGRES_CONTAINER=name,DB_USER='campusos',DB_PASSWORD=password,DB_HOST='127.0.0.1',DB_PORT='5432')
    commands=[];cleanup={'errors':[]};attempted=False;cid=None;port=None
    def run(label,args):
        began=time.monotonic()
        result=subprocess.run(args,cwd=ROOT,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        (work/(label+'.log')).write_bytes(result.stdout.replace(password.encode(),b'[redacted]'))
        commands.append({'name':label,'args':args,'exit_code':result.returncode,'seconds':round(time.monotonic()-began,3)})
        assert result.returncode==0,label+' failed; inspect owned log'
        return result.stdout.decode()
    try:
        image=run('image',['docker','image','inspect','postgres:16-alpine','--format={{.Id}}']).strip()
        attempted=True
        # Pass the password by environment, never via retained command arguments.
        env['POSTGRES_PASSWORD']=password
        cid=run('pg-start',['docker','run','--rm','--pull=never','-d','--name',name,'--label',LABEL+'='+nonce,'--tmpfs','/var/lib/postgresql/data:rw,size=1g','--cpus=2','--memory=1g','--memory-swap=1g','--pids-limit=256','-e','POSTGRES_USER=campusos','-e','POSTGRES_DB=postgres','-e','POSTGRES_PASSWORD','-p','127.0.0.1::5432',image]).strip()
        for _ in range(120):
            if subprocess.run(['docker','exec',name,'pg_isready','-h','127.0.0.1','-U','campusos'],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
            time.sleep(.25)
        else:raise AssertionError('owned PG TCP readiness deadline')
        scope=json.loads(run('scope',['docker','inspect',name,'--format={{json .}}']))
        assert scope['Id']==cid and scope['Config']['Labels'].get(LABEL)==nonce
        assert scope['HostConfig']['PortBindings']['5432/tcp'][0]['HostIp']=='127.0.0.1'
        assert all(m['Type']=='tmpfs' for m in scope['Mounts'])
        port=int(run('pg-port',['docker','port',name,'5432/tcp']).strip().rsplit(':',1)[1])
        assert run('version',['docker','exec',name,'psql','-U','campusos','-d','postgres','-Atc','SHOW server_version_num;']).startswith('16')
        for gate in GATES:run(gate,['make',gate])
        assert module.sources()==frozen,'source changed during retained gates'
    finally:
        if attempted:
            try:
                current=subprocess.run(['docker','inspect',name,'--format={{json .}}'],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
                if current.returncode==0:
                    obj=json.loads(current.stdout);assert obj['Config']['Labels'].get(LABEL)==nonce and (not cid or obj['Id']==cid)
                    run('pg-log',['docker','logs',name]);run('pg-remove',['docker','rm','-f','-v',name])
                run('daemon-after',['docker','info','--format={{.ServerVersion}}'])
                missing=subprocess.run(['docker','inspect',name],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
                cleanup['owned_container_removed']=missing.returncode==1 and b'no such object' in missing.stderr.lower()
                with socket.socket() as s:s.settimeout(.5);cleanup['owned_port_closed']=port is None or s.connect_ex(('127.0.0.1',port))!=0
            except Exception as error:cleanup['errors'].append(str(error))
        (work/'commands.json').write_text(json.dumps(commands,indent=2)+'\n')
        cleanup['credentials_persisted']=any(password.encode() in p.read_bytes() for p in work.rglob('*') if p.is_file())
        (work/'cleanup.json').write_text(json.dumps(cleanup,indent=2)+'\n')
        assert not cleanup['errors'] and not cleanup['credentials_persisted']
        if attempted:assert cleanup['owned_container_removed'] and cleanup['owned_port_closed']
    report={'schema':'campusos.release-legacy/v1','status':'passed','gates':GATES,'source_sha256':frozen,'commands':commands,'cleanup':cleanup,
            'frozen_baseline':'only000001-000003; full35 checked independently','inherited_DB_DSN_context':False,'shared_database_reset':False}
    (work/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps({'status':'passed','gates':len(GATES),'report':str(work/'report.json')}))

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--work',type=Path);main(parser.parse_args().work)
