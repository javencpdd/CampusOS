// Built Admin/Web assets proxy every API request to the actual cmd/server.
// Route delay below holds a real fetched response to exercise identity races.
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
 const context = await browser.newContext({ viewport: { width:1280,height:1000 } }); context.setDefaultTimeout(15000)
 const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message))
 await page.goto(admin.origin+'/login?redirect=/plugin-config')
 await page.locator('input[autocomplete="username"]').fill(cfg.adminUsername); await page.locator('input[autocomplete="current-password"]').fill(cfg.password)
 await page.getByRole('button',{name:'登录',exact:true}).click(); await page.waitForURL(admin.origin+'/plugin-config')
 await page.getByRole('heading',{name:'插件系统配置',exact:true}).waitFor()
 await page.getByText('尚无可查看的插件包。',{exact:true}).waitFor(); checks.push('actual Admin login, current profile and empty owned catalog')
 const identity = async () => ({ token:await page.evaluate(source=>eval('('+source+')')('admin')?.token,findStore.toString()), csrf:await page.evaluate(()=>document.cookie.split('; ').find(x=>x.startsWith('campusos_admin_csrf='))?.split('=').slice(1).join('=')||'') })
 const api = async(method,path,body,csrf=true) => {
  const id=await identity(); const headers={Authorization:'Bearer '+id.token,'X-CampusOS-Client':'browser',Cookie:'campusos_admin_csrf='+id.csrf,...(csrf?{'X-Admin-CSRF-Token':id.csrf}:{})}
  const multipart=body instanceof FormData; if(body&&!multipart)headers['Content-Type']='application/json'
  const r=await fetch(cfg.backend+'/api/v1'+path,{method,headers,...(body?{body:multipart?body:JSON.stringify(body)}:{})});return {status:r.status,data:await r.json()}
 }
 // A real MFA-gated command opens the host's existing enrollment/step-up UI.
 let r=await api('POST','/admin/plugin-v5/campusos/config-form/commands',{kind:'install',version:'1.0.0',idempotency_key:'before-mfa'})
 assert.equal(r.status,403)
 await page.getByRole('button',{name:'确认管理员身份',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'确认管理员身份'});await dialog.waitFor()
 await dialog.getByRole('button',{name:'开始配置管理员认证器',exact:true}).click()
 const secretInput=dialog.locator('input[readonly]');await secretInput.waitFor();const secret=await secretInput.inputValue();const step=Math.floor(Date.now()/30000)
 const code=dialog.locator('input[autocomplete="one-time-code"]');await code.fill(totp(secret,step));await dialog.getByRole('button',{name:'完成配置',exact:true}).click()
 await dialog.getByText('配置完成。请等待认证器显示下一组验证码，再确认管理员身份。',{exact:true}).waitFor()
 await code.fill(totp(secret,step+1));await dialog.getByRole('button',{name:'确认身份',exact:true}).click();await dialog.waitFor({state:'hidden'})
 checks.push('password-only command denied; actual own MFA enrollment and recent step-up')
 for(const p of cfg.packages){const form=new FormData();form.append('package',new Blob([await readFile(p.archive)]));form.append('release',new Blob([await readFile(p.release)]));form.append('idempotency_key','import-'+p.version);r=await api('POST','/admin/plugin-v5/packages',form);assert.equal(r.status,200)}
 r=await api('POST','/admin/plugin-v5/campusos/config-form/commands',{kind:'install',version:'1.0.0',idempotency_key:'install-config-ui'});assert.equal(r.status,200)
 checks.push('actual immutable signed package import and global installation, no fake runtime')
 const refresh=async()=>{const pending=page.waitForResponse(r=>r.request().method()==='GET'&&new URL(r.url()).pathname==='/api/v1/admin/plugin-v5');await page.getByRole('button',{name:'重新加载',exact:true}).click();assert.equal((await pending).status(),200)}
 const select=async(version)=>{await page.getByRole('button',{name:new RegExp('campusos/config-form@'+version.replaceAll('.','\\.'))}).click()}
 await refresh();await select('1.0.0');await page.getByLabel('显示名称',{exact:true}).waitFor()
 assert.equal(await page.getByLabel('显示名称',{exact:true}).inputValue(),'初始配置');assert.equal(await page.getByLabel('显示提示',{exact:true}).isChecked(),false)
 checks.push('server-defined text and boolean defaults; false and omitted optional fields preserved')
 await page.getByLabel('显示名称',{exact:true}).fill('浏览器配置');await page.getByLabel('设置显示模式',{exact:true}).check();await page.getByLabel('显示模式',{exact:true}).selectOption({label:'详细'})
 let pending=page.waitForResponse(r=>r.request().method()==='POST'&&new URL(r.url()).pathname.endsWith('/config-form/commands'))
 await page.getByRole('button',{name:'保存普通配置',exact:true}).click();assert.equal((await pending).status(),200)
 await page.getByText('普通配置已保存。运行状态以服务器当前状态为准。',{exact:true}).waitFor()
 assert.equal(await page.getByLabel('显示名称',{exact:true}).inputValue(),'浏览器配置')
 assert.equal(await page.getByLabel('显示提示',{exact:true}).isChecked(),false)
 assert.ok((await page.getByLabel('系统配置详情').textContent()).includes('期望状态：disabled'))
 checks.push('real UI save, CAS revision, independent audited operation and authoritative read-back; never claims runtime applied')
 const persisted=await page.evaluate(()=>JSON.stringify(localStorage)+JSON.stringify(sessionStorage));assert.ok(!persisted.includes('浏览器配置')&&!persisted.includes(secret))
 await page.reload();await select('1.0.0');await page.getByLabel('显示名称',{exact:true}).waitFor();assert.equal(await page.getByLabel('显示名称',{exact:true}).inputValue(),'浏览器配置')
 checks.push('reload recovers PG configuration; no config or MFA secret in persistent browser storage')
 await page.getByLabel('显示名称',{exact:true}).fill('');let writes=0;const count=req=>{if(req.method()==='POST'&&req.url().endsWith('/config-form/commands'))writes++};page.on('request',count)
 await page.getByRole('button',{name:'保存普通配置',exact:true}).click();await page.getByRole('alert').filter({hasText:'至少需要'}).waitFor();assert.equal(writes,0);page.off('request',count)
 checks.push('required/length failure rejected before network')
 await page.getByLabel('显示名称',{exact:true}).fill('过期草稿')
 const detail=await api('GET','/admin/plugin-v5/campusos/config-form/1.0.0')
 r=await api('POST','/admin/plugin-v5/campusos/config-form/commands',{kind:'configure',version:'1.0.0',expected_revision:detail.data.data.configuration.revision,idempotency_key:'concurrent-writer',values:{label:'并发更新',enabled:false},secret_refs:{}});assert.equal(r.status,200)
 pending=page.waitForResponse(r=>r.request().method()==='POST'&&new URL(r.url()).pathname.endsWith('/config-form/commands'))
 await page.getByRole('button',{name:'保存普通配置',exact:true}).click();assert.equal((await pending).status(),409);await page.getByText('配置已被其他操作修改，请重新加载后再编辑。',{exact:true}).waitFor();assert.equal(await page.getByRole('form',{name:'普通配置'}).count(),0)
 checks.push('real concurrent writer wins once; stale UI revision is409 and editor closes without retry')
 await refresh();await select('1.0.0');await page.getByLabel('显示名称',{exact:true}).waitFor();assert.equal(await page.getByLabel('显示名称',{exact:true}).inputValue(),'并发更新')
 r=await api('POST','/admin/plugin-v5/campusos/config-form/commands',{kind:'configure',version:'1.0.0',expected_revision:'0',idempotency_key:'no-csrf',values:{label:'bad',enabled:false}},false);assert.equal(r.status,403)
 checks.push('actual browser-command CSRF nonce required')
 await select('2.0.0');await page.getByRole('alert').filter({hasText:'数字'}).waitFor();assert.equal(await page.getByRole('button',{name:'保存普通配置',exact:true}).count(),0)
 checks.push('numeric definition closed without unsafe JSON number round-trip')
 await select('3.0.0');await page.getByRole('alert').filter({hasText:'没有系统配置'}).waitFor();assert.equal(await page.getByRole('form',{name:'普通配置'}).count(),0)
 checks.push('user-only scope cannot become Admin system editing')
 // Hold a fetched real response, select another version, then release the old one.
 let release,received,finished;const held=new Promise(r=>{release=r});const fetched=new Promise(r=>{received=r});const fulfilled=new Promise(r=>{finished=r})
 const handler=async route=>{const real=await route.fetch();received();await held;await route.fulfill({response:real});finished()}
 await page.route('**/api/v1/admin/plugin-v5/campusos/config-form/1.0.0',handler);await select('1.0.0');await fetched;await select('2.0.0');await page.getByRole('alert').filter({hasText:'数字'}).waitFor();release();await fulfilled;await page.unroute('**/api/v1/admin/plugin-v5/campusos/config-form/1.0.0',handler)
 assert.equal(await page.getByRole('form',{name:'普通配置'}).count(),0);checks.push('late actual detail cannot repopulate a different selected version')
 await select('1.0.0');await page.getByLabel('显示名称',{exact:true}).waitFor()
 const control=async path=>{const r=await fetch(cfg.control+path,{method:'POST',headers:{Authorization:'Bearer '+cfg.controlKey}});assert.equal(r.status,204)}
 await control('/expire-mfa');await page.getByLabel('显示名称',{exact:true}).fill('过期认证不能保存')
 pending=page.waitForResponse(r=>r.request().method()==='POST'&&new URL(r.url()).pathname.endsWith('/config-form/commands'))
 await page.getByRole('button',{name:'保存普通配置',exact:true}).click();assert.equal((await pending).status(),403);await page.getByRole('button',{name:'确认管理员身份',exact:true}).click();await dialog.waitFor();await dialog.getByRole('button',{name:'取消',exact:true}).click();await dialog.waitFor({state:'hidden'})
 assert.equal(await page.getByRole('form',{name:'普通配置'}).count(),0);await control('/restore-mfa')
 checks.push('current PG MFA expiry denies cached UI command; no automatic retry')
 // Current permission loss fences a previously fetched private response too.
 await refresh();let releaseLate,gotLate,finishedLate;const delay=new Promise(r=>{releaseLate=r});const got=new Promise(r=>{gotLate=r});const fulfilledLate=new Promise(r=>{finishedLate=r})
 const late=async route=>{const real=await route.fetch();gotLate();await delay;await route.fulfill({response:real});finishedLate()}
 await page.route('**/api/v1/admin/plugin-v5/campusos/config-form/1.0.0',late);await select('1.0.0');await got;await control('/revoke-configure')
 await page.evaluate(async source=>{await eval('('+source+')')('admin').restore()},findStore.toString())
 await page.getByRole('alert').filter({hasText:'身份或权限已变化'}).waitFor();releaseLate();await fulfilledLate;await page.unroute('**/api/v1/admin/plugin-v5/campusos/config-form/1.0.0',late)
 assert.equal(await page.getByRole('form',{name:'普通配置'}).count(),0);checks.push('current role revocation/profile refresh clears data and rejects late actual response')
 await refresh();await select('1.0.0');await page.getByText('当前管理员没有修改插件配置的权限。',{exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'保存普通配置',exact:true}).count(),0)
 r=await api('POST','/admin/plugin-v5/campusos/config-form/commands',{kind:'configure',version:'1.0.0',expected_revision:'0',idempotency_key:'revoked-permission',values:{label:'bad',enabled:false},secret_refs:{}});assert.equal(r.status,403)
 checks.push('read-only Admin retains diagnostics; actual forged write denied after permission removal')
 await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2));checks.push('mobile configuration has no horizontal overflow')
 const before=await identity();await control('/revoke-account');const revoked=await fetch(cfg.backend+'/api/v1/admin/plugin-v5',{headers:{Authorization:'Bearer '+before.token}});assert.notEqual(revoked.status,200)
 await page.reload();await page.waitForURL(/\/login/);checks.push('suspended current account cannot read; route returns to independent Admin login')
 assert.deepEqual(errors,[])
 await writeFile(cfg.result,JSON.stringify({status:'passed',mocked_api:false,actual_cmd_server:true,checks,browser_errors:errors},null,2)+'\n')
 console.log('V12-04 actual configuration browser passed: '+checks.length+' checks')
} finally {if(browser)await browser.close();await new Promise(r=>admin.server.close(r))}
