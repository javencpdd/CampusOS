import type { UIAction, UIResourceContext } from '@/campus-ui/contracts'
import api from '../../shared/api/client'

export const uiRuntimeApi = {
  manifest: () => api.get('/ui/runtime-manifest'),
  openSurface: (plugin: string, action: UIAction, context: UIResourceContext) =>
    api.post('/plugin-ui/invocations', {
      plugin_key: plugin,
      action_id: action.id,
      surface_id: action.surface_id,
      presentation: action.presentation,
      resource_type: context.resource_type,
      resource_id: context.resource_id,
      thread_id: context.thread_id,
    }),
  extension: (plugin: string, action: UIAction) => {
    if (action.kind === 'open-surface' || !action.method || !action.path) {
      throw new Error('该 UI 动作由宿主负责打开受控界面，不能作为扩展 HTTP 请求执行。')
    }
    return api.request({
      method: action.method,
      url: `/extensions/${encodeURIComponent(plugin)}${action.path}`,
      data: action.body || {},
    })
  },
}
