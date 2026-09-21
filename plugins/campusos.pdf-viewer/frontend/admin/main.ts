import { PluginBridgeClient } from '../shared/bridge'

const app = document.querySelector<HTMLElement>('#app')
const query = new URLSearchParams(window.location.search)
const hostOrigin = query.get('host_origin') || ''

if (!hostOrigin || new URL(hostOrigin).origin !== hostOrigin || window.parent === window) {
  if (app) app.textContent = '插件设置必须由 CampusOS 管理宿主在隔离页面中打开。'
} else {
  const client = new PluginBridgeClient(
    { pluginKey: 'campusos.pdf-viewer', pluginVersion: '2.0.0-dev.1', surfaceID: 'settings', audience: 'admin' },
    hostOrigin,
  )
  client
    .connect()
    .then(() => client.request('config.read', {}))
    .then(() => {
      if (app) app.textContent = 'PDF 插件设置已连接；配置由 CampusOS 宿主校验和保存。'
    })
    .catch((error: Error) => {
      if (app) app.textContent = error.message
    })
  window.addEventListener('beforeunload', () => client.close())
}
