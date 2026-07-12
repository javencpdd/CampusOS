export const UI_CONTRACT_VERSION = 'campusos.ui/v1'

export type BackendState =
  'installed' | 'starting' | 'running' | 'restarting' | 'stopping' | 'stopped' | 'pending_restart' | 'error'
export type FrontendState = 'unloaded' | 'loading' | 'loaded' | 'incompatible' | 'error'
export type HealthState = 'healthy' | 'degraded' | 'unavailable' | 'unknown'

export interface LifecycleState {
  scope: 'system' | 'user'
  backend_activation_mode: 'restart' | 'plugin-restart' | 'hot'
  frontend_activation_mode: 'hot'
  backend_state: BackendState
  frontend_state: FrontendState
  health: HealthState
  desired_enabled: boolean
  pending_restart: boolean
}

export interface UIRoute {
  id: string
  path: string
  surface_id: string
  title?: string
  requires_auth?: boolean
}
export interface UINavigation {
  id: string
  label: string
  route_id: string
  location?: string
  order?: number
}
export interface UISlot {
  id: string
  slot: string
  surface_id: string
  order?: number
}
export interface UIAction {
  id: string
  label: string
  method: string
  path: string
  permission?: string
  confirm?: boolean
  audit?: boolean
  body?: Record<string, unknown>
}
export interface UISurface {
  id: string
  version: string
  type: string
  layout_role: string
  renderer: 'schema' | 'trusted-module'
  module_id?: string
  schema?: UISchemaNode
  data_contract?: Record<string, unknown>
  action_ids?: string[]
  public_tokens?: string[]
  regions?: string[]
}
export interface UISchemaNode {
  component: 'stack' | 'grid' | 'card' | 'heading' | 'text' | 'badge' | 'alert' | 'button' | 'list'
  text?: string
  level?: 1 | 2 | 3 | 4
  action_id?: string
  tone?: 'default' | 'primary' | 'success' | 'warning' | 'danger'
  children?: UISchemaNode[]
  items?: Array<string | number>
}
export interface UIContribution {
  contract_version: string
  routes: UIRoute[]
  navigation: UINavigation[]
  slots: UISlot[]
  surfaces: UISurface[]
  actions: UIAction[]
}
export interface RuntimePlugin {
  name: string
  version: string
  runtime: string
  scope: string
  lifecycle: LifecycleState
  ui: UIContribution
}
export interface UIRuntimeManifest {
  contract_version: string
  revision: number
  current_theme?: string
  plugins: RuntimePlugin[]
}
export interface RuntimeNavigation extends UINavigation {
  plugin: string
  path: string
  requiresAuth: boolean
}
export interface RuntimeSurface extends UISurface {
  plugin: string
  lifecycle: LifecycleState
}
