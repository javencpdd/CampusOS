// Built Web assets, normal Server/router and owned PostgreSQL. Transport faults
// only abort/delay a connection; no application response data is fabricated.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile, writeFile } from 'node:fs/promises'
import { createServer, request as proxyRequest } from 'node:http'
import { extname, resolve, sep } from 'node:path'
const cfg = JSON.parse(await readFile(process.argv[2], 'utf8'))
const require = createRequire(resolve('web/package.json'))
const { chromium } = require('playwright-core')
const checks = [], errors = [], knowledgeRequests = []
let abortSearch = false, delaySource = false, releaseSource, sourceHeld
const root = resolve(cfg.webDist)
const web = createServer(async (req, res) => {
  if (req.url.startsWith('/api/')) {
    const path = new URL(req.url, cfg.backend).pathname
    if (path.startsWith('/api/v1/knowledge/')) knowledgeRequests.push({ path, query: new URL(req.url, cfg.backend).search, cookie: Boolean(req.headers.cookie), authorization: Boolean(req.headers.authorization) })
    if (abortSearch && path.endsWith('/search')) { req.socket.destroy(); return }
    const upstream = proxyRequest(new URL(req.url, cfg.backend), { method: req.method, headers: req.headers }, async response => {
      if (delaySource && path.endsWith('/source')) {
        response.pause()
        await new Promise(r => { releaseSource = r; sourceHeld?.() })
      }
      if (res.destroyed) { response.destroy(); return }
      res.writeHead(response.statusCode, response.headers); response.pipe(res)
    })
    res.on('close', () => upstream.destroy())
    upstream.on('error', () => { if (!res.headersSent) res.writeHead(502); res.end() })
    req.pipe(upstream); return
  }
  try {
    const path = resolve(root, '.' + new URL(req.url, 'http://localhost').pathname)
    if (path !== root && !path.startsWith(root + sep)) { res.writeHead(404).end(); return }
    const file = extname(path) ? path : resolve(root, 'index.html')
    const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.ico': 'image/x-icon' }
    const bytes = await readFile(file); res.writeHead(200, { 'Content-Type': types[extname(file)] || 'application/octet-stream' }).end(bytes)
  } catch { if (!res.headersSent) res.writeHead(404); res.end() }
})
await new Promise(r => web.listen(0, '127.0.0.1', r))
const origin = `http://127.0.0.1:${web.address().port}`

const control = async path => {const r=await fetch(cfg.control+path,{method:'POST',headers:{Authorization:'Bearer '+cfg.controlKey}});assert.equal(r.status,200);return r.headers.get('content-type')?.includes('json')?r.json():null}
const check=name=>checks.push(name)
const login=async(page)=>{
 await page.goto(origin+'/knowledge/private');await page.locator('input[type=email]').fill(cfg.email);await page.locator('input[type=password]').fill(cfg.password)
 await page.getByRole('main').getByRole('button',{name:'登录',exact:true}).click();await page.waitForURL(url=>url.pathname==='/');await page.goto(origin+'/knowledge/private');await page.getByRole('heading',{name:'我的私人知识',exact:true}).waitFor()
}
const panel=page=>page.getByRole('region',{name:'有据问答',exact:true})
const open=async page=>{await page.locator(`li[data-selection-id="${cfg.selectionID}"]`).getByRole('button',{name:'对这一项提问',exact:true}).click();await panel(page).getByLabel('问答问题',{exact:true}).fill(cfg.question)}
const prepare=async page=>{await panel(page).getByRole('button',{name:'核对本次发送内容',exact:true}).click();await panel(page).getByRole('heading',{name:'本次独立发送核对'}).waitFor()}
let browser
try {
 browser=await chromium.launch({executablePath:process.env.CHROME_BIN||'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']})
 const desktop=await browser.newContext({viewport:{width:1280,height:900}});desktop.setDefaultTimeout(20000)
 const page=await desktop.newPage();page.on('pageerror',e=>errors.push(e.message));let unsafeFetch=false;page.on('request',r=>{if(r.url().includes('private-must-not-fetch'))unsafeFetch=true})
 await login(page);await open(page)
 const before=await control('/counts');await prepare(page);const after=await control('/counts');assert.equal(before.model,after.model)
 const consent=panel(page).getByRole('checkbox',{name:/我已核对/});assert.equal(await consent.isChecked(),false);assert.equal(await panel(page).getByRole('button',{name:'同意并发送本次',exact:true}).isDisabled(),true)
 assert.ok((await panel(page).textContent()).includes('https://example.com:'));assert.ok((await panel(page).textContent()).includes('PRIVATE_BODY_14B'));await page.screenshot({path:cfg.desktopScreenshot,fullPage:true})
 check('real_private_Prepare_displays_exact_source_destination_and_unchecked_distinct_consent_with_zero_model_calls')
 await consent.check();await panel(page).getByRole('button',{name:'同意并发送本次',exact:true}).click();await panel(page).getByText(cfg.answer,{exact:true}).waitFor();assert.equal((await control('/counts')).model,before.model+1)
 await panel(page).getByRole('button',{name:'复核当前问答引用',exact:true}).click();await panel(page).getByLabel('问答当前引用').waitFor();assert.ok((await panel(page).getByLabel('问答当前引用').textContent()).includes('PRIVATE_BODY_14B'))
 check('explicit_private_remote_model_answer_and_exact_owner_revision_HTTP_citation_reopen')
 await panel(page).getByLabel('问答问题',{exact:true}).fill(cfg.question+' changed');assert.equal(await panel(page).getByLabel('问答结果').count(),0);assert.equal(await panel(page).getByLabel('问答当前引用').count(),0)
 await panel(page).getByLabel('问答问题',{exact:true}).fill(cfg.question);await prepare(page);await panel(page).getByRole('button',{name:'取消并清空本次问答'}).click();assert.equal(await panel(page).getByRole('checkbox',{name:/我已核对/}).count(),0)
 check('question_change_and_cancel_clear_plaintext_and_invalidate_prepared_approval')
 await control('/html');await prepare(page);await panel(page).getByRole('checkbox',{name:/我已核对/}).check();await panel(page).getByRole('button',{name:'同意并发送本次'}).click();await panel(page).getByText(/UNTRUSTED_HTML_14B/).waitFor();assert.equal(await panel(page).locator('img,script').count(),0);assert.equal(await page.evaluate(()=>window.ragInjected),undefined);assert.equal(unsafeFetch,false);await control('/success')
 check('actual_model_HTML_is_plain_text_no_script_image_fetch_or_tool_execution')
 await control('/wait');await panel(page).getByLabel('问答问题',{exact:true}).fill(cfg.question);await prepare(page);const pendingBefore=await control('/counts');await panel(page).getByRole('checkbox',{name:/我已核对/}).check();await panel(page).getByRole('button',{name:'同意并发送本次'}).click()
 for(let i=0;i<100;i++){if((await control('/counts')).model>pendingBefore.model)break;if(i===99)throw new Error('real pending model was not observed');await new Promise(r=>setTimeout(r,30))}
 await panel(page).getByRole('button',{name:'取消并清空本次问答'}).click();await control('/success');await page.waitForTimeout(200);assert.equal(await panel(page).getByLabel('问答结果').count(),0)
 check('cancel_real_pending_remote_HTTP_discards_late_answer_in_built_UI')
 await control('/public');await page.goto(origin+'/knowledge');await page.getByLabel('公开集合',{exact:true}).selectOption(cfg.collectionID);await panel(page).getByLabel('问答问题',{exact:true}).fill(cfg.question);await panel(page).getByRole('button',{name:'根据公开资料提问'}).click();await panel(page).getByLabel('问答结果').locator('pre').filter({hasText:cfg.publicAnswer}).waitFor();await panel(page).getByRole('button',{name:'复核当前问答引用'}).click();await panel(page).getByLabel('问答当前引用').waitFor()
 check('actual_public_remote_RAG_and_current_citation_in_built_public_page')
 await desktop.close();await control('/private')
 const mobile=await browser.newContext({viewport:{width:390,height:844},isMobile:true,deviceScaleFactor:1});mobile.setDefaultTimeout(20000);const mp=await mobile.newPage();mp.on('pageerror',e=>errors.push(e.message));await login(mp);await open(mp);await prepare(mp);assert.equal(await panel(mp).getByRole('checkbox',{name:/我已核对/}).isChecked(),false);await panel(mp).getByRole('checkbox',{name:/我已核对/}).check();await panel(mp).getByRole('button',{name:'同意并发送本次'}).click();await panel(mp).getByText(cfg.answer,{exact:true}).waitFor();await mp.screenshot({path:cfg.mobileScreenshot,fullPage:true});assert.ok(await mp.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+2))
 check('390_mobile_independent_login_unchecked_consent_private_answer_and_no_horizontal_overflow')
 const storage=await mp.evaluate(()=>({local:JSON.stringify({...localStorage}),session:JSON.stringify({...sessionStorage})}));assert.ok(!JSON.stringify(storage).includes('PRIVATE_BODY_14B'));assert.ok(!JSON.stringify(storage).includes(cfg.question));assert.ok(knowledgeRequests.filter(r=>r.path.includes('/rag/')).every(r=>r.query===''));assert.deepEqual(errors,[])
 check('no_private_prompt_or_answer_in_URL_or_browser_storage_no_page_errors')
 await mobile.close();await writeFile(cfg.result,JSON.stringify({status:'passed',checks,check_count:checks.length,applicationResponsesMocked:false,errors,request_count:knowledgeRequests.length},null,2)+'\n')
} finally {await browser?.close();await new Promise(r=>web.close(r))}
