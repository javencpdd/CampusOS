#!/usr/bin/env node
// Full G1 target wire Manifest, composed from previously accepted projections.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const require=createRequire(resolve(root,'sdk/typescript/package.json'));
const Ajv=require('ajv'), ts=require('typescript');
const read=(p)=>JSON.parse(readFileSync(resolve(root,p),'utf8'));
const paths=[
  'docs/api/plugin-v5-manifest-v1.schema.json',
  'docs/api/plugin-v5-manifest-v1.errors.json',
  'docs/api/plugin-v5-package-shape-v1.schema.json',
  'docs/api/plugin-v5-consumes-v1.schema.json',
  'docs/api/plugin-v5-provides-v1.schema.json',
  'docs/api/plugin-v5-config-v1.schema.json',
  'docs/api/plugin-v5-host-resources-v1.schema.json',
  'sdk/typescript/tests/plugin-v5-host-catalog.fixture.json',
  'sdk/typescript/tests/plugin-v5-extension-catalog.fixture.json',
];
const [schema,errors,shapeSchema,consumeSchema,provideSchema,configSchema,resourceSchema,hostCatalog,extensionCatalog]=paths.map(read);
const ajv=new Ajv({allErrors:true,schemaId:'auto',strictKeywords:true});
for(const item of [schema,shapeSchema,consumeSchema,provideSchema,configSchema,resourceSchema]) assert(ajv.validateSchema(item));
const valid=ajv.compile(schema), validShape=ajv.compile(shapeSchema), validConsumes=ajv.compile(consumeSchema),
  validProvides=ajv.compile(provideSchema), validConfig=ajv.compile({$ref:'#/definitions/definition',definitions:configSchema.definitions});
const errorCodes=new Set(errors.errors.map((x)=>x.code));
assert.equal(errors.contract,'campusos.plugin/v5');
assert.equal(errorCodes.size,8);
const digest=(ch)=>'sha256:'+ch.repeat(64);
const resource={network_targets:[],storage_bytes:1024,cpu_millis:100,memory_mb:64,max_concurrency:1,timeout_ms:3000};
const config={contract:'campusos.plugin-config/v1',version:'v1',schema:{type:'object',additionalProperties:false,
  properties:{locale:{type:'string',title:'Language',enum:['zh-CN','en-US']}},required:['locale']},
  defaults:{locale:'zh-CN'},selectors:[],secret_names:[]};
const provide={id:'chat-main',point:'ai.chat',contract_version:'v1',
  input_schema:'campusos.ai.chat.request/v1',output_schema:'campusos.ai.chat.response/v1',
  capabilities:['text'],data_mode:'no_host_private_data'};
const data={config_scope:'system',private_kv_bytes:0,private_object_bytes:0,
  upgrade:{schema_version:1,mode:'preserve',rollback:'snapshot'}};
const base={api_version:'campusos.plugin/v5',publisher:'campusos',key:'example-plugin',
  version:'1.0.0',host_contracts:['campusos.host/v1'],runtime:'wasm',
  backend:{artifact:{kind:'wasm',path:'backend/main.wasm',digest:digest('a')},transport:'host-call'},
  consumes:[{capability:'community.thread.public.read',scope:{kind:'public'},purpose:'Read public posts',required:true}],
  provides:[provide],host_resources:resource,configuration:config,data};
const uiOnly=structuredClone(base); uiOnly.runtime='none'; uiOnly.ui=[{audience:'user',entrypoint:'ui/index.html',digest:digest('b')}];
delete uiOnly.backend; delete uiOnly.host_resources; uiOnly.provides=[]; uiOnly.consumes=[];
const container=structuredClone(base); container.runtime='container';
container.backend={artifact:{kind:'container',image:'registry.example/plugin@'+digest('c')},transport:'grpc'};
const combined=structuredClone(base); combined.ui=[{audience:'admin',entrypoint:'ui/admin.html',digest:digest('d')}];
const release={publisher:base.publisher,key:base.key,version:base.version,package_digest:digest('e'),signature:{key_id:'test',value:'signed'}};
function check(m, r=release) {
  if(!valid(m)) return 'plugin.manifest_invalid';
  const projected={...m,provides:m.provides.map(({id,point,contract_version})=>({id,point,contract_version}))};
  delete projected.consumes; delete projected.host_resources; delete projected.configuration; delete projected.data;
  if(!validShape(projected) || (m.runtime!=='none' && m.backend.artifact.kind!==m.runtime) || (m.runtime==='none' && m.provides.length) ||
      (m.runtime!=='none' && !m.provides.length) ||
      (!m.ui?.length && !m.provides.length)) return 'plugin.manifest_shape_mismatch';
  if(!validConsumes(m.consumes) || !validProvides(m.provides) || !validConfig(m.configuration))
    return 'plugin.manifest_invalid';
  let validDefaults;
  try { validDefaults=ajv.compile(m.configuration.schema); } catch { return 'plugin.manifest_invalid'; }
  if(!validDefaults(m.configuration.defaults) ||
      m.configuration.secret_names.some((name)=>Object.hasOwn(m.configuration.schema.properties,name)))
    return 'plugin.manifest_invalid';
  if(!m.host_contracts.includes(hostCatalog.host_contract) ||
      !m.host_contracts.includes(extensionCatalog.host_contract))
    return 'plugin.manifest_schema_mismatch';
  for(const c of m.consumes) {
    const item=hostCatalog.items.find((x)=>x.code===c.capability);
    if(!item || item.scope_kind!==c.scope.kind) return 'plugin.manifest_capability_unpublished';
    if(c.scope.kind==='collection' && !c.scope.collection_ids.every((id)=>item.available_public_collection_ids.includes(id)))
      return 'plugin.manifest_capability_unpublished';
    if(c.scope.kind==='system_config' && !c.scope.keys.every((key)=>item.available_config_keys.includes(key)))
      return 'plugin.manifest_capability_unpublished';
  }
  const ids=new Set();
  for(const p of m.provides) {
    const item=extensionCatalog.points.find((x)=>x.point===p.point && x.contract_version===p.contract_version);
    if(!item || ids.has(p.id)) return 'plugin.manifest_extension_unpublished';
    ids.add(p.id);
    if(item.input_schema!==p.input_schema || item.output_schema!==p.output_schema ||
       !p.capabilities.every((cap)=>item.capabilities.includes(cap)) || !item.data_modes.includes(p.data_mode))
      return 'plugin.manifest_schema_mismatch';
  }
  if(m.host_resources && m.data.private_kv_bytes+m.data.private_object_bytes>m.host_resources.storage_bytes)
    return 'plugin.manifest_resource_invalid';
  if(m.data.upgrade.mode==='migrate' && m.data.upgrade.rollback==='snapshot' &&
      m.data.private_kv_bytes+m.data.private_object_bytes>0) return 'plugin.manifest_upgrade_unrecoverable';
  if(r.publisher!==m.publisher || r.key!==m.key || r.version!==m.version)
    return 'plugin.manifest_release_mismatch';
  return null;
}
const cases=[['headless-wasm',base,null],['headless-container',container,null],['ui-only',uiOnly,null],['combined',combined,null]];
const change=(name,mutate,expected)=>{const m=structuredClone(base);mutate(m);cases.push([name,m,expected]);};
change('missing-consumes',(m)=>{delete m.consumes;},'plugin.manifest_invalid');
change('missing-config',(m)=>{delete m.configuration;},'plugin.manifest_invalid');
change('missing-data',(m)=>{delete m.data;},'plugin.manifest_invalid');
change('undeclared-extension',(m)=>{m.provides[0].point='embedding';},'plugin.manifest_invalid');
change('wrong-input-schema',(m)=>{m.provides[0].input_schema='campusos.preview.renderer.request/v1';},'plugin.manifest_schema_mismatch');
change('unknown-capability',(m)=>{m.consumes[0].capability='host.db.query';},'plugin.manifest_capability_unpublished');
change('scope-mismatch',(m)=>{m.consumes[0].scope={kind:'self'};},'plugin.manifest_capability_unpublished');
change('private-profile',(m)=>{m.consumes[0]={capability:'knowledge.collection.public.search',scope:{kind:'collection',collection_ids:['private']},purpose:'Search',required:true};},'plugin.manifest_capability_unpublished');
change('resource-missing',(m)=>{delete m.host_resources;},'plugin.manifest_invalid');
change('resource-quota',(m)=>{m.data.private_kv_bytes=2048;},'plugin.manifest_resource_invalid');
change('unsafe-rollback',(m)=>{m.data.private_kv_bytes=100;m.data.upgrade.mode='migrate';},'plugin.manifest_upgrade_unrecoverable');
change('raw-secret',(m)=>{m.configuration.defaults.token='sk-secret';},'plugin.manifest_invalid');
change('remote-schema-ref',(m)=>{m.configuration.schema.properties.locale.$ref='https://example.test/schema';},'plugin.manifest_invalid');
change('runtime-artifact-mismatch',(m)=>{m.runtime='container';},'plugin.manifest_shape_mismatch');
change('empty-headless',(m)=>{m.provides=[];},'plugin.manifest_shape_mismatch');
change('install-script',(m)=>{m.install_script='sh';},'plugin.manifest_invalid');
change('unknown-host',(m)=>{m.host_contracts=['campusos.host/v2'];},'plugin.manifest_schema_mismatch');
change('duplicate-contribution',(m)=>{m.provides.push(structuredClone(m.provides[0]));},'plugin.manifest_extension_unpublished');
const results=[];
for(const [name,m,expected] of cases){const before=JSON.stringify(m);assert.equal(check(m),expected,name);assert.equal(JSON.stringify(m),before,name+' mutated');assert(expected===null || errorCodes.has(expected),name);results.push({name,expected:expected??'accepted',passed:true});}
assert.equal(check(base,{...release,key:'wrong'}),'plugin.manifest_release_mismatch');
const virtual=resolve(root,'sdk/typescript/tests/__plugin_v5_manifest__.ts');
const source=[
 'import type { PluginV5Manifest, PluginV5ManifestErrorCode } from "../src/index";',
 'const good: PluginV5Manifest = '+JSON.stringify(base)+';',
 'const ui: PluginV5Manifest = '+JSON.stringify(uiOnly)+';',
 '// @ts-expect-error unsupported runtime',
 'const badRuntime: PluginV5Manifest["runtime"] = "process";',
 '// @ts-expect-error unsupported data mode',
 'const badMode: PluginV5Manifest["data"]["config_scope"] = "global";',
 '// @ts-expect-error unknown error',
 'const badError: PluginV5ManifestErrorCode = "plugin.install_allowed";',
].join('\n');
for(const entry of ['index','plugin-v5-manifest']){
 const options={noEmit:true,strict:true,exactOptionalPropertyTypes:entry!=='index',skipLibCheck:true,target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ESNext,moduleResolution:ts.ModuleResolutionKind.Bundler,types:[]};
 const host=ts.createCompilerHost(options),get=host.getSourceFile.bind(host);
 host.getSourceFile=(path,language,onError,fresh)=>path===virtual?ts.createSourceFile(path,source.replace('../src/index','../src/'+entry),language,true):get(path,language,onError,fresh);
 const diagnostics=ts.getPreEmitDiagnostics(ts.createProgram([virtual],options,host));
 assert.equal(diagnostics.length,0,ts.formatDiagnosticsWithColorAndContext(diagnostics,{getCanonicalFileName:(x)=>x,getCurrentDirectory:()=>root,getNewLine:()=>'\n'}));
}
const report={schema:'campusos.v12-g1-plugin-manifest-acceptance/v1',generated_at:new Date().toISOString(),
 scope:'G1 full v5 wire Manifest: shape, consumes, provides, resources, config, private data and immutable release identity',
 implementation:'passed',automation:'passed',target_environment:process.platform+' offline Node/Ajv/TypeScript contract acceptance passed',
 runtime_acceptance:'pending 03a/03b: package bytes/signatures, real catalog, installer and Runtime still unimplemented',
 environment:{platform:process.platform,node:process.version,ajv:require('ajv/package.json').version,typescript:ts.version},
 fixtures:results,release_recheck:{expected:'plugin.manifest_release_mismatch',passed:true},
 files_sha256:Object.fromEntries([...paths,'sdk/typescript/src/plugin-v5-manifest.ts','sdk/typescript/src/index.ts','scripts/check-v12-plugin-v5-manifest-contract.mjs'].map((p)=>[p,createHash('sha256').update(readFileSync(resolve(root,p))).digest('hex')]))};
const args=process.argv.slice(2);assert(args.length===0||(args.length===2&&args[0]==='--report'),'usage: [--report <path>]');
if(args.length)writeFileSync(resolve(args[1]),JSON.stringify(report,null,2)+'\n');
console.log('Plugin v5 full Manifest: '+results.length+' fixtures, 1 release recheck, 2/3 TS positive/negative types passed');
