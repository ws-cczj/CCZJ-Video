import type { NovelChapter, ReadingProgress } from '../domain'

/** Portable chapter identity survives restored databases with different IDs. */
export function progressChapter(chapters:readonly NovelChapter[],progress:ReadingProgress|undefined):NovelChapter|undefined {
  if(!progress)return undefined
  const readable=chapters.filter(chapter=>!chapter.isVolume)
  if(progress.chapterRemoteId)return readable.find(chapter=>chapter.remoteId===progress.chapterRemoteId)
  return readable.find(chapter=>chapter.id===progress.chapterId)
    ?? (progress.chapterId===0?readable.find(chapter=>chapter.ord===progress.chapterOrd):undefined)
}
