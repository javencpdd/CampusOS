#!/usr/bin/env node
// G1 bidirectional target contract. Trusted facts must be built by the host at call time.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const require=createRequire(resolve(root,'sdk/typescript/package.json'));
const Ajv=require('ajv'),ts=require('typescript');
const schemaPath='docs/api/plugin-v5-dispatch-v1.schema.json',errorPath='docs/api/plugin-v5-dispatch-v1.errors.json';
const read=(p)=>JSON.parse(readFileSync(resolve(root,p),'utf8'));
const schema=read(schemaPath),errors=read(errorPath);
const ajv=new Ajv({allErrors:true,schemaId:'auto',strictKeywords:true});
assert(ajv.validateSchema(schema));const valid=ajv.compile(schema);
assert.equal(errors.contract,'campusos.plugin-dispatch/v1');
const codes=new Set(errors.errors.map((x)=>x.code));assert.equal(codes.size,15);
const base={direction:'host_to_plugin',
 call:{request_id:'req-1',contract:'campusos.plugin-dispatch/v1',target:{kind:'contribution',id:'chat-main'},
  instance_generation:4,deadline_ms:1500,request_bytes:2048,response_limit_bytes:4096},
 trusted:{now_ms:1000,current_generation:4,desired_state:'enabled',observed_state:'ready',
  global_installed:true,user_install_required:false,user_installed:false,grant_active:true,
  consent_required:false,consent_active:false,declared:true,published:true,feature_enabled:true,
  instance_audience:'host-api',credential_expires_at_ms:2000,selected_id:'chat-main',
  max_request_bytes:4096,max_response_bytes:8192,max_deadline_ms:2000}};
function check(x){
 if(!valid(x))return 'plugin.call_invalid';
 const {direction,call:c,trusted:t}=x;
 if((direction==='host_to_plugin')!==(c.target.kind==='contribution'))return 'plugin.call_target_mismatch';
 if(c.instance_generation!==t.current_generation)return 'plugin.instance_stale';
 if(c.deadline_ms<=t.now_ms||c.deadline_ms>t.max_deadline_ms)return 'plugin.call_deadline';
 if(c.request_bytes>t.max_request_bytes||c.response_limit_bytes>t.max_response_bytes)return 'plugin.call_budget';
 if(!t.global_installed)return 'plugin.global_install_required';
 if(t.desired_state!=='enabled'||t.observed_state!=='ready')return 'plugin.runtime_unavailable';
 if(!t.grant_active)return 'plugin.grant_revoked';
 if(t.user_install_required&&!t.user_installed)return 'plugin.user_install_required';
 if(t.consent_required&&!t.consent_active)return 'plugin.consent_required';
 if(!t.feature_enabled)return 'plugin.feature_disabled';
 if(!t.declared)return 'plugin.target_undeclared';
 if(!t.published)return 'plugin.target_unpublished';
 if(direction==='host_to_plugin'&&t.selected_id!==c.target.id)return 'plugin.contribution_selection_conflict';
 if(direction==='plugin_to_host'&&(t.instance_audience!=='host-api'||t.credential_expires_at_ms<=t.now_ms))
   return 'plugin.instance_credential_invalid';
 return null;
}
const cases=[['host-to-plugin',base,null]];
const add=(name,mutate,expected)=>{const x=structuredClone(base);mutate(x);cases.push([name,x,expected]);};
add('plugin-to-host',(x)=>{x.direction='plugin_to_host';x.call.target={kind:'host_api',id:'community.thread.public.read'};},null);
add('direction-target-mismatch',(x)=>{x.call.target.kind='host_api';},'plugin.call_target_mismatch');
add('stale-generation',(x)=>{x.trusted.current_generation=5;},'plugin.instance_stale');
add('expired-deadline',(x)=>{x.call.deadline_ms=999;},'plugin.call_deadline');
add('excess-deadline',(x)=>{x.call.deadline_ms=2001;},'plugin.call_deadline');
add('large-request',(x)=>{x.call.request_bytes=4097;},'plugin.call_budget');
add('large-response',(x)=>{x.call.response_limit_bytes=8193;},'plugin.call_budget');
add('not-installed',(x)=>{x.trusted.global_installed=false;},'plugin.global_install_required');
for(const state of ['disabled','uninstalled'])add('desired-'+state,(x)=>{x.trusted.desired_state=state;},'plugin.runtime_unavailable');
for(const state of ['pending','failed','stopped'])add('observed-'+state,(x)=>{x.trusted.observed_state=state;},'plugin.runtime_unavailable');
add('grant-revoked',(x)=>{x.trusted.grant_active=false;},'plugin.grant_revoked');
add('personal-install-missing',(x)=>{x.trusted.user_install_required=true;},'plugin.user_install_required');
add('personal-install-present',(x)=>{x.trusted.user_install_required=true;x.trusted.user_installed=true;},null);
add('consent-missing',(x)=>{x.trusted.consent_required=true;},'plugin.consent_required');
add('consent-present',(x)=>{x.trusted.consent_required=true;x.trusted.consent_active=true;},null);
add('feature-disabled',(x)=>{x.trusted.feature_enabled=false;},'plugin.feature_disabled');
add('undeclared',(x)=>{x.trusted.declared=false;},'plugin.target_undeclared');
add('unpublished',(x)=>{x.trusted.published=false;},'plugin.target_unpublished');
add('selection-conflict',(x)=>{x.trusted.selected_id='other';},'plugin.contribution_selection_conflict');
add('wrong-credential-audience',(x)=>{x.direction='plugin_to_host';x.call.target={kind:'host_api',id:'community.thread.public.read'};x.trusted.instance_audience='other';},'plugin.instance_credential_invalid');
add('expired-instance-credential',(x)=>{x.direction='plugin_to_host';x.call.target={kind:'host_api',id:'community.thread.public.read'};x.trusted.credential_expires_at_ms=1000;},'plugin.instance_credential_invalid');
for(const field of ['actor','roles','secret','db_connection','permissions'])
 add('untrusted-'+field,(x)=>{x.call[field]='forged';},'plugin.call_invalid');
const results=[];
for(const [name,input,expected] of cases){const before=JSON.stringify(input);assert.equal(check(input),expected,name);assert.equal(JSON.stringify(input),before,name+' mutated');assert(expected===null||codes.has(expected),name);results.push({name,expected:expected??'accepted',passed:true});}
const rechecks=[];
for(const [name,mutate,expected] of [
 ['grant-revocation',(x)=>{x.trusted.grant_active=false;},'plugin.grant_revoked'],
 ['generation-bump',(x)=>{x.trusted.current_generation++;},'plugin.instance_stale'],
 ['user-uninstall',(x)=>{x.trusted.user_install_required=true;x.trusted.user_installed=false;},'plugin.user_install_required'],
 ['feature-stop',(x)=>{x.trusted.feature_enabled=false;},'plugin.feature_disabled']]){
 const x=structuredClone(base);assert.equal(check(x),null);mutate(x);assert.equal(check(x),expected);rechecks.push({name,passed:true});}
const virtual=resolve(root,'sdk/typescript/tests/__plugin_v5_dispatch__.ts');
const source=[
 'import type { PluginV5DispatchDecisionInput, PluginV5DispatchErrorCode } from "../src/index";',
 'const good: PluginV5DispatchDecisionInput = '+JSON.stringify(base)+';',
 '// @ts-expect-error no arbitrary direction',
 'const direction: PluginV5DispatchDecisionInput["direction"] = "both";',
 '// @ts-expect-error no arbitrary call target',
 'const target: PluginV5DispatchDecisionInput["call"]["target"]["kind"] = "database";',
 '// @ts-expect-error unknown error',
 'const error: PluginV5DispatchErrorCode = "plugin.allow";'
].join('\n');
for(const entry of ['index','plugin-v5-dispatch']){
 const opts={noEmit:true,strict:true,exactOptionalPropertyTypes:entry!=='index',skipLibCheck:true,target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ESNext,moduleResolution:ts.ModuleResolutionKind.Bundler,types:[]};
 const host=ts.createCompilerHost(opts),get=host.getSourceFile.bind(host);
 host.getSourceFile=(path,language,onError,fresh)=>path===virtual?ts.createSourceFile(path,source.replace('../src/index','../src/'+entry),language,true):get(path,language,onError,fresh);
 const diagnostics=ts.getPreEmitDiagnostics(ts.createProgram([virtual],opts,host));
 assert.equal(diagnostics.length,0,ts.formatDiagnosticsWithColorAndContext(diagnostics,{getCanonicalFileName:(x)=>x,getCurrentDirectory:()=>root,getNewLine:()=>'\n'}));
}
const report={schema:'campusos.v12-g1-plugin-dispatch-acceptance/v1',generated_at:new Date().toISOString(),
 scope:'G1 bidirectional call envelope, generation, grants, consent, feature state and budgets',
 implementation:'passed',automation:'passed',target_environment:process.platform+' offline Node/Ajv/TypeScript contract acceptance passed',
 runtime_acceptance:'pending 03b/03c/14c real runtime, catalogs, independent permissions and feature integration',
 environment:{platform:process.platform,node:process.version,ajv:require('ajv/package.json').version,typescript:ts.version},
 fixtures:results,rechecks,files_sha256:Object.fromEntries([schemaPath,errorPath,'sdk/typescript/src/plugin-v5-dispatch.ts','sdk/typescript/src/index.ts','scripts/check-v12-plugin-v5-dispatch-contract.mjs'].map((p)=>[p,createHash('sha256').update(readFileSync(resolve(root,p))).digest('hex')]))};
const args=process.argv.slice(2);assert(args.length===0||(args.length===2&&args[0]==='--report'),'usage: [--report <path>]');
if(args.length)writeFileSync(resolve(args[1]),JSON.stringify(report,null,2)+'\n');
console.log('Plugin v5 dispatch: '+results.length+' fixtures, '+rechecks.length+' rechecks, 1/3 TS positive/negative types passed');
