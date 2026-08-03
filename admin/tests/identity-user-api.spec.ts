import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('../src/shared/api/client', () => ({
  default: { get },
}))

import { userApi } from '../src/modules/identity/api'

describe('Admin user API', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('uses the protected admin projection for the user directory', async () => {
    const params = { page: 2, page_size: 20 }

    await userApi.list(params)

    expect(get).toHaveBeenCalledWith('/admin/users', { params })
  })
})
