export const UI_CONTRACT_VERSION = 'campusos.ui/v3'
export const SUPPORTED_UI_CONTRACT_VERSIONS = ['campusos.ui/v1', 'campusos.ui/v2', UI_CONTRACT_VERSION] as const

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
// Supplied by a trusted host after the user selects a resource, not by schema.
export interface UIResourceContext {
  resource_type: 'article_attachment' | 'personal_asset' | 'personal_document'
  resource_id: string
  thread_id?: string
}
export interface UIAction {
  id: string
  label: string
  kind?: 'request' | 'open-surface'
  // A request action has method/path.  An open-surface action deliberately
  // has neither: it may select only a declared surface and a host-owned
  // presentation, never an arbitrary navigation URL.
  method?: string
  path?: string
  surface_id?: string
  presentation?: 'modal' | 'drawer' | 'fullscreen' | 'new-tab'
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
  renderer: 'schema' | 'trusted-module' | 'isolated-iframe'
  module_id?: string
  frame?: {
    src: string
    origin: string
    audience: 'user' | 'admin'
  }
  schema?: UISchemaNode
  data_contract?: Record<string, unknown>
  action_ids?: string[]
  public_tokens?: string[]
  regions?: string[]
  presentations?: Array<'modal' | 'drawer' | 'fullscreen' | 'new-tab'>
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
export interface RuntimeModule extends RuntimePlugin {
  module_id: string
  feature_id: string
  kind: 'core' | 'builtin-feature'
}
export interface UIRuntimeManifest {
  contract_version: string
  revision: number
  current_theme?: string
  plugins: RuntimePlugin[]
  modules?: RuntimeModule[]
}
export interface RuntimeNavigation extends UINavigation {
  plugin: string
  path: string
  requiresAuth: boolean
}
export interface RuntimeSurface extends UISurface {
  plugin: string
  plugin_version?: string
  lifecycle: LifecycleState
}
