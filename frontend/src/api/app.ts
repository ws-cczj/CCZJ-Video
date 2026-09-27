/**
 * The only frontend entry point for Wails generated bindings.
 *
 * Keeping this adapter deliberately boring gives the rest of the application
 * a stable seam when bindings are regenerated or a DTO needs compatibility
 * translation.
 */
export * from '../../bindings/cczjVideo/app/service/app'

export type ApiErrorCode =
  | 'VALIDATION'
  | 'CONFLICT'
  | 'NOT_FOUND'
  | 'CANCELLED'
  | 'DOWNLOAD_DUPLICATE'
  | 'INTERNAL'

export interface ApiError {
  code: ApiErrorCode | string
  message: string
}

/** Converts Wails' stringified errors into a stable UI-facing shape. */
export function normalizeApiError(error: unknown): ApiError {
  const message = error instanceof Error ? error.message : String(error ?? 'Unknown error')
  const separator = message.indexOf(':')
  if (separator > 0) {
    return { code: message.slice(0, separator).trim(), message: message.slice(separator + 1).trim() }
  }
  return { code: 'INTERNAL', message }
}
