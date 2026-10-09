#!/usr/bin/env node
// G1 exact Scope and delegation target, separate from the board governance grant.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const require=createRequire(resolve(root,'sdk/typescript/package.json'));
const Ajv=require('ajv'),ts=require('typescript');
const schemaPath='docs/api/scope-delegation-v1.schema.json',errorPath='docs/api/scope-delegation-v1.errors.json';
const read=(p)=>JSON.parse(readFileSync(resolve(root,p),'utf8'));
const schema=read(schemaPath),errors=read(errorPath),ajv=new Ajv({allErrors:true,schemaId:'auto',strictKeywords:true});
assert(ajv.validateSchema(schema));const valid=ajv.compile(schema);
assert.equal(errors.contract,'campusos.scope-delegation/v1');const codes=new Set(errors.errors.map((x)=>x.code));assert.equal(codes.size,8);
const base={contract:'campusos.scope-delegation/v1',
 actor:{kind:'admin',id:'admin-1',authentication_strength:'mfa'},
 management:{action:'plugin.grant.manage',active:true,expires_at_ms:5000},
 bound:{action:'knowledge.source.read',scope:{kind:'public_collection',ids:['public-a','public-b']},
  not_before_ms:1000,expires_at_ms:4000,required_strength:'password',status:'active',delegable:true},
 candidate:{recipient:{kind:'plugin',id:'plugin-1'},action:'knowledge.source.read',
  scope:{kind:'public_collection',ids:['public-a']},not_before_ms:1500,expires_at_ms:3000,required_strength:'mfa'},
 now_ms:1500,facts_version:'rev-1'};
const actionScopes={'knowledge.source.read':'public_collection','plugin.config.system.read':'system_config',
 'integration.webhook.invoke':'endpoint'};
function check(x){
 if(!valid(x))return 'scope.input_invalid';
 const {actor:a,management:m,bound:b,candidate:c,now_ms:now}=x;
 if(a.kind!=='admin'||a.authentication_strength!=='mfa')return 'scope.actor_denied';
 const requiredManagement=c.recipient.kind==='plugin'?'plugin.grant.manage':
  c.recipient.kind==='integration'?'integration.grant.manage':'identity.role.assign';
 if(m.action!==requiredManagement||!m.active||m.expires_at_ms<=now)return 'scope.management_denied';
 if(c.action==='personal.document.read'||b.action==='personal.document.read')return 'scope.non_delegable';
 if(c.recipient.kind==='user')return 'scope.recipient_mismatch';
 if(b.status!=='active'||!b.delegable||b.not_before_ms>now||b.expires_at_ms<=now)return 'scope.bound_inactive';
 if(actionScopes[c.action]!==c.scope.kind||actionScopes[b.action]!==b.scope.kind||c.action!==b.action ||
    c.scope.kind!==b.scope.kind||c.not_before_ms<Math.max(now,b.not_before_ms)||
    c.expires_at_ms>b.expires_at_ms||c.expires_at_ms<=c.not_before_ms||
    (b.required_strength==='mfa'&&c.required_strength!=='mfa'))return 'scope.outside_bounds';
 if(!c.scope.ids.every((id)=>b.scope.ids.includes(id)))return 'scope.outside_bounds';
 if(c.scope.kind==='endpoint'&&c.scope.ids.some((id)=>!/^https:\/\/[a-z0-9.-]+(:[1-9][0-9]*)?$/.test(id)||
     id.includes('*')||id.includes('localhost')||/https:\/\/[0-9.]+/.test(id)))return 'scope.outside_bounds';
 return null;
}
const cases=[['collection-subset',base,null]];
const add=(name,mutate,expected)=>{const x=structuredClone(base);mutate(x);cases.push([name,x,expected]);};
add('config-keys',(x)=>{x.bound.action='plugin.config.system.read';x.candidate.action=x.bound.action;x.bound.scope={kind:'system_config',ids:['display.locale','display.theme']};x.candidate.scope={kind:'system_config',ids:['display.locale']};},null);
add('exact-endpoint',(x)=>{x.candidate.recipient.kind='integration';x.management.action='integration.grant.manage';x.bound.action='integration.webhook.invoke';x.candidate.action=x.bound.action;x.bound.scope={kind:'endpoint',ids:['https://hooks.example.test']};x.candidate.scope={kind:'endpoint',ids:['https://hooks.example.test']};},null);
add('user-admin-role-cannot-grant-plugin',(x)=>{x.actor.kind='user';},'scope.actor_denied');
add('admin-no-mfa',(x)=>{x.actor.authentication_strength='password';},'scope.actor_denied');
add('management-wrong-action',(x)=>{x.management.action='identity.role.assign';},'scope.management_denied');
add('management-expired',(x)=>{x.management.expires_at_ms=1500;},'scope.management_denied');
add('management-revoked',(x)=>{x.management.active=false;},'scope.management_denied');
for(const status of ['suspended','revoked'])add('bound-'+status,(x)=>{x.bound.status=status;},'scope.bound_inactive');
add('bound-nondelegable',(x)=>{x.bound.delegable=false;},'scope.bound_inactive');
add('bound-expired',(x)=>{x.bound.expires_at_ms=1500;},'scope.bound_inactive');
add('wrong-action',(x)=>{x.candidate.action='plugin.config.system.read';},'scope.outside_bounds');
add('wrong-scope-kind',(x)=>{x.candidate.scope={kind:'system_config',ids:['display.locale']};},'scope.outside_bounds');
add('outside-collection',(x)=>{x.candidate.scope.ids=['private-c'];},'scope.outside_bounds');
add('wide-collection',(x)=>{x.candidate.scope.ids.push('private-c');},'scope.outside_bounds');
add('candidate-before-now',(x)=>{x.candidate.not_before_ms=1499;},'scope.outside_bounds');
add('candidate-after-bound',(x)=>{x.candidate.expires_at_ms=4001;},'scope.outside_bounds');
add('mfa-downgrade',(x)=>{x.bound.required_strength='mfa';x.candidate.required_strength='password';},'scope.outside_bounds');
add('private-document',(x)=>{x.bound.action='personal.document.read';x.candidate.action=x.bound.action;},'scope.non_delegable');
add('user-recipient',(x)=>{x.candidate.recipient.kind='user';x.management.action='identity.role.assign';},'scope.recipient_mismatch');
add('wildcard-endpoint',(x)=>{x.candidate.recipient.kind='integration';x.management.action='integration.grant.manage';x.bound.action='integration.webhook.invoke';x.candidate.action=x.bound.action;x.bound.scope={kind:'endpoint',ids:['https://*.example.test']};x.candidate.scope=structuredClone(x.bound.scope);},'scope.outside_bounds');
add('raw-secret',(x)=>{x.candidate.secret='token';},'scope.input_invalid');
const results=[];
for(const [name,input,expected] of cases){const before=JSON.stringify(input);assert.equal(check(input),expected,name);assert.equal(JSON.stringify(input),before,name+' mutated');assert(expected===null||codes.has(expected),name);results.push({name,expected:expected??'accepted',passed:true});}
const rechecks=[];
for(const [name,mutate,expected] of [
 ['revocation',(x)=>{x.bound.status='revoked';},'scope.bound_inactive'],
 ['bound-expiration',(x)=>{x.now_ms=4000;},'scope.bound_inactive'],
 ['management-expiration',(x)=>{x.now_ms=5000;},'scope.management_denied'],
 ['scope-narrowing',(x)=>{x.bound.scope.ids=['public-b'];},'scope.outside_bounds']
]){const x=structuredClone(base);assert.equal(check(x),null);mutate(x);assert.equal(check(x),expected);rechecks.push({name,passed:true});}
const virtual=resolve(root,'sdk/typescript/tests/__scope_delegation__.ts');
const source=[
 'import type { ScopeDelegationDecisionInput, ScopeDelegationErrorCode } from "../src/index";',
 'const good: ScopeDelegationDecisionInput = '+JSON.stringify(base)+';',
 '// @ts-expect-error no wildcard scope kind',
 'const scope: ScopeDelegationDecisionInput["candidate"]["scope"]["kind"] = "all";',
 '// @ts-expect-error no unknown action',
 'const action: ScopeDelegationDecisionInput["candidate"]["action"] = "host.db.query";',
 '// @ts-expect-error unknown error',
 'const code: ScopeDelegationErrorCode = "scope.allow";'
].join('\n');
for(const entry of ['index','scope-delegation']){
 const opts={noEmit:true,strict:true,exactOptionalPropertyTypes:entry!=='index',skipLibCheck:true,target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ESNext,moduleResolution:ts.ModuleResolutionKind.Bundler,types:[]};
 const host=ts.createCompilerHost(opts),get=host.getSourceFile.bind(host);
 host.getSourceFile=(path,language,onError,fresh)=>path===virtual?ts.createSourceFile(path,source.replace('../src/index','../src/'+entry),language,true):get(path,language,onError,fresh);
 const diagnostics=ts.getPreEmitDiagnostics(ts.createProgram([virtual],opts,host));
 assert.equal(diagnostics.length,0,ts.formatDiagnosticsWithColorAndContext(diagnostics,{getCanonicalFileName:(x)=>x,getCurrentDirectory:()=>root,getNewLine:()=>'\n'}));
}
const report={schema:'campusos.v12-g1-scope-delegation-acceptance/v1',generated_at:new Date().toISOString(),
 scope:'G1 non-board exact Scope, admin management authority, single complete bound, time and MFA subset',
 implementation:'passed',automation:'passed',target_environment:process.platform+' offline Node/Ajv/TypeScript contract acceptance passed',
 runtime_acceptance:'pending 02a/02c/03b transactional grants, machine/plugin principals and real endpoint enforcement',
 environment:{platform:process.platform,node:process.version,ajv:require('ajv/package.json').version,typescript:ts.version},
 fixtures:results,rechecks,files_sha256:Object.fromEntries([schemaPath,errorPath,'sdk/typescript/src/scope-delegation.ts','sdk/typescript/src/index.ts','scripts/check-v12-scope-delegation-contract.mjs'].map((p)=>[p,createHash('sha256').update(readFileSync(resolve(root,p))).digest('hex')]))};
const args=process.argv.slice(2);assert(args.length===0||(args.length===2&&args[0]==='--report'),'usage: [--report <path>]');
if(args.length)writeFileSync(resolve(args[1]),JSON.stringify(report,null,2)+'\n');
console.log('Scope delegation: '+results.length+' fixtures, '+rechecks.length+' rechecks, 1/3 TS positive/negative types passed');
