import type { Router } from 'vue-router'
import type { RuntimeNavigation, RuntimePlugin, RuntimeSurface, UIAction, UIRuntimeManifest } from './contracts'

export interface RegistrySnapshot {
  revision: number
  navigation: RuntimeNavigation[]
  surfaces: Map<string, RuntimeSurface>
  actions: Map<string, UIAction & { plugin: string }>
  slots: Map<string, RuntimeSurface[]>
}

export class RuntimeRegistry {
  private disposers: Array<() => void> = []
  private snapshot: RegistrySnapshot = {
    revision: 0,
    navigation: [],
    surfaces: new Map(),
    actions: new Map(),
    slots: new Map(),
  }

  constructor(private readonly router: Router) {}

  current() {
    return this.snapshot
  }

  replace(manifest: UIRuntimeManifest): RegistrySnapshot {
    if (manifest.contract_version !== 'campusos.ui/v1')
      throw new Error(`不兼容的 UI Contract: ${manifest.contract_version}`)
    const next: RegistrySnapshot = {
      revision: manifest.revision,
      navigation: [],
      surfaces: new Map(),
      actions: new Map(),
      slots: new Map(),
    }
    const nextDisposers: Array<() => void> = []
    try {
      for (const plugin of manifest.plugins) this.registerPlugin(plugin, next, nextDisposers)
    } catch (error) {
      nextDisposers.reverse().forEach((dispose) => dispose())
      throw error
    }
    this.disposers.reverse().forEach((dispose) => dispose())
    this.disposers = nextDisposers
    this.snapshot = next
    const current = this.router.currentRoute.value
    const browserPath = `${window.location.pathname}${window.location.search}`
    if (current.matched.length === 0 && browserPath === current.fullPath) void this.router.replace(current.fullPath)
    return next
  }

  clear() {
    this.disposers.reverse().forEach((dispose) => dispose())
    this.disposers = []
    this.snapshot = {
      revision: 0,
      navigation: [],
      surfaces: new Map(),
      actions: new Map(),
      slots: new Map(),
    }
  }

  private registerPlugin(plugin: RuntimePlugin, target: RegistrySnapshot, disposers: Array<() => void>) {
    const routes = plugin.ui.routes || []
    const navigation = plugin.ui.navigation || []
    const slots = plugin.ui.slots || []
    const surfaces = plugin.ui.surfaces || []
    const actions = plugin.ui.actions || []
    const routeContracts = new Map(routes.map((route) => [route.id, route]))
    for (const action of actions) target.actions.set(action.id, { ...action, plugin: plugin.name })
    for (const surface of surfaces)
      target.surfaces.set(surface.id, {
        ...surface,
        plugin: plugin.name,
        lifecycle: plugin.lifecycle,
      })
    for (const route of routes) {
      if (!target.surfaces.has(route.surface_id)) throw new Error(`路由 ${route.id} 缺少 Surface`)
      const name = `plugin:${plugin.name}:${route.id}`
      const remove = this.router.addRoute({
        name,
        path: route.path,
        component: () => import('@/campus-ui/PluginSurfacePage.vue'),
        meta: {
          plugin: plugin.name,
          surfaceId: route.surface_id,
          requiresAuth: Boolean(route.requires_auth),
          title: route.title,
        },
      })
      disposers.push(remove)
    }
    for (const nav of navigation) {
      const route = routeContracts.get(nav.route_id)
      if (route) {
        target.navigation.push({
          ...nav,
          plugin: plugin.name,
          path: route.path,
          requiresAuth: Boolean(route.requires_auth),
        })
      }
    }
    for (const slot of slots) {
      const surface = target.surfaces.get(slot.surface_id)
      if (!surface) continue
      const items = target.slots.get(slot.slot) || []
      items.push(surface)
      target.slots.set(slot.slot, items)
    }
    target.navigation.sort((a, b) => (a.order || 0) - (b.order || 0))
  }
}
