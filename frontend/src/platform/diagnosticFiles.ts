import { OpenFolder, OpenFileInExplorer } from '../api/app'
import { presentationProfile } from './presentation'
import { tr } from '../locales'
import { isIOSRuntime } from '../api/runtime'

type DiagnosticBridge = {
  diagnosticShare?: (path: string) => string
  diagnosticSave?: (path: string, id: string) => string
  diagnosticSavesToDownloads?: () => boolean
  diagnosticOpenSaved?: (id: string) => boolean
}
const nativeFiles = () => (window as unknown as { wails?: DiagnosticBridge }).wails
let nextSaveId = 0

export function supportsDiagnosticSave(): boolean {
  return typeof nativeFiles()?.diagnosticSave === 'function'
}

export function diagnosticSavesToDownloads(): boolean {
  return nativeFiles()?.diagnosticSavesToDownloads?.() === true
}

export function diagnosticExportName(path: string): string {
  return path.split(/[\\/]/).at(-1) || path
}

function saveError(state: string): string {
  switch (state) {
    case 'BUSY': return tr('diagnostics.saveBusy')
    case 'MISSING': return tr('diagnostics.saveSourceMissing')
    case 'DOWNLOAD_FAILED': return tr('diagnostics.saveDownloadsFailed')
    case 'PICKER_UNAVAILABLE': return tr('diagnostics.savePickerUnavailable')
    case 'DOCUMENT_FAILED': return tr('diagnostics.saveDocumentFailed')
    default: return tr('diagnostics.saveFailed')
  }
}

export interface SavedDiagnosticExport {
  destination: 'downloads' | 'document'
  open?: () => void
}

export function saveDiagnosticExport(path: string, signal: AbortSignal): Promise<SavedDiagnosticExport | null> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) { resolve(null); return }
    const bridge = nativeFiles()
    const id = String(++nextSaveId)
    const cleanup = () => {
      window.removeEventListener('cczj:diagnostic-save', onResult)
      signal.removeEventListener('abort', onCancel)
    }
    const onCancel = () => { cleanup(); resolve(null) }
    const fail = (state: string) => { cleanup(); reject(new Error(saveError(state))) }
    const onResult = (event: Event) => {
      const result = (event as CustomEvent<{ id?: string; state?: string }>).detail
      if (result?.id !== id) return
      if (result.state === 'cancelled') { onCancel(); return }
      if (result.state !== 'saved_downloads' && result.state !== 'saved') { fail(result.state ?? ''); return }
      cleanup()
      resolve({
        destination: result.state === 'saved_downloads' ? 'downloads' : 'document',
        open: bridge?.diagnosticOpenSaved ? () => {
          if (!bridge.diagnosticOpenSaved?.(id)) throw new Error(tr('diagnostics.openExportFailed'))
        } : undefined,
      })
    }
    window.addEventListener('cczj:diagnostic-save', onResult)
    signal.addEventListener('abort', onCancel, { once: true })
    try {
      const refusal = bridge?.diagnosticSave?.(path, id) ?? 'UNAVAILABLE'
      if (refusal) fail(refusal)
    } catch { fail('UNAVAILABLE') }
  })
}

/** Native sharing is initiated only by a user's export action. */
export async function revealDiagnosticExport(path: string): Promise<void> {
  if (!path) return
  if (isIOSRuntime()) {
    if (!await OpenFileInExplorer(path)) throw new Error(tr('mobile.shareFailed'))
    return
  }
  if (presentationProfile().ui === 'mobile') {
    const bridge = nativeFiles()
    if (!bridge?.diagnosticShare) throw new Error(tr('diagnostics.mobileHostUpdate'))
    const error = bridge.diagnosticShare(path)
    if (error) throw new Error(error)
    return
  }
  if (!await OpenFileInExplorer(path)) throw new Error(tr('diagnostics.openExportFailed'))
}

export async function revealLogDirectory(path: string): Promise<void> {
  if (presentationProfile().ui === 'mobile') return
  await OpenFolder(path)
}
