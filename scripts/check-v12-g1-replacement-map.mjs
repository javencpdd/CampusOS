#!/usr/bin/env node
// Verify the G1 replacement map against the current source, plan and target contracts.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const path='docs/api/v1.2旧机制删除与替换清单.json';
const planPath='docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md';
const read=(p)=>readFileSync(resolve(root,p),'utf8');
const map=JSON.parse(read(path)),plan=read(planPath);
assert.equal(map.contract,'campusos.v12-g1-replacement-map/v1');
assert.equal(map.status,'target_frozen_runtime_pending');
assert(map.items.length>=15);
const ids=new Set(),results=[];
for(const item of map.items){
 assert(!ids.has(item.id),item.id);ids.add(item.id);
 assert(item.current_path.startsWith('internal/')||item.current_path.startsWith('pkg/')||item.current_path.startsWith('migrations/'));
 assert(item.current_needle.length>=10,item.id);
 assert(read(item.current_path).includes(item.current_needle),item.id+' source anchor drifted');
 assert(plan.includes('| '+item.replacement_stage+' /'),item.id+' stage missing');
 assert(read('docs/api/'+item.target_contract).length>0,item.id+' target missing');
 assert.equal(item.runtime_status,'pending',item.id+' falsely marked implemented');
 assert(item.removal_acceptance.length>=10,item.id);
 results.push({id:item.id,source_anchor_present:true,target_contract_present:true,
   stage_present:true,runtime_status:'pending',passed:true});
}
for(const stage of ['V12-02a','V12-02d','V12-03a','V12-03b','V12-03c'])
 assert(results.some((x)=>map.items.find((item)=>item.id===x.id)?.replacement_stage===stage),stage);
const report={schema:'campusos.v12-g1-replacement-map-acceptance/v1',generated_at:new Date().toISOString(),
 scope:'G1 old mechanisms tracked to exact current code anchors, target contract and responsibility stage',
 implementation:'passed',automation:'passed',environment:{platform:process.platform,node:process.version},target_environment:process.platform+' offline source/contract mapping acceptance passed',
 runtime_acceptance:'pending per item in 02a/02d/03a/03b/03c; current paths intentionally remain live until replacements pass',
 entries:results,files_sha256:Object.fromEntries([path,planPath,'scripts/check-v12-g1-replacement-map.mjs'].map((p)=>[p,createHash('sha256').update(readFileSync(resolve(root,p))).digest('hex')]))};
const args=process.argv.slice(2);assert(args.length===0||(args.length===2&&args[0]==='--report'),'usage: [--report <path>]');
if(args.length)writeFileSync(resolve(args[1]),JSON.stringify(report,null,2)+'\n');
console.log('G1 replacement map: '+results.length+' exact source anchors, target contracts and stages passed');
