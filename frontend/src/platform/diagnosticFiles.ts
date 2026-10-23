import { OpenFolder, OpenFileInExplorer } from '../api/app'
import { presentationProfile } from './presentation'
import { tr } from '../locales'

/** Native sharing is initiated only by a user's export action. */
export async function revealDiagnosticExport(path: string): Promise<void> {
  if (!path) return
  if (presentationProfile().ui === 'mobile') {
    const bridge = (window as unknown as {wails?:{diagnosticShare?:(path:string)=>string}}).wails
    if (!bridge?.diagnosticShare) throw new Error(tr('diagnostics.mobileHostUpdate'))
    const error = bridge.diagnosticShare(path)
    if (error) throw new Error(error)
    return
  }
  await OpenFileInExplorer(path)
}

export async function revealLogDirectory(path: string): Promise<void> {
  if (presentationProfile().ui === 'mobile') return
  await OpenFolder(path)
}
