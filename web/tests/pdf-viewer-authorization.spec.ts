import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ authorization: vi.fn(), setConsent: vi.fn(), confirm: vi.fn() }))
vi.mock('element-plus', () => ({ ElMessageBox: { confirm: mocks.confirm } }))
vi.mock('../src/modules/plugin-center/api', () => ({
  pluginCenterApi: { authorization: mocks.authorization, setConsent: mocks.setConsent },
}))

import { ensurePDFViewerConsent } from '../src/modules/pdf-viewer/authorization'

const overview = (adminStatus = 'granted', consentStatus?: string) => ({
  data: {
    version: { id: '1789489244156976761' },
    declarations: [{ capability_code: 'article_attachment.self.preview' }],
    catalog: [{ code: 'article_attachment.self.preview', consent_required: true, description: '预览当前文章 PDF' }],
    admin_grants: [{ capability_code: 'article_attachment.self.preview', status: adminStatus }],
    user_consents: consentStatus ? [{ capability_code: 'article_attachment.self.preview', status: consentStatus }] : [],
  },
})

describe('PDF Viewer user consent', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.confirm.mockResolvedValue(undefined)
    mocks.setConsent.mockResolvedValue({ data: {} })
  })

  it('records explicit self-scoped consent only after the user confirms', async () => {
    mocks.authorization.mockResolvedValue(overview())

    await ensurePDFViewerConsent('article_attachment.self.preview')

    expect(mocks.confirm).toHaveBeenCalledOnce()
    expect(mocks.setConsent).toHaveBeenCalledWith(
      'builtin.pdf-viewer',
      '1789489244156976761',
      'article_attachment.self.preview',
      'granted',
      {
        scope: 'self',
      },
    )
  })

  it('does not request consent when the administrator has denied the capability', async () => {
    mocks.authorization.mockResolvedValue(overview('denied'))

    await expect(ensurePDFViewerConsent('article_attachment.self.preview')).rejects.toThrow('管理员当前未授予')
    expect(mocks.confirm).not.toHaveBeenCalled()
    expect(mocks.setConsent).not.toHaveBeenCalled()
  })

  it('does not show a duplicate dialog after a valid consent exists', async () => {
    mocks.authorization.mockResolvedValue(overview('granted', 'granted'))

    await ensurePDFViewerConsent('article_attachment.self.preview')

    expect(mocks.confirm).not.toHaveBeenCalled()
    expect(mocks.setConsent).not.toHaveBeenCalled()
  })
})
