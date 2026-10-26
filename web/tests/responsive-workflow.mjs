import { chromium } from 'playwright-core'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {
  assertResponsiveMatrix,
  assertResponsiveRoute,
  responsivePaths,
  responsiveViewports,
} from './helpers/responsive-contract.mjs'

const chrome = process.env.CHROME_BIN
const webURL = (process.env.WEB_URL || 'http://localhost:3000').replace(/\/$/, '')
const adminURL = (process.env.ADMIN_URL || 'http://localhost:3001').replace(/\/$/, '')
const email = process.env.CAMPUSOS_ADMIN_EMAIL || 'admin@campusos.local'
const password = process.env.CAMPUSOS_ADMIN_PASSWORD || 'Admin@123456'
const screenshotDir = process.env.RESPONSIVE_SCREENSHOT_DIR || path.join(os.tmpdir(), 'campusos-responsive-smoke')
const requestedViewports = (process.env.RESPONSIVE_VIEWPORTS || '')
  .split(',')
  .map((value) => value.trim())
  .filter(Boolean)
const viewports = requestedViewports.length
  ? responsiveViewports.filter((viewport) => requestedViewports.includes(viewport.name))
  : responsiveViewports

if (!chrome) throw new Error('CHROME_BIN is required')
if (!viewports.length) throw new Error(`no responsive viewports matched: ${requestedViewports.join(', ')}`)
fs.mkdirSync(screenshotDir, { recursive: true })

const browser = await chromium.launch({ executablePath: chrome, headless: true, args: ['--no-sandbox'] })
const routeRecords = []
let activePage
let activeRoute

function diagnosticURL(value) {
  try {
    const url = new URL(value)
    return ['http:', 'https:'].includes(url.protocol) ? url.origin + url.pathname : url.protocol + url.pathname
  } catch {
    return 'invalid-url'
  }
}

function diagnosticMessage(value) {
  return String(value)
    .replace(/https?:\/\/[^\s"'<>]+/g, (url) => diagnosticURL(url))
    .replace(/[?#][^\s"'<>)]*/g, '')
    .slice(0, 600)
}

function flushRouteRecords() {
  fs.writeFileSync(path.join(screenshotDir, 'route-records.json'), JSON.stringify(routeRecords, null, 2) + '\n')
}

function recordPageEvent(page, type, details) {
  if (page !== activePage || !activeRoute) return
  activeRoute.actual_page_url = diagnosticURL(page.url())
  if (activeRoute.events.length < 64) {
    activeRoute.events.push({ at: new Date().toISOString(), type, ...details })
  } else {
    activeRoute.omitted_events += 1
  }
}

function trackPageDiagnostics(page) {
  page.on('framenavigated', (frame) => {
    if (frame === page.mainFrame()) recordPageEvent(page, 'mainframe_navigation', { url: diagnosticURL(frame.url()) })
  })
  page.on('pageerror', (error) => {
    recordPageEvent(page, 'pageerror', { name: error.name, message: diagnosticMessage(error.message) })
  })
  page.on('console', (message) => {
    if (['error', 'warning'].includes(message.type())) {
      recordPageEvent(page, 'console', { level: message.type(), message: diagnosticMessage(message.text()) })
    }
  })
  page.on('requestfailed', (request) => {
    recordPageEvent(page, 'requestfailed', {
      url: diagnosticURL(request.url()),
      resource_type: request.resourceType(),
      error: diagnosticMessage(request.failure()?.errorText || ''),
    })
  })
}

function beginRoute(page, surface, requestedPathname, viewport) {
  activePage = page
  activeRoute = {
    surface,
    requested_pathname: requestedPathname,
    viewport: { ...viewport },
    started_at: new Date().toISOString(),
    actual_page_url: diagnosticURL(page.url()),
    status: 'running',
    events: [],
    omitted_events: 0,
  }
  routeRecords.push(activeRoute)
  flushRouteRecords()
}

function completeRoute(page) {
  activeRoute.actual_page_url = diagnosticURL(page.url())
  assertResponsiveRoute(activeRoute, activeRoute.surface === 'web' ? webURL : adminURL)
  activeRoute.completed_at = new Date().toISOString()
  activeRoute.status = 'passed'
  flushRouteRecords()
}

async function recordFailure(error) {
  const failure = {
    status: 'failed',
    error: { name: error.name, message: diagnosticMessage(error.message) },
    route: activeRoute,
    diagnostic_errors: [],
  }
  if (activeRoute) {
    activeRoute.status = 'failed'
    activeRoute.actual_page_url = diagnosticURL(activePage.url())
  }
  try {
    failure.dom = await activePage.evaluate(() => {
      const describe = (selector) => {
        const elements = [...document.querySelectorAll(selector)]
        return {
          count: elements.length,
          first: elements.slice(0, 3).map((element) => {
            const style = getComputedStyle(element)
            const box = element.getBoundingClientRect()
            return {
              visible: box.width > 0 && box.height > 0 && style.display !== 'none' && style.visibility !== 'hidden',
              display: style.display,
              visibility: style.visibility,
              width: box.width,
              height: box.height,
            }
          }),
        }
      }
      const app = document.querySelector('#app')?.__vue_app__
      const route = app?.config.globalProperties.$route
      return {
        ready_state: document.readyState,
        route: route
          ? {
              pathname: route.path,
              name: typeof route.name === 'string' ? route.name : null,
              has_id_parameter: Boolean(route.params.id),
              id_is_create: route.params.id === 'create',
              matched: route.matched.slice(0, 8).map(({ path, name }) => ({ path, name: String(name || '') })),
            }
          : null,
        roots: Object.fromEntries(
          [
            '#app',
            '[data-campusos-required-region="page-outlet"]',
            '.thread-list',
            '.create-thread',
            '.publish-mode-picker',
          ].map((selector) => [selector, describe(selector)]),
        ),
        headings: [...document.querySelectorAll('h1, h2, h3')]
          .slice(0, 12)
          .map((element) => element.textContent.trim().slice(0, 100)),
        active_element: { tag: document.activeElement?.tagName, role: document.activeElement?.getAttribute('role') },
      }
    })
  } catch (cause) {
    failure.diagnostic_errors.push('DOM: ' + diagnosticMessage(cause.message))
  }
  try {
    flushRouteRecords()
    fs.writeFileSync(path.join(screenshotDir, 'failure.json'), JSON.stringify(failure, null, 2) + '\n')
  } catch (cause) {
    console.error('responsive diagnostic write failed: ' + diagnosticMessage(cause.message))
  }
  try {
    await activePage.screenshot({ path: path.join(screenshotDir, 'failure.png'), fullPage: false, timeout: 5000 })
  } catch (cause) {
    failure.diagnostic_errors.push('screenshot: ' + diagnosticMessage(cause.message))
    try {
      fs.writeFileSync(path.join(screenshotDir, 'failure.json'), JSON.stringify(failure, null, 2) + '\n')
    } catch {
      // Diagnostic I/O must not replace the original browser assertion.
    }
  }
}

async function login(page, baseURL, emailPlaceholder, passwordPlaceholder) {
  await navigate(page, `${baseURL}/login`)
  await page
    .getByPlaceholder(emailPlaceholder)
    .fill(baseURL === adminURL ? process.env.CAMPUSOS_ADMIN_USERNAME || email : email)
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
  let focusable = { available: false, focused: false, candidates: 0 }
  for (let attempt = 0; attempt < 5; attempt += 1) {
    focusable = await evaluateWhenStable(page, () => {
      const candidates = [
        ...document.querySelectorAll(
          'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ),
      ].filter((element) => {
        const style = window.getComputedStyle(element)
        return (
          element.getClientRects().length > 0 &&
          style.display !== 'none' &&
          style.visibility !== 'hidden' &&
          !element.closest('[inert]')
        )
      })
      const target = candidates[0]
      target?.focus({ preventScroll: true })
      return {
        available: Boolean(target),
        focused: Boolean(target) && document.activeElement === target,
        candidates: candidates.length,
      }
    })
    if (focusable.available && focusable.focused) return
    await page.waitForTimeout(200)
  }
  throw new Error(`${label} has no keyboard-focusable control: ${JSON.stringify(focusable)}`)
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
  trackPageDiagnostics(webPage)
  await login(webPage, webURL, '请输入邮箱', '请输入密码')
  // The login page probes the HttpOnly refresh cookie before credentials are
  // submitted. Its expected 401 is not part of the authenticated page matrix.
  webErrors.length = 0
  const webPaths = responsivePaths.web
  for (const viewport of viewports) {
    await webPage.setViewportSize(viewport)
    const compactWeb = viewport.width <= 760 || (viewport.height <= 540 && viewport.width <= 1000)
    for (const route of webPaths) {
      beginRoute(webPage, 'web', route, viewport)
      await navigate(webPage, `${webURL}${route}`)
      await webPage.evaluate(() => window.scrollTo(0, 0))
      await waitForSettledPage(webPage)
      if (route === '/threads') {
        await webPage.getByRole('button', { name: '消息通知' }).waitFor()
        if (compactWeb) {
          await webPage.locator('[aria-label="移动端帖子列表"]').waitFor()
          await webPage.getByRole('button', { name: '打开主导航' }).click()
          await webPage.locator('.mobile-navigation-list').waitFor()
          await webPage.getByRole('navigation', { name: '移动端站点导航' }).waitFor()
          await webPage.keyboard.press('Escape')
          await webPage.locator('.mobile-navigation-list').waitFor({ state: 'hidden' })
        } else {
          await webPage.locator('.primary-nav[aria-label="站点主导航"]').waitFor()
          if (await webPage.getByRole('button', { name: '打开主导航' }).count()) {
            throw new Error(`web desktop navigation exposed the compact trigger at ${viewport.name}`)
          }
        }
      }
      if (route === '/threads/create') {
        const picker = webPage.locator('.publish-mode-picker')
        await picker.waitFor()
        if (await picker.locator('.publish-mode-select').isVisible()) {
          await picker.locator('.publish-mode-select').click()
          await webPage.getByRole('option', { name: '校园互助' }).waitFor()
          await webPage.getByRole('option', { name: '校园二手' }).waitFor()
          await webPage.keyboard.press('Escape')
        } else {
          await picker.getByText('校园互助', { exact: true }).waitFor()
          await picker.getByText('校园二手', { exact: true }).waitFor()
        }
      }
      if (route === '/mutual-aid/create') {
        await webPage.getByRole('heading', { name: '发布校园互助' }).waitFor()
      }
      if (route === '/secondhand/create') {
        await webPage.getByRole('heading', { name: '发布校园二手' }).waitFor()
      }
      if (route === '/schedule') {
        const untranslatedControls = await webPage
          .locator('[aria-label="decrease number"], [aria-label="increase number"]')
          .count()
        if (untranslatedControls) {
          throw new Error(`schedule exposed ${untranslatedControls} untranslated number controls at ${viewport.name}`)
        }
        if (compactWeb) {
          const periods = webPage.locator('.toolbar .el-input-number')
          await periods.waitFor()
          const controls = periods.locator('[aria-label="减少数值"], [aria-label="增加数值"]')
          await controls.first().waitFor()
          const count = await controls.count()
          if (count !== 2) throw new Error(`schedule daily-period controls are missing at ${viewport.name}`)
          for (let index = 0; index < count; index += 1) {
            const box = await controls.nth(index).boundingBox()
            if (box && (box.width < 42 || box.height < 42)) {
              throw new Error(
                `schedule number control ${index} is ${Math.round(box.width)}x${Math.round(box.height)} at ${viewport.name}`,
              )
            }
          }
        }
      }
      await webPage.evaluate(() => window.scrollTo(0, 0))
      await assertNoOverflow(webPage, `web ${route} at ${viewport.name}`)
      await assertRenderedMedia(webPage, `web ${route} at ${viewport.name}`)
      await webPage.screenshot({
        path: path.join(
          screenshotDir,
          `web-${route === '/' ? 'home' : route.slice(1).replaceAll('/', '-')}-${viewport.name}.png`,
        ),
        fullPage: false,
      })
      completeRoute(webPage)
    }
  }
  if (webErrors.length) throw new Error(`web console errors: ${webErrors.join(' | ')}`)
  await webContext.close()

  const adminContext = await browser.newContext({ viewport: viewports[0] })
  const adminPage = await adminContext.newPage()
  const adminErrors = []
  collectPageErrors(adminPage, adminErrors)
  trackPageDiagnostics(adminPage)
  await login(adminPage, adminURL, '请输入管理员用户名或邮箱', '请输入密码')
  adminErrors.length = 0
  const adminPaths = responsivePaths.admin
  for (const viewport of viewports) {
    await adminPage.setViewportSize(viewport)
    const compactAdmin = viewport.width <= 800 || (viewport.height <= 540 && viewport.width <= 1000)
    for (const route of adminPaths) {
      beginRoute(adminPage, 'admin', route, viewport)
      await navigate(adminPage, `${adminURL}${route}`)
      await adminPage.evaluate(() => window.scrollTo(0, 0))
      await waitForSettledPage(adminPage)
      if (compactAdmin && route === '/extensions') {
        await adminPage.getByRole('button', { name: '打开导航' }).click()
        await adminPage.getByRole('menuitem', { name: '扩展与集成', exact: true }).waitFor()
        const scrim = adminPage.locator('.nav-scrim')
        const scrimBox = await scrim.boundingBox()
        if (!scrimBox) throw new Error(`admin navigation scrim is not visible at ${viewport.name}`)
        await scrim.click({
          position: {
            x: Math.max(1, scrimBox.width - 8),
            y: Math.min(24, Math.max(1, scrimBox.height - 1)),
          },
        })
        await adminPage.waitForFunction(() => !document.querySelector('.admin-aside')?.classList.contains('is-open'))
        await adminPage.waitForTimeout(220)
      }
      if (route === '/threads') {
        await adminPage.locator('[aria-label="帖子批量操作"]').waitFor()
        await adminPage.locator('.thread-table .el-table__header-wrapper .el-checkbox').waitFor()
      }
      if (!compactAdmin && route === '/extensions') {
        await adminPage.getByRole('button', { name: '收起侧边栏' }).click()
        await adminPage.waitForFunction(() => {
          const aside = document.querySelector('.admin-aside')
          return aside?.classList.contains('is-collapsed') && aside.getBoundingClientRect().width <= 66
        })
        const collapsedBox = await adminPage.locator('.admin-aside').boundingBox()
        if (!collapsedBox || collapsedBox.width > 66) {
          throw new Error(`admin sidebar did not collapse at ${viewport.name}: ${JSON.stringify(collapsedBox)}`)
        }
        await adminPage.getByRole('button', { name: '展开侧边栏' }).click()
        await adminPage.waitForFunction(() => {
          const aside = document.querySelector('.admin-aside')
          return aside && !aside.classList.contains('is-collapsed') && aside.getBoundingClientRect().width >= 218
        })
      }
      await assertNoOverflow(adminPage, `admin ${route} at ${viewport.name}`)
      await assertRenderedMedia(adminPage, `admin ${route} at ${viewport.name}`)
      await adminPage.screenshot({
        path: path.join(screenshotDir, `admin-${route === '/' ? 'home' : route.slice(1)}-${viewport.name}.png`),
        fullPage: false,
      })
      completeRoute(adminPage)
    }
  }
  if (adminErrors.length) throw new Error(`admin console errors: ${adminErrors.join(' | ')}`)
  assertResponsiveMatrix(routeRecords, { web: webURL, admin: adminURL }, viewports)
  await adminContext.close()
  console.log(
    `responsive workflow passed at ${viewports.map((viewport) => viewport.name).join(', ')}; screenshots: ${screenshotDir}`,
  )
} catch (error) {
  try {
    await recordFailure(error)
  } catch (cause) {
    console.error('responsive diagnostic capture failed: ' + diagnosticMessage(cause.message))
  }
  throw error
} finally {
  await browser.close()
}
