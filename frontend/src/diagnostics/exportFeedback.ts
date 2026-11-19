import { tr } from '../locales'
import { diagnosticExportName, revealDiagnosticExport } from '../platform/diagnosticFiles'
import { presentationProfile } from '../platform/presentation'
import { useErrorStore } from '../stores/error'

export function notifyExportReady(title: string, message: string, open?: () => void | Promise<void>, label = 'diagnostics.openSavedExport'): void {
  const errors = useErrorStore()
  if (!open) { errors.info(title, message); return }
  errors.push({
    level: 'info', title, message, actionLabel: tr(label), autoDismiss: 8000,
    action: () => {
      void Promise.resolve().then(open).catch(() => {
        errors.warn(tr('diagnostics.openExportFailed'), '', '', 'Diagnostics.openExport')
      })
    },
  })
}

export function notifyDiagnosticReport(path: string): void {
  if (!path) return
  const desktop = presentationProfile().ui === 'desktop'
  notifyExportReady(tr('diagnostics.exportDone'), desktop ? path : diagnosticExportName(path),
    desktop ? () => revealDiagnosticExport(path) : undefined, 'diagnostics.openReportFolder')
}
