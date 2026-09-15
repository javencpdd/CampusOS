// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { closePluginSurface, openPluginSurface, usePluginSurfaceHost } from '../src/campus-ui/surfaceHost'

const mocks = vi.hoisted(() => ({ surface: vi.fn(), routes: vi.fn() }))
vi.mock('../src/modules/plugin-runtime/store', () => ({ useUIRuntimeStore: () => ({ surface: mocks.surface }) }))
vi.mock('../src/router', () => ({ default: { getRoutes: mocks.routes } }))
const request = { surfaceID: 'test.preview', invocationID: 'opaque', presentation: 'modal' as const }
describe('registered surface host', () => {
  beforeEach(() => {
    closePluginSurface()
    mocks.surface.mockReturnValue({
      lifecycle: { desired_enabled: true, frontend_state: 'loaded' },
      presentations: ['modal', 'new-tab'],
    })
    mocks.routes.mockReturnValue([{ path: '/extensions/test/preview', meta: { surfaceId: 'test.preview' } }])
  })
  it('rejects unknown, disabled and unsupported surfaces', () => {
    mocks.surface.mockReturnValueOnce(undefined)
    expect(() => openPluginSurface(request)).toThrow('未启用')
    mocks.surface.mockReturnValueOnce({ lifecycle: { desired_enabled: false } })
    expect(() => openPluginSurface(request)).toThrow('未启用')
    expect(() => openPluginSurface({ ...request, presentation: 'drawer' })).toThrow('不支持')
  })
  it('keeps a single active surface', () => {
    openPluginSurface(request)
    openPluginSurface({ ...request, invocationID: 'next' })
    expect(usePluginSurfaceHost().activeSurface.value?.invocationID).toBe('next')
  })
  it('opens only the registered same-origin route', () => {
    const opened = vi.spyOn(window, 'open').mockImplementation(() => null)
    openPluginSurface({ ...request, presentation: 'new-tab' })
    expect(opened).toHaveBeenCalledWith('/extensions/test/preview?invocation=opaque', '_blank', 'noopener,noreferrer')
    mocks.routes.mockReturnValue([{ path: '//evil.test', meta: { surfaceId: 'test.preview' } }])
    expect(() => openPluginSurface({ ...request, presentation: 'new-tab' })).toThrow('同源')
    expect(opened).toHaveBeenCalledTimes(1)
    opened.mockRestore()
  })
})
