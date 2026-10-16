#!/usr/bin/env node
// Verify the G1 replacement map against the current source, plan and target contracts.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync, writeFileSync, existsSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const path='docs/api/v1.2旧机制删除与替换清单.json';
const overlayPath='docs/api/v1.2-03a旧机制退役覆盖.json';
const planPath='docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md';
const read=(p)=>readFileSync(resolve(root,p),'utf8');
const base=JSON.parse(read(path)),overlay=JSON.parse(read(overlayPath)),plan=read(planPath);
assert.equal(overlay.contract,'campusos.v12-03a-cutover/v1');
assert.equal(overlay.base,'v1.2旧机制删除与替换清单.json');
assert.equal(overlay.stage,'V12-03a');
assert.deepEqual(overlay.overrides.map((x)=>x.id).sort(),['plugin.legacy_manifest','plugin.v4_adapter','plugin.v4_ui_self_only']);
const map={...base,items:base.items.map((item)=>{
 const override=overlay.overrides.find((v)=>v.id===item.id);
 if(override)for(const key of Object.keys(override))assert(['id','replacement_stage','runtime_status','retired_evidence','replaced_evidence','retirement_boundary'].includes(key),'invalid override field '+key);
 if(override){assert.equal(item.replacement_stage,'V12-03a');assert.equal(override.replacement_stage,'V12-03a');}
 return {...item,...override};
})};
assert.equal(map.contract,'campusos.v12-g1-replacement-map/v1');
assert.equal(map.status,'target_frozen_runtime_pending');
assert(map.items.length>=15);
const ids=new Set(),results=[];
for(const item of map.items){
 assert(!ids.has(item.id),item.id);ids.add(item.id);
 assert(item.current_path.startsWith('internal/')||item.current_path.startsWith('pkg/')||item.current_path.startsWith('migrations/'));
 assert(item.current_needle.length>=10,item.id);
 const source=read(item.current_path),needlePresent=source.includes(item.current_needle);
 if(item.runtime_status==='retired'){
   assert(['plugin.legacy_manifest','plugin.v4_ui_self_only'].includes(item.id));
   assert.equal(item.retired_evidence,'v12-03a-exit-linux.json');
   const startup=read('internal/server/platform_modules.go');
   assert(!/\.LoadV4Releases\(|\.InstallV4DevelopmentSources\(|\.InstallFromPluginsDir\(/.test(startup));
   assert(!read('internal/plugin/v4_runtime.go').includes('func (m *Manager) RegisterV4Release'));
   assert(!existsSync(resolve(root,'cmd/campusosctl/plugin_v4_commands.go')));
   assert(!existsSync(resolve(root,'cmd/campusosctl/plugin_commands.go')));
   assert(read('cmd/campusosctl/plugin_v5_commands.go').includes('plugin.BuildPluginV5Package(target)'));
   assert(read('cmd/campusos-plugin-v4-check/main.go').includes('legacy_protocol_removed'));
   assert(read('cmd/campusos-plugin-v5-check/main.go').includes('plugin.ValidatePluginV5SourceDeclaration'));
   assert(item.retirement_boundary.length>20);
 }else if(item.runtime_status==='replaced'){
   // Replaced items lose their old anchor: code-anchored mechanisms must be
   // gone from the source; migration-anchored mechanisms live in the frozen
   // baseline and are removed by a later forward migration's DROP statement.
   const triggerMatch=item.current_needle.match(/^CREATE TRIGGER (\S+)/);
   if(triggerMatch){
     const name=triggerMatch[1];
     const removed=readdirSync(resolve(root,'migrations')).filter((f)=>f.endsWith('.up.sql'))
       .some((f)=>read('migrations/'+f).includes('DROP TRIGGER')&&read('migrations/'+f).includes(name));
     assert(removed,item.id+' trigger not removed by a forward migration');
   }else{
     assert(!needlePresent,item.id+' replaced but old anchor still present');
   }
 }else{
   assert.equal(item.runtime_status,'pending',item.id+' falsely marked implemented');
   assert(needlePresent,item.id+' source anchor drifted');
 }
 assert(plan.includes('| '+item.replacement_stage+' /'),item.id+' stage missing');
 assert(read('docs/api/'+item.target_contract).length>0,item.id+' target missing');
 assert(item.removal_acceptance.length>=10,item.id);
 results.push({id:item.id,source_anchor_present:needlePresent,target_contract_present:true,
   stage_present:true,runtime_status:item.runtime_status,passed:true});
}
for(const stage of ['V12-02a','V12-02d','V12-03a','V12-03b','V12-03c'])
 assert(results.some((x)=>map.items.find((item)=>item.id===x.id)?.replacement_stage===stage),stage);
const report={schema:'campusos.v12-g1-replacement-map-acceptance/v1',generated_at:new Date().toISOString(),
 scope:'G1 old mechanisms tracked to exact current code anchors, target contract and responsibility stage',
 implementation:'passed',automation:'passed',environment:{platform:process.platform,node:process.version},target_environment:process.platform+' offline source/contract mapping acceptance passed',
 runtime_acceptance:'pending per item in 02a/02d/03a/03b/03c; current paths intentionally remain live until replacements pass',
 entries:results,files_sha256:Object.fromEntries([path,overlayPath,planPath,'scripts/check-v12-03a-replacement-cutover.mjs'].map((p)=>[p,createHash('sha256').update(readFileSync(resolve(root,p))).digest('hex')]))};
const args=process.argv.slice(2);assert(args.length===0||(args.length===2&&args[0]==='--report'),'usage: [--report <path>]');
if(args.length)writeFileSync(resolve(args[1]),JSON.stringify(report,null,2)+'\n');
console.log('03a composed replacement map: '+results.length+' exact source anchors, target contracts and stages passed');
