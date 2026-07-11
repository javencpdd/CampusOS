export const UI_CONTRACT_VERSION = 'campusos.ui/v1' as const

export type ActivationMode = 'restart' | 'plugin-restart' | 'hot'
export type BackendState = 'installed' | 'starting' | 'running' | 'restarting' | 'stopping' | 'stopped' | 'pending_restart' | 'error'
export type FrontendState = 'unloaded' | 'loading' | 'loaded' | 'incompatible' | 'error'
export type HealthState = 'healthy' | 'degraded' | 'unavailable' | 'unknown'

export interface RuntimeContext {
  plugin: string
  revision: number
  lifecycle: {
    scope: 'system' | 'user'
    backend_activation_mode: ActivationMode
    frontend_activation_mode: 'hot'
    backend_state: BackendState
    frontend_state: FrontendState
    health: HealthState
    desired_enabled: boolean
    pending_restart: boolean
  }
}

export interface ActionContract {
  id: string
  label: string
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  path: `/${string}`
  permission?: string
  confirm?: boolean
  audit?: boolean
  body?: Record<string, unknown>
}

export interface RuntimeManifest {
  contract_version: typeof UI_CONTRACT_VERSION
  revision: number
  current_theme?: string
  plugins: Array<{ name: string; version: string; runtime: string; lifecycle: RuntimeContext['lifecycle']; ui: Record<string, unknown> }>
}

export interface ClientOptions {
  baseURL?: string
  token: () => string | undefined
  fetch?: typeof globalThis.fetch
}

export class CampusExtensionClient {
  private readonly baseURL: string
  private readonly request: typeof globalThis.fetch
  constructor(private readonly plugin: string, private readonly options: ClientOptions) {
    if (!/^[a-z0-9][a-z0-9-]{1,62}$/.test(plugin)) throw new Error('invalid plugin name')
    this.baseURL = (options.baseURL || '/api/v1').replace(/\/$/, '')
    this.request = options.fetch || globalThis.fetch.bind(globalThis)
  }

  async runtimeManifest(): Promise<RuntimeManifest> {
    const response = await this.request(`${this.baseURL}/ui/runtime-manifest`, { headers: this.headers() })
    if (!response.ok) throw new Error(`runtime manifest failed: ${response.status}`)
    const envelope = await response.json() as { data: RuntimeManifest }
    return envelope.data
  }

  async invoke<T>(action: ActionContract): Promise<T> {
    if (!action.path.startsWith('/') || action.path.includes('..')) throw new Error('invalid action path')
    const response = await this.request(`${this.baseURL}/extensions/${encodeURIComponent(this.plugin)}${action.path}`, {
      method: action.method,
      headers: { ...this.headers(), 'Content-Type': 'application/json' },
      body: action.method === 'GET' ? undefined : JSON.stringify(action.body || {}),
    })
    if (!response.ok) throw new Error(`extension action failed: ${response.status}`)
    return await response.json() as T
  }

  private headers(): Record<string, string> {
    const token = this.options.token()
    return token ? { Authorization: `Bearer ${token}` } : {}
  }
}
