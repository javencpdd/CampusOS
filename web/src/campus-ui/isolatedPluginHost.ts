import { pluginV5Api } from '@/modules/plugin-center/v5-api'
import { stringifyExactJSON } from '../../../sdk/typescript/src/exact-json'
import { richTextApi } from '@/modules/richtext/api'
import type { RuntimeSurface } from './contracts'
import { MAX_PLUGIN_RANGE_BYTES, type PluginBridgeRequest } from './isolatedPluginBridge'

type InvocationSummary = {
  invocation?: { plugin_key?: string; surface_id?: string }
  attachment?: {
    display_name?: string
    asset?: { size_bytes?: number; mime_type?: string }
  }
}

export function isolatedPluginFrameURL(src: string, invocationID: string): string {
  const url = new URL(src)
  url.searchParams.set('host_origin', window.location.origin)
  url.searchParams.set('resource_handle', invocationID)
  return url.toString()
}

// The iframe gets neither a bearer token nor an API URL. This host broker uses
// the active opaque invocation and asks the API to re-authorize every read.
export async function handleIsolatedPluginRequest(
  surface: RuntimeSurface,
  invocationID: string,
  request: PluginBridgeRequest,
): Promise<unknown> {
  if (!surface.frame || surface.renderer !== 'isolated-iframe') throw new Error('受限插件上下文不可用。')
  const parts = surface.plugin.split('.')
  const coordinate = {
    publisher: parts[0],
    plugin_key: parts.slice(1).join('.'),
    version: surface.plugin_version || '',
  }
  const current = async () => {
    if (!surface.context_proof) throw new Error('界面缺少当前版本证明。')
    const proof = await pluginV5Api.uiState(coordinate)
    if (proof.code !== 0 || proof.data?.proof !== surface.context_proof) throw new Error('界面身份、配置或授权已变化。')
  }
  await current()
  if (request.method === 'config.read') {
    if (Object.keys(request.params).length) throw new Error('配置读取参数无效。')
    const detail = await pluginV5Api.detail(coordinate)
    if (detail.code !== 0) throw new Error('配置不可用。')
    const result = {
      revision: detail.data.configuration?.revision || '0',
      values_json: stringifyExactJSON(detail.data.configuration?.values || {}),
    }
    await current()
    return result
  }
  if (!invocationID) throw new Error('请先在宿主中选择有权访问的资源。')
  if (request.method !== 'resource.describe' && request.method !== 'resource.readRange') {
    throw new Error('当前插件未获此 Bridge 方法授权。')
  }
  if (request.params.handle !== invocationID) throw new Error('插件资源句柄与当前预览上下文不一致。')

  const response = (await richTextApi.getPDFInvocation(invocationID)) as InvocationSummary & {
    data?: InvocationSummary
  }
  const payload = response.data || response
  if (payload.invocation?.plugin_key !== surface.plugin || payload.invocation?.surface_id !== surface.id) {
    throw new Error('当前资源不属于此插件界面。')
  }
  const asset = payload.attachment?.asset
  const size = Number(asset?.size_bytes || 0)
  if (!Number.isSafeInteger(size) || size < 5 || asset?.mime_type !== 'application/pdf') {
    throw new Error('当前资源不是可预览的 PDF 文件。')
  }
  if (request.method === 'resource.describe') {
    await current()
    return { handle: invocationID, size, name: payload.attachment?.display_name || 'document.pdf' }
  }
  const offset = request.params.offset
  const length = request.params.length
  if (
    !Number.isSafeInteger(offset) ||
    !Number.isSafeInteger(length) ||
    (offset as number) < 0 ||
    (length as number) < 1 ||
    (length as number) > MAX_PLUGIN_RANGE_BYTES ||
    (offset as number) + (length as number) > size
  ) {
    throw new Error('PDF 分段读取范围无效。')
  }
  const bytes = await richTextApi.getPDFInvocationContentRange(invocationID, offset as number, length as number)
  if (!(bytes instanceof ArrayBuffer) || bytes.byteLength > (length as number)) {
    throw new Error('宿主未返回有效的 PDF 分段数据。')
  }
  const verification = (await richTextApi.getPDFInvocation(invocationID)) as any
  const latest = verification.data || verification
  if (
    latest.invocation?.plugin_key !== payload.invocation?.plugin_key ||
    latest.invocation?.surface_id !== payload.invocation?.surface_id ||
    latest.invocation?.context_digest !== (payload.invocation as any)?.context_digest
  )
    throw new Error('预览上下文已变化，请重新打开。')
  await current()
  return { offset, bytes }
}
