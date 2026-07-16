import { chromium } from 'playwright-core'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'

const chrome = process.env.CHROME_BIN
const webURL = (process.env.WEB_URL || 'http://localhost:3000').replace(/\/$/, '')
const adminURL = (process.env.ADMIN_URL || 'http://localhost:3001').replace(/\/$/, '')
const email = process.env.CAMPUSOS_ADMIN_EMAIL || 'admin@campusos.local'
const password = process.env.CAMPUSOS_ADMIN_PASSWORD || 'Admin@123456'
const screenshotDir = process.env.RESPONSIVE_SCREENSHOT_DIR || path.join(os.tmpdir(), 'campusos-responsive-smoke')
const allViewports = [
  { name: 'phone-narrow', width: 320, height: 568 },
  { name: 'phone', width: 360, height: 800 },
  { name: 'phone-large', width: 393, height: 852 },
  { name: 'tablet-portrait', width: 768, height: 1024 },
  { name: 'tablet-landscape', width: 1024, height: 768 },
  { name: 'desktop', width: 1366, height: 768 },
  { name: 'phone-landscape', width: 852, height: 393 },
]
const requestedViewports = (process.env.RESPONSIVE_VIEWPORTS || '')
  .split(',')
  .map((value) => value.trim())
  .filter(Boolean)
const viewports = requestedViewports.length
  ? allViewports.filter((viewport) => requestedViewports.includes(viewport.name))
  : allViewports

if (!chrome) throw new Error('CHROME_BIN is required')
if (!viewports.length) throw new Error(`no responsive viewports matched: ${requestedViewports.join(', ')}`)
fs.mkdirSync(screenshotDir, { recursive: true })

const browser = await chromium.launch({ executablePath: chrome, headless: true, args: ['--no-sandbox'] })

async function login(page, baseURL, emailPlaceholder, passwordPlaceholder) {
  await navigate(page, `${baseURL}/login`)
  await page.getByPlaceholder(emailPlaceholder).fill(email)
  await page.getByPlaceholder(passwordPlaceholder).fill(password)
  await Promise.all([
    page.waitForURL((url) => url.pathname === '/', { timeout: 15_000 }),
    page.locator('form').getByRole('button', { name: '登录', exact: true }).click(),
  ])
}

async function navigate(page, url) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      await page.goto(url, { waitUntil: 'domcontentloaded' })
      return
    } catch (error) {
      if (!String(error).includes('net::ERR_ABORTED') || attempt === 2) throw error
      await page.waitForTimeout(500)
    }
  }
}

async function waitForSettledPage(page) {
  await page.waitForFunction(() => document.querySelector('#app')?.childElementCount > 0)
  // Vue mounts the route before onMounted starts its API requests. Allow that
  // tick, then require Element Plus loading masks to be gone before capturing.
  await page.waitForTimeout(250)
  await page.waitForFunction(() => {
    return ![...document.querySelectorAll('.el-loading-mask')].some((element) => {
      const style = window.getComputedStyle(element)
      return style.display !== 'none' && style.visibility !== 'hidden' && style.opacity !== '0'
    })
  })
  await page.waitForTimeout(120)
}

async function evaluateWhenStable(page, expression) {
  let lastError
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      return await page.evaluate(expression)
    } catch (error) {
      lastError = error
      if (!String(error).includes('Execution context was destroyed') || attempt === 2) throw error
      await page.waitForLoadState('domcontentloaded')
      await waitForSettledPage(page)
    }
  }
  throw lastError
}

async function assertNoOverflow(page, label) {
  const overflow = await evaluateWhenStable(page, () => document.documentElement.scrollWidth - window.innerWidth)
  if (overflow > 1) throw new Error(`${label} has ${overflow}px horizontal overflow`)
  const focusable = await evaluateWhenStable(page, () => {
    const target = document.querySelector(
      'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled])',
    )
    target?.focus()
    return Boolean(target) && document.activeElement === target
  })
  if (!focusable) throw new Error(`${label} has no keyboard-focusable control`)
}

async function assertRenderedMedia(page, label) {
  const failures = await evaluateWhenStable(page, () => {
    const brokenImages = [...document.images]
      .filter((image) => image.currentSrc && (!image.complete || image.naturalWidth === 0))
      .map((image) => image.currentSrc)
    const blankCanvas = [...document.querySelectorAll('canvas')].filter(
      (canvas) => canvas.width === 0 || canvas.height === 0,
    ).length
    return { brokenImages, blankCanvas }
  })
  if (failures.brokenImages.length || failures.blankCanvas) {
    throw new Error(`${label} has media render failures: ${JSON.stringify(failures)}`)
  }
}

function collectPageErrors(page, errors) {
  page.on('pageerror', (error) => errors.push(`pageerror: ${error.message}`))
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(`console: ${message.text()}`)
  })
}

try {
  const webContext = await browser.newContext({ viewport: viewports[0] })
  const webPage = await webContext.newPage()
  const webErrors = []
  collectPageErrors(webPage, webErrors)
  await login(webPage, webURL, '请输入邮箱', '请输入密码')
  const webPaths = ['/', '/threads', '/appearance', '/space/settings', '/schedule', '/plugins']
  for (const viewport of viewports) {
    await webPage.setViewportSize(viewport)
    for (const route of webPaths) {
      await navigate(webPage, `${webURL}${route}`)
      await webPage.evaluate(() => window.scrollTo(0, 0))
      await waitForSettledPage(webPage)
      await assertNoOverflow(webPage, `web ${route} at ${viewport.name}`)
      await assertRenderedMedia(webPage, `web ${route} at ${viewport.name}`)
      await webPage.screenshot({
        path: path.join(
          screenshotDir,
          `web-${route === '/' ? 'home' : route.slice(1).replaceAll('/', '-')}-${viewport.name}.png`,
        ),
        fullPage: false,
      })
    }
  }
  if (webErrors.length) throw new Error(`web console errors: ${webErrors.join(' | ')}`)
  await webContext.close()

  const adminContext = await browser.newContext({ viewport: viewports[0] })
  const adminPage = await adminContext.newPage()
  const adminErrors = []
  collectPageErrors(adminPage, adminErrors)
  await login(adminPage, adminURL, '请输入管理员邮箱', '请输入密码')
  const adminPaths = [
    '/',
    '/users',
    '/permissions',
    '/plugins',
    '/plugin-center',
    '/extensions',
    '/features',
    '/appearance',
    '/integrations',
    '/architecture',
  ]
  for (const viewport of viewports) {
    await adminPage.setViewportSize(viewport)
    for (const route of adminPaths) {
      await navigate(adminPage, `${adminURL}${route}`)
      await adminPage.evaluate(() => window.scrollTo(0, 0))
      await waitForSettledPage(adminPage)
      if ((viewport.width <= 800 || viewport.height <= 480) && route === '/extensions') {
        await adminPage.getByRole('button', { name: '打开导航' }).click()
        await adminPage.getByRole('menuitem', { name: '扩展与集成', exact: true }).waitFor()
        await adminPage.locator('.nav-scrim').click()
        await adminPage.waitForFunction(() => !document.querySelector('.admin-aside')?.classList.contains('is-open'))
        await adminPage.waitForTimeout(220)
      }
      await assertNoOverflow(adminPage, `admin ${route} at ${viewport.name}`)
      await assertRenderedMedia(adminPage, `admin ${route} at ${viewport.name}`)
      await adminPage.screenshot({
        path: path.join(screenshotDir, `admin-${route === '/' ? 'home' : route.slice(1)}-${viewport.name}.png`),
        fullPage: false,
      })
    }
  }
  if (adminErrors.length) throw new Error(`admin console errors: ${adminErrors.join(' | ')}`)
  await adminContext.close()
  console.log(
    `responsive workflow passed at ${viewports.map((viewport) => viewport.name).join(', ')}; screenshots: ${screenshotDir}`,
  )
} finally {
  await browser.close()
}
