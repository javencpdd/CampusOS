export const UI_CONTRACT_VERSION = 'campusos.ui/v1' as const

export interface CampusOSErrorDetail {
  code: string
  message: string
  details?: unknown
  request_id?: string
  retryable: boolean
}

export class CampusOSError extends Error {
  readonly status: number
  readonly code: number
  readonly machineCode: string
  readonly msg: string
  readonly requestId?: string
  readonly retryable: boolean
  readonly details?: unknown
  readonly error: CampusOSErrorDetail

  constructor(input: { status: number; legacyCode: number; machineCode: string; message: string; requestId?: string; retryable: boolean; details?: unknown }) {
    super(input.message)
    this.name = 'CampusOSError'
    this.status = input.status
    this.code = input.legacyCode
    this.machineCode = input.machineCode
    this.msg = input.message
    this.requestId = input.requestId
    this.retryable = input.retryable
    this.details = input.details
    this.error = { code: input.machineCode, message: input.message, details: input.details, request_id: input.requestId, retryable: input.retryable }
  }
}

const errorRecord = (value: unknown): Record<string, unknown> | undefined =>
  value !== null && typeof value === 'object' ? value as Record<string, unknown> : undefined

const errorString = (value: unknown): string | undefined =>
  typeof value === 'string' && value.trim() ? value.trim() : undefined

export function parseCampusOSError(payload: unknown, status = 0, fallback = 'CampusOS request failed'): CampusOSError {
  if (payload instanceof CampusOSError) return payload
  const envelope = errorRecord(payload)
  const nested = errorRecord(envelope?.error)
  const message = errorString(nested?.message) || errorString(envelope?.msg) || errorString(envelope?.error) || fallback
  const requestId = errorString(nested?.request_id) || errorString(envelope?.request_id)
  const retryable = typeof nested?.retryable === 'boolean' ? nested.retryable : status === 429 || status === 503 || status >= 500
  return new CampusOSError({
    status,
    legacyCode: typeof envelope?.code === 'number' ? envelope.code : 0,
    machineCode: errorString(nested?.code) || 'request.failed',
    message,
    requestId,
    retryable,
    details: nested?.details,
  })
}

async function errorFromResponse(response: Response, fallback: string): Promise<CampusOSError> {
  let payload: unknown
  try {
    const body = await response.text()
    payload = body ? JSON.parse(body) : undefined
  } catch {
    payload = undefined
  }
  return parseCampusOSError(payload, response.status, fallback)
}

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

export interface ResponsiveUI {
  supported_viewports?: Array<'mobile' | 'tablet' | 'desktop'>
  minimum_width?: number
  mobile_behavior?: 'responsive' | 'unsupported'
  overflow_policy?: 'internal-only' | 'none'
}

export interface ManagedRecord {
	id: number
  plugin_name: string
  owner_type: 'system' | 'user'
  owner_id: string
  collection: string
  record_key: string
  data: Record<string, unknown>
  version: number
  created_at: string
  updated_at: string
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
    if (!response.ok) throw await errorFromResponse(response, 'runtime manifest failed')
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
    if (!response.ok) throw await errorFromResponse(response, 'extension action failed')
    return await response.json() as T
  }

  async listMyRecords(collection: string, page = 1, pageSize = 20): Promise<{ items: ManagedRecord[]; total: number }> {
	const response = await this.request(`${this.baseURL}/plugin-market/${encodeURIComponent(this.plugin)}/records/${encodeURIComponent(collection)}?page=${page}&page_size=${pageSize}`, { headers: this.headers() })
	if (!response.ok) throw await errorFromResponse(response, 'plugin records failed')
    const envelope = await response.json() as { data: { items: ManagedRecord[]; total: number } }
    return envelope.data
  }

	async createMyRecord(collection: string, input: { record_key?: string; data: Record<string, unknown> }): Promise<ManagedRecord> {
		const response = await this.request(`${this.baseURL}/plugin-market/${encodeURIComponent(this.plugin)}/records/${encodeURIComponent(collection)}`, { method: 'POST', headers: { ...this.headers(), 'Content-Type': 'application/json' }, body: JSON.stringify(input) })
		if (!response.ok) throw await errorFromResponse(response, 'create plugin record failed')
		return (await response.json() as { data: ManagedRecord }).data
	}

	async updateMyRecord(collection: string, recordKey: string, input: { version: number; data: Record<string, unknown> }): Promise<ManagedRecord> {
		const response = await this.request(`${this.baseURL}/plugin-market/${encodeURIComponent(this.plugin)}/records/${encodeURIComponent(collection)}/${encodeURIComponent(recordKey)}`, { method: 'PUT', headers: { ...this.headers(), 'Content-Type': 'application/json' }, body: JSON.stringify(input) })
		if (!response.ok) throw await errorFromResponse(response, 'update plugin record failed')
		return (await response.json() as { data: ManagedRecord }).data
	}

	async deleteMyRecord(collection: string, recordKey: string, version: number): Promise<void> {
		const response = await this.request(`${this.baseURL}/plugin-market/${encodeURIComponent(this.plugin)}/records/${encodeURIComponent(collection)}/${encodeURIComponent(recordKey)}?version=${version}`, { method: 'DELETE', headers: this.headers() })
		if (!response.ok) throw await errorFromResponse(response, 'delete plugin record failed')
	}

  private headers(): Record<string, string> {
    const token = this.options.token()
    return token ? { Authorization: `Bearer ${token}` } : {}
  }
}
