import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright-core'

const chrome = process.env.CHROME_BIN
const webURL = (process.env.WEB_URL || 'http://localhost:3000').replace(/\/$/, '')
const adminURL = (process.env.ADMIN_URL || 'http://localhost:3001').replace(/\/$/, '')
const email = process.env.CAMPUSOS_ADMIN_EMAIL || 'admin@campusos.local'
const password = process.env.CAMPUSOS_ADMIN_PASSWORD || 'Admin@123456'
const outputDir = process.env.STYLE_PACK_SCREENSHOT_DIR || path.join(os.tmpdir(), 'campusos-style-pack-smoke')
const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')

function discoverResourcePacks(directory, expectedType) {
  const root = path.join(repositoryRoot, 'data', 'resources', directory)
  const packs = fs
    .readdirSync(root, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => {
      const manifestPath = path.join(root, entry.name, 'resource.json')
      if (!fs.existsSync(manifestPath)) throw new Error(`${directory}/${entry.name} is missing resource.json`)
      const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'))
      if (manifest.id !== entry.name || manifest.type !== expectedType) {
        throw new Error(
          `${directory}/${entry.name} has inconsistent resource metadata: ${JSON.stringify({ id: manifest.id, type: manifest.type })}`,
        )
      }
      return manifest.id
    })
    .sort()
  if (!packs.length) throw new Error(`no ${expectedType} resource packages found under ${root}`)
  return packs
}

const systemPacks = discoverResourcePacks('themes', 'theme')
const homepagePacks = discoverResourcePacks('homepage-packs', 'homepage-pack')
const spacePacks = discoverResourcePacks('space-style-packs', 'space-style-pack')
const profiles = [
  { id: 'desktop', width: 1440, height: 1000 },
  { id: 'mobile-narrow', width: 320, height: 568 },
  { id: 'mobile', width: 390, height: 844 },
  { id: 'mobile-landscape', width: 852, height: 393 },
  { id: 'bridge', width: 768, height: 1024 },
]

if (!chrome) throw new Error('CHROME_BIN is required')
fs.mkdirSync(outputDir, { recursive: true })

const browser = await chromium.launch({ executablePath: chrome, headless: true, args: ['--no-sandbox'] })

async function login(page) {
  await page.goto(`${webURL}/login`, { waitUntil: 'domcontentloaded' })
  await page.getByPlaceholder('请输入邮箱').fill(email)
  await page.getByPlaceholder('请输入密码').fill(password)
  await Promise.all([
    page.waitForURL((url) => url.pathname === '/', { timeout: 15_000 }),
    page.locator('form').getByRole('button', { name: '登录', exact: true }).click(),
  ])
}

async function loginAdmin(page) {
  await page.goto(`${adminURL}/login`, { waitUntil: 'domcontentloaded' })
  await page.getByPlaceholder('请输入管理员邮箱').fill(email)
  await page.getByPlaceholder('请输入密码').fill(password)
  await Promise.all([
    page.waitForURL((url) => url.origin === adminURL && url.pathname === '/', { timeout: 15_000 }),
    page.locator('form').getByRole('button', { name: '登录', exact: true }).click(),
  ])
}

async function checkViewport(page, evidence) {
  const diagnostics = await page.evaluate(() => {
    const overflow = Math.max(0, document.documentElement.scrollWidth - window.innerWidth)
    const fixed = [...document.querySelectorAll('button, input, select, textarea, [role="button"]')]
      .filter((element) => {
        const rect = element.getBoundingClientRect()
        const style = getComputedStyle(element)
        return rect.width > 0 && rect.height > 0 && style.visibility !== 'hidden' && style.display !== 'none'
      })
      .map((element) => {
        const rect = element.getBoundingClientRect()
        return {
          tag: element.tagName,
          label: (element.getAttribute('aria-label') || element.textContent || '').trim().slice(0, 80),
          width: Math.round(rect.width),
          height: Math.round(rect.height),
          clipped: rect.right > window.innerWidth + 1 || rect.left < -1,
        }
      })
    return { overflow, clipped: fixed.filter((item) => item.clipped), controls: fixed.length }
  })
  if (diagnostics.overflow > 1 || diagnostics.clipped.length) {
    throw new Error(
      `${evidence.profile}/${evidence.pack}/${evidence.page} layout violation: ${JSON.stringify(diagnostics)}`,
    )
  }
}

async function screenshot(page, evidence) {
  const name = [evidence.profile, evidence.pack, evidence.page, evidence.motion].join('--')
  await page.screenshot({ path: path.join(outputDir, `${name}.png`), fullPage: true })
}

async function withEvidence(page, evidence, operation) {
  const failureStem = `${evidence.profile}--${evidence.pack}--${evidence.page}--failure`
  fs.rmSync(path.join(outputDir, `${failureStem}.png`), { force: true })
  fs.rmSync(path.join(outputDir, `${failureStem}.json`), { force: true })
  const failures = []
  const listener = (response) => {
    if (response.status() >= 400) failures.push({ url: response.url(), status: response.status() })
  }
  page.on('response', listener)
  try {
    await operation()
    await checkViewport(page, evidence)
    await screenshot(page, evidence)
  } catch (error) {
    const report = { ...evidence, failures, error: String(error), url: page.url() }
    await page
      .screenshot({
        path: path.join(outputDir, `${failureStem}.png`),
        fullPage: true,
      })
      .catch(() => {})
    fs.writeFileSync(path.join(outputDir, `${failureStem}.json`), `${JSON.stringify(report, null, 2)}\n`)
    throw error
  } finally {
    page.off('response', listener)
  }
}

async function applySystemPack(page, name) {
  await page.goto(`${webURL}/appearance`, { waitUntil: 'domcontentloaded' })
  const item = page.locator(`[data-style-pack-name="${name}"]`)
  await item.waitFor()
  const apply = item.getByRole('button', { name: '应用风格', exact: true })
  if (await apply.isEnabled()) await apply.click()
  await page.locator(`[data-web-theme="${name}"]`).waitFor()
}

async function applyHomepagePack(page, name) {
  await page.goto(`${adminURL}/appearance`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: '外观与风格包', exact: true }).waitFor()
  await page.locator('.pack-manager .el-select').click()
  const option = page.locator(`[data-homepage-pack-name="${name}"]`)
  await option.waitFor()
  await option.click()
  await page.getByRole('button', { name: '切换首页包', exact: true }).click()
  await page.getByText('首页资源包已切换', { exact: true }).waitFor()
}

async function rollbackHomepagePack(page) {
  await page.goto(`${adminURL}/appearance`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: '外观与风格包', exact: true }).waitFor()
  await page.getByRole('button', { name: '回滚当前首页', exact: true }).click()
  await page.getByText('首页风格已回滚', { exact: true }).waitFor()
}

async function openSpaceSettings(page) {
  const avatar = page.locator('.nav-avatar')
  await avatar.waitFor()
  await avatar.click()
  const personalSpaceEntry = page.getByRole('menuitem', { name: '个人空间', exact: true })
  await personalSpaceEntry.waitFor()
  await personalSpaceEntry.click()
  await page.waitForURL(/\/space\/settings$/)
  await page.getByText('拓展风格包', { exact: true }).waitFor()
}

async function applySpacePack(page, name) {
  await openSpaceSettings(page)
  const item = page.locator(`[data-source-style-pack-name="${name}"]`)
  await item.waitFor()
  await item.click()
  await page.getByRole('button', { name: '应用源码目录', exact: true }).click()
  await page.getByText('源码目录风格包已应用', { exact: true }).waitFor()
}

async function currentPublicProfile(page) {
  let link = page.getByRole('link', { name: '个人主页', exact: true })
  if (!(await link.count())) {
    await page.getByRole('button', { name: '打开主导航' }).click()
    link = page.getByRole('link', { name: '个人主页', exact: true })
    await link.waitFor()
  }
  const href = await link.getAttribute('href')
  await page.keyboard.press('Escape')
  if (!href?.startsWith('/u/')) throw new Error(`personal profile route is unavailable: ${href || 'empty'}`)
  return `${webURL}${href}`
}

let adminContext
let homepageApplied = false

try {
  adminContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
  const adminPage = await adminContext.newPage()
  await loginAdmin(adminPage)

  for (const pack of homepagePacks) {
    await applyHomepagePack(adminPage, pack)
    homepageApplied = true
    for (const profile of profiles) {
      const context = await browser.newContext({
        viewport: { width: profile.width, height: profile.height },
        reducedMotion: 'reduce',
      })
      try {
        const page = await context.newPage()
        await login(page)
        await withEvidence(page, { profile: profile.id, pack, page: 'homepage', motion: 'reduced' }, async () => {
          await page.goto(webURL, { waitUntil: 'domcontentloaded' })
          await page.locator('style[data-campusos-style-pack="homepage"]').waitFor({ state: 'attached' })
          await page.locator('[data-campusos-required-region="page-outlet"]').waitFor()
        })
      } finally {
        await context.close()
      }
    }
    await rollbackHomepagePack(adminPage)
    homepageApplied = false
  }

  for (const profile of profiles) {
    const context = await browser.newContext({
      viewport: { width: profile.width, height: profile.height },
      reducedMotion: 'reduce',
    })
    const page = await context.newPage()
    await login(page)
    const profileURL = await currentPublicProfile(page)

    for (const pack of systemPacks) {
      await withEvidence(page, { profile: profile.id, pack, page: 'system-theme', motion: 'reduced' }, async () => {
        await applySystemPack(page, pack)
        await page.goto(`${webURL}/threads`, { waitUntil: 'domcontentloaded' })
        await page.locator(`[data-web-theme="${pack}"]`).waitFor()
        await page.locator('.app-container[data-campusos-web]').waitFor()
        await page.locator('style[data-campusos-style-pack="web"]').waitFor({ state: 'attached' })
      })
    }

    for (const pack of spacePacks) {
      await withEvidence(page, { profile: profile.id, pack, page: 'space-owner', motion: 'reduced' }, async () => {
        await applySpacePack(page, pack)
        await page.goto(profileURL, { waitUntil: 'domcontentloaded' })
        await page.getByText('个人主页').first().waitFor()
      })

      const visitor = await browser.newContext({
        viewport: { width: profile.width, height: profile.height },
        reducedMotion: 'reduce',
      })
      const visitorPage = await visitor.newPage()
      await withEvidence(
        visitorPage,
        { profile: profile.id, pack, page: 'space-visitor', motion: 'reduced' },
        async () => {
          await visitorPage.goto(profileURL, { waitUntil: 'domcontentloaded' })
          await visitorPage.getByText('个人主页').first().waitFor()
        },
      )
      await visitor.close()
    }
    await context.close()
  }
  console.log(`style pack responsive matrix passed: ${outputDir}`)
} finally {
  let rollbackError
  if (homepageApplied && adminContext) {
    const pages = adminContext.pages()
    const adminPage = pages[0] || (await adminContext.newPage())
    try {
      await rollbackHomepagePack(adminPage)
    } catch (error) {
      rollbackError = error
    }
  }
  await adminContext?.close()
  await browser.close()
  if (rollbackError) throw new Error(`homepage rollback failed: ${String(rollbackError)}`)
}
