import json,os,socket,subprocess,tempfile,time,urllib.request,urllib.error,hashlib
from pathlib import Path
from proxies import FixtureProxies
root=Path.cwd(); work=root/'.cache/web-companion-20261115/e2e';work.mkdir(parents=True,exist_ok=True);database=f'campusos_rss_e2e_{os.getpid()}';api=None;created=False;report={};phase='fixture setup';proxies=FixtureProxies();protocol=os.environ.get('CAMPUSOS_RSS_E2E_PROXY','http');assert protocol in ('http','socks5');client_mode=os.environ.get('CAMPUSOS_RSS_E2E_CLIENT_MODE','1')=='1'
def sql(query,name='campusos'):
 return subprocess.check_output(['docker','exec','campusos-dev-postgres-1','psql','-U','campusos','-d',name,'-v','ON_ERROR_STOP=1','-Atqc',query],text=True,stderr=subprocess.DEVNULL).strip()
def port():
 with socket.socket() as sock:sock.bind(('127.0.0.1',0));return sock.getsockname()[1]
def host_fingerprint():
 digest=hashlib.sha256()
 for scope in ['internal','cmd','web/src','admin/src','sdk','migrations','go.mod','go.sum','web/pnpm-lock.yaml']:
  path=root/scope
  for file in sorted(path.rglob('*')) if path.is_dir() else [path]:
   if file.is_file() and 'node_modules' not in file.parts:
    digest.update(str(file.relative_to(root)).encode());digest.update(file.read_bytes())
 return digest.hexdigest()
try:
 fingerprint=host_fingerprint()
 actual_path=root/'deploy/docker/.env.dev.local';actual=actual_path.read_bytes() if actual_path.exists() else None
 with (work/'server-build.log').open('wb') as log:subprocess.run(['go','build','-o',str(work/'server'),'./cmd/server'],env=dict(os.environ,GOCACHE='/tmp/campusos-go-cache'),stdout=log,stderr=subprocess.STDOUT,check=True)
 pg=json.loads(subprocess.check_output(['docker','inspect','campusos-dev-postgres-1'],text=True))[0]
 password=dict(v.split('=',1) for v in pg['Config']['Env'] if '=' in v)['POSTGRES_PASSWORD']
 sql(f'CREATE DATABASE {database}');created=True
 env=dict(os.environ,PSQL_MODE='docker',POSTGRES_CONTAINER='campusos-dev-postgres-1',DB_USER='campusos',DB_NAME=database,DB_PASSWORD=password)
 with (work/'migrations.log').open('wb') as log:subprocess.run(['bash','scripts/migrate.sh','up'],env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
 with tempfile.TemporaryDirectory(prefix='campusos-rss-owned-') as temp:
  temp=Path(temp)
  import shutil
  subprocess.run(['go','run',str(root/'scripts/verify/web-companion/prepare.go'),str(temp)],env=dict(os.environ,GOCACHE='/tmp/campusos-go-cache'),check=True,stdout=subprocess.DEVNULL)
  credential=(temp/'credential').read_text()
  sql(f"INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES(9901001,'rss_alice','RSS Alice','rss-alice@example.test','active',1),(9901002,'rss_bob','RSS Bob','rss-bob@example.test','active',1); INSERT INTO accounts(id,user_id,type,identifier,identifier_normalized,credential,verified,verification_state,verified_at,verification_source,password_changed_at,credential_version) VALUES(9902001,9901001,'email','rss-alice@example.test','rss-alice@example.test','{credential}',true,'verified',now(),'registration_challenge',now(),1),(9902002,9901002,'email','rss-bob@example.test','rss-bob@example.test','{credential}',true,'verified',now(),'registration_challenge',now(),1);",database)
  sql("INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(9903001,'RSS group','rss-group','group','active');INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(9903002,'RSS 测试版块','rss-board',9903001,'board','active');INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(9903002,'article',true);",database)
  api_port,ui_port,web_port,admin_port=port(),port(),port(),port()
  env=dict(os.environ,CAMPUSOS_MANAGED_ENV_FILE='',CAMPUSOS_SKIP_DOTENV='true',DATABASE_DSN=f'postgres://campusos:{password}@127.0.0.1:55432/{database}?sslmode=disable',SERVER_HOST='127.0.0.1',SERVER_PORT=str(api_port),CAMPUSOS_ENV='development',REDIS_ENABLED='false',NATS_URL='nats://127.0.0.1:1',HOST_API_ENABLED='false',AUTH_PASSWORD_HASH_ENABLED='true',AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN='true',AUTH_BOOTSTRAP_ADMIN_SECRET='owned-rss-legacy-bootstrap-proof',AUTH_ADMIN_BOOTSTRAP_SECRET='',AUTH_ADMIN_MFA_REQUIRED='false',AUTH_MFA_ACTIVE_KEY_ID='fixture',AUTH_MFA_ENCRYPTION_KEYS='fixture:fixture-mfa-encryption-key-32-bytes',JWT_SECRET='fixture-rss-jwt-signing-key',PLUGIN_DATA_DIR=str(temp/'plugins'),MODULE_DATA_DIR=str(temp/'modules'),RESOURCE_DIR=str(temp/'resources'),EMAIL_PROVIDER='fake',AI_ENABLED='false',OBSERVABILITY_PROMETHEUS_ENABLED='false',GIN_MODE='release',CAMPUSOS_PLUGIN_V5_TRUST_STORE=str(temp/'trust.json'),CAMPUSOS_PLUGIN_WEB_PROXY_PROFILES=json.dumps(proxies.profiles),CAMPUSOS_PLUGIN_WEB_PROXY_PROFILE='owned-server' if client_mode else 'owned-'+protocol,CAMPUSOS_WEB_COMPANION_TOOLS_DIR=str(root/'.campusos/tools/web-companion'),CAMPUSOS_PLUGIN_UI_LISTEN='',CAMPUSOS_PLUGIN_UI_ORIGIN='',CAMPUSOS_PLUGIN_UI_PARENTS=f'http://127.0.0.1:{web_port} http://127.0.0.1:{admin_port}')
  phase='API start'
  with (work/'api.log').open('wb') as log:
   api=subprocess.Popen([str(work/'server')],cwd=temp,env=env,stdout=log,stderr=subprocess.STDOUT)
   opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
   for _ in range(180):
    if api.poll() is not None:raise RuntimeError('owned API exited')
    try:
     with opener.open(f'http://127.0.0.1:{api_port}/api/v1/health',timeout=1) as response:
      if response.status==200:break
    except (OSError,urllib.error.URLError):pass
    time.sleep(.2)
   else:raise RuntimeError('owned API start timeout')
   config=temp/'browser.json';config.write_text(json.dumps(dict(ownedFixture=True,noLocalCapability=not client_mode,helper=str(root/'.campusos/tools/web-companion/campusos-web-companion-linux-amd64') if client_mode else '',clientProxy=next(p['url'] for p in proxies.profiles if p['id']=='owned-'+protocol),windowsArtifact=str(root/'.campusos/tools/web-companion/campusos-web-companion-windows-amd64.exe'),database=database,backend=f'http://127.0.0.1:{api_port}',webPort=web_port,adminPort=admin_port,containerPackage=str(temp/'container.tar.gz'),containerRelease=str(temp/'container.release.json'),renamedPackage=str(temp/'renamed.tar.gz'),renamedRelease=str(temp/'renamed.release.json'),package=str(temp/'rss.tar.gz'),release=str(temp/'rss.release.json'),output=str(work))));config.chmod(0o600)
   binary_fingerprint=hashlib.sha256((work/'server').read_bytes()).hexdigest()
   phase='real signed RSS plugin browser and publication'
   with (work/'browser.log').open('wb') as browserlog:subprocess.run(['node','web/tests/rss-publisher-browser.mjs'],env=dict(os.environ,CAMPUSOS_RSS_BROWSER_CONFIG=str(config)),stdout=browserlog,stderr=subprocess.STDOUT,check=True)
   report['browser']=json.loads((work/'browser-report.json').read_text())
   report['ownedDatabaseAndRealAuthentication']=True
   assert proxies.counts[protocol]>0 and sum(proxies.counts.values())==proxies.counts[protocol]
   report['clientMode']=client_mode
   report['selectedProxyProtocol']=protocol
   report['realProxyPublicHTTPS']=dict(proxies.counts)
   assert (actual_path.read_bytes() if actual_path.exists() else None)==actual
   report['realDeploymentPreserved']=True
   assert host_fingerprint()==fingerprint
   assert hashlib.sha256((work/'server').read_bytes()).hexdigest()==binary_fingerprint
   report['hostSourceLockfilesSchemaAndBinariesUnmodified']=True
 report['passed']=True
except Exception as error:
 report['failure']={'phase':phase,'type':type(error).__name__};raise
finally:
 proxies.close()
 if api and api.poll() is None:api.terminate();api.wait(timeout=30)
 if created:sql(f'DROP DATABASE {database} WITH (FORCE)')
 (work/'e2e-report.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report),flush=True)
