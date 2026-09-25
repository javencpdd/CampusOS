import { ElMessageBox } from 'element-plus'
import { pluginCenterApi } from '@/modules/plugin-center/api'

type AuthorizationOverview = {
  version?: { id?: string }
  declarations?: Array<{ capability_code?: string }>
  catalog?: Array<{ code?: string; consent_required?: boolean; display_name?: string }>
  admin_grants?: Array<{ capability_code?: string; status?: string }>
  user_consents?: Array<{ capability_code?: string; status?: string }>
}

const unwrap = <T>(payload: unknown): T => ((payload as { data?: T })?.data ?? payload) as T

// The host owns the user-consent prompt. Plugins only declare the purpose in
// their immutable manifest and never receive a session token or consent API.
export async function ensurePluginCapabilityConsent(pluginKey: string, capabilityCode: string): Promise<void> {
  const overview = unwrap<AuthorizationOverview>(await pluginCenterApi.authorization(pluginKey))
  const versionID = overview.version?.id || ''
  const declaration = overview.declarations?.find((item) => item.capability_code === capabilityCode)
  const descriptor = overview.catalog?.find((item) => item.code === capabilityCode)
  const adminGrant = overview.admin_grants?.find((item) => item.capability_code === capabilityCode)
  const consent = overview.user_consents?.find((item) => item.capability_code === capabilityCode)

  if (!versionID || !declaration) throw new Error('插件尚未由服务器完成注册，请刷新页面后重试。')
  if (adminGrant?.status !== 'granted') throw new Error('管理员尚未授予该插件所需能力，暂不能继续。')
  if (!descriptor?.consent_required || consent?.status === 'granted') return

  const capabilityName = descriptor.display_name || '当前受控资源'
  await ElMessageBox.confirm(
    `该插件需要获得“${capabilityName}”的个人授权，且仅会在你当前操作的受控范围内使用。你可以随时在“插件中心”中撤销。`,
    '确认插件个人授权',
    { confirmButtonText: '同意并继续', cancelButtonText: '取消', type: 'info', distinguishCancelAndClose: true },
  )
  await pluginCenterApi.setConsent(pluginKey, versionID, capabilityCode, 'granted', { scope: 'self' })
}
