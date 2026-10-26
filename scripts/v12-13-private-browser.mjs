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
const control = async path => {const r=await fetch(cfg.control+path,{method:'POST',headers:{Authorization:'Bearer '+cfg.controlKey}});assert.equal(r.status,200)}
const check=name=>checks.push(name)
const login=async(page)=>{
 await page.goto(origin+'/knowledge/private')
 await page.locator('input[type=email]').fill(cfg.email)
 await page.locator('input[type=password]').fill(cfg.password)
 await page.getByRole('main').getByRole('button',{name:'登录',exact:true}).click()
 await page.waitForURL(url=>url.pathname==='/')
 await page.goto(origin+'/knowledge/private')
 await page.getByRole('heading',{name:'我的私人知识',exact:true}).waitFor()
 await page.getByRole('button',{name:'加载本人来源',exact:true}).waitFor()
}
let browser
try {
 browser=await chromium.launch({executablePath:process.env.CHROME_BIN||'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']})
 const desktop=await browser.newContext({viewport:{width:1280,height:900}});desktop.setDefaultTimeout(15000)
 const page=await desktop.newPage();page.on('pageerror',e=>errors.push(e.message));let imageLeak=false;page.on('request',r=>{if(r.url().includes('/private-must-not-fetch'))imageLeak=true})
 await login(page);check('actual_current_User_password_login_and_private_route_guard')
 assert.equal(await page.getByText('尚未选择来源，默认不入库。').count(),1)
 await page.getByRole('button',{name:'加载本人来源',exact:true}).click()
 await page.locator('main.private-knowledge').getByRole('combobox').nth(1).selectOption(cfg.documentID)
 await page.getByRole('button',{name:'核对当前来源',exact:true}).click()
 await page.getByRole('checkbox').waitFor()
 assert.equal(await page.getByRole('button',{name:'确认选入这一项',exact:true}).isDisabled(),true)
 check('loaded_owned_documents_and_preview_never_imply_consent')
 await page.getByRole('checkbox').check();await page.getByRole('button',{name:'确认选入这一项',exact:true}).click()
 await page.getByText('已记录本项同意，索引任务已提交；请刷新状态后搜索').waitFor()
 for(let i=0;i<40;i++){await page.getByRole('button',{name:'刷新状态与列表',exact:true}).click();await page.waitForTimeout(120);if(await page.getByText(/可检索/).count())break}
 assert.equal(await page.getByText(/可检索/).count(),1);check('explicit_one_source_selection_real_durable_resident_worker_ready')
 await page.getByLabel('关键词',{exact:true}).fill('校园图书馆');await page.getByRole('button',{name:'搜索本人已选内容',exact:true}).click();await page.getByRole('button',{name:'读取当前引用',exact:true}).waitFor()
 assert.ok((await page.locator('main.private-knowledge').innerText()).includes('BROWSER_PRIVATE_SECRET'))
 await page.getByRole('button',{name:'读取当前引用',exact:true}).click();await page.getByRole('region',{name:'私人引用'}).waitFor()
 assert.equal(imageLeak,false);assert.equal(await page.locator('main.private-knowledge img').count(),0)
 check('current_private_search_and_exact_source_plain_text_without_image_or_script_execution')
 await page.screenshot({path:cfg.desktopScreenshot,fullPage:true})
 // Pause only the real server response, then change query and release it.
 delaySource=true;const held=new Promise(r=>{sourceHeld=r})
 await page.getByRole('button',{name:'读取当前引用',exact:true}).click();await held
 await page.getByLabel('关键词',{exact:true}).fill('新词');delaySource=false;releaseSource?.();await page.waitForTimeout(250)
 assert.equal(await page.getByRole('region',{name:'私人引用'}).count(),0);assert.equal(await page.getByRole('button',{name:'读取当前引用',exact:true}).count(),0)
 check('late_actual_source_response_is_ignored_after_query_change')
 await page.getByRole('button',{name:'撤回并清理',exact:true}).click();await page.getByText('已撤回，后续请求无法再读取这项索引').waitFor()
 await page.getByLabel('关键词',{exact:true}).fill('校园图书馆');await page.getByRole('button',{name:'搜索本人已选内容',exact:true}).click();await page.getByText('没有当前可读的匹配结果；索引处理中可刷新状态后重新搜索。').waitFor()
 check('withdraw_immediately_clears_page_and_new_search_results')
 await control('/feature/off');await page.getByRole('button',{name:'刷新状态与列表',exact:true}).click();await page.getByRole('alert').waitFor();assert.equal(await page.getByRole('button',{name:'确认选入这一项',exact:true}).count(),0)
 await control('/feature/on');await page.getByRole('button',{name:'刷新状态与列表',exact:true}).click();await page.getByRole('button',{name:'加载本人来源',exact:true}).waitFor();check('hot_Feature_disable_clears_output_and_requires_explicit_refresh')
 const stored=await page.evaluate(()=>JSON.stringify({local:{...localStorage},session:{...sessionStorage},url:location.href}))
 assert.ok(!stored.includes('BROWSER_PRIVATE_SECRET')&&!stored.includes('校园图书馆'));assert.ok(knowledgeRequests.every(x=>!x.query));check('private_query_body_and_results_never_persist_or_enter_URL')
 const mobile=await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true});mobile.setDefaultTimeout(15000);const phone=await mobile.newPage();phone.on('pageerror',e=>errors.push(e.message));await login(phone)
 assert.equal(await phone.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),true)
 for(const b of await phone.locator('main.private-knowledge button').all()){const box=await b.boundingBox();if(box)assert.ok(box.height>=44)}
 await phone.screenshot({path:cfg.mobileScreenshot,fullPage:true});check('actual_mobile_current_User_login_responsive_390px_and_44px_controls')
 assert.deepEqual(errors,[]);await writeFile(cfg.result,JSON.stringify({status:'passed',checks,check_count:checks.length,actual_built_web:true,actual_user_login:true,actual_server_composition:true,actual_postgresql:true,mocked_api_data:false,private_http_requests:knowledgeRequests.length,query_in_url_count:knowledgeRequests.filter(x=>x.query).length,browser_errors:errors,desktop:{width:1280,height:900},mobile:{width:390,height:844}},null,2)+'\n')
}finally{releaseSource?.();await browser?.close();await new Promise(r=>web.close(r))}
