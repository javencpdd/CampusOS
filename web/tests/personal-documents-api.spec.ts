import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../src/shared/api/client', () => ({
  default: { get: mocks.get, post: mocks.post },
}))

import { personalDocumentsApi } from '../src/modules/personal-documents/api'

describe('personal document binary access', () => {
  beforeEach(() => vi.clearAllMocks())

  it('downloads through the authenticated API client instead of a bare browser URL', () => {
    personalDocumentsApi.download('document-42')

    expect(mocks.get).toHaveBeenCalledWith('/documents/document-42/download', { responseType: 'blob' })
  })

  it('creates an opaque plugin preview invocation for exactly one document', () => {
    personalDocumentsApi.createPDFInvocation('document-42', 'modal')

    expect(mocks.post).toHaveBeenCalledWith('/documents/document-42/pdf-invocations', { presentation: 'modal' })
  })
})
