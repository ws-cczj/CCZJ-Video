export type ApiErrorCode =
  | 'VALIDATION'
  | 'CONFLICT'
  | 'NOT_FOUND'
  | 'CANCELLED'
  | 'TIMEOUT'
  | 'UNAVAILABLE'
  | 'STORAGE'
  | 'CORRUPT'
  | 'UNSUPPORTED'
  | 'AUTHENTICATION_REQUIRED'
  | 'PERMISSION_DENIED'
  | 'RATE_LIMITED'
  | 'DOWNLOAD_DUPLICATE'
  | 'INTERNAL'

export interface ApiError {
  code: ApiErrorCode | string
  message: string
  retryAfterSeconds?: number
  operation?: string
  requestId?: string
  logged?: boolean
}

const numericCodes: Record<number, string> = { 400: 'VALIDATION', 40000: 'VALIDATION', 401: 'AUTHENTICATION_REQUIRED', 40100: 'AUTHENTICATION_REQUIRED', 403: 'PERMISSION_DENIED', 40300: 'PERMISSION_DENIED', 404: 'NOT_FOUND', 40400: 'NOT_FOUND', 409: 'CONFLICT', 40900: 'CONFLICT', 40901: 'DOWNLOAD_DUPLICATE', 42200: 'CORRUPT', 429: 'RATE_LIMITED', 42900: 'RATE_LIMITED', 49900: 'CANCELLED', 500: 'INTERNAL', 50000: 'INTERNAL', 50010: 'STORAGE', 50100: 'UNSUPPORTED', 50300: 'UNAVAILABLE', 50400: 'TIMEOUT' }

// Compatibility with errors from older hosts and extensions.
const CODE_PREFIX = /^([A-Z][A-Z0-9_]*|[a-z][a-z0-9]*(?:_[a-z0-9]+)+): (.*)$/s

function publicError(value: unknown, depth = 0): ApiError | null {
  if (!value || typeof value !== 'object' || depth > 6) return null
  if (Array.isArray(value)) {
    for (const entry of value.slice(0, 16)) {
      const found = publicError(entry, depth + 1)
      if (found) return found
    }
    return null
  }
  const record = value as Record<string, unknown>
  const rawCode = record.code ?? record.Code
  const code = typeof rawCode === 'number' ? (typeof record.kind === 'string' && /^[A-Z][A-Z_]+$/.test(record.kind) ? record.kind : numericCodes[rawCode] || String(rawCode)) : rawCode
  const message = record.message ?? record.Message ?? record.msg
  if (typeof code === 'string' && code && typeof message === 'string') {
    const result: ApiError = { code, message }
    if (typeof record.retryAfterSeconds === 'number' && Number.isFinite(record.retryAfterSeconds) && record.retryAfterSeconds > 0) {
      result.retryAfterSeconds = Math.min(86400, record.retryAfterSeconds)
    }
    if (typeof record.operation === 'string') result.operation = record.operation.slice(0, 128)
    if (typeof record.requestId === 'string') result.requestId = record.requestId.slice(0, 128)
    if (typeof record.logged === 'boolean') result.logged = record.logged
    return result
  }
  return publicError(record.cause, depth + 1)
}

/** Reads Wails' structured cause; text parsing only supports older hosts. */
export function normalizeApiError(error: unknown): ApiError {
  const structured = publicError(error)
  if (structured) return structured
  if (error instanceof Error && (error.name === 'CancelError' || error.name === 'AbortError')) {
    return { code: 'CANCELLED', message: '操作已取消' }
  }
  const raw = error instanceof Error ? error.message : String(error ?? 'Unknown error')
  // Wails 3 wraps service errors before handing them to the frontend.
  const message = raw.replace(/^(?:Binding call failed: )?Bound method returned an error: /, '')
  const coded = CODE_PREFIX.exec(message)
  if (coded) {
    return { code: coded[1], message: coded[2] }
  }
  return { code: 'INTERNAL', message: '操作失败，请查看日志' }
}

/** A transport failure is decoded once, while its original cause remains
 * available for diagnostics. Presentation code consumes the public fields. */
export class ApiFailure extends Error implements ApiError {
  readonly code: string
  readonly retryAfterSeconds?: number
  readonly operation?: string
  readonly requestId?: string
  readonly logged?: boolean
  constructor(value: ApiError, cause: unknown) { super(value.message, { cause }); this.name = 'ApiFailure'; this.code = value.code; this.retryAfterSeconds = value.retryAfterSeconds; this.operation = value.operation; this.requestId = value.requestId; this.logged = value.logged }
}
export function toApiFailure(value: unknown): ApiFailure { return value instanceof ApiFailure ? value : new ApiFailure(normalizeApiError(value), value) }
