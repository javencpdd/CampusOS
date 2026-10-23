#!/usr/bin/env node
// Compare the actual Go kernel with the independent frozen G1/Ajv oracle.
import assert from 'node:assert/strict';
import {readFileSync,writeFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {resolve} from 'node:path';
import vm from 'node:vm';
const require=createRequire(resolve('sdk/typescript/package.json')),Ajv=require('ajv');
const source=readFileSync('scripts/check-v12-plugin-v5-dispatch-contract.mjs','utf8');
const valid=new Ajv({allErrors:true}).compile(JSON.parse(readFileSync('docs/api/plugin-v5-dispatch-v1.schema.json','utf8')));
const sandbox={valid,structuredClone};
vm.createContext(sandbox);
vm.runInContext(source.slice(source.indexOf('const base='),source.indexOf('const results=[];'))+'\nglobalThis.fixtures=cases.map(([name,input,expected])=>({name,input,expected:expected??"accepted"}));globalThis.oracle=check;',sandbox);
const fixtures=JSON.parse(JSON.stringify(sandbox.fixtures));
const fixturePath='sdk/typescript/tests/plugin-v5-dispatch-native.fixtures.json';
if(process.argv[2]==='--generate') {writeFileSync(fixturePath,JSON.stringify(fixtures,null,2)+'\n');}
else {
 assert.deepEqual(JSON.parse(readFileSync(fixturePath,'utf8')),fixtures,'native fixtures drifted from frozen G1');
 for(const f of fixtures) assert.equal(sandbox.oracle(f.input)??'accepted',f.expected,f.name);
 if(process.argv[2]) {
  const actual=JSON.parse(readFileSync(process.argv[2],'utf8'));
  assert.deepEqual(actual,fixtures.map(f=>({name:f.name,actual:f.expected})));
 }
 console.log('03c: '+fixtures.length+(process.argv[2]?' actual Go decisions':' fixture inputs')+' match frozen G1/Ajv dispatch decisions');
}
