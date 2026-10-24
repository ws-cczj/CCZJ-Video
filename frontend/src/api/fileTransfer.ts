import { CancelError, CancellablePromise } from '@wailsio/runtime'
import { tr } from '../locales'

// The portable source and backup endpoints both bound input to 64 MiB.
export const PORTABLE_IMPORT_MAX_BYTES = 64 * 1024 * 1024

/** The caller owns this file read through requestScope, including abort and callbacks. */
export function readFileDataURL(file: File, maximum = PORTABLE_IMPORT_MAX_BYTES): CancellablePromise<string> {
  return readFile(file, maximum, 'dataURL')
}

/** Text consumers keep their own format/size policy while sharing reader ownership. */
export function readFileText(file: File, maximum = PORTABLE_IMPORT_MAX_BYTES): CancellablePromise<string> {
  return readFile(file, maximum, 'text')
}

function readFile(file: File, maximum: number, format: 'dataURL' | 'text'): CancellablePromise<string> {
  if (!Number.isFinite(maximum) || maximum < 1 || file.size > maximum) {
    return CancellablePromise.reject({ code: 'VALIDATION', message: tr('common.fileTooLarge', { max: Math.floor(maximum / 1024 / 1024) }) })
  }
  let reader: FileReader | null = null
  let settled = false
  const detach = () => { settled = true; if (reader) { reader.onload = null; reader.onerror = null; reader.onabort = null } }
  return new CancellablePromise<string>((resolve, reject) => {
    reader = new FileReader()
    reader.onload = () => {
      if (settled) return
      const value = reader?.result
      detach()
      if (typeof value === 'string') resolve(value)
      else reject({ code: 'CORRUPT', message: tr('common.fileReadFailed') })
    }
    reader.onerror = () => { if (settled) return; detach(); reject({ code: 'STORAGE', message: tr('common.fileReadFailed') }) }
    reader.onabort = () => { if (settled) return; detach(); reject(new CancelError('Request cancelled')) }
    try {
      if (format === 'text') reader.readAsText(file)
      else reader.readAsDataURL(file)
    } catch (error) { detach(); reject(error) }
  }, () => { detach(); reader?.abort(); reader = null })
}
