#!/usr/bin/env python3
"""Execute all frozen real-model FAQ cases against normal production composition; no protocol peer.
"""
import argparse,hashlib,json,os,secrets,socket,subprocess,time
from pathlib import Path
REPO=Path(__file__).resolve().parent.parent
PRIOR=REPO/'docs/项目计划书v1/项目计划v1.2/evidence/v12-14b-private-local-linux.json'
PRIOR_SHA='afaa050a472cdbb18c5683e43440820d65ae25f45f14f304857b7f6271bf2435'
DATABASE='campusos_v12_14b_quality'
def sha(p):
 with Path(p).open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def sources():
 names=set(subprocess.check_output(['git','ls-files','-co','--exclude-standard','-z'],cwd=REPO).decode().split('\0'))
 return {n:sha(REPO/n) for n in sorted(names) if n and (REPO/n).is_file() and not n.endswith('.md') and not n.startswith('docs/项目计划书v1/项目计划v1.2/evidence/')}
def norm(s):return '\n'.join(x for x in s.splitlines() if not x.startswith(('\\restrict ','\\unrestrict ')))
def main(work):
 assert os.uname().sysname=='Linux' and work.is_relative_to(REPO/'.cache/v12-14b-exit') and sha(PRIOR)==PRIOR_SHA
 work.mkdir(parents=True,exist_ok=False);work.chmod(0o700)
 for name in ['native','runtime','keys']:(work/name).mkdir(mode=0o700)
 frozen=sources();(work/'source-freeze.json').write_text(json.dumps(frozen,ensure_ascii=False,indent=2)+'\n')
 env={k:os.environ[k] for k in ['PATH','HOME'] if k in os.environ};env.update(GOCACHE=str(REPO/'.cache/v12-g0/go-cache'),GOMAXPROCS='2',GOTOOLCHAIN='auto',GOFLAGS='-buildvcs=false',CAMPUSOS_SKIP_DOTENV='true',CAMPUSOS_ENV='test',TMPDIR='/tmp')
 (work/'docker-config').mkdir(mode=0o700)
 env.update(DOCKER_HOST='unix:///var/run/docker.sock',DOCKER_CONFIG=str(work/'docker-config'))
 name='campusos-v14b-private-'+secrets.token_hex(6);password=secrets.token_hex(24);commands=[];started=False;port=None;report=None
 def run(label,args,extra={}):
  begin=time.monotonic()
  with (work/(label+'.log')).open('wb') as f:p=subprocess.run(args,cwd=REPO,env=env|extra,stdout=f,stderr=subprocess.STDOUT)
  commands.append(dict(name=label,args=args,exit_code=p.returncode,seconds=round(time.monotonic()-begin,3)));assert p.returncode==0,label+' failed; inspect owned log'
  return (work/(label+'.log')).read_text()
 def sql(db,statement):return run('sql-'+str(len(commands)),['docker','exec',name,'psql','-U','campusos','-d',db,'-XAt','-v','ON_ERROR_STOP=1','-c',statement])
 def mandatory(log,name):assert '--- PASS: '+name+' ' in log and not any(x in log for x in ['--- FAIL','--- SKIP','DATA RACE'])
 try:
  run('build-server',['go','test','-c','-o',str(work/'native/server.test'),'./internal/server'])
  run('build-ai',['go','test','-race','-c','-o',str(work/'native/ai.test'),'./internal/modules/features/ai'])
  run('build-knowledge',['go','test','-race','-c','-o',str(work/'native/knowledge.test'),'./internal/modules/features/knowledge'])
  run('build-production-server',['go','build','-o',str(work/'native/server'),'./cmd/server'])
  binaries={str(p.relative_to(REPO)):sha(p) for p in (work/'native').iterdir()}
  run('pg-start',['docker','run','--rm','--pull=never','-d','--name',name,'--tmpfs','/var/lib/postgresql/data:rw,size=768m','-e','POSTGRES_USER=campusos','-e','POSTGRES_DB='+DATABASE,'-e','POSTGRES_PASSWORD','-p','127.0.0.1::5432','postgres:16-alpine'],{'POSTGRES_PASSWORD':password});started=True
  for _ in range(120):
   if subprocess.run(['docker','exec',name,'pg_isready','-h','127.0.0.1','-U','campusos','-d',DATABASE],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
   time.sleep(.25)
  else:raise AssertionError('owned PG deadline')
  port=int(run('pg-port',['docker','port',name,'5432/tcp']).strip().rsplit(':',1)[1])
  pg={'PSQL_MODE':'docker','POSTGRES_CONTAINER':name,'DB_USER':'campusos','DB_NAME':DATABASE,'DB_PASSWORD':password,'DB_HOST':'127.0.0.1','DB_PORT':str(port)}
  run('migrate-up',['bash','scripts/migrate.sh','up'],pg);run('migrate-check',['bash','scripts/migrate.sh','check'],pg);run('migrate-status',['bash','scripts/migrate.sh','status'],pg);run('database-entry',['bash','scripts/database-check.sh','all'],pg)
  assert sql(DATABASE,"SELECT count(*) FROM pg_tables WHERE schemaname='public';SELECT max(version)::int FROM schema_migrations;SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='public'::regnamespace").splitlines()==['132','35','143']
  dump=['docker','exec',name,'pg_dump','-U','campusos','-d',DATABASE,'--schema-only','--no-owner','--no-privileges'];before=run('schema-before',dump)
  unitDB='campusos_v12_14a_private_rag_unit';sql('postgres','CREATE DATABASE '+unitDB+' TEMPLATE '+DATABASE)
  privateDB='campusos_v12_13_private_rag_unit';sql('postgres','CREATE DATABASE '+privateDB+' TEMPLATE '+DATABASE)
  dsn=lambda db:f'postgres://campusos:{password}@127.0.0.1:{port}/{db}?sslmode=disable'
  unit=run('native-ai-unit',[str(work/'native/ai.test'),'-test.v','-test.timeout=120s'],{'CAMPUSOS_AI_MANAGED_DSN':dsn(unitDB)});mandatory(unit,'TestManagedGatewayPostgres')
  unit=run('native-Knowledge-unit',[str(work/'native/knowledge.test'),'-test.v','-test.run=^Test(RAGApproval|PublicLocalRAG|PrivateIndexPostgres|PrivateLocalRAG)','-test.timeout=120s'],{'CAMPUSOS_PRIVATE_INDEX_DSN':dsn(privateDB)});mandatory(unit,'TestPrivateIndexPostgres')
  actual_env=pg|{'DATABASE_DSN':dsn(DATABASE),'CAMPUSOS_RAG_QUALITY_DSN':dsn(DATABASE),'CAMPUSOS_RAG_QUALITY_ROOT':str(work/'runtime'),'CAMPUSOS_RAG_QUALITY_MODEL_URL':'http://127.0.0.1:18942/v1','CAMPUSOS_RAG_QUALITY_GOLDEN':str(REPO/'docs/项目计划书v1/项目计划v1.2/evidence/v12-14b-chinese-rag-golden.json'),'KNOWLEDGE_PRIVATE_KEY_DIR':str(work/'keys'),'REDIS_ENABLED':'false','NATS_URL':'nats://127.0.0.1:1','HOST_API_ENABLED':'false','JWT_SECRET':secrets.token_hex(32),'AUTH_PASSWORD_HASH_ENABLED':'true','AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN':'false','AUTH_BOOTSTRAP_ADMIN_SECRET':secrets.token_hex(24),'EMAIL_PROVIDER':'fake','AI_ENABLED':'false','AUTH_MFA_ACTIVE_KEY_ID':'owned14btarget','AUTH_MFA_ENCRYPTION_KEYS':'owned14btarget:'+secrets.token_hex(32),'PLUGIN_DATA_DIR':str(work/'runtime/data/plugin_data'),'GIN_MODE':'release','OBSERVABILITY_PROMETHEUS_ENABLED':'false'}
  actual=run('native-private-local-RAG',[str(work/'native/server.test'),'-test.v','-test.run=^TestManagedRAGRealModelQualityPostgres$','-test.timeout=90m'],actual_env);mandatory(actual,'TestManagedRAGRealModelQualityPostgres')
  proof=json.loads((work/'runtime/quality-proof.json').read_text());assert proof['status']=='passed' and proof['actual_model_runs']==180
  run('database-exit',['bash','scripts/database-check.sh','all'],pg);after=run('schema-after',dump);assert norm(before)==norm(after),'whole Schema changed'
  assert sources()==frozen,'source changed during frozen target';assert all(sha(REPO/p)==h for p,h in binaries.items())
  report=dict(schema='campusos.v12-14b-real-model-quality/v1',status='passed',proof=proof,source_sha256=frozen,binary_sha256=binaries,dependency={str(PRIOR.relative_to(REPO)):PRIOR_SHA},schema_before_after_sha256=hashlib.sha256(norm(after).encode()).hexdigest(),public_tables=132,foreign_keys=143,migration_version=35,new_migrations=[],commit_or_push=False)
 finally:
  cleanup={'postgres_started':started,'errors':[],'credentials_persisted':False}
  if started:
   for label,args in [('pg-server',['docker','logs',name]),('pg-stop',['docker','stop',name])]:
    try:run(label,args)
    except Exception as e:cleanup['errors'].append(str(e))
   for _ in range(30):
    inspect=subprocess.run(['docker','inspect',name],stdout=subprocess.PIPE,stderr=subprocess.PIPE);healthy=subprocess.run(['docker','info','--format','{{.ServerVersion}}'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0
    cleanup.update(docker_daemon_healthy=healthy,container_removed=healthy and inspect.returncode==1 and b'no such object' in inspect.stderr.lower())
    with socket.socket() as s:s.settimeout(.25);cleanup['loopback_port_closed']=port is not None and s.connect_ex(('127.0.0.1',port))!=0
    if cleanup['container_removed'] and cleanup['loopback_port_closed']:break
    time.sleep(.1)
  import shutil
  try:shutil.rmtree(work/'keys')
  except OSError:cleanup['errors'].append('owned_private_key_cleanup_failed')
  cleanup['private_key_directory_removed']=not (work/'keys').exists()
  (work/'commands.json').write_text(json.dumps(commands,ensure_ascii=False,indent=2)+'\n');(work/'cleanup.json').write_text(json.dumps(cleanup,indent=2)+'\n')
  if started:assert cleanup['container_removed'] and cleanup['loopback_port_closed'] and cleanup['private_key_directory_removed'] and not cleanup['errors']
 assert report is not None
 report['commands'],report['cleanup']=commands,cleanup;(work/'target.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps({'status':'passed','cases':200,'work':str(work.relative_to(REPO))}))
if __name__=='__main__':
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--work',required=True);main(Path(parser.parse_args().work).resolve())
