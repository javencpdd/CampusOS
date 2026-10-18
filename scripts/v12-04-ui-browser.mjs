// Built Admin/Web assets proxy every API request to the actual cmd/server.
// Route delay below holds a real fetched response to exercise identity races.
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { readFile, writeFile } from "node:fs/promises";
import { createServer, request as proxyRequest } from "node:http";
import { createHmac } from "node:crypto";
import { extname, resolve, sep } from "node:path";
const cfg = JSON.parse(await readFile(process.argv[2], "utf8"));
const require = createRequire(resolve("web/package.json"));
const { chromium } = require("playwright-core");
async function staticProxy(folder, port) {
  const root = resolve(folder);
  const server = createServer(async (req, res) => {
    if (req.url.startsWith("/api/")) {
      const upstream = proxyRequest(
        new URL(req.url, cfg.backend),
        { method: req.method, headers: req.headers },
        (response) => {
          res.writeHead(response.statusCode, response.headers);
          response.pipe(res);
        },
      );
      res.on("close", () => upstream.destroy());
      upstream.on("error", () => {
        if (!res.headersSent) res.writeHead(502);
        res.end();
      });
      req.pipe(upstream);
      return;
    }
    try {
      const path = resolve(
        root,
        "." + new URL(req.url, "http://localhost").pathname,
      );
      if (path !== root && !path.startsWith(root + sep)) {
        res.writeHead(404).end();
        return;
      }
      const file = extname(path) ? path : resolve(root, "index.html");
      const types = {
        ".html": "text/html",
        ".js": "text/javascript",
        ".css": "text/css",
        ".svg": "image/svg+xml",
        ".png": "image/png",
        ".ico": "image/x-icon",
      };
      const data = await readFile(file);
      res
        .writeHead(200, {
          "Content-Type": types[extname(file)] || "application/octet-stream",
        })
        .end(data);
    } catch {
      if (!res.headersSent) res.writeHead(404);
      res.end();
    }
  });
  await new Promise((r) => server.listen(port, "127.0.0.1", r));
  return { server, origin: `http://127.0.0.1:${server.address().port}` };
}
function totp(secret, step) {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = 0,
    acc = 0;
  const bytes = [];
  for (const c of secret.replace(/=+$/, "")) {
    const value = alphabet.indexOf(c);
    assert.ok(value >= 0);
    acc = (acc << 5) | value;
    bits += 5;
    if (bits >= 8) {
      bits -= 8;
      bytes.push((acc >> bits) & 255);
    }
  }
  const message = Buffer.alloc(8);
  message.writeBigUInt64BE(BigInt(step));
  const digest = createHmac("sha1", Buffer.from(bytes))
    .update(message)
    .digest();
  const offset = digest.at(-1) & 15;
  return String((digest.readUInt32BE(offset) & 0x7fffffff) % 1000000).padStart(
    6,
    "0",
  );
}
// Pinia's production injection key is a Symbol, so inspect the actual app's
// providers rather than assuming a development-only global or replacing a store.
const findStore = (name) => {
  const provides =
    document.querySelector("#app")?.__vue_app__?._context.provides;
  const pinia =
    provides &&
    Reflect.ownKeys(provides)
      .map((k) => provides[k])
      .find((value) => value?._s instanceof Map);
  return pinia?._s.get(name);
};
const admin = await staticProxy("admin/dist", cfg.adminPort),
  web = await staticProxy("web/dist", cfg.webPort);
let evilRequests = 0;
const evil = createServer((_, res) => {
  evilRequests++;
  res
    .writeHead(200, { "Content-Type": "text/html" })
    .end(
      `<script>window.__connections=0;addEventListener('message',e=>{if(e.data?.type==='campusos.bridge.connect')window.__connections++});parent.postMessage({type:'campusos.bridge.ready',version:'campusos.bridge/v1',plugin_key:'campusos.pdf-viewer',plugin_version:'3.0.0-dev.2',surface_id:'preview',audience:'user'},'*')</script>`,
    );
});
await new Promise((r) => evil.listen(0, "127.0.0.1", r));
const evilOrigin = `http://127.0.0.1:${evil.address().port}`;
const uiHost = new URL(cfg.uiOrigin.replace("{isolation}", "probe"));
const isPluginFrame = (f) => {
  try {
    const u = new URL(f.url());
    return (
      u.hostname.endsWith(".localhost") &&
      u.port === uiHost.port &&
      u.pathname.startsWith("/ui/")
    );
  } catch {
    return false;
  }
};
// Node fetch rewrites Host from the URL. Use the HTTP client so this owned
// loopback probe exercises the actual lease hostname, just as Chrome does.
const assetFetch = (src) => {
  const u = new URL(src);
  return new Promise((resolve, reject) => {
    const request = proxyRequest(
      `http://127.0.0.1:${uiHost.port}${u.pathname}${u.search}`,
      {
        headers: { Host: u.host, "Sec-Fetch-Dest": "iframe" },
        timeout: 10000,
      },
      (response) => {
        response.resume();
        resolve({ status: response.statusCode, headers: response.headers });
      },
    );
    request.on("error", reject);
    request.on("timeout", () =>
      request.destroy(new Error("asset probe timeout")),
    );
    request.end();
  });
};
const checks = [],
  errors = [];
let browser;
const check = (name) => {
  checks.push(name);
  console.log(`PASS ${name}`);
};
function pdf(text) {
  const content = `BT /F1 18 Tf 30 100 Td (${text}) Tj ET\n`;
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
    `<< /Length ${Buffer.byteLength(content)} >>\nstream\n${content}endstream`,
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
  ];
  let data = "%PDF-1.4\n",
    offsets = [0];
  for (let i = 0; i < objects.length; i++) {
    offsets.push(Buffer.byteLength(data));
    data += `${i + 1} 0 obj\n${objects[i]}\nendobj\n`;
  }
  const offset = Buffer.byteLength(data);
  data +=
    `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n` +
    offsets
      .slice(1)
      .map((x) => String(x).padStart(10, "0") + " 00000 n \n")
      .join("") +
    `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${offset}\n%%EOF\n`;
  return Buffer.from(data);
}
try {
  browser = await chromium.launch({
    executablePath: process.env.CHROME_BIN || "/usr/bin/google-chrome",
    headless: true,
    args: ["--no-sandbox"],
  });
  const ac = await browser.newContext({
    viewport: { width: 1280, height: 1000 },
  });
  ac.setDefaultTimeout(15000);
  const ap = await ac.newPage();
  ap.on("pageerror", (e) => errors.push(e.message));
  await ap.goto(admin.origin + "/login?redirect=/plugins");
  await ap.locator('input[autocomplete="username"]').fill(cfg.adminUsername);
  await ap.locator('input[autocomplete="current-password"]').fill(cfg.password);
  await ap.getByRole("button", { name: "登录", exact: true }).click();
  await ap.waitForURL(admin.origin + "/plugins");
  await ap.getByRole("heading", { name: "插件管理", exact: true }).waitFor();
  const identity = async (page, domain) => ({
    token: await page.evaluate(
      ({ source, domain }) => eval("(" + source + ")")(domain)?.token,
      {
        source: findStore.toString(),
        domain: domain === "admin" ? "admin" : "user",
      },
    ),
    csrf: await page.evaluate(
      (domain) =>
        document.cookie
          .split("; ")
          .find((x) =>
            x.startsWith(
              domain === "admin" ? "campusos_admin_csrf=" : "campusos_csrf=",
            ),
          )
          ?.split("=")
          .slice(1)
          .join("=") || "",
      domain,
    ),
  });
  const api = async (page, domain, method, path, body, withCSRF = true) => {
    const id = await identity(page, domain);
    const headers = {
      Authorization: "Bearer " + id.token,
      "X-CampusOS-Client": "browser",
      Cookie:
        (domain === "admin" ? "campusos_admin_csrf=" : "campusos_csrf=") +
        id.csrf,
    };
    if (withCSRF)
      headers[domain === "admin" ? "X-Admin-CSRF-Token" : "X-CSRF-Token"] =
        id.csrf;
    const multipart = body instanceof FormData;
    if (body && !multipart) headers["Content-Type"] = "application/json";
    const r = await fetch(cfg.backend + "/api/v1" + path, {
      method,
      headers,
      ...(body
        ? {
            body:
              multipart || typeof body === "string"
                ? body
                : JSON.stringify(body),
          }
        : {}),
    });
    const text = await r.text();
    let data;
    try {
      data = JSON.parse(text);
    } catch {
      data = text;
    }
    return { status: r.status, data, text };
  };
  const control = async (path) => {
    const r = await fetch(cfg.control + path, {
      method: "POST",
      headers: { Authorization: "Bearer " + cfg.controlKey },
    });
    assert.equal(r.status, 204);
  };
  const ok = (r, status = 200) => {
    assert.equal(
      r.status,
      status,
      `HTTP ${r.status}: ${r.data?.msg || r.data?.message || r.data?.error?.message || ""}`,
    );
    return r.data.data;
  };
  let r = await api(
    ap,
    "admin",
    "POST",
    "/admin/plugin-v5/campusos/pdf-viewer/commands",
    { kind: "install", version: "3.0.0-dev.2", idempotency_key: "without-mfa" },
  );
  assert.equal(r.status, 403);
  await ap.getByRole("button", { name: "确认管理员身份", exact: true }).click();
  const dialog = ap.getByRole("dialog", { name: "确认管理员身份" });
  await dialog.waitFor();
  await dialog
    .getByRole("button", { name: "开始配置管理员认证器", exact: true })
    .click();
  const secretInput = dialog.locator("input[readonly]");
  await secretInput.waitFor();
  const secret = await secretInput.inputValue(),
    step = Math.floor(Date.now() / 30000),
    code = dialog.locator('input[autocomplete="one-time-code"]');
  await code.fill(totp(secret, step));
  await dialog.getByRole("button", { name: "完成配置", exact: true }).click();
  await dialog
    .getByText("配置完成。请等待认证器显示下一组验证码，再确认管理员身份。", {
      exact: true,
    })
    .waitFor();
  await code.fill(totp(secret, step + 1));
  await dialog.getByRole("button", { name: "确认身份", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  check(
    "independent Admin login; password-only denied; actual MFA enrollment and step-up",
  );
  for (const p of cfg.packages) {
    await ap.getByLabel("包文件", { exact: true }).setInputFiles(p.archive);
    await ap
      .getByLabel("签名发布文件", { exact: true })
      .setInputFiles(p.release);
    const sent = ap.waitForResponse(
      (res) =>
        res.request().method() === "POST" &&
        res.url().endsWith("/admin/plugin-v5/packages"),
    );
    await ap.getByRole("button", { name: "验证并导入", exact: true }).click();
    assert.equal((await sent).status(), 200);
    await ap
      .getByText("签名包已验证并导入，请单独安装和审批。", { exact: true })
      .waitFor();
  }
  check(
    "built Admin imports detached signatures and immutable packages through real HTTP",
  );
  const select = async (key) => {
    await ap
      .getByRole("navigation", { name: "插件版本" })
      .getByRole("button")
      .filter({ hasText: "campusos/" + key + "@" })
      .click();
    await ap
      .getByRole("heading", { name: new RegExp("campusos/" + key + "@") })
      .waitFor();
  };
  async function adminCommand(button) {
    const sent = ap.waitForResponse(
      (res) =>
        res.request().method() === "POST" && res.url().includes("/commands"),
    );
    await ap.getByRole("button", { name: button, exact: true }).click();
    const submitted = await sent;
    assert.equal(submitted.status(), 200);
    await ap.getByRole("status").filter({ hasText: "操作已提交" }).waitFor();
    return (await submitted.json()).data.id;
  }
  let pdfEnableID;
  for (const key of ["full-config", "pdf-viewer"]) {
    await select(key);
    await adminCommand("安装");
    await ap
      .getByLabel("审批原因", { exact: true })
      .fill("已核对当前版本用途与声明上限");
    const caps = ap
      .getByRole("group", { name: "业务能力" })
      .locator('input[type="checkbox"]');
    for (let i = 0; i < (await caps.count()); i++) await caps.nth(i).check();
    await adminCommand("保存此版本审批");
    const enabledID = await adminCommand("启用");
    if (key === "pdf-viewer") pdfEnableID = enabledID;
  }
  check(
    "version-bound capability/resource approval panels; explicit reason; separate install/enable",
  );
  const pdfPackage = cfg.packages.find((p) => p.key === "pdf-viewer");
  let pd = ok(
    await api(
      ap,
      "admin",
      "GET",
      `/admin/plugin-v5/campusos/pdf-viewer/${pdfPackage.version}`,
    ),
  );
  assert.notEqual(pd.installation.observed, "ready");
  assert.equal(pd.grant.approved, true);
  assert.equal(pd.grant.reason, "已核对当前版本用途与声明上限");
  assert.ok(pd.grant.approved_by);
  check(
    "UI readiness does not fabricate backend Runner observation; approval attribution persisted",
  );
  await ap.getByRole("button", { name: "打开隔离界面", exact: true }).click();
  await ap
    .frameLocator("iframe")
    .getByText("PDF 插件设置已连接；配置由 CampusOS 宿主校验和保存。", {
      exact: true,
    })
    .waitFor();
  check(
    "actual v5 Admin iframe and finite config.read Bridge on dedicated origin",
  );
  await ap.getByRole("button", { name: "关闭隔离界面", exact: true }).click();
  let pendingOperation;
  for (let attempt = 0; attempt < 30; attempt++) {
    pendingOperation = ok(
      await api(
        ap,
        "admin",
        "GET",
        `/admin/plugin-v5/operations/${pdfEnableID}`,
      ),
    );
    if (pendingOperation.status === "waiting") break;
    await ap.waitForTimeout(500);
  }
  assert.equal(pendingOperation.status, "waiting");
  assert.equal(
    (
      await api(
        ap,
        "admin",
        "POST",
        "/admin/plugin-v5/campusos/pdf-viewer/commands",
        {
          kind: "retry",
          version: pdfPackage.version,
          operation_id: pdfEnableID,
          expected_revision: "0",
          idempotency_key: "stale-operation-retry",
        },
      )
    ).status,
    409,
  );
  await ap.getByRole("button", { name: "查询操作状态", exact: true }).click();
  await ap.getByRole("button", { name: "重试此操作", exact: true }).waitFor();
  await adminCommand("重试此操作");
  check(
    "actual Admin operation query and explicit retry use current revision; stale operation CAS is rejected",
  );
  await ap.goto(admin.origin + "/plugin-config");
  await ap
    .getByLabel("插件版本列表")
    .getByRole("button")
    .filter({ hasText: "campusos/full-config@1.0.0" })
    .click();
  await ap.getByLabel("精确整数", { exact: true }).waitFor();
  assert.equal(
    await ap.getByLabel("精确整数", { exact: true }).inputValue(),
    "9007199254740993",
  );
  assert.equal(
    await ap.getByLabel("精确小数", { exact: true }).inputValue(),
    "0.10000000000000000001",
  );
  await ap.getByLabel("精确整数", { exact: true }).fill("9007199254740994");
  await ap
    .getByLabel("精确小数", { exact: true })
    .fill("0.10000000000000000002");
  await ap.getByLabel("设置数字枚举", { exact: true }).check();
  await ap
    .getByLabel("数字枚举", { exact: true })
    .selectOption({ label: "0.10000000000000000002" });
  let saved = ap.waitForResponse(
    (res) =>
      res.request().method() === "POST" &&
      res.url().includes("/full-config/commands"),
  );
  await ap.getByRole("button", { name: "保存普通配置", exact: true }).click();
  const exactSave = await saved;
  assert.equal(exactSave.status(), 200);
  assert.match(exactSave.request().postData(), /"count":9007199254740994/);
  assert.match(
    exactSave.request().postData(),
    /"ratio":0\.10000000000000000002/,
  );
  assert.ok(!exactSave.request().postData().includes("secret_refs"));
  await ap.getByRole("status").filter({ hasText: "普通配置已保存" }).waitFor();
  check(
    "exact BIGINT/adjacent decimal/numeric enum remain numbers across UI, wire, PG and read-back",
  );
  await ap.getByLabel("设置模型配置", { exact: true }).check();
  let writes = 0;
  const onWrite = (req) => {
    if (req.method() === "POST" && req.url().includes("/full-config/commands"))
      writes++;
  };
  ap.on("request", onWrite);
  await ap.getByRole("button", { name: "保存普通配置", exact: true }).click();
  await ap.getByRole("alert").filter({ hasText: "不可选" }).waitFor();
  assert.equal(writes, 0);
  ap.off("request", onWrite);
  await ap.getByLabel("设置模型配置", { exact: true }).uncheck();
  assert.equal(
    await ap
      .getByLabel("密钥引用 api_key", { exact: true })
      .locator("option")
      .count(),
    1,
  );
  assert.equal(await ap.locator('input[type="password"]').count(), 0);
  check(
    "restricted current-fact selectors are empty without providers; Secret reference form has no plaintext input",
  );
  let detail = ok(
    await api(
      ap,
      "admin",
      "GET",
      "/admin/plugin-v5/campusos/full-config/1.0.0",
    ),
  );
  r = await api(
    ap,
    "admin",
    "POST",
    "/admin/plugin-v5/campusos/full-config/commands",
    {
      kind: "configure",
      version: "1.0.0",
      configuration_part: "secret_refs",
      secret_refs: { api_key: "secret-ref:invented" },
      expected_revision: detail.configuration.revision,
      idempotency_key: "invented-ref",
    },
  );
  assert.equal(r.status, 403);
  r = await api(
    ap,
    "admin",
    "POST",
    "/admin/plugin-v5/campusos/full-config/commands",
    {
      kind: "configure",
      version: "1.0.0",
      configuration_part: "secret_refs",
      secret_refs: {},
      expected_revision: detail.configuration.revision,
      idempotency_key: "no-csrf",
    },
    false,
  );
  assert.equal(r.status, 403);
  check(
    "actual API rejects invented Secret references and missing browser CSRF",
  );
  await control("/expire-mfa");
  await ap.getByLabel("名称", { exact: true }).fill("过期认证不得保存");
  saved = ap.waitForResponse(
    (res) =>
      res.request().method() === "POST" &&
      res.url().includes("/full-config/commands"),
  );
  await ap.getByRole("button", { name: "保存普通配置", exact: true }).click();
  assert.equal((await saved).status(), 403);
  assert.equal(
    await ap.getByRole("form", { name: "普通配置", exact: true }).count(),
    0,
  );
  await control("/restore-mfa");
  check("current PG MFA expiry rejects cached ordinary editor without replay");
  const uc = await browser.newContext({
    viewport: { width: 1280, height: 1000 },
  });
  uc.setDefaultTimeout(15000);
  await uc.addInitScript(() => {
    window.__v5Connections = [];
    window.__v5Responses = [];
    window.addEventListener("message", (e) => {
      if (e.data?.type === "campusos.bridge.connect") {
        window.__v5Connections.push(e.data);
        window.__v5Port = e.ports[0];
        e.ports[0]?.addEventListener("message", (m) => {
          window.__v5Responses.push({
            request_id: m.data?.request_id,
            error: m.data?.error?.code,
            size: m.data?.result?.bytes?.byteLength,
            name: m.data?.result?.name,
          });
        });
      }
    });
  });
  const up = await uc.newPage();
  up.on("pageerror", (e) => errors.push(e.message));
  const login = async (page, email) => {
    await page.goto(web.origin + "/login");
    await page.locator('input[autocomplete="email"]').fill(email);
    await page
      .locator('input[autocomplete="current-password"]')
      .fill(cfg.password);
    await page.locator('button[type="submit"]').click();
    await page.waitForURL(web.origin + "/");
  };
  await login(up, cfg.alice);
  await up.goto(web.origin + "/plugins");
  await up
    .getByLabel("可用插件")
    .getByRole("button")
    .filter({ hasText: "campusos/full-config@" })
    .click();
  await up.getByRole("button", { name: "安装到我的账号", exact: true }).click();
  await up.getByRole("status").filter({ hasText: "操作已提交" }).waitFor();
  await up.goto(web.origin + "/plugin-config");
  await up
    .getByLabel("插件版本列表")
    .getByRole("button")
    .filter({ hasText: "campusos/full-config@" })
    .click();
  await up.getByLabel("精确整数", { exact: true }).waitFor();
  assert.equal(
    await up.getByLabel("精确整数", { exact: true }).inputValue(),
    "9007199254740993",
  );
  await up.getByLabel("名称", { exact: true }).fill("本人独立配置");
  saved = up.waitForResponse(
    (res) =>
      res.request().method() === "POST" &&
      res.url().includes("/full-config/commands"),
  );
  await up.getByRole("button", { name: "保存普通配置", exact: true }).click();
  assert.equal((await saved).status(), 200);
  await up.getByRole("status").filter({ hasText: "普通配置已保存" }).waitFor();
  check(
    "actual user center install and owner-scoped numeric configuration do not expose system values",
  );
  detail = ok(
    await api(
      ap,
      "admin",
      "GET",
      "/admin/plugin-v5/campusos/full-config/1.0.0",
    ),
  );
  assert.equal(detail.configuration.values.label, "初始配置");
  assert.ok(
    (
      await api(
        ap,
        "admin",
        "GET",
        "/admin/plugin-v5/campusos/full-config/1.0.0",
      )
    ).text.includes("9007199254740994"),
  );
  check(
    "separate user and system configuration stay independent in actual PostgreSQL",
  );
  await up.goto(web.origin + "/plugins");
  await up
    .getByLabel("可用插件")
    .getByRole("button")
    .filter({ hasText: "campusos/full-config@" })
    .click();
  const userEnabled = up.waitForResponse(
    (res) =>
      res.request().method() === "POST" &&
      res.url().includes("/full-config/commands"),
  );
  await up.getByRole("button", { name: "启用", exact: true }).click();
  assert.equal((await userEnabled).status(), 200);
  await up.goto(web.origin + "/extensions/campusos.full-config/ui");
  await up
    .frameLocator("iframe")
    .getByText(/本人独立配置/)
    .waitFor();
  assert.ok(
    !(await up.frameLocator("iframe").locator("body").textContent()).includes(
      "secret_refs",
    ),
  );
  check(
    "actual generic User UI opens without a selected resource and reads only own ordinary configuration",
  );
  const article = ok(
    await api(up, "user", "POST", "/richtext/articles", {
      title: "V12-04 PDF 文章",
      category_id: "8601101",
      content_html: "<p>受控 PDF 验收内容</p>",
    }),
    201,
  );
  const articlePDF = pdf("Article selected resource"),
    documentPDF = pdf("Independent personal document selected resource");
  const form = new FormData();
  form.append(
    "file",
    new Blob([articlePDF], { type: "application/pdf" }),
    "article-stage.pdf",
  );
  const attachmentAck = ok(
    await api(
      up,
      "user",
      "POST",
      `/richtext/articles/${article.thread_id}/attachments/upload`,
      form,
    ),
    201,
  );
  ok(
    await api(
      up,
      "user",
      "POST",
      `/richtext/articles/${article.thread_id}/publish`,
    ),
  );
  const docform = new FormData();
  docform.append(
    "file",
    new Blob([documentPDF], { type: "application/pdf" }),
    "personal-stage.pdf",
  );
  docform.append("name", "个人PDF验收.pdf");
  const document = ok(
    await api(up, "user", "POST", "/documents/upload", docform),
    201,
  );
  const attachmentList = ok(
    await api(
      up,
      "user",
      "GET",
      `/richtext/articles/${article.thread_id}/attachments`,
    ),
  );
  const attachment = (attachmentList.items || attachmentList).find(
    (a) => a.id === attachmentAck.attachment_id,
  );
  const assetID = attachment?.asset_id || attachment?.asset?.id;
  assert.ok(assetID && article.thread_id && attachment.id && document.id);
  check(
    "three preview sources created through actual author upload, publish and personal document APIs",
  );
  const modes = {
    modal: "弹窗预览",
    drawer: "侧边预览",
    fullscreen: "全屏预览",
    "new-tab": "新标签页预览",
  };
  async function resourcePage(kind, page = up) {
    if (kind === "article") {
      await page.goto(web.origin + "/threads/" + article.thread_id);
      await page
        .getByLabel("文章附件")
        .getByText("article-stage.pdf", { exact: true })
        .waitFor();
      return page.getByLabel("文章附件");
    }
    await page.goto(web.origin + "/documents");
    if (kind === "asset")
      await page.getByRole("tab", { name: "文章附件", exact: true }).click();
    return page
      .getByRole("row")
      .filter({
        hasText: kind === "asset" ? "article-stage.pdf" : "个人PDF验收",
      })
      .first();
  }
  let previewConsentConfirmed = false;
  async function open(kind, mode, page = up) {
    const scope = await resourcePage(kind, page);
    await scope.getByRole("button", { name: "预览", exact: true }).click();
    const popup =
      mode === "new-tab" ? page.context().waitForEvent("page") : null;
    await page
      .getByRole("menuitem", { name: modes[mode], exact: true })
      .click();
    const consent = page.getByRole("button", {
      name: "同意并启用",
      exact: true,
    });
    if (!previewConsentConfirmed) {
      await consent.waitFor();
      await consent.click();
      previewConsentConfirmed = true;
    }
    let target = popup ? await popup : page;
    await target
      .frameLocator("iframe")
      .locator("canvas")
      .waitFor({ timeout: 60000 });
    return target;
  }
  async function close(page, mode) {
    if (mode === "new-tab") {
      await page.close();
      await up.reload();
      await up.waitForLoadState("networkidle").catch(() => {});
    } else
      await page
        .locator(
          mode === "drawer" ? ".el-drawer__close-btn" : ".el-dialog__headerbtn",
        )
        .last()
        .click();
  }
  let presentations = 0;
  for (const width of cfg.tailOnly ? [] : [1280, 390]) {
    await up.setViewportSize({ width, height: 900 });
    for (const kind of ["article", "asset", "document"])
      for (const mode of Object.keys(modes)) {
        const target = await open(kind, mode);
        presentations++;
        const frame = target.frames().find(isPluginFrame);
        assert.ok(frame);
        const connects = await frame.evaluate(() => window.__v5Connections);
        assert.equal(connects.length, 1);
        assert.deepEqual(
          Object.keys(connects[0]).sort(),
          [
            "audience",
            "challenge",
            "plugin_key",
            "plugin_version",
            "surface_id",
            "type",
            "version",
          ].sort(),
        );
        assert.ok(
          !frame.url().includes("access_token") &&
            !frame.url().includes("Bearer") &&
            !frame.url().includes("/api/"),
        );
        if (mode === "modal")
          await target.screenshot({
            path: resolve(cfg.output, `${width}-${kind}.png`),
          });
        assert.ok(
          await target.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth + 2,
          ),
        );
        check(
          `${width}px ${kind} ${mode}: real isolated PDF canvas and finite credential-free handshake`,
        );
        await close(target, mode);
      }
  }
  // Real MessagePort probes from the untrusted iframe, including replay.
  await open("article", "modal");
  let pluginFrame = up.frames().find(isPluginFrame);
  const handle = new URL(pluginFrame.url()).searchParams.get("resource_handle");
  await pluginFrame.evaluate((handle) => {
    window.__v5Port.postMessage({
      version: "campusos.bridge/v1",
      request_id: "duplicate-probe",
      method: "resource.describe",
      params: { handle },
    });
    window.__v5Port.postMessage({
      version: "campusos.bridge/v1",
      request_id: "duplicate-probe",
      method: "resource.describe",
      params: { handle },
    });
    window.__v5Port.postMessage({
      version: "campusos.bridge/v1",
      request_id: "forbidden-probe",
      method: "filesystem.read",
      params: { path: "/etc/passwd" },
    });
    window.__v5Port.postMessage({
      version: "campusos.bridge/v1",
      request_id: "extra-probe",
      method: "resource.describe",
      params: { handle, url: "https://attacker.invalid" },
    });
  }, handle);
  await pluginFrame.waitForFunction(
    () =>
      window.__v5Responses.filter((x) =>
        ["duplicate-probe", "forbidden-probe", "extra-probe"].includes(
          x.request_id,
        ),
      ).length === 4,
  );
  const probes = await pluginFrame.evaluate(() =>
    window.__v5Responses.filter((x) => x.request_id.endsWith("-probe")),
  );
  assert.equal(
    probes.filter((p) => p.error === "bridge.invalid_request").length,
    3,
  );
  check(
    "actual iframe MessagePort rejects replay, arbitrary filesystem method and surplus URL parameter",
  );
  const frameURL = pluginFrame.url();
  const attached = up.waitForEvent("frameattached");
  await up.evaluate((src) => {
    const f = document.createElement("iframe");
    f.id = "wrong-source";
    f.src = src;
    document.body.append(f);
  }, frameURL);
  const wrong = await attached;
  await wrong.waitForLoadState();
  assert.equal(
    (await wrong.evaluate(() => window.__v5Connections || [])).length,
    0,
  );
  await up.locator("#wrong-source").evaluate((f) => f.remove());
  check(
    "second real iframe at the same plugin origin cannot claim the active iframe source",
  );
  const evilAttached = up.waitForEvent("frameattached");
  await up.evaluate((src) => {
    const f = document.createElement("iframe");
    f.id = "wrong-origin";
    f.src = src;
    document.body.append(f);
  }, evilOrigin);
  await evilAttached;
  await up.waitForTimeout(200);
  assert.equal(evilRequests, 0);
  await up.locator("#wrong-origin").evaluate((f) => f.remove());
  check(
    "parent CSP blocks actual wrong-Origin iframe navigation before any network request or Bridge port",
  );
  assert.equal(
    (await api(up, "user", "GET", `/plugin-ui/invocations/${handle}`)).status,
    200,
  );
  const bc = await browser.newContext();
  bc.setDefaultTimeout(15000);
  const bp = await bc.newPage();
  await login(bp, cfg.bob);
  for (const path of [
    `/assets/${assetID}/download`,
    `/documents/${document.id}/download`,
    `/plugin-ui/invocations/${handle}`,
  ]) {
    r = await api(bp, "user", "GET", path);
    assert.ok([403, 404, 410].includes(r.status));
  }
  check(
    "second current user cannot read owner asset, personal document or opaque invocation",
  );
  await bc.close();
  await close(up, "modal");
  let releaseHeld, notifyHeld;
  const hold = new Promise((r) => {
      releaseHeld = r;
    }),
    gotHeld = new Promise((r) => {
      notifyHeld = r;
    });
  let heldOnce = false;
  const delayRange = async (route) => {
    if (heldOnce) {
      await route.continue();
      return;
    }
    heldOnce = true;
    const real = await route.fetch();
    notifyHeld();
    await hold;
    await route.fulfill({ response: real }).catch(() => {});
  };
  await up.route("**/api/v1/plugin-ui/invocations/*/content", delayRange);
  const oldOpen = open("article", "modal").catch(() => null);
  await gotHeld;
  await up.locator(".el-dialog__headerbtn").last().click();
  await open("document", "modal");
  releaseHeld();
  await oldOpen;
  await up.unroute("**/api/v1/plugin-ui/invocations/*/content", delayRange);
  pluginFrame = up.frames().find(isPluginFrame);
  const ranges = await pluginFrame.evaluate(() =>
    window.__v5Responses.filter((x) => x.request_id === "pdf-2"),
  );
  assert.equal(ranges.length, 1);
  assert.equal(ranges[0].size, documentPDF.length);
  await close(up, "modal");
  check(
    "late real range response after closing old Port cannot reach replacement iframe with a reused request ID",
  );
  await open("article", "modal");
  pluginFrame = up.frames().find(isPluginFrame);
  assert.equal((await assetFetch(pluginFrame.url())).status, 200);
  // Revoke current Admin Grant while an existing resource/iframe is open.
  await control("/restore-mfa");
  pd = ok(
    await api(
      ap,
      "admin",
      "GET",
      `/admin/plugin-v5/campusos/pdf-viewer/${pdfPackage.version}`,
    ),
  );
  ok(
    await api(
      ap,
      "admin",
      "POST",
      "/admin/plugin-v5/campusos/pdf-viewer/commands",
      {
        kind: "revoke",
        version: pdfPackage.version,
        expected_revision: pd.grant.revision,
        idempotency_key: "revoke-live",
        reason: "验收当前撤权",
      },
    ),
  );
  r = await api(
    up,
    "user",
    "GET",
    "/plugin-ui/invocations/" + handle + "/content",
  );
  assert.equal(r.status, 403);
  const stale = await assetFetch(pluginFrame.url());
  assert.equal(stale.status, 404);
  await up
    .locator(".isolated-plugin-frame iframe")
    .waitFor({ state: "detached", timeout: 10000 });
  check(
    "current Grant revocation denies old resource bytes and old UI asset lease; mounted frame withdrawn",
  );
  for (const path of [
    `/richtext/articles/${article.thread_id}/attachments/${attachment.id}/download`,
    `/assets/${assetID}/download`,
    `/documents/${document.id}/download`,
  ]) {
    r = await api(up, "user", "GET", path);
    assert.equal(r.status, 200);
    assert.ok(r.text.startsWith("%PDF-"));
  }
  check(
    "no active plugin: all three authorized download paths remain available",
  );
  // An actual short-lived Grant expires without a revoke command or DB patch.
  await control("/restore-mfa");
  pd = ok(
    await api(
      ap,
      "admin",
      "GET",
      `/admin/plugin-v5/campusos/pdf-viewer/${pdfPackage.version}`,
    ),
  );
  const deadline = Date.now() + 25000;
  ok(
    await api(
      ap,
      "admin",
      "POST",
      "/admin/plugin-v5/campusos/pdf-viewer/commands",
      {
        kind: "grant",
        version: pdfPackage.version,
        approved: true,
        consumes: pd.release.manifest.consumes,
        expected_revision: pd.grant.revision,
        expires_at_ms: String(deadline),
        reason: "验收真实授权期限",
        idempotency_key: "finite-expiry",
      },
    ),
  );
  const toEnable = ok(
    await api(
      ap,
      "admin",
      "GET",
      `/admin/plugin-v5/campusos/pdf-viewer/${pdfPackage.version}`,
    ),
  );
  ok(
    await api(
      ap,
      "admin",
      "POST",
      "/admin/plugin-v5/campusos/pdf-viewer/commands",
      {
        kind: "enable",
        version: pdfPackage.version,
        expected_generation: toEnable.installation.generation,
        idempotency_key: "finite-expiry-explicit-enable",
      },
    ),
  );
  const own = ok(
    await api(
      up,
      "user",
      "GET",
      `/plugin-v5/campusos/pdf-viewer/${pdfPackage.version}`,
    ),
  );
  ok(
    await api(up, "user", "POST", "/plugin-v5/campusos/pdf-viewer/commands", {
      kind: "consent",
      version: pdfPackage.version,
      accepted: true,
      purpose_hash: own.purpose_hash,
      expected_revision: own.consent?.revision || "0",
      idempotency_key: "finite-expiry-explicit-consent",
    }),
  );
  const finiteManifest = ok(
    await api(
      up,
      "user",
      "GET",
      "/plugin-v5/ui?parent_origin=" + encodeURIComponent(web.origin),
    ),
  );
  const finiteSurface = finiteManifest.plugins.find(
    (p) => p.name === "campusos.pdf-viewer",
  ).ui.surfaces[0];
  assert.equal((await assetFetch(finiteSurface.frame.src)).status, 200);
  const finiteInvocation = ok(
    await api(
      up,
      "user",
      "POST",
      `/plugin-ui/invocations?thread_id=${article.thread_id}`,
      { attachment_id: attachment.id, presentation: "modal" },
    ),
    201,
  );
  const finiteID = finiteInvocation.id;
  assert.ok(finiteID);
  assert.ok(Date.now() < deadline);
  await up.waitForTimeout(Math.max(0, deadline - Date.now() + 100));
  assert.equal((await assetFetch(finiteSurface.frame.src)).status, 404);
  assert.equal(
    (await api(up, "user", "GET", `/plugin-ui/invocations/${finiteID}/content`))
      .status,
    403,
  );
  check(
    "elapsed real Grant expiry rejects a previously valid UI lease and opaque resource invocation",
  );
  await up.goto(web.origin + "/plugins");
  await up
    .getByLabel("可用插件")
    .getByRole("button")
    .filter({ hasText: "campusos/pdf-viewer@" })
    .click();
  const removed = up.waitForResponse(
    (res) =>
      res.request().method() === "POST" &&
      res.url().includes("/pdf-viewer/commands"),
  );
  await up.getByRole("button", { name: "卸载", exact: true }).click();
  assert.equal((await removed).status(), 200);
  await up
    .getByRole("button", { name: "安装到我的账号", exact: true })
    .waitFor();
  check(
    "expired own installation stays visible for explicit uninstall; no plugin is required for business downloads",
  );
  const userID = await identity(up, "user"),
    adminID = await identity(ap, "admin");
  let cross = await fetch(cfg.backend + "/api/v1/admin/plugin-v5", {
    headers: { Authorization: "Bearer " + userID.token },
  });
  assert.notEqual(cross.status, 200);
  cross = await fetch(cfg.backend + "/api/v1/plugin-v5", {
    headers: { Authorization: "Bearer " + adminID.token },
  });
  assert.notEqual(cross.status, 200);
  check(
    "User and Admin credentials remain non-interchangeable on v5 read APIs",
  );
  await ap.goto(admin.origin + "/plugin-config");
  await ap
    .getByLabel("插件版本列表")
    .getByRole("button")
    .filter({ hasText: "campusos/full-config@" })
    .click();
  await ap.getByLabel("精确整数", { exact: true }).waitFor();
  await control("/revoke-configure");
  r = await api(
    ap,
    "admin",
    "POST",
    "/admin/plugin-v5/campusos/full-config/commands",
    {
      kind: "configure",
      version: "1.0.0",
      configuration_part: "secret_refs",
      secret_refs: {},
      expected_revision: "0",
      idempotency_key: "role-revoked",
    },
  );
  assert.equal(r.status, 403);
  await control("/restore-configure");
  check(
    "current PG role loss rejects cached Admin write regardless of visible controls",
  );
  assert.deepEqual(errors, []);
  await writeFile(
    cfg.result,
    JSON.stringify(
      {
        status: "passed",
        mocked_api: false,
        actual_cmd_server: true,
        checks,
        browser_errors: errors,
        pdf_presentations: presentations,
        diagnostic_tail_only: Boolean(cfg.tailOnly),
      },
      null,
      2,
    ) + "\n",
  );
} catch (error) {
  if (browser)
    for (const context of browser.contexts())
      for (const page of context.pages())
        await page
          .screenshot({
            path: resolve(
              cfg.output,
              `failure-${browser.contexts().indexOf(context)}-${context.pages().indexOf(page)}.png`,
            ),
          })
          .catch(() => {});
  throw error;
} finally {
  if (browser) await browser.close();
  await Promise.all([
    new Promise((r) => admin.server.close(r)),
    new Promise((r) => web.server.close(r)),
    new Promise((r) => evil.close(r)),
  ]);
}
