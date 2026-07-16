import { chromium, request } from 'playwright-core'

const chrome = process.env.CHROME_BIN
const webURL = (process.env.WEB_URL || 'http://localhost:3000').replace(/\/$/, '')
const adminURL = (process.env.ADMIN_URL || 'http://localhost:3001').replace(/\/$/, '')
const docsURL = (process.env.DOCS_URL || 'http://localhost:3002').replace(/\/$/, '')
const apiURL = (process.env.API_BASE_URL || 'http://localhost:8080/api/v1').replace(/\/$/, '')
const email = process.env.CAMPUSOS_ADMIN_EMAIL || 'admin@campusos.local'
const password = process.env.CAMPUSOS_ADMIN_PASSWORD || 'Admin@123456'

if (!chrome) throw new Error('CHROME_BIN is required')

const browser = await chromium.launch({ executablePath: chrome, headless: true, args: ['--no-sandbox'] })
let threadID = ''
let accessToken = ''

async function login(page, baseURL, emailPlaceholder, passwordPlaceholder) {
  await page.goto(`${baseURL}/login`, { waitUntil: 'domcontentloaded' })
  await page.getByPlaceholder(emailPlaceholder).fill(email)
  await page.getByPlaceholder(passwordPlaceholder).fill(password)
  await Promise.all([
    page.waitForURL((url) => url.pathname === '/', { timeout: 15_000 }),
    page.locator('form').getByRole('button', { name: '登录', exact: true }).click(),
  ])
}

async function selectPlainText(page) {
  const option = page.getByText('普通文本', { exact: true })
  await page.waitForFunction(
    () =>
      document.body.innerText.includes('普通文本') ||
      Boolean(document.querySelector('input[placeholder="请输入帖子标题"]')),
  )
  if (await option.count()) await option.first().click()
  await page.getByPlaceholder('请输入帖子标题').waitFor()
}

async function ensureCategorySelected(page) {
  const category = page.locator('.el-form-item').filter({ hasText: '版块' }).locator('.el-select').first()
  await category.waitFor()
  const selected = category.locator('.el-select__selected-item')
  if ((await selected.count()) && (await selected.first().innerText()).trim()) return
  await category.click()
  await page.locator('.el-select-dropdown:visible .el-select-dropdown__item').first().click()
}

try {
  const webContext = await browser.newContext()
  const page = await webContext.newPage()
  await login(page, webURL, '请输入邮箱', '请输入密码')
  accessToken = await page.evaluate(() => localStorage.getItem('access_token') || '')
  if (!accessToken) throw new Error('Web login did not persist access_token')

  await page.goto(`${webURL}/space/settings`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: '个人主页' }).waitFor()
  await page.getByText('主页配置', { exact: true }).waitFor()

  const stamp = Date.now()
  const originalTitle = `v0.6 browser smoke ${stamp}`
  const editedTitle = `${originalTitle} edited`
  await page.goto(`${webURL}/threads/create`, { waitUntil: 'domcontentloaded' })
  await selectPlainText(page)
  await ensureCategorySelected(page)
  await page.getByPlaceholder('请输入帖子标题').fill(originalTitle)
  await page.getByPlaceholder('请输入帖子内容').fill('CampusOS v0.6 browser workflow content')
  await Promise.all([
    page.waitForURL(/\/threads\/\d+$/, { timeout: 15_000 }),
    page.getByRole('button', { name: '发布帖子', exact: true }).click(),
  ])
  threadID = page.url().split('/').pop() || ''
  if (!threadID) throw new Error('Created thread ID is missing')
  await page.getByRole('heading', { name: originalTitle }).waitFor()

  await page.getByPlaceholder('写下你的回复...').fill('v0.6 browser workflow reply')
  await page.getByRole('button', { name: '提交回复', exact: true }).click()
  await page.getByText('第 1 楼', { exact: true }).waitFor()

  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await page.waitForURL(new RegExp(`/threads/${threadID}/edit$`))
  await page.getByPlaceholder('请输入帖子标题').fill(editedTitle)
  await Promise.all([
    page.waitForURL(new RegExp(`/threads/${threadID}$`)),
    page.getByRole('button', { name: '保存修改', exact: true }).click(),
  ])
  await page.getByRole('heading', { name: editedTitle }).waitFor()

  await page.getByRole('button', { name: '设为私密', exact: true }).click()
  await page.getByText('私密', { exact: true }).waitFor()
  await page.getByRole('button', { name: '设为公开', exact: true }).click()
  await page.getByRole('button', { name: '设为私密', exact: true }).waitFor()

  const authenticatedAPI = await request.newContext({
    baseURL: apiURL,
    extraHTTPHeaders: { Authorization: `Bearer ${accessToken}`, Accept: 'application/json' },
  })
  const meResponse = await authenticatedAPI.get(`${apiURL}/auth/me`)
  if (!meResponse.ok()) throw new Error(`GET /auth/me failed: ${meResponse.status()}`)
  const me = (await meResponse.json()).data
  const usersResponse = await authenticatedAPI.get(`${apiURL}/users?page=1&page_size=100`)
  const users = (await usersResponse.json()).data?.items || []
  const otherUser = users.find((item) => item.id !== me.id)
  if (!otherUser) throw new Error('Cross-user authorization smoke needs at least two users')
  const crossUpdate = await authenticatedAPI.put(`${apiURL}/users/${otherUser.id}`, {
    data: { nickname: 'forbidden update' },
  })
  if (crossUpdate.status() !== 403) throw new Error(`Cross-user update returned ${crossUpdate.status()}, expected 403`)
  const illegalField = await authenticatedAPI.put(`${apiURL}/users/${me.id}`, { data: { status: 'suspended' } })
  if (illegalField.status() !== 400)
    throw new Error(`Illegal user field returned ${illegalField.status()}, expected 400`)
  const invalidPage = await authenticatedAPI.get(`${apiURL}/threads?page=0`)
  if (invalidPage.status() !== 400) throw new Error(`Invalid pagination returned ${invalidPage.status()}, expected 400`)
  await authenticatedAPI.dispose()

  const anonymousAPI = await request.newContext({ baseURL: apiURL })
  const anonymousPlugins = await anonymousAPI.get(`${apiURL}/plugins`)
  if (anonymousPlugins.status() !== 401)
    throw new Error(`Anonymous plugin access returned ${anonymousPlugins.status()}, expected 401`)
  const errorPayload = await anonymousPlugins.json()
  if (!errorPayload.error?.code || !errorPayload.error?.request_id) {
    throw new Error('Anonymous plugin denial does not use the structured error contract')
  }
  await anonymousAPI.dispose()

  await page.getByRole('button', { name: '删除', exact: true }).first().click()
  await page.waitForURL(/\/threads$/)
  threadID = ''
  await webContext.close()

  const adminContext = await browser.newContext()
  const adminPage = await adminContext.newPage()
  await login(adminPage, adminURL, '请输入管理员邮箱', '请输入密码')
  await adminPage.getByText('用户总数', { exact: true }).waitFor()
  await adminPage.goto(`${adminURL}/plugins`, { waitUntil: 'domcontentloaded' })
  await adminPage.getByRole('main').getByText('外部插件', { exact: true }).first().waitFor()
  await adminPage.getByText(/已安装 \d+ 个插件/).waitFor()
  await adminPage.goto(`${adminURL}/features`, { waitUntil: 'domcontentloaded' })
  await adminPage.getByRole('heading', { name: '内置功能', exact: true }).waitFor()
  await adminPage.getByText('core.moderation', { exact: true }).waitFor()
  await adminPage.getByText('个人课表', { exact: true }).waitFor()
  await adminPage.goto(`${adminURL}/appearance`, { waitUntil: 'domcontentloaded' })
  await adminPage.getByRole('heading', { name: '外观与风格包', exact: true }).waitFor()
  await adminPage.getByRole('heading', { name: '系统主题目录', exact: true }).waitFor()
  await adminPage.getByText('主页所有者选择', { exact: true }).waitFor()
  await adminPage.goto(`${adminURL}/architecture`, { waitUntil: 'domcontentloaded' })
  await adminPage.getByRole('heading', { name: '系统数据架构' }).waitFor()
  await adminContext.close()

  const docsContext = await browser.newContext()
  const docsPage = await docsContext.newPage()
  await docsPage.goto(docsURL, { waitUntil: 'domcontentloaded' })
  await docsPage
    .getByText(/CampusOS/)
    .first()
    .waitFor()
  await docsPage.goto(`${docsURL}/guide/getting-started`, { waitUntil: 'domcontentloaded' })
  await docsPage.getByRole('heading', { name: /CampusOS 完整入门路径/ }).waitFor()
  await docsPage.goto(`${docsURL}/guide/permission-configuration`, { waitUntil: 'domcontentloaded' })
  await docsPage.getByRole('heading', { name: /CampusOS 权限配置入门/ }).waitFor()
  await docsPage.goto(`${docsURL}/plugins/schedule-plugin-tutorial`, { waitUntil: 'domcontentloaded' })
  await docsPage.getByRole('heading', { name: /以课表为例编写 CampusOS 插件/ }).waitFor()
  await docsContext.close()
  console.log(
    'browser workflow passed: auth, thread CRUD, reply, privacy, space, permissions, admin plugins/features/appearance, architecture, onboarding and permission docs',
  )
} finally {
  if (threadID && accessToken) {
    const cleanup = await request.newContext({
      baseURL: apiURL,
      extraHTTPHeaders: { Authorization: `Bearer ${accessToken}` },
    })
    await cleanup.delete(`${apiURL}/threads/${threadID}`).catch(() => {})
    await cleanup.dispose()
  }
  await browser.close()
}
