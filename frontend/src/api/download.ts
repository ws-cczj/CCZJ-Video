import {
  CancelDownload,
  GetDownloadDir,
  GetDownloadProgress,
  GetSetting,
  ListDownloads,
  OpenFileInExplorer,
  PauseDownload,
  RemoveDownload,
  ResumeDownload,
  SetDownloadDir,
  StartVideoDownload,
} from './app'

export {
  CancelDownload,
  GetDownloadDir,
  GetDownloadProgress,
  GetSetting,
  ListDownloads,
  OpenFileInExplorer,
  PauseDownload,
  RemoveDownload,
  ResumeDownload,
  SetDownloadDir,
  StartVideoDownload,
}

export type DownloadErrorCode = 'DOWNLOAD_DUPLICATE' | 'UNKNOWN'

export function downloadErrorCode(error: unknown): DownloadErrorCode {
  const message = error instanceof Error ? error.message : String(error ?? '')
  return message.startsWith('DOWNLOAD_DUPLICATE:') ? 'DOWNLOAD_DUPLICATE' : 'UNKNOWN'
}
