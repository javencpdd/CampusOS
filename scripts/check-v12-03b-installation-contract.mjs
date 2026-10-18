#!/usr/bin/env node
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, mkdirSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { resolve } from 'node:path';
import { execFileSync } from 'node:child_process';
const root=process.cwd(),require=createRequire(resolve(root,'sdk/typescript/package.json')),Ajv=require('ajv');
const ajv=new Ajv({allErrors:true});
for(const name of readdirSync('docs/api').filter(x=>x.startsWith('plugin-v5-')&&x.endsWith('.schema.json')))ajv.addSchema(JSON.parse(readFileSync('docs/api/'+name,'utf8')));
const base='https://campusos.local/schemas/plugin-v5-installation-v1.schema.json#/definitions/';
const admin=ajv.getSchema(base+'PluginV5AdminCommand'),user=ajv.getSchema(base+'PluginV5UserCommand'),operation=ajv.getSchema(base+'PluginV5Operation');
const request={version:'1.0.0',idempotency_key:'typed-1'};
for(const kind of ['install','enable','disable','upgrade'])for(const validate of [admin,user])assert(validate({...request,kind,expected_generation:'1'}),JSON.stringify(validate.errors));
assert(admin({...request,kind:'grant',approved:true,consumes:[],expected_revision:'0'}));
assert(!user({...request,kind:'grant',approved:true,consumes:[],expected_revision:'0'}));
assert(user({...request,kind:'consent',accepted:true,purpose_hash:'sha256:'+'a'.repeat(64),expected_revision:'0'}));
assert(!admin({...request,kind:'consent',accepted:true,purpose_hash:'sha256:'+'a'.repeat(64),expected_revision:'0'}));
for(const extra of [{owner_user_id:'2'},{observed:'ready'},{lease_generation:'1'},{actor_kind:'admin'},{database_url:'postgres://x'}])assert(!user({...request,kind:'install',expected_generation:'0',...extra}));
for(const generation of [1,-1,'-1','1.0','01'])assert(!user({...request,kind:'enable',expected_generation:generation}));
const safe={publisher:'campusos',plugin_key:'sample',version:'1.0.0',id:'a'.repeat(32),owner_user_id:'1',kind:'enable',status:'waiting',generation:'2',error_code:'plugin.runtime_unavailable',purge_data:false,attempts:1};
assert(operation(safe));for(const field of ['request_hash','idempotency_key','lease_generation','preparation','values'])assert(!operation({...safe,[field]:'private'}));
const work=resolve('.cache/v12-03b-completion/contracts');mkdirSync(work,{recursive:true});
const source=`import type { PluginV5InstallationCommand as U, PluginV5AdminInstallationCommand as A } from '${resolve('sdk/typescript/src/index')}';
const good:U={kind:'install',version:'1.0.0',idempotency_key:'x',expected_generation:'0'};
const grant:A={kind:'grant',version:'1.0.0',idempotency_key:'x',expected_revision:'0',approved:true,consumes:[]};
// @ts-expect-error owner comes from the current session
const owner:U={...good,owner_user_id:'2'};
// @ts-expect-error user commands cannot approve a grant
const userGrant:U={kind:'grant',version:'1.0.0',idempotency_key:'x',expected_revision:'0',approved:true,consumes:[]};
// @ts-expect-error Admin is not a user consent domain
const adminConsent:A={kind:'consent',version:'1.0.0',idempotency_key:'x',expected_revision:'0',accepted:true,purpose_hash:'x'};
// @ts-expect-error counters are decimal strings
const number:U={...good,expected_generation:2};
`;
writeFileSync(resolve(work,'types.ts'),source);
execFileSync('pnpm',['--dir','sdk/typescript','exec','tsc','--noEmit','--strict','--skipLibCheck','--target','ES2022','--moduleResolution','node',resolve(work,'types.ts')],{stdio:'pipe'});
console.log('03b installation: command/owner/domain/counter/private-response schema and TypeScript negative checks passed');
