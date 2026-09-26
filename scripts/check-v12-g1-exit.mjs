#!/usr/bin/env node
// G1 contract-stage exit: exact mapping to DTO, error, target evidence and later runtime owner.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const read=(p)=>readFileSync(resolve(root,p));
const json=(p)=>JSON.parse(read(p).toString('utf8'));
const path='docs/api/v1.2-G1合同退出矩阵.json',matrix=json(path);
assert.equal(matrix.contract,'campusos.v12-g1-exit-matrix/v1');
assert.equal(matrix.status,'target_contracts_frozen');
const dependency=json(matrix.dependency_evidence);
for(const field of ['implementation','automation','target_environment'])assert.equal(dependency[field],'passed','G0 '+field);
const required=['principal','thread_policy','resource_policy','board_delegation','scope_delegation','identity_domains','v5_shape','v5_release','v5_consumes','v5_provides','v5_manifest','host_resources','noninvasive_install','bounded_config','bidirectional_calls','events','lifecycle','old_mechanism_replacement'];
assert.deepEqual(matrix.clauses.map((x)=>x.id),required);
const results=[];
for(const item of matrix.clauses){
 assert.equal(item.contract_status,'passed',item.id);
 assert.equal(item.runtime_status,'pending',item.id);
 assert(item.runtime_owner.startsWith('V12-'),item.id);
 for(const key of ['schema','errors','dto','contract_doc','acceptance_evidence','checker'])assert(read(item[key]).length>0,item.id+' '+key);
 const evidence=json(item.acceptance_evidence);
 assert.equal(evidence.implementation,'passed',item.id+' implementation');
 assert.equal(evidence.automation,'passed',item.id+' automation');
 assert.equal(evidence.environment?.platform,'linux',item.id+' target platform');
 assert(evidence.target_environment.includes('passed'),item.id+' target acceptance');
 assert(evidence.runtime_acceptance.startsWith('pending')||evidence.runtime_acceptance.startsWith('not applicable'),item.id+' runtime boundary');
 const hashes=evidence.files_sha256??{};
 for(const p of [item.schema,item.dto,item.checker]){
  if(!hashes[p])continue;
  const actual=createHash('sha256').update(read(p)).digest('hex');
  assert.equal(actual,hashes[p],item.id+' source drift '+p);
 }
 results.push({id:item.id,implementation:'passed',automation:'passed',target_environment:'linux offline passed',
   runtime_status:'pending',evidence:item.acceptance_evidence,passed:true});
}
const report={schema:'campusos.v12-g1-exit/v1',generated_at:new Date().toISOString(),stage:'V12-G1',
 implementation:'passed',automation:'passed',target_environment:'passed',
 scope:'18 G1 external target contract clauses with Linux offline acceptance; runtime acceptance remains in assigned stages',
 dependency:'V12-G0 passed',clauses:results,
 runtime_acceptance:'pending V12-01a/01b/02a/02d/03a/03b/03c/04/08a/14c; G1 is contract freeze only',
 files_sha256:{[path]:createHash('sha256').update(read(path)).digest('hex'),
  'scripts/check-v12-g1-exit.mjs':createHash('sha256').update(read('scripts/check-v12-g1-exit.mjs')).digest('hex')}};
const args=process.argv.slice(2);assert(args.length===0||(args.length===2&&args[0]==='--report'),'usage: [--report <path>]');
if(args.length)writeFileSync(resolve(args[1]),JSON.stringify(report,null,2)+'\n');
console.log('V12-G1 exit: G0 passed, '+results.length+' DTO/error/evidence clauses passed on Linux offline target');
