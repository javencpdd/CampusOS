import assert from 'node:assert/strict'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright-core'
import { createServer } from 'vite'

// Real ThemeRoot, theme CSS and Element Plus; no accounts, API or user data.
const root = fileURLToPath(new URL('../../', import.meta.url))
const webRoot = resolve(root, 'web')
const reportDir = resolve(root, process.env.CAMPUSOS_DIALOG_REPORT_DIR || '.cache/dialog-scroll-browser')
const canvasCSS = await readFile(resolve(root, 'data/resources/themes/campus-canvas/styles/theme.css'), 'utf8')
const fixturePath = '/__dialog-scroll-fixture.js'
const fixture = `
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'
import { ElDialog } from 'element-plus'
import 'element-plus/dist/index.css'
import '/src/styles/responsive.css'
import ThemeRoot from '/src/campus-ui/ThemeRoot.vue'
import { useWebThemeStore } from '/src/modules/appearance/store'
const theme = new URLSearchParams(location.search).get('theme')
if (theme === 'canvas') {
  const style = document.createElement('style')
  style.textContent = ${JSON.stringify(canvasCSS)}
  document.head.appendChild(style)
}
const visible = ref(false), mounted = ref(true)
const app = createApp({ render: () => h(ThemeRoot, {}, { default: () => [
  h('header', { style: 'height:340px;background:#cfe2e8' }, 'CampusOS'),
  h('main', { class: 'campus-shell-main', style: 'padding:20px' }, [
    h('section', { class: 'thread-list' }, [
      h('button', { onClick: () => visible.value = true }, '打开长弹窗'),
      mounted.value ? h(ElDialog, {
        modelValue: visible.value,
        'onUpdate:modelValue': value => visible.value = value,
        title: '滚动回归', width: 'min(600px, calc(100vw - 24px))', destroyOnClose: true,
      }, { default: () => [
        h('p', {}, '弹窗内容应当可以上下滚动，背景页面在关闭后恢复滚动。'),
        h('div', { style: 'height:1200px' }),
        h('button', { id: 'dialog-bottom' }, '底部操作'),
      ] }) : null,
    ]),
    h('div', { style: 'height:1800px;background:linear-gradient(white,#b4cbc5)' }, '背景页面'),
  ]),
] }) })
const pinia = createPinia()
app.use(pinia)
useWebThemeStore(pinia).activePackage = {
  manifest: { layout: { animation_preset: theme === 'reveal' ? 'reveal' : 'none' } },
}
app.mount('#app')
window.dialogFixture = { unmount: () => mounted.value = false }
`
const cases = ['reveal', 'canvas', 'none'].flatMap((theme) =>
  [
    { name: 'desktop', width: 1280, height: 710 },
    { name: 'phone', width: 375, height: 667 },
    { name: 'landscape', width: 844, height: 390 },
  ].map((viewport) => ({ theme, viewport, reducedMotion: 'no-preference' })),
)
cases.push({ theme: 'reveal', viewport: { name: 'reduced-motion', width: 1280, height: 710 }, reducedMotion: 'reduce' })
const report = { cases: [], errors: [], apiRequests: 0 }
let browser, server, page, cacheDir
try {
  await mkdir(reportDir, { recursive: true })
  cacheDir = await mkdtemp(resolve(tmpdir(), 'campusos-dialog-vite-'))
  server = await createServer({
    root: webRoot,
    configFile: resolve(webRoot, 'vite.config.ts'),
    cacheDir,
    plugins: [
      {
        name: 'dialog-scroll-regression',
        resolveId(id) {
          if (id === fixturePath) return '\0dialog-scroll-regression'
        },
        load(id) {
          if (id === '\0dialog-scroll-regression') return fixture
        },
        configureServer(vite) {
          vite.middlewares.use((request, response, next) => {
            if (!request.url?.startsWith('/dialog-scroll.html')) return next()
            response.setHeader('Content-Type', 'text/html; charset=utf-8')
            response.end(
              '<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1"><div id="app"></div><script type="module" src="' +
                fixturePath +
                '"></script>',
            )
          })
        },
      },
    ],
    server: { host: '127.0.0.1', port: 0, strictPort: true, open: false },
  })
  await server.listen()
  const origin = `http://127.0.0.1:${server.httpServer.address().port}`
  browser = await chromium.launch({
    executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome',
    headless: true,
    args: ['--no-sandbox', '--no-proxy-server'],
  })
  for (const { theme, viewport, reducedMotion } of cases) {
    const context = await browser.newContext({ viewport, reducedMotion })
    page = await context.newPage()
    page.setDefaultTimeout(10000)
    page.on('pageerror', (error) => report.errors.push(error.message))
    page.on('request', (request) => {
      if (new URL(request.url()).pathname.startsWith('/api/')) report.apiRequests++
    })
    const row = { theme, viewport: viewport.name }
    report.cases.push(row)
    await page.goto(`${origin}/dialog-scroll.html?theme=${theme}`)
    await page.waitForFunction(
      () => window.dialogFixture && document.getAnimations().every((a) => a.playState === 'finished'),
    )
    await page.evaluate(() => window.scrollTo(0, 100))
    await page.getByRole('button', { name: '打开长弹窗', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '滚动回归', exact: true })
    await dialog.waitFor()
    await page.waitForFunction(
      () =>
        document.body.classList.contains('el-popup-parent--hidden') &&
        document.getAnimations().every((a) => a.playState === 'finished'),
    )
    row.geometry = await dialog.evaluate((element) => {
      const overlay = element.closest('.el-overlay-dialog').getBoundingClientRect()
      const heading = element.querySelector('.el-dialog__header').getBoundingClientRect()
      return {
        overlayTop: overlay.top,
        overlayHeight: overlay.height,
        headingTop: heading.top,
        headingBottom: heading.bottom,
      }
    })
    assert.ok(Math.abs(row.geometry.overlayTop) < 1, 'overlay must start at the viewport top')
    assert.ok(Math.abs(row.geometry.overlayHeight - viewport.height) < 1, 'overlay must cover the full viewport')
    assert.ok(
      row.geometry.headingTop >= 0 && row.geometry.headingBottom <= viewport.height,
      'dialog heading must remain accessible',
    )
    const backgroundY = await page.evaluate(() => window.scrollY)
    await page.mouse.move(viewport.width / 2, viewport.height / 2)
    await page.mouse.wheel(0, 10000)
    await page.waitForFunction(() => {
      const element = document.querySelector('#dialog-bottom').getBoundingClientRect()
      return element.top >= 0 && element.bottom <= innerHeight
    })
    assert.equal(
      await page.evaluate(() => window.scrollY),
      backgroundY,
      'scrolling the modal must keep the background fixed',
    )
    await page.screenshot({ path: resolve(reportDir, `${theme}-${viewport.name}-bottom.png`) })
    await page.mouse.wheel(0, -10000)
    await page.waitForFunction(() => document.querySelector('.el-overlay-dialog').scrollTop === 0)
    await dialog.locator('.el-dialog__headerbtn').click()
    await dialog.waitFor({ state: 'hidden' })
    await page.waitForFunction(() => !document.body.classList.contains('el-popup-parent--hidden'))
    assert.equal(
      await page.evaluate(() => window.scrollY),
      backgroundY,
      'closing must preserve the background position',
    )
    await page.mouse.wheel(0, 400)
    await page.waitForFunction((previous) => window.scrollY > previous, backgroundY)
    row.wheelBothDirections = true
    row.backgroundRestored = true
    for (const action of ['escape', 'unmount']) {
      await page.getByRole('button', { name: '打开长弹窗', exact: true }).click()
      await dialog.waitFor()
      await page.waitForFunction(() => document.body.classList.contains('el-popup-parent--hidden'))
      if (action === 'escape') await page.keyboard.press('Escape')
      else await page.evaluate(() => window.dialogFixture.unmount())
      await dialog.waitFor({ state: 'hidden' })
      await page.waitForFunction(() => !document.body.classList.contains('el-popup-parent--hidden'))
    }
    row.escapeAndUnmountReleaseLock = true
    row.passed = true
    await context.close()
  }
  assert.deepEqual(report.errors, [], 'no uncaught browser errors')
  assert.equal(report.apiRequests, 0, 'layout regression must not access real accounts or pairing endpoints')
  report.passed = true
} catch (error) {
  report.failure = { type: error.name, message: error.message.split('\n')[0] }
  await page?.screenshot({ path: resolve(reportDir, 'failure.png') }).catch(() => {})
  process.exitCode = 1
} finally {
  await browser?.close()
  await server?.close()
  if (cacheDir) await rm(cacheDir, { recursive: true, force: true })
  await writeFile(resolve(reportDir, 'report.json'), JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify(report))
}
