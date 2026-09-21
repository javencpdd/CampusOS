export const bridgeVersion = 'campusos.bridge/v1'

type BridgeResponse = {
  version: string
  request_id: string
  result?: unknown
  error?: { code: string; message: string }
}

type ConnectMessage = {
  type: 'campusos.bridge.connect'
  version: string
  challenge: string
  plugin_key: string
  plugin_version: string
  surface_id: string
  audience: 'user' | 'admin'
}

export class PluginBridgeClient {
  private port?: MessagePort
  private sequence = 0
  private readonly pending = new Map<string, { resolve: (value: unknown) => void; reject: (reason: Error) => void }>()

  constructor(
    private readonly identity: {
      pluginKey: string
      pluginVersion: string
      surfaceID: string
      audience: 'user' | 'admin'
    },
    private readonly hostOrigin: string,
  ) {}

  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      const timeout = window.setTimeout(() => reject(new Error('连接宿主超时，请关闭后重试。')), 10_000)
      const listener = (event: MessageEvent<ConnectMessage>) => {
        const data = event.data
        const port = event.ports[0]
        if (
          event.origin !== this.hostOrigin ||
          event.source !== window.parent ||
          !port ||
          data?.type !== 'campusos.bridge.connect' ||
          data.version !== bridgeVersion ||
          data.plugin_key !== this.identity.pluginKey ||
          data.plugin_version !== this.identity.pluginVersion ||
          data.surface_id !== this.identity.surfaceID ||
          data.audience !== this.identity.audience
        ) {
          return
        }
        window.removeEventListener('message', listener)
        window.clearTimeout(timeout)
        this.port = port
        port.onmessage = (portEvent) => this.receive(portEvent.data as BridgeResponse)
        port.start()
        port.postMessage({ type: 'campusos.bridge.handshake', version: bridgeVersion, challenge: data.challenge })
        resolve()
      }
      window.addEventListener('message', listener)
      window.parent.postMessage(
        {
          type: 'campusos.bridge.ready',
          version: bridgeVersion,
          plugin_key: this.identity.pluginKey,
          plugin_version: this.identity.pluginVersion,
          surface_id: this.identity.surfaceID,
          audience: this.identity.audience,
        },
        this.hostOrigin,
      )
    })
  }

  request(method: string, params: Record<string, unknown>, transfer: Transferable[] = []): Promise<unknown> {
    if (!this.port) return Promise.reject(new Error('插件尚未连接宿主。'))
    const requestID = `pdf-${++this.sequence}`
    return new Promise((resolve, reject) => {
      this.pending.set(requestID, { resolve, reject })
      this.port?.postMessage({ version: bridgeVersion, request_id: requestID, method, params }, transfer)
    })
  }

  close() {
    this.port?.close()
    this.port = undefined
    for (const pending of this.pending.values()) pending.reject(new Error('插件连接已关闭。'))
    this.pending.clear()
  }

  private receive(message: BridgeResponse) {
    if (message?.version !== bridgeVersion || typeof message.request_id !== 'string') return
    const pending = this.pending.get(message.request_id)
    if (!pending) return
    this.pending.delete(message.request_id)
    if (message.error) pending.reject(new Error(message.error.message))
    else pending.resolve(message.result)
  }
}
