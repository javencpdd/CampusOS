import { ElMessageBox } from 'element-plus'
import { pluginV5Api } from '@/modules/plugin-center/v5-api'
import { getAccessToken } from '@/modules/identity/session'
import { useUIRuntimeStore } from '@/modules/plugin-runtime/store'
import type { ConfigEntry } from '../../../sdk/typescript/src/v5-configuration-form'

// Host-owned confirmation covers only the current immutable version and Grant.
// It does not grant global capabilities or consent for another user's data.
export async function ensurePluginCapabilityConsent(pluginKey: string, capabilityCode: string): Promise<void> {
  if (
    pluginKey !== 'campusos.pdf-viewer' ||
    !['article_attachment.self.preview', 'personal_space_file.self.read', 'plugin_ui.surface.open'].includes(
      capabilityCode,
    )
  )
    throw new Error('当前界面没有可用的资源适配器。')
  const token = getAccessToken()
  const current = () => {
    if (!token || token !== getAccessToken()) throw new Error('用户身份已变化，请重新打开。')
  }
  let cursor = '',
    found: ConfigEntry | undefined
  for (let page = 0; page < 8; page++) {
    const r = await pluginV5Api.list(cursor)
    current()
    if (r.code !== 0) throw new Error('插件清单不可用。')
    for (const e of r.data.items)
      if (e.release.publisher === 'campusos' && e.release.plugin_key === 'pdf-viewer') {
        if (found) throw new Error('插件版本状态冲突，请重新加载。')
        found = e
      }
    cursor = r.data.next_cursor || ''
    if (!cursor) break
  }
  if (cursor || !found) throw new Error('PDF 插件尚未由管理员安装并批准，可继续使用下载功能。')
  const coordinate = { ...found.release }
  const detail = async () => {
    const r = await pluginV5Api.detail(coordinate)
    current()
    if (r.code !== 0) throw new Error('插件版本不可用。')
    return r.data
  }
  let entry = await detail()
  for (const code of ['ui.resource.selected.read', 'ui.surface.open'])
    if (
      !entry.grant?.approved ||
      !entry.grant?.consumes?.some((c: any) => c.capability === code && c.scope.kind === 'self')
    )
      throw new Error('管理员尚未授予此版本所需能力。')
  const consentCurrent = () =>
    entry.consent?.accepted &&
    entry.consent?.grant_revision === entry.grant?.revision &&
    entry.consent?.purpose_hash === entry.purpose_hash
  const ready = () =>
    entry.installation?.installed &&
    entry.installation.version === coordinate.version &&
    entry.installation.desired === 'enabled'
  if (!consentCurrent() || !ready()) {
    await ElMessageBox.confirm(
      `PDF 插件 ${coordinate.version} 将仅通过宿主读取你选择且有权访问的 PDF。用途：${(entry.release.manifest.consumes || []).map((c) => c.purpose).join('；')}。同意后安装并启用此版本，可在插件中心撤回。`,
      '确认插件用途与启用',
      { confirmButtonText: '同意并启用', cancelButtonText: '取消', distinguishCancelAndClose: true },
    )
    current()
    const command = async (intent: Record<string, unknown>) => {
      const r = await pluginV5Api.command(coordinate, intent)
      current()
      if (r.code !== 0 || !r.data?.id) throw new Error('操作结果未确认，请到插件中心检查。')
      entry = await detail()
    }
    if (!entry.installation?.installed)
      await command({ kind: 'install', expected_generation: entry.installation?.generation || '0' })
    if (!consentCurrent())
      await command({
        kind: 'consent',
        accepted: true,
        purpose_hash: entry.purpose_hash,
        expected_revision: entry.consent?.revision || '0',
      })
    if (!ready()) await command({ kind: 'enable', expected_generation: entry.installation?.generation || '0' })
  }
  current()
  await useUIRuntimeStore().sync()
  current()
  if (!useUIRuntimeStore().surface('campusos.pdf-viewer.preview'))
    throw new Error('PDF 隔离界面暂不可用，请检查宿主 UI 配置或下载文件。')
}
