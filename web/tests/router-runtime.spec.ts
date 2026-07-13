// @vitest-environment jsdom
import type { Router } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { RuntimeRegistry } from '../src/campus-ui/runtimeRegistry'
import type { UIRuntimeManifest } from '../src/campus-ui/contracts'
import { webAuthGuard } from '../src/router'

describe('Web navigation and runtime gates', () => {
  beforeEach(() => localStorage.clear())

  it('redirects protected routes to login and preserves the target', () => {
    const next = vi.fn()
    webAuthGuard({ meta: { requiresAuth: true }, fullPath: '/plugins' } as never, {} as never, next)
    expect(next).toHaveBeenCalledWith({ path: '/login', query: { redirect: '/plugins' } })

    localStorage.setItem('access_token', 'test-token')
    next.mockClear()
    webAuthGuard({ meta: { requiresAuth: true }, fullPath: '/plugins' } as never, {} as never, next)
    expect(next).toHaveBeenCalledWith()
  })

  it('removes routes and navigation when a plugin disappears from the runtime manifest', () => {
    const paths = new Set<string>()
    const router = {
      currentRoute: { value: { matched: [{}], fullPath: '/' } },
      replace: vi.fn(),
      addRoute: vi.fn((route: { path: string }) => {
        paths.add(route.path)
        return () => paths.delete(route.path)
      }),
    } as unknown as Router
    const registry = new RuntimeRegistry(router)
    const enabled = runtimeManifest(1, true)
    expect(registry.replace(enabled).navigation).toHaveLength(1)
    expect(paths.has('/plugin-notes')).toBe(true)

    const disabled = runtimeManifest(2, false)
    expect(registry.replace(disabled).navigation).toHaveLength(0)
    expect(paths.has('/plugin-notes')).toBe(false)
  })
})

function runtimeManifest(revision: number, includePlugin: boolean): UIRuntimeManifest {
  return {
    contract_version: 'campusos.ui/v1',
    revision,
    plugins: includePlugin
      ? [
          {
            name: 'notes',
            version: '1.0.0',
            runtime: 'wasm',
            scope: 'user',
            lifecycle: {
              scope: 'user',
              backend_activation_mode: 'hot',
              frontend_activation_mode: 'hot',
              backend_state: 'running',
              frontend_state: 'loaded',
              health: 'healthy',
              desired_enabled: true,
              pending_restart: false,
            },
            ui: {
              contract_version: 'campusos.ui/v1',
              routes: [{ id: 'notes', path: '/plugin-notes', surface_id: 'notes-page', requires_auth: true }],
              navigation: [{ id: 'notes-nav', label: '笔记', route_id: 'notes' }],
              slots: [],
              actions: [],
              surfaces: [
                {
                  id: 'notes-page',
                  version: '1',
                  type: 'page',
                  layout_role: 'main',
                  renderer: 'schema',
                  schema: { component: 'text', text: 'Notes' },
                },
              ],
            },
          },
        ]
      : [],
  }
}
