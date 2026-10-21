// Built Admin assets and real browser fetch reach the actual cmd/server.
// This09a API target does not claim a Knowledge management/search page.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile, writeFile } from 'node:fs/promises'
import { createServer, request as proxyRequest } from 'node:http'
import { createHmac } from 'node:crypto'
import { extname, resolve, sep } from 'node:path'
const cfg = JSON.parse(await readFile(process.argv[2], 'utf8'))
const require = createRequire(resolve('web/package.json'))
const { chromium } = require('playwright-core')
async function staticProxy(folder) {
 const root = resolve(folder)
 const server = createServer(async (req, res) => {
  if (req.url.startsWith('/api/')) {
   const upstream = proxyRequest(new URL(req.url, cfg.backend), { method: req.method, headers: req.headers }, response => { res.writeHead(response.statusCode, response.headers); response.pipe(res) })
   res.on('close', () => upstream.destroy())
   upstream.on('error', () => { if (!res.headersSent) res.writeHead(502); res.end() }); req.pipe(upstream); return
  }
  try {
   const path = resolve(root, '.' + new URL(req.url, 'http://localhost').pathname)
   if (path !== root && !path.startsWith(root + sep)) { res.writeHead(404).end(); return }
   const file = extname(path) ? path : resolve(root, 'index.html')
   const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.ico': 'image/x-icon' }
   const data = await readFile(file)
   res.writeHead(200, { 'Content-Type': types[extname(file)] || 'application/octet-stream' }).end(data)
  } catch { if (!res.headersSent) res.writeHead(404); res.end() }
 })
 await new Promise(r => server.listen(0, '127.0.0.1', r))
 return { server, origin: `http://127.0.0.1:${server.address().port}` }
}
function totp(secret, step) {
 const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'; let bits = 0, acc = 0; const bytes = []
 for (const c of secret.replace(/=+$/, '')) { const value = alphabet.indexOf(c); assert.ok(value >= 0); acc = (acc << 5) | value; bits += 5; if (bits >= 8) { bits -= 8; bytes.push((acc >> bits) & 255) } }
 const message = Buffer.alloc(8); message.writeBigUInt64BE(BigInt(step)); const digest = createHmac('sha1', Buffer.from(bytes)).update(message).digest(); const offset = digest.at(-1) & 15
 return String((digest.readUInt32BE(offset) & 0x7fffffff) % 1000000).padStart(6, '0')
}
// Pinia's production injection key is a Symbol, so inspect the actual app's
// providers rather than assuming a development-only global or replacing a store.
const findStore = (name) => {
 const provides = document.querySelector('#app')?.__vue_app__?._context.provides
 const pinia = provides && Reflect.ownKeys(provides).map(k => provides[k]).find(value => value?._s instanceof Map)
 return pinia?._s.get(name)
}
const admin = await staticProxy('admin/dist'); const checks = [], errors = []
let browser
try {
 browser = await chromium.launch({ executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox'] })
 const context=await browser.newContext({viewport:{width:1280,height:900}});context.setDefaultTimeout(15000)
 const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message))
 await page.goto(admin.origin+'/login?redirect=/')
 await page.locator('input[autocomplete="username"]').fill(cfg.adminUsername);await page.locator('input[autocomplete="current-password"]').fill(cfg.password)
 await page.getByRole('button',{name:'登录',exact:true}).click();await page.waitForURL(admin.origin+'/')
 const api=async(method,path,body,csrf=true)=>await page.evaluate(async({method,path,body,csrf,find})=>{
  const store=eval('('+find+')')('admin');const token=store?.token
  const cookie=document.cookie.split('; ').find(v=>v.startsWith('campusos_admin_csrf='))?.split('=').slice(1).join('=')
  const r=await fetch('/api/v1'+path,{method,headers:{Authorization:'Bearer '+token,'X-CampusOS-Client':'browser',...(csrf?{'X-Admin-CSRF-Token':decodeURIComponent(cookie||'')}:{ }),...(body!==undefined?{'Content-Type':'application/json'}:{})},...(body!==undefined?{body:JSON.stringify(body)}:{})})
  return {status:r.status,json:await r.json(),cache:r.headers.get('cache-control')}
 },{method,path,body,csrf,find:findStore.toString()})
 const manage='/admin/knowledge'
 let r=await api('GET',manage+'/collections');assert.equal(r.status,200);checks.push('actual independent Admin SPA login and permission-checked Knowledge inventory')
 const persisted=r.json.data.find(v=>v.title==='concurrent');assert.ok(persisted);let persistedRead=await fetch(cfg.backend+'/api/v1/knowledge/collections/'+persisted.id+'/sources');assert.equal(persistedRead.status,200);persistedRead=await persistedRead.json();assert.equal(persistedRead.data.total,3);assert.ok(persistedRead.data.items.some(v=>v.source.content==='09a newer article text'));checks.push('new actual cmd/server process reloads persisted collection rules members and live source revisions')
 r=await api('POST',manage+'/collections',{title:'Browser collection',status:'active'});assert.equal(r.status,403);checks.push('password-only Admin cannot create collection')
 r=await api('POST','/admin/identity/mfa/enroll',{});assert.equal(r.status,200);const secret=new URL(r.json.data.otpauth_url).searchParams.get('secret');const step=Math.floor(Date.now()/30000)
 r=await api('POST','/admin/identity/mfa/confirm',{code:totp(secret,step)});assert.equal(r.status,200)
 r=await api('POST','/admin/identity/mfa/step-up',{code:totp(secret,step+1)});assert.equal(r.status,200);checks.push('real current Admin TOTP enrollment confirmation and step-up')
 r=await api('POST',manage+'/collections',{title:'Browser collection',status:'active'},false);assert.equal(r.status,403);checks.push('browser command without independent Admin CSRF is denied')
 r=await api('POST',manage+'/collections',{title:'Browser collection',status:'active'});assert.equal(r.status,200);let collection=r.json.data;assert.ok(collection.id.startsWith('kc_'));assert.match(collection.version,/^[1-9][0-9]*$/)
 r=await api('POST',manage+'/officials',{title:'Browser official',content:'OFFICIAL_BROWSER_BODY',format:'markdown',status:'draft'});assert.equal(r.status,200);let official=r.json.data
 r=await api('POST',manage+'/collections/'+collection.id+'/selections',{kind:'official',source_id:official.id,active:true,collection_version:collection.version});assert.equal(r.status,404);checks.push('draft official source cannot be selected even by managing Admin')
 const saveOfficial=async(status,content=official.content)=>{const r=await api('PUT',manage+'/officials/'+official.id,{title:official.title,content,format:official.format,status,version:official.version});assert.equal(r.status,200);official=r.json.data}
 await saveOfficial('published')
 const select=async(kind,source_id)=>{const r=await api('POST',manage+'/collections/'+collection.id+'/selections',{kind,source_id,active:true,collection_version:collection.version});assert.equal(r.status,200);collection=r.json.data.collection;return r.json.data}
 let state=await select('official',official.id);let single=await select('thread',cfg.thread)
 r=await api('POST',manage+'/collections/'+collection.id+'/selections',{kind:'thread',source_id:cfg.privateThread,active:true,collection_version:collection.version});assert.equal(r.status,404);assert.ok(!JSON.stringify(r.json).includes('PRIVATE_BROWSER_CONTENT'))
 checks.push('published official and single public thread materialize separate references; private owner source remains denied')
 const publicRead=async(path)=>{const r=await fetch(cfg.backend+'/api/v1'+path);return {status:r.status,json:await r.json(),cache:r.headers.get('cache-control')}}
 const path='/knowledge/collections/'+collection.id+'/sources'
 r=await publicRead(path+'?page=1&page_size=1');assert.equal(r.status,200);assert.equal(r.json.data.total,2);assert.equal(r.json.data.items.length,1);assert.ok(r.cache.includes('no-store'))
 r=await publicRead(path);assert.equal(r.status,200);let source=r.json.data.items.find(v=>v.source.reference.kind==='community.thread');let off=r.json.data.items.find(v=>v.source.reference.kind==='knowledge.official');assert.equal(off.source.content,'OFFICIAL_BROWSER_BODY');assert.equal(source.source.content,'PUBLIC_BROWSER_CONTENT body');checks.push('anonymous real HTTP source pages use exact current visible total and no-store')
 let control=await fetch(cfg.control+'/make-private',{method:'POST',headers:{Authorization:'Bearer '+cfg.controlKey}});assert.equal(control.status,204)
 r=await publicRead(path);assert.equal(r.status,200);assert.equal(r.json.data.total,1);assert.ok(!JSON.stringify(r.json).includes('PRIVATE_AFTER_WHITELIST'))
 r=await publicRead(path+'/'+source.member_id);assert.equal(r.status,404);checks.push('source made private after selection disappears from detail list and total immediately')
 control=await fetch(cfg.control+'/make-public',{method:'POST',headers:{Authorization:'Bearer '+cfg.controlKey}});assert.equal(control.status,204)
 r=await publicRead(path+'/'+source.member_id+'?revision='+source.revision);assert.equal(r.status,409);checks.push('saved collection reference cannot revive after public-to-private-to-public ABA')
 await saveOfficial('published','OFFICIAL_BROWSER_NEW')
 r=await publicRead(path+'/'+off.member_id+'?revision='+off.revision);assert.equal(r.status,409)
 r=await publicRead(path+'/'+off.member_id);assert.equal(r.status,200);assert.equal(r.json.data.source.content,'OFFICIAL_BROWSER_NEW')
 await saveOfficial('withdrawn');r=await publicRead(path);assert.equal(r.json.data.total,1);checks.push('official revision edit and withdrawal are live; old conditions fail')
 r=await api('GET',manage+'/officials/'+official.id+'/revisions');assert.equal(r.status,200);assert.equal(r.json.data.items.length,4);assert.equal(r.json.data.items[0].snapshot.status,'withdrawn');checks.push('official revision history has current immutable snapshots and typed Admin authorship')
 const rule=single.selections.find(v=>v.kind==='thread')
 r=await api('PUT',manage+'/collections/'+collection.id+'/selections/'+rule.id,{kind:rule.kind,source_id:rule.source_id,active:false,version:rule.version,collection_version:collection.version});assert.equal(r.status,200);collection=r.json.data.collection
 r=await publicRead(path);assert.equal(r.json.data.total,0)
 r=await api('POST',manage+'/collections/'+collection.id+'/selections/'+rule.id+'/refresh',{collection_version:collection.version,selection_version:rule.version,page:1});assert.equal(r.status,409);checks.push('withdrawn rule removes materialized reference; old refresh cannot recreate it')
 r=await api('PUT',manage+'/collections/'+collection.id,{title:'stale',status:'active',version:state.collection.version});assert.equal(r.status,409);checks.push('stale collection version cannot overwrite concurrent management changes')
 control=await fetch(cfg.control+'/withdraw-permission',{method:'POST',headers:{Authorization:'Bearer '+cfg.controlKey}});assert.equal(control.status,204)
 r=await api('POST',manage+'/collections',{title:'denied',status:'active'});assert.equal(r.status,403);checks.push('current permission revocation denies still-signed MFA Admin')
 control=await fetch(cfg.control+'/suspend-admin',{method:'POST',headers:{Authorization:'Bearer '+cfg.controlKey}});assert.equal(control.status,204)
 r=await api('GET',manage+'/collections');assert.equal(r.status,401);checks.push('current Admin account suspension denies management inventory')
 // Public Knowledge HTTP remains a source reader; there is no Integration,
 // plugin, MCP or model grant derived from collection membership.
 const logs=await readFile(cfg.serverLog,'utf8');for(const value of [secret,cfg.password,cfg.controlKey])assert.ok(!logs.includes(value))
 assert.deepEqual(errors,[])
 await writeFile(cfg.result,JSON.stringify({status:'passed',mocked_api:false,actual_cmd_server:true,knowledge_ui:false,checks,browser_errors:errors},null,2)+'\n')
 console.log('Knowledge actual backend browser passed: '+checks.length+' checks')
}finally{if(browser)await browser.close();await new Promise(r=>admin.server.close(r))}
