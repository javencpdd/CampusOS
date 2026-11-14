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
    exactKeys(params, ['url', 'kind'])
    if (!['document', 'image'].includes(String(params.kind))) throw new Error('资源类型无效。')
    const result = await call('web.read', { url: publicURL(params.url), kind: params.kind })
    return { bytes: bytes(result.bytes), mime: result.mime }
  }
  if (request.method !== 'editor.article.import' || !openComposer) throw new Error('图文编辑器不可用。')
  exactKeys(params, ['title', 'summary', 'content_html', 'source'])
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
  const allowed = await call('editor.article.import')
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
        const result = await call('web.read', { url: publicURL(target), kind: 'image' })
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
  setTimeout(() => void openComposer(id).catch(() => {}), 100)
  return { opened: true, images_imported: imported.size, warnings: [...warnings] }
}
