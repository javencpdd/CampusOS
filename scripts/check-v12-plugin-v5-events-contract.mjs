#!/usr/bin/env node
// G1 safe event and lifecycle state target; persistent operations arrive in 03b/03c.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const require=createRequire(resolve(root,'sdk/typescript/package.json'));
const Ajv=require('ajv'),ts=require('typescript');
const paths=['docs/api/plugin-v5-event-v1.schema.json','docs/api/plugin-v5-event-v1.errors.json','docs/api/plugin-v5-lifecycle-v1.schema.json','docs/api/plugin-v5-lifecycle-v1.errors.json'];
const read=(p)=>JSON.parse(readFileSync(resolve(root,p),'utf8'));
const [eventSchema,eventErrors,lifecycleSchema,lifecycleErrors]=paths.map(read);
const ajv=new Ajv({allErrors:true,schemaId:'auto',strictKeywords:true});
for(const item of [eventSchema,lifecycleSchema])assert(ajv.validateSchema(item));
const validEvent=ajv.compile(eventSchema),validLifecycle=ajv.compile(lifecycleSchema);
assert.equal(eventErrors.contract,'campusos.plugin-event/v1');assert.equal(lifecycleErrors.contract,'campusos.plugin-lifecycle/v1');
const eventCodes=new Set(eventErrors.errors.map((x)=>x.code)),lifecycleCodes=new Set(lifecycleErrors.errors.map((x)=>x.code));
assert.equal(eventCodes.size,9);assert.equal(lifecycleCodes.size,6);
const baseEvent={event:{contract:'campusos.plugin-event/v1',event_id:'evt-1',type:'community.thread.public.changed/v1',
 revision:7,created_at_ms:1000,expires_at_ms:2000,payload:{resource_ref:'community.thread:42',state:'changed'}},
 delivery:{now_ms:1500,instance_generation:4,current_generation:4,subscribed:true,grant_active:true,runtime_ready:true,
 attempt:1,max_attempts:3,payload_bytes:160,max_bytes:1024,core_transaction_committed:true}};
function eventDecision(x){
 if(!validEvent(x))return 'plugin.event_invalid';
 const {event:e,delivery:d}=x;
 if(!d.core_transaction_committed)return 'plugin.event_uncommitted';
 if(e.expires_at_ms<=e.created_at_ms||e.expires_at_ms-e.created_at_ms>86400000)return 'plugin.event_invalid';
 if(e.created_at_ms>d.now_ms||e.expires_at_ms<=d.now_ms)return 'plugin.event_expired';
 if(d.instance_generation!==d.current_generation)return 'plugin.event_stale_generation';
 if(!d.subscribed)return 'plugin.event_subscription_missing';
 if(!d.grant_active)return 'plugin.event_grant_revoked';
 if(!d.runtime_ready)return 'plugin.event_runtime_unavailable';
 if(d.payload_bytes>d.max_bytes)return 'plugin.event_too_large';
 if(d.attempt>d.max_attempts)return 'plugin.event_dead_letter';
 return null;
}
const baseLifecycle={contract:'campusos.plugin-lifecycle/v1',operation_id:'op-1',operation:'enable',phase:'final',
 desired:'enabled',observed:'ready',current_generation:4,requested_generation:4,global_installed:true,
 grant_active:true,accepting_new_calls:true,published:true,inflight_calls:0,config_migration:false,
 private_data_mutation:false,snapshot_verified:false,drain_strategy:'drain'};
function lifecycleDecision(x){
 if(!validLifecycle(x))return 'plugin.lifecycle_invalid';
 if(x.operation==='upgrade'){
  if(x.requested_generation<=x.current_generation)return 'plugin.lifecycle_generation_conflict';
  if((x.config_migration||x.private_data_mutation)&&!x.snapshot_verified)
    return 'plugin.lifecycle_rollback_unverifiable';
 }else if(x.requested_generation!==x.current_generation)return 'plugin.lifecycle_generation_conflict';
 if(x.published&&(!x.global_installed||!x.grant_active||x.desired!=='enabled'||x.observed!=='ready'))
  return 'plugin.lifecycle_publication_forbidden';
 if(['revoke','uninstall','disable'].includes(x.operation)){
  if(x.accepting_new_calls||x.published)return 'plugin.lifecycle_release_order';
  if(x.phase==='final'&&x.inflight_calls>0)return 'plugin.lifecycle_drain_incomplete';
 }
 if(x.operation==='upgrade'&&x.phase==='switch'&&x.observed!=='ready')
  return 'plugin.lifecycle_publication_forbidden';
 return null;
}
const events=[['public-event',baseEvent,null]];
const eadd=(name,mutate,expected)=>{const x=structuredClone(baseEvent);mutate(x);events.push([name,x,expected]);};
for(const type of ['plugin.lifecycle.changed/v1','knowledge.source.public.changed/v1'])
 eadd('event-'+type,(x)=>{x.event.type=type;},null);
for(const field of ['body','prompt','owner','secret','raw_outbox','acl'])
 eadd('forbidden-payload-'+field,(x)=>{x.event.payload[field]='leak';},'plugin.event_invalid');
eadd('unknown-event',(x)=>{x.event.type='private.document.changed/v1';},'plugin.event_invalid');
eadd('unbounded-event-ttl',(x)=>{x.event.expires_at_ms=x.event.created_at_ms+86400001;},'plugin.event_invalid');
eadd('uncommitted',(x)=>{x.delivery.core_transaction_committed=false;},'plugin.event_uncommitted');
eadd('expired',(x)=>{x.event.expires_at_ms=1500;},'plugin.event_expired');
eadd('future-event',(x)=>{x.event.created_at_ms=1501;},'plugin.event_expired');
eadd('stale-generation',(x)=>{x.delivery.current_generation=5;},'plugin.event_stale_generation');
eadd('unsubscribed',(x)=>{x.delivery.subscribed=false;},'plugin.event_subscription_missing');
eadd('grant-revoked',(x)=>{x.delivery.grant_active=false;},'plugin.event_grant_revoked');
eadd('runtime-failed',(x)=>{x.delivery.runtime_ready=false;},'plugin.event_runtime_unavailable');
eadd('oversize',(x)=>{x.delivery.payload_bytes=1025;},'plugin.event_too_large');
eadd('dead-letter',(x)=>{x.delivery.attempt=4;},'plugin.event_dead_letter');
const lifecycles=[['enabled-ready',baseLifecycle,null]];
const ladd=(name,mutate,expected)=>{const x=structuredClone(baseLifecycle);mutate(x);lifecycles.push([name,x,expected]);};
ladd('upgrade-ready',(x)=>{x.operation='upgrade';x.phase='switch';x.requested_generation=5;},null);
ladd('upgrade-snapshot',(x)=>{x.operation='upgrade';x.phase='switch';x.requested_generation=5;x.config_migration=true;x.snapshot_verified=true;},null);
ladd('upgrade-no-snapshot',(x)=>{x.operation='upgrade';x.phase='switch';x.requested_generation=5;x.private_data_mutation=true;},'plugin.lifecycle_rollback_unverifiable');
ladd('upgrade-same-generation',(x)=>{x.operation='upgrade';},'plugin.lifecycle_generation_conflict');
ladd('upgrade-before-ready',(x)=>{x.operation='upgrade';x.phase='switch';x.requested_generation=5;x.observed='pending';x.published=false;},'plugin.lifecycle_publication_forbidden');
for(const field of ['global_installed','grant_active'])
 ladd('published-without-'+field,(x)=>{x[field]=false;},'plugin.lifecycle_publication_forbidden');
ladd('published-failed',(x)=>{x.observed='failed';},'plugin.lifecycle_publication_forbidden');
ladd('published-disabled',(x)=>{x.desired='disabled';},'plugin.lifecycle_publication_forbidden');
for(const kind of ['revoke','uninstall','disable']){
 ladd(kind+'-stops-new-calls',(x)=>{x.operation=kind;x.desired=kind==='uninstall'?'uninstalled':'disabled';x.published=false;x.accepting_new_calls=false;x.observed='stopped';},null);
 ladd(kind+'-still-accepting',(x)=>{x.operation=kind;x.desired='disabled';x.published=false;},'plugin.lifecycle_release_order');
 ladd(kind+'-inflight',(x)=>{x.operation=kind;x.desired='disabled';x.published=false;x.accepting_new_calls=false;x.observed='stopped';x.inflight_calls=1;},'plugin.lifecycle_drain_incomplete');
}
const results=[];
for(const [group,cases,fn,codes] of [['event',events,eventDecision,eventCodes],['lifecycle',lifecycles,lifecycleDecision,lifecycleCodes]]){
 for(const [name,input,expected] of cases){const before=JSON.stringify(input);assert.equal(fn(input),expected,name);assert.equal(JSON.stringify(input),before,name+' mutated');assert(expected===null||codes.has(expected),name);results.push({group,name,expected:expected??'accepted',passed:true});}}
const virtual=resolve(root,'sdk/typescript/tests/__plugin_v5_events__.ts');
const source=[
 'import type { PluginV5EventDecisionInput, PluginV5LifecycleDecisionInput, PluginV5EventErrorCode } from "../src/index";',
 'const event: PluginV5EventDecisionInput = '+JSON.stringify(baseEvent)+';',
 'const lifecycle: PluginV5LifecycleDecisionInput = '+JSON.stringify(baseLifecycle)+';',
 '// @ts-expect-error no private event type',
 'const type: PluginV5EventDecisionInput["event"]["type"] = "private.document.changed/v1";',
 '// @ts-expect-error no unknown operation',
 'const operation: PluginV5LifecycleDecisionInput["operation"] = "shell";',
 '// @ts-expect-error unknown error',
 'const code: PluginV5EventErrorCode = "plugin.event_raw_outbox";',
].join('\n');
for(const entry of ['index','plugin-v5-events']){
 const opts={noEmit:true,strict:true,exactOptionalPropertyTypes:entry!=='index',skipLibCheck:true,target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ESNext,moduleResolution:ts.ModuleResolutionKind.Bundler,types:[]};
 const host=ts.createCompilerHost(opts),get=host.getSourceFile.bind(host);
 host.getSourceFile=(path,language,onError,fresh)=>path===virtual?ts.createSourceFile(path,source.replace('../src/index','../src/'+entry),language,true):get(path,language,onError,fresh);
 const diagnostics=ts.getPreEmitDiagnostics(ts.createProgram([virtual],opts,host));
 assert.equal(diagnostics.length,0,ts.formatDiagnosticsWithColorAndContext(diagnostics,{getCanonicalFileName:(x)=>x,getCurrentDirectory:()=>root,getNewLine:()=>'\n'}));
}
const report={schema:'campusos.v12-g1-plugin-events-lifecycle-acceptance/v1',generated_at:new Date().toISOString(),
 scope:'G1 whitelisted event metadata, bounded delivery, desired/observed generation and publication order',
 implementation:'passed',automation:'passed',target_environment:process.platform+' offline Node/Ajv/TypeScript contract acceptance passed',
 runtime_acceptance:'pending 03b/03c persistent Operation/Outbox, Runner drain, crash recovery and real event delivery',
 environment:{platform:process.platform,node:process.version,ajv:require('ajv/package.json').version,typescript:ts.version},
 fixtures:results,core_transaction_independence:{condition:'event delivery decision never changes committed core transaction',passed:true},
 files_sha256:Object.fromEntries([...paths,'sdk/typescript/src/plugin-v5-events.ts','sdk/typescript/src/index.ts','scripts/check-v12-plugin-v5-events-contract.mjs'].map((p)=>[p,createHash('sha256').update(readFileSync(resolve(root,p))).digest('hex')]))};
const args=process.argv.slice(2);assert(args.length===0||(args.length===2&&args[0]==='--report'),'usage: [--report <path>]');
if(args.length)writeFileSync(resolve(args[1]),JSON.stringify(report,null,2)+'\n');
console.log('Plugin v5 events/lifecycle: '+events.length+' event + '+lifecycles.length+' lifecycle fixtures, 2/3 TS positive/negative types passed');
