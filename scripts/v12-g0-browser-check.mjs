import { createRequire } from 'node:module'
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { resolve } from 'node:path'
const require = createRequire(resolve('web/package.json'))
const { chromium } = require('playwright-core')
const web = process.env.V12_G0_WEB_URL
const admin = process.env.V12_G0_ADMIN_URL
const output = process.env.V12_G0_OUTPUT
const screenshots = resolve('.cache/v12-g0-browser-screenshots')
mkdirSync(screenshots, { recursive: true })
for (const url of [web, admin]) if (new URL(url).hostname !== '127.0.0.1') throw Error('isolated loopback required')
const checks = []
const browser = await chromium.launch({ executablePath: process.env.CHROME_BIN, headless: true, args: ['--no-sandbox'] })
function check(value, message) { if (!value) throw Error(message) }
async function login(page, origin, email, password, isAdmin = false) {
  await page.goto(origin + '/login', { waitUntil: 'domcontentloaded' })
  await page.getByPlaceholder(isAdmin ? '请输入管理员邮箱' : '请输入邮箱').fill(email)
  await page.getByPlaceholder('请输入密码').fill(password)
  await Promise.all([page.waitForURL(url => url.pathname === '/'), page.locator('form').getByRole('button', { name: '登录', exact: true }).click()])
  check(!await page.evaluate(() => localStorage.getItem('access_token') || sessionStorage.getItem('access_token')), 'bearer token persisted in browser storage')
}
async function noOverflow(page, name) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)
  check(overflow <= 1, name + ': horizontal page overflow ' + overflow)
  checks.push({ name, horizontal_overflow_px: overflow })
}
try {
  for (const viewport of [{ name: 'desktop', width: 1366, height: 900 }, { name: 'mobile', width: 390, height: 844 }]) {
    const context = await browser.newContext({ viewport })
    const page = await context.newPage()
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    await login(page, web, 'g0-owner-a@example.invalid', process.env.V12_G0_OWNER_A_PASSWORD)
    await page.goto(web + '/documents', { waitUntil: 'domcontentloaded' })
    await page.getByRole('heading', { name: '我的文档', exact: true }).waitFor()
    await page.getByRole('cell', { name: 'g0.pdf', exact: true }).waitFor()
    await noOverflow(page, viewport.name + '_documents')
    const pdfRow = page.getByRole('row').filter({ has: page.getByRole('cell', { name: 'g0.pdf', exact: true }) })
    await pdfRow.getByRole('button', { name: '预览', exact: true }).click()
    const frame = page.frameLocator('.isolated-plugin-frame iframe')
    await frame.locator('canvas').waitFor({ timeout: 30000 })
    const isolation = await page.locator('.isolated-plugin-frame iframe').evaluate(element => ({ src: element.getAttribute('src'), sandbox: element.getAttribute('sandbox') }))
    check(new URL(isolation.src).origin !== new URL(web).origin && isolation.sandbox.includes('allow-scripts'), 'PDF iframe isolation absent')
    check(!/access_token|Bearer|password/.test(isolation.src), 'credential leaked into plugin URL')
    checks.push({ name: viewport.name + '_pdf_plugin_canvas', rendered: true, separate_origin: true })
    await page.screenshot({ path: resolve(screenshots, viewport.name + '-pdf.png') })
    await page.keyboard.press('Escape')
    await page.goto(web + '/documents', { waitUntil: 'domcontentloaded' })
    await page.getByRole('button', { name: '新建文本', exact: true }).click()
    const name = 'G0-browser-' + viewport.name + '.txt'
    await page.getByRole('textbox', { name: '文档名称' }).fill(name)
    await page.getByPlaceholder('请输入文本内容', { exact: true }).fill('G0 browser private document')
    await page.getByRole('button', { name: '保存为新版本', exact: true }).click()
    const row = page.getByRole('row').filter({ has: page.getByRole('cell', { name, exact: true }) })
    await row.waitFor()
    const downloadPromise = page.waitForEvent('download')
    await row.getByRole('button', { name: '下载', exact: true }).click()
    const download = await downloadPromise
    check(readFileSync(await download.path(), 'utf8') === 'G0 browser private document', 'browser download bytes mismatch')
    await row.getByRole('button', { name: '移入回收站', exact: true }).click()
    await page.getByRole('button', { name: '确定', exact: true }).click()
    await row.waitFor({ state: 'hidden' })
    await page.getByRole('button', { name: '回收站', exact: true }).click()
    await row.getByRole('button', { name: '恢复', exact: true }).click()
    await page.getByRole('button', { name: '确定', exact: true }).click()
    await page.getByRole('button', { name: '我的文档', exact: true }).click()
    await row.waitFor()
    checks.push({ name: viewport.name + '_create_download_trash_restore', passed: true })
    await page.screenshot({ path: resolve(screenshots, viewport.name + '-documents.png') })
    check(errors.length === 0, viewport.name + ': uncaught browser errors: ' + errors.join('; '))
    await context.close()

    const adminContext = await browser.newContext({ viewport })
    const adminPage = await adminContext.newPage()
    await login(adminPage, admin, 'admin@campusos.local', process.env.V12_G0_BOOTSTRAP_PASSWORD, true)
    await adminPage.goto(admin + '/admin-admission', { waitUntil: 'domcontentloaded' })
    await adminPage.getByRole('heading', { name: '管理员准入', exact: true }).waitFor()
    await noOverflow(adminPage, viewport.name + '_admin_admission')
    await adminPage.goto(admin + '/plugins', { waitUntil: 'domcontentloaded' })
    await adminPage.getByText('PDF 文档预览', { exact: true }).first().waitFor()
    await noOverflow(adminPage, viewport.name + '_admin_plugins')
    await adminPage.screenshot({ path: resolve(screenshots, viewport.name + '-admin.png') })
    await adminContext.close()
  }
  const report = { schema: 'campusos.v12-g0-browser/v1', generated_at: new Date().toISOString(), browser: await browser.version(),
    platform: process.platform, plugin_and_document_checks: JSON.parse(readFileSync(resolve(process.env.V12_G0_WORK_DIR, 'plugin-baseline.json'), 'utf8')).checks, checks, viewport_scope: 'Chromium desktop and mobile viewport; not physical mobile or Windows',
    screenshots: '.cache/v12-g0-browser-screenshots', gates: 'passed' }
  writeFileSync(output, JSON.stringify(report, null, 2) + '\n')
  console.log('G0 browser passed: desktop/mobile Web, isolated PDF canvas, document lifecycle and Admin')
} finally { await browser.close() }
