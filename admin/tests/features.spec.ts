import { describe, expect, it } from 'vitest'
import { mapBuiltinFeatures } from '../src/modules/features/catalog'

describe('Built-in Feature compatibility projection', () => {
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
})
