export interface APIErrorDetail {
  code: string
  message: string
  details?: unknown
  request_id?: string
  retryable: boolean
}

export interface APIErrorEnvelope {
  code?: number
  msg?: string
  error?: APIErrorDetail | string
  request_id?: string
}

const asRecord = (value: unknown): Record<string, unknown> | undefined =>
  value !== null && typeof value === 'object' ? (value as Record<string, unknown>) : undefined

const asString = (value: unknown): string | undefined =>
  typeof value === 'string' && value.trim() ? value.trim() : undefined

export class CampusOSError extends Error {
  readonly status: number
  readonly code: number
  readonly machineCode: string
  readonly msg: string
  readonly requestId?: string
  readonly request_id?: string
  readonly retryable: boolean
  readonly details?: unknown
  readonly error: APIErrorDetail

  constructor(input: {
    status: number
    legacyCode: number
    machineCode: string
    message: string
    requestId?: string
    retryable: boolean
    details?: unknown
  }) {
    super(input.message)
    this.name = 'CampusOSError'
    this.status = input.status
    this.code = input.legacyCode
    this.machineCode = input.machineCode
    this.msg = input.message
    this.requestId = input.requestId
    this.request_id = input.requestId
    this.retryable = input.retryable
    this.details = input.details
    this.error = {
      code: input.machineCode,
      message: input.message,
      details: input.details,
      request_id: input.requestId,
      retryable: input.retryable,
    }
  }
}

export function parseAPIError(payload: unknown, status = 0, fallback = 'Request failed'): CampusOSError {
  if (payload instanceof CampusOSError) return payload

  const envelope = asRecord(payload)
  const nested = asRecord(envelope?.error)
  const legacyError = asString(envelope?.error)
  const message =
    asString(nested?.message) || asString(envelope?.msg) || legacyError || asString(envelope?.message) || fallback
  const requestId = asString(nested?.request_id) || asString(envelope?.request_id)
  const retryable =
    typeof nested?.retryable === 'boolean' ? nested.retryable : status === 429 || status === 503 || status >= 500

  return new CampusOSError({
    status,
    legacyCode: typeof envelope?.code === 'number' ? envelope.code : 0,
    machineCode: asString(nested?.code) || 'request.failed',
    message,
    requestId,
    retryable,
    details: nested?.details,
  })
}
