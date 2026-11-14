import { ElMessageBox } from 'element-plus'
import { h } from 'vue'
import { pluginV5Api } from '@/modules/plugin-center/v5-api'
import { richTextApi } from '@/modules/richtext/api'
import { useUIRuntimeStore } from '@/modules/plugin-runtime/store'
import { stageImportedArticle } from './importedArticle'
import { CampusOSError } from '@/shared/api/error'
import { PluginBridgePublicError } from './isolatedPluginBridge'
import type { RuntimeSurface } from './contracts'
import type { PluginBridgeRequest } from './isolatedPluginBridge'

function bytes(value: unknown): ArrayBuffer {
  if (typeof value !== 'string' || value.length > 1400000) throw new Error('资源超出大小限制。')
  const decoded = Uint8Array.from(atob(value), (c) => c.charCodeAt(0))
  if (decoded.byteLength > 1024 * 1024) throw new Error('资源超出大小限制。')
  return decoded.buffer
}
function publicURL(value: unknown): string {
  if (typeof value !== 'string' || value.length > 2048) throw new Error('资源地址无效。')
  const url = new URL(value)
  if (url.protocol !== 'https:' || url.username || url.password || url.hash)
    throw new Error('请使用公开 HTTPS 资源地址。')
  return url.href
}
function exactKeys(value: Record<string, unknown>, keys: string[]) {
  if (Object.keys(value).some((key) => !keys.includes(key))) throw new Error('插件参数无效。')
}
// All adapters are platform contracts. No plugin identity, subscription schema
// or feed parser is embedded in the host shell or these privileged actions.
export async function handlePluginHostAction(
  surface: RuntimeSurface,
  request: PluginBridgeRequest,
  current: () => Promise<void>,
  openComposer?: (id: string) => Promise<unknown>,
) {
  const parts = surface.plugin.split('.')
  const coordinate = {
    publisher: parts[0],
    plugin_key: parts.slice(1).join('.'),
    version: surface.plugin_version || '',
  }
  if (surface.frame?.audience !== 'user') throw new Error('此能力只供用户界面使用。')
  const params = request.params
  const call = async (action: string, extra: Record<string, unknown> = {}) => {
    await current()
    let response
    try {
      response = await pluginV5Api.uiAction(coordinate, { proof: surface.context_proof, action, ...extra })
    } catch (error) {
      if (error instanceof CampusOSError) {
        if (error.status === 0 || error.status === 429 || error.status >= 500)
          throw new PluginBridgePublicError('bridge.unavailable')
        if (error.status === 400) throw new PluginBridgePublicError('bridge.invalid_source')
        if (error.status === 409) throw new PluginBridgePublicError('bridge.context_changed')
      }
      throw error
    }
    await current()
    if (response.code !== 0) throw new Error('宿主操作暂不可用。')
    return response.data
  }
  const clientID = (value: unknown) => {
    if (value === undefined || value === '') return ''
    if (typeof value !== 'string' || !/^[a-f0-9]{32}$/.test(value)) throw new Error('本机助手编号无效。')
    return value
  }
  const selection = async () => {
    const state = await call('web.clients.status')
    if (typeof state.scope !== 'string' || !/^sha256:[a-f0-9]{64}$/.test(state.scope))
      throw new Error('读取位置不可用。')
    const key = 'campusos.web-client.' + state.scope
    const selected = clientID(sessionStorage.getItem(key) || '')
    return { state, key, selected }
  }
  const ensureChoice = async (expected: string) => {
    if ((await selection()).selected !== expected) throw new Error('读取位置已变化，请重新读取。')
  }
  if (request.method.startsWith('web.clients.')) {
    if (request.method === 'web.clients.status') {
      exactKeys(params, [])
      const { state, selected } = await selection()
      return {
        clients: state.clients || [],
        local_available: state.local_available === true,
        selected_client_id: selected,
      }
    }
    if (request.method === 'web.clients.pair') {
      exactKeys(params, [])
      await ElMessageBox.confirm(
        '将公开网页或图片读取交给你主动配对的电脑？助手只处理只读任务，本机代理由你在助手中配置；地址和口令不会上传服务器。',
        '连接本机抓取助手',
        { confirmButtonText: '生成配对码', cancelButtonText: '取消' },
      )
      const result = await call('web.clients.pair')
      return { pairing_code: result.pairing_code, expires_at: result.expires_at, server: window.location.origin }
    }
    if (request.method === 'web.clients.select') {
      exactKeys(params, ['client_id'])
      const id = clientID(params.client_id)
      await ElMessageBox.confirm(
        id
          ? '后续读取将使用选中电脑的网络和本机代理。助手失联会报错，请保持窗口开启。'
          : '后续读取将使用服务器网络及管理员统一配置的代理。',
        '切换读取位置',
        { confirmButtonText: '确认切换', cancelButtonText: '取消' },
      )
      const result = await call('web.clients.select', id ? { client_id: id } : {})
      if (typeof result.scope !== 'string' || !/^sha256:[a-f0-9]{64}$/.test(result.scope))
        throw new Error('读取位置不可用。')
      sessionStorage.setItem('campusos.web-client.' + result.scope, id)
      return { selected_client_id: id }
    }
    if (request.method === 'web.clients.disconnect') {
      exactKeys(params, ['client_id'])
      const id = clientID(params.client_id)
      if (!id) throw new Error('请选择本机助手。')
      await call('web.clients.disconnect', { client_id: id })
      return { disconnected: true }
    }
    if (request.method === 'web.clients.download') {
      exactKeys(params, ['platform'])
      if (
        typeof params.platform !== 'string' ||
        !['windows-amd64', 'linux-amd64', 'linux-arm64', 'darwin-amd64', 'darwin-arm64'].includes(params.platform)
      )
        throw new Error('下载平台无效。')
      const { state } = await selection()
      if (!state.local_available) throw new Error('本机读取能力尚未批准。')
      await ElMessageBox.confirm(
        `下载 ${params.platform} 本机助手？程序需要你在自己的电脑上手动运行，插件不会自动执行或安装。`,
        '下载本机助手',
        { confirmButtonText: '确认下载', cancelButtonText: '取消' },
      )
      await current()
      const blob = await pluginV5Api.companionTool(params.platform)
      await current()
      if (!(blob instanceof Blob) || blob.size < 1 || blob.size > 32 * 1024 * 1024) throw new Error('助手文件不可用。')
      const url = URL.createObjectURL(blob),
        link = document.createElement('a')
      link.href = url
      link.download =
        'campusos-web-companion-' + params.platform + (params.platform.startsWith('windows') ? '.exe' : '')
      link.click()
      setTimeout(() => URL.revokeObjectURL(url), 10000)
      return { downloaded: true }
    }
    throw new Error('本机助手操作无效。')
  }
  if (request.method === 'config.update') {
    exactKeys(params, ['revision', 'values_json'])
    if (
      typeof params.revision !== 'string' ||
      typeof params.values_json !== 'string' ||
      params.values_json.length > 16000
    )
      throw new Error('个人配置参数无效。')
    const values = JSON.parse(params.values_json)
    if (!values || Array.isArray(values) || typeof values !== 'object') throw new Error('个人配置无效。')
    const confirmation = h('div', [
      h('p', `保存 ${surface.plugin} 的个人配置？可在插件中心检查或撤回。`),
      h(
        'pre',
        { style: { maxHeight: '40vh', overflow: 'auto', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' } },
        JSON.stringify(values, null, 2),
      ),
    ])
    await ElMessageBox.confirm(confirmation, '保存插件个人配置', {
      confirmButtonText: '确认保存',
      cancelButtonText: '取消',
    })
    await current()
    const result = await pluginV5Api.configure(coordinate, params.revision, values, surface.context_proof)
    if (result.code !== 0) throw new Error('个人配置保存失败。')
    setTimeout(() => void useUIRuntimeStore().sync(), 100)
    return { reload_required: true }
  }
  if (request.method === 'web.read') {
    exactKeys(params, ['url', 'kind', 'client_id'])
    const id = clientID(params.client_id)
    await ensureChoice(id)
    if (!['document', 'image'].includes(String(params.kind))) throw new Error('资源类型无效。')
    const result = await call('web.read', {
      url: publicURL(params.url),
      kind: params.kind,
      ...(id ? { client_id: id } : {}),
    })
    return { bytes: bytes(result.bytes), mime: result.mime }
  }
  if (request.method !== 'editor.article.import' || !openComposer) throw new Error('图文编辑器不可用。')
  exactKeys(params, ['title', 'summary', 'content_html', 'source', 'client_id'])
  if (
    typeof params.title !== 'string' ||
    !params.title.trim() ||
    params.title.length > 255 ||
    typeof params.content_html !== 'string' ||
    new TextEncoder().encode(params.content_html).byteLength > 40000 ||
    typeof params.summary !== 'string' ||
    params.summary.length > 1000
  )
    throw new Error('图文内容超出限制。')
  const source = publicURL(params.source)
  const selectedClient = clientID(params.client_id)
  await ensureChoice(selectedClient)
  const allowed = await call('editor.article.import', selectedClient ? { client_id: selectedClient } : {})
  if (!allowed.authorized) throw new Error('图文导入未获授权。')
  await ElMessageBox.confirm(
    `将 ${surface.plugin} 提供的“${params.title}”导入图文编辑器？来源：${source}。图片会转存到你的空间；请核对内容与转载权限，再手动发布。`,
    '导入插件图文',
    { confirmButtonText: '导入并编辑', cancelButtonText: '取消' },
  )
  await current()
  const deadline = Date.now() + 150000
  const initial: any = await richTextApi.preview(params.content_html)
  await current()
  // A template's contents remain inert: parsing cannot start image downloads,
  // execute script, or navigate the host document before media is rewritten.
  const template = document.createElement('template')
  template.innerHTML = (initial.data || initial).sanitized_html
  const images = [...template.content.querySelectorAll('img')]
  const sources = [...new Set(images.map((image) => image.getAttribute('src') || '').filter(Boolean))].slice(0, 8)
  const queue = [...sources],
    imported = new Map<string, string>(),
    warnings = new Set<string>()
  async function worker() {
    while (queue.length) {
      const target = queue.shift()!
      try {
        if (Date.now() >= deadline) throw new Error('导入超时')
        await ensureChoice(selectedClient)
        const result = await call('web.read', {
          url: publicURL(target),
          kind: 'image',
          ...(selectedClient ? { client_id: selectedClient } : {}),
        })
        const extension = (
          { 'image/png': 'png', 'image/jpeg': 'jpg', 'image/gif': 'gif', 'image/webp': 'webp' } as Record<
            string,
            string
          >
        )[result.mime]
        if (!extension) throw new Error('图片类型不支持。')
        await current()
        const uploaded: any = await richTextApi.uploadAsset(
          new File([bytes(result.bytes)], `plugin-image.${extension}`, { type: result.mime }),
        )
        await current()
        const url = (uploaded.data || uploaded).file_url
        if (typeof url !== 'string' || !url.startsWith('/api/v1/richtext/assets/')) throw new Error('图片保存失败。')
        imported.set(target, url)
      } catch {
        warnings.add('部分图片未能导入，已保留位置提示；请在编辑器中检查并补充。')
      }
    }
  }
  await Promise.all([worker(), worker()])
  for (const image of images) {
    const local = imported.get(image.getAttribute('src') || '')
    if (local) {
      image.removeAttribute('srcset')
      image.src = local
    } else {
      const placeholder = document.createElement('p')
      placeholder.textContent = `[图片待补充：${image.getAttribute('alt') || '插件图片'}]`
      image.replaceWith(placeholder)
    }
  }
  const attribution = document.createElement('p'),
    link = document.createElement('a')
  link.href = source
  link.textContent = source
  link.rel = 'noopener noreferrer'
  attribution.append('来源：', link)
  template.content.append(attribution)
  await current()
  if (Date.now() >= deadline) throw new Error('导入超时，请重新操作。')
  const final: any = await richTextApi.preview(template.innerHTML)
  await current()
  const id = stageImportedArticle({
    title: params.title,
    summary: params.summary,
    content_html: (final.data || final).sanitized_html,
    cover_url: sources.map((url) => imported.get(url)).find(Boolean) || '',
    warnings: [...warnings],
    source,
  })
  await ensureChoice(selectedClient)
  setTimeout(() => void openComposer(id).catch(() => {}), 100)
  return { opened: true, images_imported: imported.size, warnings: [...warnings] }
}
