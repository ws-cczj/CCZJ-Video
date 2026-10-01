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
// Go 侧的带码错误一律是「CODE: 消息」（见 app/apperror）。这里必须只认那种前缀：
// 早先按第一个冒号拆会把 "catalog lookup: no such row" 这类散文拆成 code="catalog lookup"，
// 界面显示的就是被切掉半截的原始错误。要求前缀是全大写码或扩展包的 snake_case 码，
// 认不出来就整句按 INTERNAL 显示，不再破坏文案。
const CODE_PREFIX = /^([A-Z][A-Z0-9_]*|[a-z][a-z0-9]*(?:_[a-z0-9]+)+): (.*)$/s

export function normalizeApiError(error: unknown): ApiError {
  const message = error instanceof Error ? error.message : String(error ?? 'Unknown error')
  const coded = CODE_PREFIX.exec(message)
  if (coded) {
    return { code: coded[1], message: coded[2] }
  }
  return { code: 'INTERNAL', message }
}
