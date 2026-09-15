import { ElMessageBox } from 'element-plus'
import { pluginCenterApi } from '@/modules/plugin-center/api'

const viewerPluginName = 'builtin.pdf-viewer'

type AuthorizationOverview = {
  // API IDs are strings deliberately: PostgreSQL Snowflake BIGINT values are
  // larger than JavaScript's safe integer range.
  version?: { id?: string }
  declarations?: Array<{ capability_code?: string }>
  catalog?: Array<{ code?: string; consent_required?: boolean; display_name?: string }>
  admin_grants?: Array<{ capability_code?: string; status?: string }>
  user_consents?: Array<{ capability_code?: string; status?: string }>
}

const unwrap = <T>(payload: any): T => (payload?.data ?? payload) as T

// ensurePDFViewerConsent deliberately stores only a consent record on the
// server. PDF bytes remain protected by the normal short-lived invocation and
// are never copied to localStorage, IndexedDB or the plugin cache.
export const ensurePDFViewerConsent = async (capabilityCode: string): Promise<void> => {
  const overview = unwrap<AuthorizationOverview>(await pluginCenterApi.authorization(viewerPluginName))
  const versionID = overview.version?.id || ''
  const declaration = overview.declarations?.find((item) => item.capability_code === capabilityCode)
  const descriptor = overview.catalog?.find((item) => item.code === capabilityCode)
  const adminGrant = overview.admin_grants?.find((item) => item.capability_code === capabilityCode)
  const consent = overview.user_consents?.find((item) => item.capability_code === capabilityCode)

  if (!versionID || !declaration) {
    throw new Error('PDF 阅读器尚未由服务器完成注册，请稍后刷新页面重试。')
  }
  if (adminGrant?.status !== 'granted') {
    throw new Error('管理员当前未授予 PDF 阅读器所需能力，暂不能在线预览。')
  }
  if (!descriptor?.consent_required || consent?.status === 'granted') return

  const capabilityName = descriptor.display_name || '读取当前允许预览的 PDF'
  await ElMessageBox.confirm(
    `PDF 阅读器需要获得“${capabilityName}”的个人授权，且仅会读取你这次有权访问的文件。你可以随时在“插件中心 → PDF 文档预览 → 查看并授权”中撤销。`,
    '授权 PDF 在线预览',
    { confirmButtonText: '同意并预览', cancelButtonText: '取消', type: 'info', distinguishCancelAndClose: true },
  )
  await pluginCenterApi.setConsent(viewerPluginName, versionID, capabilityCode, 'granted', { scope: 'self' })
}
