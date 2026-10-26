#!/usr/bin/env node
// Real primary CLI, actual package bytes, independent SHA-256 and Ed25519.
import assert from 'node:assert/strict';
import {generateKeyPairSync,createHash,verify,sign} from 'node:crypto';
import {readFileSync,writeFileSync,mkdirSync,unlinkSync} from 'node:fs';
import {resolve,dirname,sep} from 'node:path';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const [binaryArg,workArg]=process.argv.slice(2);assert(binaryArg&&workArg);
const binary=resolve(binaryArg),work=resolve(workArg);assert(work.startsWith(resolve(root,'.cache')+sep));mkdirSync(work,{recursive:true});
const key=generateKeyPairSync('ed25519');
const pub=key.publicKey.export({format:'der',type:'spki'}).subarray(-32);
const seed=key.privateKey.export({format:'der',type:'pkcs8'}).subarray(-32);
const keyFile=resolve(work,'signing.private');writeFileSync(keyFile,Buffer.concat([seed,pub]).toString('base64'),{mode:0o600});
const trustPath=resolve(work,'trust.json');
const trust={contract:'campusos.plugin-trust/v1',keys:[{publisher:'campusos',key_id:'phase-test',public_key:pub.toString('base64'),status:'active'}]};
writeFileSync(trustPath,JSON.stringify(trust));
let processes=0;const checks=[];
function cli(args,accepted=true){
 const p=spawnSync(binary,['plugin',...args],{cwd:work,encoding:'utf8',timeout:15000,env:{PATH:process.env.PATH,TMPDIR:work}});processes++;
 assert.equal(p.error,undefined,args[0]);assert.equal(p.status,accepted?0:1,args[0]+': '+p.stderr);assert(!p.stdout.includes(seed.toString('base64')));
 return p.stdout?JSON.parse(p.stdout):null;
}
try{
 for(const name of ['campusos.pdf-viewer','campusos.chat-echo']){
  const archive=resolve(work,name+'.tar.gz'),envelope=resolve(work,name+'.release.json');
  const info=cli(['pack',resolve(root,'plugins',name),'--out',archive]);
  const bytes=readFileSync(archive),digest='sha256:'+createHash('sha256').update(bytes).digest('hex');assert.equal(info.package_digest,digest);assert.equal(info.installed,false);
  cli(['sign',archive,'--out',envelope,'--key-id','phase-test','--key-file',keyFile]);
  const release=JSON.parse(readFileSync(envelope,'utf8'));
  const message=Buffer.from('campusos.plugin-release/v5\0'+JSON.stringify({publisher:release.publisher,key:release.key,version:release.version,package_digest:release.package_digest,key_id:release.signature.key_id}));
  assert(verify(null,message,key.publicKey,Buffer.from(release.signature.value,'base64')),'independent Node signature');
  assert.equal(cli(['verify',archive,'--release',envelope,'--trust',trustPath]).signature_verified,true);
  release.signature.value=sign(null,message,key.privateKey).toString('base64');writeFileSync(envelope,JSON.stringify(release));
  assert.equal(cli(['verify',archive,'--release',envelope,'--trust',trustPath]).signature_verified,true,'Go verifies independent Node signer');
  cli(['pack',resolve(root,'plugins',name),'--out',archive],false); // no overwrite
  cli(['verify',archive],false); // no implicit unsigned trust
  trust.keys[0].status='revoked';writeFileSync(trustPath,JSON.stringify(trust));cli(['verify',archive,'--release',envelope,'--trust',trustPath],false);
  trust.keys[0].status='active';writeFileSync(trustPath,JSON.stringify(trust));
  const invalid=resolve(work,name+'.trailing.tar.gz');writeFileSync(invalid,Buffer.concat([bytes,Buffer.from('hidden')]));cli(['inspect',invalid],false);
  checks.push({name,package_digest:digest,package_size:bytes.length,independent_ed25519:'passed',revocation:'passed',unsigned_and_trailing_and_overwrite_denied:true});
 }
 for(const command of ['v4','install','dev'])cli([command,'untrusted'],false);
 const catalog=cli(['catalog']);assert.equal(catalog.host.items.length,0);assert.equal(catalog.extensions.points.length,3);
 writeFileSync(resolve(work,'package-cli-result.json'),JSON.stringify({schema:'campusos.v12-03a-package-cli/v1',status:'passed',platform:process.platform,actual_cli_processes:processes,cli_binary_sha256:createHash('sha256').update(readFileSync(binary)).digest('hex'),checks,independent_crypto:'Node Ed25519 and SHA-256 both directions',production_catalog:'real host metadata; zero callable capabilities until03c',no_install_or_execution:true},null,2)+'\n');
}finally{seed.fill(0);unlinkSync(keyFile)}
console.log('Actual v5 primary CLI processes passed: '+processes);
