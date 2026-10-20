import assert from "node:assert/strict";
import {createRequire} from "node:module";
import {readFile,writeFile,access} from "node:fs/promises";
import {createServer,request} from "node:http";
import {resolve,extname,sep} from "node:path";
const cfg=JSON.parse(await readFile(process.argv[2],"utf8"));
const require=createRequire(resolve("web/package.json"));
const {chromium}=require("playwright-core");
async function proxy(folder){
 const root=resolve(folder);
 const server=createServer(async(req,res)=>{
  if(req.url.startsWith("/api/")){
   const upstream=request(new URL(req.url,cfg.backend),{method:req.method,headers:req.headers},r=>{res.writeHead(r.statusCode,r.headers);r.pipe(res)});
   upstream.on("error",()=>{if(!res.headersSent)res.writeHead(502);res.end()});res.on("close",()=>upstream.destroy());req.pipe(upstream);return;
  }
  try{let file=resolve(root,"."+new URL(req.url,"http://localhost").pathname);assert.ok(file===root||file.startsWith(root+sep));if(!extname(file))file=resolve(root,"index.html");
   const types={".html":"text/html",".js":"text/javascript",".css":"text/css",".svg":"image/svg+xml",".png":"image/png",".ico":"image/x-icon"};res.writeHead(200,{"Content-Type":types[extname(file)]||"application/octet-stream"});res.end(await readFile(file));
  }catch{if(!res.headersSent)res.writeHead(404);res.end()}
 });await new Promise(r=>server.listen(0,"127.0.0.1",r));return {server,origin:`http://127.0.0.1:${server.address().port}`};
}
const web=await proxy("web/dist"),admin=await proxy("admin/dist");
const checks=[],errors=[];let browser;
async function login(page,domain,email){
 const origin=domain==="admin"?admin.origin:web.origin;
 await page.goto(origin+"/login");
 await page.locator(`input[autocomplete="${domain==="admin"?"username":"email"}"]`).fill(email);
 await page.locator('input[autocomplete="current-password"]').fill(cfg.password);
 await page.locator('button[type="submit"]').click();
 await page.waitForURL(url=>!url.pathname.startsWith("/login"));
 await page.waitForFunction(domain=>{
  const provides=document.querySelector("#app")?.__vue_app__?._context.provides;
  return provides&&Reflect.ownKeys(provides).map(k=>provides[k]).find(v=>v?._s instanceof Map)?._s.get(domain)?.token;
 },domain==="admin"?"admin":"user");
}
async function api(page,domain,path){
 await page.waitForFunction(domain=>{
  const provides=document.querySelector("#app")?.__vue_app__?._context.provides;
  return provides&&Reflect.ownKeys(provides).map(k=>provides[k]).find(v=>v?._s instanceof Map)?._s.get(domain)?.token;
 },domain==="admin"?"admin":"user");
 return page.evaluate(async({domain,path})=>{
  const provides=document.querySelector("#app").__vue_app__._context.provides;
  const token=Reflect.ownKeys(provides).map(k=>provides[k]).find(v=>v?._s instanceof Map)._s.get(domain).token;
  const r=await fetch("/api/v1"+path,{headers:{Authorization:"Bearer "+token}});
  const body=await r.text();return {status:r.status,body};
 },{domain:domain==="admin"?"admin":"user",path});
}
const check=name=>checks.push(name);
try{
 browser=await chromium.launch({executablePath:process.env.CHROME_BIN||"/usr/bin/google-chrome",headless:true,args:["--no-sandbox"]});
 if(cfg.before){
  const uc=await browser.newContext(),ac=await browser.newContext();const up=await uc.newPage(),ap=await ac.newPage();
  await login(up,"user",cfg.alice);await login(ap,"admin","restore-operator");
  await up.goto(web.origin+"/documents");await ap.goto(admin.origin+"/plugins");
  assert.ok((await uc.cookies()).some(c=>c.name==="campusos_refresh"));
  assert.ok((await ac.cookies()).some(c=>c.name==="campusos_admin_refresh"));
  // Unload polling SPAs during the intentional offline interval while keeping
  // their real HttpOnly cookies in the same browser contexts. A 502 must not
  // erase the credential before the restored server can actually reject it.
  await up.goto(web.origin+"/favicon.ico");await ap.goto(admin.origin+"/favicon.ico");
  check("pre-backup actual Web/Admin sessions established");
  await writeFile(cfg.ready,JSON.stringify({status:"ready",checks}));
  const deadline=Date.now()+15*60*1000;
  while(true){try{await access(cfg.resume);break}catch{assert.ok(Date.now()<deadline,"recovery resume deadline");await new Promise(r=>setTimeout(r,250))}}
  const userRefresh=up.waitForResponse(r=>r.request().method()==="POST"&&r.url().endsWith("/auth/refresh"));
  await up.goto(web.origin+"/documents",{waitUntil:"domcontentloaded"});assert.ok([401,403].includes((await userRefresh).status()));await up.waitForURL(url=>url.pathname==="/login");
  const adminRefresh=ap.waitForResponse(r=>r.request().method()==="POST"&&r.url().endsWith("/admin/identity/refresh"));
  await ap.goto(admin.origin+"/plugins",{waitUntil:"domcontentloaded"});assert.ok([401,403].includes((await adminRefresh).status()));await ap.waitForURL(url=>url.pathname==="/login");
  check("old User/Admin browser refresh cookies rejected after whole restore");await uc.close();await ac.close();
 }
 for(const [name,viewport] of [["desktop",{width:1280,height:1000}],["mobile",{width:390,height:844}]]){
  const a=await browser.newContext({viewport}),b=await browser.newContext({viewport}),m=await browser.newContext({viewport});
  const ap=await a.newPage(),bp=await b.newPage(),mp=await m.newPage();for(const page of [ap,bp,mp]){page.setDefaultTimeout(20000);page.on("pageerror",e=>errors.push(e.message))}
  await login(ap,"user",cfg.alice);await login(bp,"user",cfg.bob);await login(mp,"admin","restore-operator");
  await ap.goto(web.origin+"/documents");await bp.goto(web.origin+"/documents");
  for(const [page,owner] of [[ap,"alice"],[bp,"bob"]]){
   await page.getByRole("heading",{name:"我的文档",exact:true}).waitFor();
   await page.getByRole("row").filter({hasText:`restore-${owner}-0.txt`}).first().waitFor();
  }
  const alice=await api(ap,"user","/documents?status=active"),bob=await api(bp,"user","/documents?status=active");assert.equal(alice.status,200);assert.equal(bob.status,200);
  const aa=JSON.parse(alice.body).data.items,bb=JSON.parse(bob.body).data.items;assert.equal(aa.length,100);assert.equal(bb.length,100);assert.ok(aa.every(x=>x.owner_id==="2801001"));assert.ok(bb.every(x=>x.owner_id==="2801002"));
  check(`${name}: restored own document lists separated for two users`);
  const download=await api(ap,"user",`/documents/${cfg.document}/download`);assert.equal(download.status,200);assert.equal(download.body,cfg.documentText);
  const denied=await api(bp,"user",`/documents/${cfg.document}/download`);assert.ok([403,404].includes(denied.status));assert.ok(!denied.body.includes(cfg.documentText));
  check(`${name}: actual restored bytes available only to owner`);
  await ap.goto(web.origin+"/plugins");await mp.goto(admin.origin+"/plugins");
  const plugins=await api(mp,"admin","/admin/plugin-v5"),owned=await api(ap,"user","/plugin-v5");assert.equal(plugins.status,200);assert.equal(owned.status,200);
  assert.ok(plugins.body.includes("backup-offline"));assert.ok(owned.body.includes("backup-offline"));
  check(`${name}: real Admin/User v5 inventory retains quarantined plugin`);
  await mp.goto(admin.origin+"/architecture");await mp.getByText("实例恢复认证围栏",{exact:true}).first().waitFor();
  await mp.screenshot({path:resolve(cfg.output,`${name}-architecture.png`),fullPage:true});
  await ap.goto(web.origin+"/documents");
  await ap.getByRole("heading",{name:"我的文档",exact:true}).waitFor();
  await ap.getByRole("row").filter({hasText:"restore-alice-0.txt"}).first().waitFor();
  await ap.screenshot({path:resolve(cfg.output,`${name}-documents.png`),fullPage:true});
  check(`${name}: built architecture and document pages rendered`);
  await a.close();await b.close();await m.close();
 }
 assert.deepEqual(errors,[]);
 await writeFile(resolve(cfg.output,"browser.json"),JSON.stringify({status:"passed",backend_kind:cfg.backendKind,actual_cmd_server:true,mocked_api:false,checks,page_errors:errors,screenshots:4},null,2)+"\n");
 console.log(`PASS actual ${cfg.backendKind} restore browser ${checks.length} checks`);
}finally{await browser?.close();web.server.close();admin.server.close()}
