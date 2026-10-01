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
  normalizeApiError,
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

// 拆码只有一个入口（normalizeApiError），这里只做「是不是覆盖确认」这一件事。
export function downloadErrorCode(error: unknown): DownloadErrorCode {
  return normalizeApiError(error).code === 'DOWNLOAD_DUPLICATE' ? 'DOWNLOAD_DUPLICATE' : 'UNKNOWN'
}
