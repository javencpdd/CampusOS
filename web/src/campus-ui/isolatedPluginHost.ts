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
  if (!surface.frame || surface.renderer !== 'isolated-iframe' || !invocationID) {
    throw new Error('受限插件上下文不可用。')
  }
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
  return { offset, bytes }
}
