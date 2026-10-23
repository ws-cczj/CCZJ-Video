import { readingContentCache } from '../../reading/contentCache'
import { readingRequest } from '../../api/requestChain'
import { fetchChapterText, fetchNovelContext } from '../../api/library'
import type { ChapterTextDTO, NovelReadingContextDTO, NovelReadingLocationDTO } from '../../api/library'

export type NovelReaderData = NovelReadingContextDTO & { text: ChapterTextDTO | null }

/** A reader-owned hot copy avoids IPC on backtracking. The existing Go blob
 * cache owns persistence; its identity also invalidates changed source rules.
 * Never retain the whole book or failed/cancelled content in this session.
 */
export function novelReaderSession(
  context = fetchNovelContext, content = fetchChapterText,
  limits = { entries: 24, bytes: 8 * 1024 * 1024, ttl: 10 * 60 * 1000 },
) {
  const cache=readingContentCache<ChapterTextDTO>(limits)
  const clear=cache.clear
  function read(location:NovelReadingLocationDTO) {
    const started=cache.generation()
    return readingRequest<NovelReaderData>(async request => {
      const view=await request(context(location)), chapter=view.chapter
      if(!chapter?.available)return {...view,text:null}
      const key=view.cacheIdentity, saved=key?cache.get(key):undefined
      if(saved) {
        return {...view,text:{...saved,chapterId:chapter.id,paragraphs:[...saved.paragraphs],warnings:[...(saved.warnings??[])]}}
      }
      const text=await request(content(chapter.id))
      const size=256+2*(key.length+text.paragraphs.reduce((sum,p)=>sum+p.length,0)+(text.warnings??[]).join('').length)
      if(text.paragraphs.length)cache.put(key,text,size,started)
      return {...view,text}
    })
  }
  return {read,clear}
}
