import { describe, expect, it } from 'vitest'

import { CampusOSError, parseAPIError } from '../src/shared/api/error'

describe('API error parser', () => {
  it('prefers the structured error and keeps legacy fields', () => {
    const error = parseAPIError(
      {
        code: 10010,
        msg: 'legacy message',
        error: {
          code: 'identity.registration_verification_rate_limited',
          message: 'verification request is temporarily limited',
          request_id: 'request-1',
          retryable: true,
          details: { retry_after: 60 },
        },
      },
      429,
    )

    expect(error).toBeInstanceOf(CampusOSError)
    expect(error.code).toBe(10010)
    expect(error.machineCode).toBe('identity.registration_verification_rate_limited')
    expect(error.msg).toBe('verification request is temporarily limited')
    expect(error.requestId).toBe('request-1')
    expect(error.retryable).toBe(true)
  })

  it('falls back to the legacy envelope', () => {
    const error = parseAPIError({ code: 10001, msg: 'legacy invalid request' }, 400)
    expect(error.code).toBe(10001)
    expect(error.machineCode).toBe('request.failed')
    expect(error.msg).toBe('legacy invalid request')
    expect(error.retryable).toBe(false)
  })
})
