#!/usr/bin/env node
// Only PDF UI assets; package metadata cannot invoke this explicit author step.
import {readFileSync,writeFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {dirname,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const digest=(p)=>'sha256:'+createHash('sha256').update(readFileSync(resolve(root,p))).digest('hex');
for(const directory of ['plugins/campusos.pdf-viewer']){
 const path=resolve(root,directory,'plugin.json');const m=JSON.parse(readFileSync(path,'utf8'));
 if(m.api_version!=='campusos.plugin/v5')throw new Error('v5 example protocol required');
 for(const ui of m.ui??[]){
  if(ui.entrypoint!==`dist/${ui.audience}/index.html`||!['user','admin'].includes(ui.audience))throw new Error('unexpected example UI path');
  ui.digest=digest(directory+'/'+ui.entrypoint);
 }
 writeFileSync(path,JSON.stringify(m,null,2)+'\n');
}
