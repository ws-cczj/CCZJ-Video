import type { ComicChapter, ComicProgress } from '../domain'

export interface ComicChapterLocator {
  remoteId?: string
  ord?: number
}

/**
 * Resolve a chapter by its durable source identity. The display order is only
 * a compatibility fallback for records created before remoteId was stored.
 */
export function locateComicChapter(
  chapters: readonly ComicChapter[],
  locator: ComicChapterLocator,
): ComicChapter | undefined {
  if (locator.remoteId) return chapters.find(chapter => chapter.remoteId === locator.remoteId)
  if (Number.isInteger(locator.ord)) return chapters.find(chapter => chapter.ord === locator.ord)
  return undefined
}

export function progressChapter(
  chapters: readonly ComicChapter[],
  progress: ComicProgress | undefined,
): ComicChapter | undefined {
  if (!progress) return undefined
  return locateComicChapter(chapters, {
    remoteId: progress.episodeRemoteId,
    ord: progress.episodeOrd,
  })
}
