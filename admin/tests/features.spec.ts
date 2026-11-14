import { describe, expect, it } from 'vitest'
import { mapBuiltinFeatures } from '../src/modules/features/catalog'

describe('Built-in Feature compatibility projection', () => {
  it('shows disabled Knowledge from the server registry with an actionable management entry', () => {
    const rows = mapBuiltinFeatures([{ id: 'knowledge', kind: 'builtin-feature', display_name: 'Knowledge', status: 'stopped', desired_enabled: false, activation_mode: 'hot-gated', config_sources: [] }])
    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({ id: 'knowledge', label: '知识检索', management: { path: '/knowledge', label: '知识来源管理' }, state: { desired_enabled: false, activation_mode: 'hot-gated' } })
    expect(rows[0].configSources).toEqual([])
  })

  it('does not hide newly registered built-ins behind the local label catalog', () => {
    const rows = mapBuiltinFeatures([{ id: 'new-campus-feature', kind: 'builtin-feature', display_name: '新增功能', description: 'server description', status: 'running', desired_enabled: true, config_sources: [{ id: 'new-campus-feature', label: '服务端配置' }] }])
    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({ id: 'new-campus-feature', label: '新增功能', description: 'server description', configSources: [{ name: 'new-campus-feature', label: '服务端配置' }], state: { status: 'running' } })
  })

  it('does not fabricate stopped controls for missing features or include Core and external lifecycle', () => {
    expect(mapBuiltinFeatures([])).toEqual([])
    expect(mapBuiltinFeatures([
      { id: 'moderation', kind: 'core', activation_mode: 'always-on' },
      { id: 'personal-space', capability_class: 'external' },
      { id: 'third-party', kind: 'external' },
    ])).toEqual([])
  })

  it('maps legacy plugin payloads to stable feature labels and state', () => {
    const features = mapBuiltinFeatures([
      {
        name: 'personal-schedule',
        capability_class: 'legacy-builtin',
        status: 'running',
        activation_mode: 'restart',
        desired_enabled: true,
      },
      {
        name: 'web-theme',
        capability_class: 'legacy-builtin',
        status: 'running',
        activation_mode: 'hot-gated',
        desired_enabled: true,
      },
      { name: 'third-party', capability_class: 'external' },
    ])

    const schedule = features.find((feature) => feature.id === 'personal-schedule')
    expect(schedule?.label).toBe('个人课表')
    expect(schedule?.state?.status).toBe('running')
    expect(schedule?.state?.activation_mode).toBe('restart')

    const appearance = features.find((feature) => feature.id === 'appearance')
    expect(appearance?.label).toBe('界面与风格')
    expect(appearance?.state?.activation_mode).toBe('hot-gated')
  })

  it('uses the authoritative feature DTO before compatibility aliases', () => {
    const features = mapBuiltinFeatures([
      {
        id: 'appearance',
        name: 'appearance',
        status: 'running',
        activation_mode: 'hot-gated',
        config_sources: [
          { id: 'web-theme', label: 'Web Theme' },
          { id: 'homepage-customizer', label: 'Homepage Customizer' },
        ],
      },
      {
        name: 'web-theme',
        status: 'stopped',
        activation_mode: 'restart',
      },
    ])

    const appearance = features.find((feature) => feature.id === 'appearance')
    expect(appearance?.state?.status).toBe('running')
    expect(appearance?.state?.activation_mode).toBe('hot-gated')
    expect(appearance?.configSources.map((source) => source.name)).toEqual([
      'web-theme',
      'homepage-customizer',
    ])
  })

  it('groups mutual aid and secondhand under image-text without coupling their lifecycle state', () => {
    const features = mapBuiltinFeatures([
      { id: 'controlled-richtext-article', status: 'stopped', desired_enabled: false },
      {
        id: 'mutual-aid',
        status: 'running',
        desired_enabled: true,
        presentation_parent: 'controlled-richtext-article',
      },
      {
        id: 'secondhand',
        status: 'running',
        desired_enabled: true,
        presentation_parent: 'controlled-richtext-article',
      },
    ])

    const mutualAid = features.find((feature) => feature.id === 'mutual-aid')
    const secondhand = features.find((feature) => feature.id === 'secondhand')
    expect(mutualAid?.parentId).toBe('controlled-richtext-article')
    expect(secondhand?.parentId).toBe('controlled-richtext-article')
    expect(mutualAid?.state?.status).toBe('running')
    expect(secondhand?.state?.status).toBe('running')
  })
})
