#!/usr/bin/env python3
"""Linux08b: real offline recovery, browser sessions and frozen capacity.

Only owns a fresh .cache directory, randomly named containers/image/network.
Uses cached images; never resets a shared DB or starts a development stack.
"""
import argparse
import base64
from concurrent.futures import ThreadPoolExecutor
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import subprocess
import time
import urllib.error
import urllib.request

REPO=Path(__file__).resolve().parent.parent
PROFILE=REPO/'docs/项目计划书v1/项目计划v1.2/evidence/v12-08b-capacity-profile-linux.json'
PRIOR=REPO/'docs/项目计划书v1/项目计划v1.2/evidence/v12-08b-backup-offline-linux.json'
PRIOR_SHA='56d74cd772e2ee0f10fd98d185e5a00a14e65d118e35e0925cc228bc254560b2'
SEED='campusos_v12_08b_backup_seed'
TARGET='campusos_v12_08b_backup_restored'


def sha(p):
    with Path(p).open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()


def source_map():
    spec=importlib.util.spec_from_file_location('backup_drill',REPO/'scripts/v12-08b-backup-drill.py')
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    return module.source_snapshot()


def free_port():
    with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]


def actual_elf_pid(parent, expected):
    pending=[parent];found=[]
    while pending:
        pid=pending.pop()
        try:
            if Path(os.readlink(f'/proc/{pid}/exe')).name=='campusos-server':
                assert sha(f'/proc/{pid}/exe')==expected
                found.append(pid)
            pending.extend(map(int,Path(f'/proc/{pid}/task/{pid}/children').read_text().split()))
        except (FileNotFoundError,ProcessLookupError):pass
    assert len(found)==1,'must measure the actual production Server ELF'
    return found[0]


def main(work,reuse_build=None):
    assert os.uname().sysname=='Linux'
    assert sha(PRIOR)==PRIOR_SHA
    profile=json.loads(PROFILE.read_text());profile_sha=sha(PROFILE)
    work.mkdir(parents=True,exist_ok=False);work.chmod(0o700)
    native=work/'native';native.mkdir();source=work/'source';source.mkdir(mode=0o700)
    restored=work/'restored';restored.mkdir(mode=0o700)
    suffix=secrets.token_hex(6);pg='campusos-v12-08b-pg-'+suffix;network='campusos-v12-08b-net-'+suffix
    image='campusos-v12-08b-api:'+suffix;api_container='campusos-v12-08b-api-'+suffix
    password,key,login_password,jwt_key,mfa_key=[secrets.token_hex(32) for _ in range(5)]
    machine='cint1_0123456789abcdef.'+base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip('=')
    env={k:os.environ[k] for k in ('HOME','PATH') if k in os.environ}
    env.update(GOCACHE=str(REPO/'.cache/v12-g0/go-cache'),GOFLAGS='-buildvcs=false',CAMPUSOS_SKIP_DOTENV='true',DOCKER_HOST='unix:///var/run/docker.sock')
    records=[];cleanup=[];processes=[];private_inputs=[]

    def run(name,args,*,extra=None,cwd=REPO,stdin=None,required=True,timeout=600):
        current=env|dict(extra or {});started=time.monotonic()
        with (work/(name+'.log')).open('wb') as log:
            result=subprocess.run(args,env=current,cwd=cwd,input=stdin,stdout=log,stderr=subprocess.STDOUT,timeout=timeout)
        records.append({'name':name,'exit_code':result.returncode,'seconds':round(time.monotonic()-started,3)})
        assert not required or result.returncode==0,name+' failed; inspect private log'
        return (work/(name+'.log')).read_text()

    def sql(name,db,statement):
        return run(name,['docker','exec','-i',pg,'psql','-U','campusos','-d',db,'-XAtv','ON_ERROR_STOP=1'],stdin=statement.encode())

    def migrate(name,db,action):
        return run(name,['bash','scripts/migrate.sh',action],extra={'PSQL_MODE':'docker','POSTGRES_CONTAINER':pg,'DB_NAME':db,'DB_USER':'campusos','DB_PASSWORD':password})

    def dsn(db,host='127.0.0.1',port=None):return f'postgres://campusos:{password}@{host}:{port or pg_port}/{db}?sslmode=disable'

    def http(base,path,*,method='GET',body=None,token=None,scheme='Bearer'):
        headers={}
        if token:headers['Authorization']=scheme+' '+token
        if body is not None:headers['Content-Type']='application/json'
        req=urllib.request.Request(base+'/api/v1'+path,data=json.dumps(body).encode() if body is not None else None,headers=headers,method=method)
        try:
            with urllib.request.urlopen(req,timeout=20) as response:return response.status,response.read(),response.headers
        except urllib.error.HTTPError as e:return e.code,e.read(),e.headers

    def data(base,path,**kwargs):
        status,raw,_=http(base,path,**kwargs);assert status in (200,201),(path,status)
        payload=json.loads(raw);assert payload['code']==0,path
        return payload['data']

    def login(base,email,admin=False):
        return data(base,'/admin/identity/login' if admin else '/auth/login',method='POST',body={'username':email,'password':login_password} if admin else {'email':email,'password':login_password})['access_token']

    def wait_health(base,process=None):
        for _ in range(160):
            if process is not None:assert process.poll() is None,'actual API exited before health'
            try:
                if http(base,'/health')[0]==200:return
            except (urllib.error.URLError,TimeoutError,OSError):pass
            time.sleep(.25)
        raise AssertionError('actual API health deadline')

    def api_env(db,root,port,host='127.0.0.1',docker=False):
        return {'DATABASE_DSN':dsn(db,host,5432 if docker else pg_port),'SERVER_HOST':'0.0.0.0' if docker else '127.0.0.1','SERVER_PORT':str(port),
                'CAMPUSOS_ENV':'test','REDIS_ENABLED':'false','NATS_URL':'nats://127.0.0.1:1','HOST_API_ENABLED':'false','AUTH_PASSWORD_HASH_ENABLED':'true','AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN':'false','AUTH_BOOTSTRAP_ADMIN_SECRET':login_password,
                'AUTH_REFRESH_BODY_COMPAT':'false','JWT_SECRET':jwt_key,'AUTH_MFA_ACTIVE_KEY_ID':'owned08b','AUTH_MFA_ENCRYPTION_KEYS':'owned08b:'+mfa_key,'EMAIL_PROVIDER':'fake','AI_ENABLED':'true',
                'AI_API_KEY':'owned-disabled-outbound-canary','AI_BASE_URL':'https://example.invalid/never','OBSERVABILITY_PROMETHEUS_ENABLED':'false','GIN_MODE':'release','GOMAXPROCS':str(profile['api_gomaxprocs']),
                'PLUGIN_DATA_DIR':str(root/'data/plugin_data'),'CAMPUSOS_PLUGIN_V5_TRUST_STORE':str(root/'data/config/v12-backup-public-trust.json'),
                'CAMPUSOS_RUN_MIGRATIONS':'false','CAMPUSOS_SKIP_DOTENV':'true'}

    def start_native(name,db,root,port):
        settings=env|api_env(db,root,port)
        log=(work/(name+'.log')).open('wb')
        cmd=['bwrap','--die-with-parent','--ro-bind','/','/','--dev','/dev','--proc','/proc','--tmpfs','/tmp','--bind',str(root),str(root),'--ro-bind',str(native),str(native),'--chdir',str(root),'--',str(native/'campusos-server')]
        p=subprocess.Popen(cmd,cwd=root,env=settings,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
        processes.append((p,log));return p

    def stop_native(p):
        if p.poll() is None:
            os.killpg(p.pid,signal.SIGTERM)
            try:p.wait(timeout=15)
            except subprocess.TimeoutExpired:os.killpg(p.pid,signal.SIGKILL);p.wait(timeout=5)

    def browser(name,base,before=False):
        out=work/name;out.mkdir();cfg={'backend':base,'backendKind':name,'password':login_password,'alice':'v5-chat@example.test','bob':'v5-chat-2801002@example.test',
            'document':document,'documentText':document_text,'output':str(out),'before':before,'ready':str(out/'ready.json'),'resume':str(out/'resume.json')}
        path=out/'private-input.json';path.write_text(json.dumps(cfg));path.chmod(0o600);private_inputs.append(path)
        log=(out/'browser.log').open('wb');p=subprocess.Popen(['node','scripts/v12-08b-recovery-browser.mjs',str(path)],cwd=REPO,env=env,stdout=log,stderr=subprocess.STDOUT)
        processes.append((p,log));return p,out

    def wait_file(path,p):
        for _ in range(240):
            assert p.poll() is None,'real browser exited; inspect browser.log'
            if path.exists():return
            time.sleep(.25)
        raise AssertionError('real browser readiness deadline')

    def check_api(base,old_tokens):
        for domain,token in old_tokens.items():
            path='/admin/plugin-v5' if domain=='admin' else '/documents'
            assert http(base,path,token=token)[0]==401,'restored old access credential revived'
        assert http(base,'/integration/threads/8800001',token=machine,scheme='Integration')[0] in (401,403)
        a=login(base,'v5-chat@example.test');b=login(base,'v5-chat-2801002@example.test');m=login(base,'restore-operator',True)
        assert len(data(base,'/documents?status=active',token=a)['items'])==100
        assert len(data(base,'/documents?status=active',token=b)['items'])==100
        assert http(base,f'/documents/{document}/download',token=a)[1].decode()==document_text
        denied=http(base,f'/documents/{document}/download',token=b);assert denied[0] in (403,404) and document_text.encode() not in denied[1]
        assert http(base,'/threads/8810000')[0] in (403,404)
        inventory=data(base,'/admin/plugin-v5',token=m);assert 'backup-offline' in json.dumps(inventory)
        return a,b,m

    def capacity(name,base,tokens,pid):
        a,b,_=tokens
        endpoints=[('health','/health',None,200),('threads','/threads?page=1&page_size=20',None,200),('private_documents','/documents?status=active',a,200),
                   ('private_document_download',f'/documents/{document}/download',a,200),('cross_owner_denial',f'/documents/{document}/download',b,404),('plugin_inventory','/plugin-v5',a,200)]
        results=[];rss=[]
        for label,path,token,expected in endpoints:
            def sample(_):
                started=time.perf_counter();status,raw,_=http(base,path,token=token);valid=status==expected or label=='cross_owner_denial' and status==403
                if label=='private_document_download':valid=valid and raw.decode()==document_text
                elif label=='cross_owner_denial':valid=valid and document_text.encode() not in raw
                elif status==200:
                    payload=json.loads(raw);valid=valid and payload['code']==0
                    if label=='threads':valid=valid and len(payload['data']['items'])==20
                    if label=='private_documents':valid=valid and len(payload['data']['items'])==100 and all(x['owner_id']=='2801001' for x in payload['data']['items'])
                    if label=='plugin_inventory':valid=valid and b'backup-offline' in raw
                return {'status':status,'ms':round((time.perf_counter()-started)*1000,3),'valid':valid,'bytes':len(raw)}
            with ThreadPoolExecutor(max_workers=profile['concurrency']) as executor:
                assert all(x['valid'] for x in executor.map(sample,range(8)))
                samples=list(executor.map(sample,range(profile['samples_per_endpoint'])))
            assert all(x['valid'] for x in samples),label+' incorrect response'
            p95=sorted(x['ms'] for x in samples)[math.ceil(.95*len(samples))-1]
            assert p95<=profile['max_p95_ms'],label+' exceeded frozen latency'
            memory={k.rstrip(':'):int(v)*1024 for line in Path(f'/proc/{pid}/status').read_text().splitlines() if line.startswith(('VmRSS:','VmHWM:')) for k,v,_ in [line.split()]}
            rss.append(memory);assert memory['VmHWM']<=profile['max_api_rss_bytes']
            results.append({'name':label,'p95_ms':p95,'samples':samples})
        report={'status':'passed','profile_sha256':profile_sha,'actual_cmd_server':True,'backend':name,'endpoints':results,'memory_samples':rss,'measured_requests':600,'concurrency':8}
        (work/(name+'-capacity.json')).write_text(json.dumps(report,indent=2)+'\n');return report

    try:
        build_source=source_map();(work/'build-source-freeze.json').write_text(json.dumps(build_source,ensure_ascii=False,indent=2)+'\n')
        shutil.copytree(REPO/'migrations',source/'migrations')
        (source/'.env').write_text('CAMPUSOS_SECRET_ENCRYPTION_KEYS=owned:'+key+'\nJWT_SECRET='+jwt_key+'\n');(source/'.env').chmod(0o600);private_inputs.append(source/'.env')
        if reuse_build:
            prior=json.loads((reuse_build/'build-source-freeze.json').read_text())
            compiled=lambda name:name in ('go.mod','go.sum') or name.startswith(('internal/','pkg/','cmd/','modules/','sdk/','admin/','web/','plugins/','migrations/','deploy/'))
            old_inputs={n:v for n,v in prior.items() if compiled(n)}
            new_inputs={n:v for n,v in build_source.items() if compiled(n)}
            assert old_inputs==new_inputs,'compiled inputs changed; rebuild is required'
            binaries={}
            for name in ['campusos-server','campusosctl','plugin.test','server.test','recovery.test']:
                old=reuse_build/'native'/name;shutil.copy2(old,native/name);assert sha(old)==sha(native/name);binaries[name]=sha(native/name)
            for name in ['web-build','admin-build']:assert (reuse_build/(name+'.log')).is_file()
            (work/'build-reuse.json').write_text(json.dumps({'origin':str(reuse_build.relative_to(REPO)),'matched_build_inputs':new_inputs,'binary_sha256':binaries,'target_results_reused':False,'reason':'only browser/runner acceptance scripts changed; compiled inputs match'},ensure_ascii=False,indent=2)+'\n')
        else:
            run('build-server',['go','build','-o',str(native/'campusos-server'),'./cmd/server'],extra={'CGO_ENABLED':'0'})
            run('build-cli',['go','build','-o',str(native/'campusosctl'),'./cmd/campusosctl'],extra={'CGO_ENABLED':'0'})
            for package,name in [('./internal/plugin','plugin.test'),('./internal/server','server.test'),('./internal/operations/recovery','recovery.test')]:run('build-'+name,['go','test','-race','-c','-o',str(native/name),package])
        env['CAMPUSOSCTL']=str(native/'campusosctl')
        if not reuse_build:run('web-build',['pnpm','--dir','web','build']);run('admin-build',['pnpm','--dir','admin','build'])
        # Use the deployment edge bridge pattern: this daemon suppresses port
        # publication on internal-only networks. All published ports remain
        # explicitly loopback-only; no shared network/container is touched.
        run('network-create',['docker','network','create',network]);cleanup.append(['docker','network','rm',network])
        run('pg-create',['docker','run','--rm','--pull=never','-d','--name',pg,'--network',network,'--tmpfs','/var/lib/postgresql/data:rw,size=1g','-e','POSTGRES_USER=campusos','-e','POSTGRES_DB='+SEED,'-e','POSTGRES_PASSWORD','-p','127.0.0.1::5432','postgres:16-alpine'],extra={'POSTGRES_PASSWORD':password});cleanup.insert(0,['docker','stop',pg])
        for _ in range(120):
            if subprocess.run(['docker','exec',pg,'pg_isready','-h','127.0.0.1','-U','campusos'],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
            time.sleep(.25)
        else:raise AssertionError('owned PG readiness')
        pg_port=int(run('pg-port',['docker','port',pg,'5432/tcp']).strip().rsplit(':',1)[1])
        migrate('migration-up',SEED,'up');migrate('migration-check',SEED,'check')
        seed_env={'CAMPUSOS_V12_BACKUP_DSN':dsn(SEED),'CAMPUSOS_V12_BACKUP_ROOT':str(source),'CAMPUSOS_V12_BACKUP_PHASE':'seed','CAMPUSOS_V12_BACKUP_KEY':key}
        raw=run('offline-seed',[str(native/'plugin.test'),'-test.v','-test.run=^TestV12BackupRestoreOffline$'],extra=seed_env,cwd=REPO/'internal/plugin');assert '--- PASS: TestV12BackupRestoreOffline ' in raw and '--- SKIP' not in raw
        run('capacity-seed',[str(native/'server.test'),'-test.v','-test.run=^TestV12RecoverySeedAccountsAndCapacity$'],extra=seed_env|{'CAMPUSOS_RECOVERY_PASSWORD':login_password,'CAMPUSOS_RECOVERY_MACHINE_KEY':machine},cwd=REPO/'internal/server')
        native_port=free_port();base=f'http://127.0.0.1:{native_port}';p=start_native('source-api',SEED,source,native_port);wait_health(base,p)
        alice=login(base,'v5-chat@example.test');bob=login(base,'v5-chat-2801002@example.test');old={'user':alice,'admin':login(base,'restore-operator',True)}
        assert http(base,'/integration/threads/8800001',token=machine,scheme='Integration')[0]==200,'seed machine credential must really work'
        document_text='OWNED08B_PRIVATE_DOCUMENT\n'+'x'*4070
        document=None
        for owner,token in [('alice',alice),('bob',bob)]:
            for n in range(100):
                doc=data(base,'/documents',method='POST',body={'name':f'restore-{owner}-{n}.txt','format':'text','content':document_text},token=token)
                if document is None:document=doc['id']
        browser_process,browser_dir=browser('native-browser',base,True);wait_file(browser_dir/'ready.json',browser_process)
        stop_native(p)
        schema_before=run('schema-before',['docker','exec',pg,'pg_dump','-U','campusos','-d',SEED,'--schema-only','--no-owner','--no-privileges'])
        def normalized_schema(raw):return '\n'.join(line for line in raw.splitlines() if not line.startswith(('\\restrict ','\\unrestrict ')))
        # PostgreSQL can deparse a varchar-array cast differently after a
        # dump/restore. Establish the expected full DDL by independently
        # restoring the source's schema-only dump, retaining every constraint.
        reference_db='campusos_v12_08b_schema_reference'
        sql('schema-reference-create','postgres','CREATE DATABASE '+reference_db+';')
        sql('schema-reference-restore',reference_db,schema_before)
        schema_reference=run('schema-reference',['docker','exec',pg,'pg_dump','-U','campusos','-d',reference_db,'--schema-only','--no-owner','--no-privileges'])
        # All application writers stopped before the consistent SQL/files set.
        archive=Path(run('backup',['bash',str(REPO/'scripts/backup.sh'),str(work/'archives')],cwd=source,extra={'POSTGRES_CONTAINER':pg,'DB_NAME':SEED,'DB_USER':'campusos','DB_PASSWORD':password}).strip())
        assert archive.stat().st_mode&0o777==0o600
        run('verify',['bash','scripts/restore.sh','verify',str(archive)])
        run('extract',['bash','scripts/restore.sh','extract',str(archive),str(work/'extracted')])
        before={str(f.relative_to(source)):sha(f) for f in source.rglob('*') if f.is_file() and f.name!='.env'}
        actual={str(f.relative_to(work/'extracted/files')):sha(f) for f in (work/'extracted/files').rglob('*') if f.is_file()}
        assert actual==before and not (work/'extracted/files/.env').exists()
        for f in (work/'extracted').rglob('*'):
            if f.is_file() and f.name!='files.tar.gz':assert key.encode() not in f.read_bytes() and jwt_key.encode() not in f.read_bytes()
        sql('post-backup-revoke',SEED,"UPDATE users SET auth_version=auth_version+1; UPDATE integration_credentials SET status='revoked';")
        sql('target-create','postgres','CREATE DATABASE '+TARGET+';');migrate('target-migration',TARGET,'up')
        # Native psql transport adapter forwards to the same owned actual PG;
        # the application/maintenance code remains the normal production ELF.
        tools=work/'tools';tools.mkdir();wrapper=tools/'psql'
        wrapper.write_text('#!/usr/bin/env bash\nset -euo pipefail\nfile=""; args=(); while (( $# )); do if [[ "$1" == -f ]]; then file="$2"; shift 2; else args+=("$1"); shift; fi; done\nexec docker exec -i -e PGPASSWORD "$CAMPUSOS_OWNED_PG" psql -U "$PGUSER" -d "$PGDATABASE" "${args[@]}" <"$file"\n');wrapper.chmod(0o700)
        cli_env={'DATABASE_DSN':dsn(TARGET),'PATH':str(tools)+':'+env['PATH'],'CAMPUSOS_OWNED_PG':pg,'DB_NAME':TARGET,'DB_USER':'campusos','DB_PASSWORD':password,'DB_HOST':'127.0.0.1','DB_PORT':str(pg_port),'CAMPUSOS_RECOVERY_DATA_ROOT':str(restored/'data'),'MIGRATIONS_DIR':str(REPO/'migrations')}
        frozen=source_map();(work/'source-freeze.json').write_text(json.dumps(frozen,ensure_ascii=False,indent=2)+'\n')
        assert frozen==build_source,'inputs changed after binary/frontend build began; use a fresh target'
        production={str(f.relative_to(REPO)):sha(f) for root in [native,REPO/'web/dist',REPO/'admin/dist'] for f in root.rglob('*') if f.is_file()}
        started=time.monotonic()
        apply=json.loads(run('native-apply',['bash',str(REPO/'scripts/restore.sh'),'apply',str(archive),'--confirm'],extra=cli_env))

        def restored_facts(name,data_root):
            raw=run(name,[str(native/'plugin.test'),'-test.v','-test.run=^TestV12RecoveryRestoredSecretsPackagesAndObjects$'],cwd=REPO/'internal/plugin',extra={'CAMPUSOS_V12_BACKUP_DSN':dsn(TARGET),'CAMPUSOS_RECOVERY_DATA_ROOT':str(data_root),'CAMPUSOS_V12_BACKUP_KEY':key})
            assert '--- PASS: TestV12RecoveryRestoredSecretsPackagesAndObjects ' in raw and '--- SKIP' not in raw and 'DATA RACE' not in raw
            reconcile=json.loads(run(name+'-object-reconcile',[str(native/'campusosctl'),'storage','reconcile','--root',str(data_root/'personal-space'),'--dry-run'],extra=cli_env))
            assert reconcile['dry_run'] and not reconcile.get('differences_truncated',False)
            assert reconcile['report']['differences'] in (None,[]) and all(v==0 for v in reconcile['report']['counts'].values())

        restored_facts('native-restored-facts',restored/'data')
        sql('native-authority-review-change',TARGET,'DELETE FROM user_roles WHERE user_id=1000000000000000001 AND role_id=1;')
        blocked=start_native('quarantined-api',TARGET,restored,native_port);assert blocked.wait(timeout=15)!=0
        assert 'recovery.quarantined' in (work/'quarantined-api.log').read_text()
        snapshot=json.loads(run('native-inventory',[str(native/'campusosctl'),'recovery','status'],extra=cli_env))
        stale=run('stale-review',[str(native/'campusosctl'),'recovery','review','--data-root',str(restored/'data'),'--epoch',apply['epoch'],'--inventory-sha256','0'*64,'--operator','owned08b','--reason','test stale inventory','--confirm-offline'],extra=cli_env,required=False)
        assert records[-1]['exit_code']!=0
        run('native-review',[str(native/'campusosctl'),'recovery','review','--data-root',str(restored/'data'),'--epoch',apply['epoch'],'--inventory-sha256',snapshot['inventory_sha256'],'--operator','owned08b','--reason','reviewed exact owned authorization inventory; no plugin or egress reapproval','--confirm-offline'],extra=cli_env)
        restored_p=start_native('restored-native-api',TARGET,restored,native_port);wait_health(base,restored_p)
        native_recovery_seconds=time.monotonic()-started;assert native_recovery_seconds<=profile['max_restore_to_reviewed_health_seconds']
        (browser_dir/'resume.json').write_text('{}')
        assert browser_process.wait(timeout=180)==0,'native recovery browser failed'
        tokens=check_api(base,old)
        schema_native=run('schema-native',['docker','exec',pg,'pg_dump','-U','campusos','-d',TARGET,'--schema-only','--no-owner','--no-privileges'])
        assert normalized_schema(schema_reference)==normalized_schema(schema_native),'native restored schema differs from independent full-DDL roundtrip reference'
        # Identify the actual ELF child below bwrap, not its supervisor.
        native_pid=actual_elf_pid(restored_p.pid,sha(native/'campusos-server'))
        with open(f'/proc/{native_pid}/root{REPO}/migrations/000029_instance_recovery_epochs.up.sql','rb') as f:assert hashlib.sha256(f.read()).hexdigest()==sha(REPO/'migrations/000029_instance_recovery_epochs.up.sql')
        try:os.open(f'/proc/{native_pid}/root{native}/campusosctl',os.O_WRONLY)
        except OSError as error:assert error.errno==30
        else:raise AssertionError('native host was writable')
        native_capacity=capacity('native',base,tokens,native_pid);stop_native(restored_p)
        migrate('restored-check',TARGET,'check')
        # Build an offline runtime image from the cached deployment runtime;
        # replace all application ELFs/scripts/migrations before target use.
        image_root=work/'image';image_root.mkdir();shutil.copytree(REPO/'migrations',image_root/'migrations')
        for name in ['campusos-server','campusosctl']:shutil.copy2(native/name,image_root/name)
        shutil.copytree(REPO/'deploy/docker',image_root/'deployment',ignore=shutil.ignore_patterns('.env*'))
        (image_root/'scripts').mkdir()
        for name in ['restore.sh','backup.sh','migrate.sh']:shutil.copy2(REPO/'scripts'/name,image_root/'scripts'/name)
        runtime_id=run('runtime-base',['docker','image','inspect','campusos-api:0.13.0','--format','{{.Id}}']).strip()
        (image_root/'Dockerfile').write_text(f'FROM campusos-api:0.13.0\nUSER root\nRUN rm -rf /app/migrations /app/seed-data\nCOPY migrations /app/migrations\nCOPY campusos-server campusosctl /app/bin/\nCOPY scripts /app/scripts\nCOPY deployment/entrypoint.sh /app/entrypoint.sh\nCOPY deployment/restore-container.sh /app/scripts/docker-restore.sh\nRUN chmod 0755 /app/bin/* /app/scripts/*.sh /app/entrypoint.sh\nUSER {os.getuid()}:{os.getgid()}\n')
        run('docker-image-build',['docker','build','--pull=false','--network=none','-t',image,str(image_root)]);cleanup.insert(0,['docker','image','rm',image])
        assert run('runtime-base-after',['docker','image','inspect','campusos-api:0.13.0','--format','{{.Id}}']).strip()==runtime_id
        image_id=run('docker-image-before',['docker','image','inspect',image,'--format','{{.Id}}']).strip()
        docker_root=work/'docker-root';docker_root.mkdir(mode=0o700)
        # Restore the same old archive a second time on the same target DB;
        # this must generate a different random instance authentication epoch.
        docker_env=api_env(TARGET,Path('/app'),8080,pg,True)|{'DB_NAME':TARGET,'DB_HOST':pg,'DB_USER':'campusos','DB_PASSWORD':password}
        docker_flags=['--rm','--pull=never','--network',network,'--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--tmpfs','/tmp:rw,noexec,nosuid,nodev,size=64m','--mount',f'type=bind,src={docker_root},dst=/app/data','--mount',f'type=bind,src={work/"archives"},dst=/backups,readonly']
        for name in docker_env:docker_flags+=['-e',name]
        started=time.monotonic()
        second=json.loads(run('docker-apply',['docker','run',*docker_flags,'--entrypoint','/app/scripts/docker-restore.sh',image,'/backups/'+archive.name,'--confirm'],extra=docker_env))
        assert second['epoch']!=apply['epoch']
        restored_facts('docker-restored-facts',docker_root)
        sql('docker-authority-review-change',TARGET,'DELETE FROM user_roles WHERE user_id=1000000000000000001 AND role_id=1;')
        current=json.loads(run('docker-inventory',['docker','run',*docker_flags,'--entrypoint','/app/bin/campusosctl',image,'recovery','status'],extra=docker_env))
        run('docker-review',['docker','run',*docker_flags,'--entrypoint','/app/bin/campusosctl',image,'recovery','review','--data-root','/app/data','--epoch',second['epoch'],'--inventory-sha256',current['inventory_sha256'],'--operator','owned08b-docker','--reason','reviewed current owned authority inventory; keep plugin and outbound quarantined','--confirm-offline'],extra=docker_env)
        run('docker-api-start',['docker','run','-d','--name',api_container,*docker_flags,'--cpus=2','--memory=768m','--memory-swap=768m','--pids-limit=256','-p','127.0.0.1::8080',image],extra=docker_env);cleanup.insert(0,['docker','stop',api_container])
        docker_port=run('docker-api-port',['docker','port',api_container,'8080/tcp']).strip().rsplit(':',1)[1];docker_base='http://127.0.0.1:'+docker_port
        wait_health(docker_base);docker_recovery_seconds=time.monotonic()-started;assert docker_recovery_seconds<=profile['max_restore_to_reviewed_health_seconds']
        docker_tokens=check_api(docker_base,old)
        for token,path in [(tokens[0],'/documents'),(tokens[2],'/admin/plugin-v5')]:assert http(docker_base,path,token=token)[0]==401,'first restored instance token crossed second recovery epoch'
        docker_browser,docker_browser_dir=browser('docker-browser',docker_base);assert docker_browser.wait(timeout=180)==0,'Docker recovery browser failed'
        docker_init_pid=int(run('docker-api-pid',['docker','inspect',api_container,'--format','{{.State.Pid}}']).strip())
        docker_pid=actual_elf_pid(docker_init_pid,sha(native/'campusos-server'))
        docker_capacity=capacity('docker',docker_base,docker_tokens,docker_pid)
        isolation=json.loads(run('docker-api-isolation',['docker','inspect',api_container,'--format','{"readonly":{{json .HostConfig.ReadonlyRootfs}},"user":{{json .Config.User}},"cap_drop":{{json .HostConfig.CapDrop}},"security":{{json .HostConfig.SecurityOpt}},"memory":{{.HostConfig.Memory}},"swap":{{.HostConfig.MemorySwap}},"pids":{{.HostConfig.PidsLimit}},"mounts":{{json .Mounts}}}']))
        assert isolation['readonly'] and not isolation['user'].startswith('0:') and isolation['memory']==805306368 and isolation['swap']==805306368
        assert all(x['Destination'] in ['/app/data','/backups','/tmp'] for x in isolation['mounts'])
        run('docker-api-log',['docker','logs',api_container]);run('docker-api-stop',['docker','stop',api_container]);cleanup.pop(0)
        schema_docker=run('schema-docker',['docker','exec',pg,'pg_dump','-U','campusos','-d',TARGET,'--schema-only','--no-owner','--no-privileges'])
        assert normalized_schema(schema_reference)==normalized_schema(schema_docker),'Docker restored schema differs from independent full-DDL roundtrip reference'
        assert run('docker-image-after',['docker','image','inspect',image,'--format','{{.Id}}']).strip()==image_id
        assert frozen==source_map() and profile_sha==sha(PROFILE)
        assert all(sha(REPO/name)==value for name,value in production.items())
        checks=json.loads(sql('final-state-check',TARGET,"SELECT json_build_object('users',(SELECT count(*) FROM users WHERE auth_version>1),'live_user_sessions',(SELECT count(*) FROM sessions WHERE revoked_at IS NULL),'machine_active',(SELECT count(*) FROM integration_credentials WHERE status='active'),'grants_approved',(SELECT count(*) FROM plugin_v5_admin_grants WHERE (state->>'approved')::boolean),'consents_accepted',(SELECT count(*) FROM plugin_v5_user_consents WHERE (state->>'accepted')::boolean),'installed_enabled',(SELECT count(*) FROM plugin_installations WHERE state->>'desired'='enabled'),'webhook_enabled',(SELECT count(*) FROM webhook_endpoints WHERE enabled),'documents',(SELECT count(*) FROM personal_documents),'threads',(SELECT count(*) FROM threads));"))
        assert checks['machine_active']==checks['grants_approved']==checks['consents_accepted']==checks['installed_enabled']==checks['webhook_enabled']==0
        assert checks['documents']==200 and checks['threads']==10000
        assert sql('reviewed-bootstrap-role-not-recreated',TARGET,'SELECT count(*) FROM user_roles WHERE user_id=1000000000000000001 AND role_id=1;').strip()=='0'
        target={'schema':'campusos.v12-08b-full-linux-target/v1','stage_status':'completed_linux_windows_deferred','status':'passed','source_sha256':frozen,'production_build_sha256':production,'profile_sha256':profile_sha,'dependency':{str(PRIOR.relative_to(REPO)):PRIOR_SHA},
                'native_recovery_seconds':native_recovery_seconds,'docker_recovery_seconds':docker_recovery_seconds,'archive_sha256':sha(archive),'restored_files_exact':len(actual),'instance_epochs_distinct':True,'first_restored_tokens_denied_second_epoch':True,'schema_source_raw_sha256':hashlib.sha256(normalized_schema(schema_before).encode()).hexdigest(),'schema_roundtrip_reference_native_docker_sha256':hashlib.sha256(normalized_schema(schema_reference).encode()).hexdigest(),'schema_comparison':'independent source schema-only restore; full DDL retained; only random pg_dump restrict tokens removed','old_access_and_machine_denied_both_targets':True,
                'native_browser':json.loads((browser_dir/'browser.json').read_text()),'docker_browser':json.loads((docker_browser_dir/'browser.json').read_text()),'native_capacity':native_capacity,'docker_capacity':docker_capacity,'docker_isolation':isolation,
                'actual_api_image':image_id,'runtime_base_image':runtime_id,'final_facts':checks,'commands':records,'windows':'deferred_by_user_not_run','shared_database_reset':False,'commit_or_push':False}
        (work/'target.json').write_text(json.dumps(target,ensure_ascii=False,indent=2)+'\n')
        print(json.dumps({'status':'passed','work':str(work.relative_to(REPO)),'restored_files':len(actual),'native_browser_checks':len(target['native_browser']['checks']),'docker_browser_checks':len(target['docker_browser']['checks']),'measured_capacity_requests':1200}))
    finally:
        for process,log in reversed(processes):
            if process.poll() is None:
                if process.args[0]=='bwrap':stop_native(process)
                else:process.terminate();process.wait(timeout=15)
            log.close()
        for path in private_inputs:path.unlink(missing_ok=True)
        for number,command in enumerate(cleanup):run('cleanup-'+str(number),command,required=False)
        (work/'cleanup.json').write_text(json.dumps({'private_inputs_removed':all(not p.exists() for p in private_inputs),'owned_cleanup_commands':cleanup,'command_results':records[-len(cleanup):] if cleanup else []},indent=2)+'\n')


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--work',required=True);parser.add_argument('--reuse-build');args=parser.parse_args();work=Path(args.work).resolve()
    assert work.is_relative_to(REPO/'.cache/v12-08b-exit') and work!=REPO/'.cache/v12-08b-exit'
    reuse=Path(args.reuse_build).resolve() if args.reuse_build else None
    if reuse:assert reuse.is_relative_to(REPO/'.cache/v12-08b-exit') and reuse!=work
    main(work,reuse)
