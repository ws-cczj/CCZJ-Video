import { response } from './response'
import { boolean, choice, each, integer, object, required, text } from './decode'
import { search } from './libraryDecode'
import { ComicEpisodeMatch, ComicSourceAlternatives, ComicSourceDirectory, NovelChapterMatch, NovelSourceAlternatives, NovelSourceDirectory } from './app'
import type { BuiltinSourceRow, ChapterMatch, SourceDirectory } from '../../bindings/cczjVideo/app/library/models'

export type { BuiltinSourceRow, ChapterMatch, SourceDirectory }
function directory(value: SourceDirectory | null, kind: 'novel' | 'comic'): SourceDirectory {
  const dto = required(value), v = object(dto)
  choice(v.kind, [kind]); integer(v.registered); integer(v.awaitingAdapter); boolean(v.auditRunning)
  each(v.rows, entry => { const row = object(entry); text(row.id); text(row.sourceKey); text(row.name); boolean(row.enabled); choice(row.pool, ['known','preferred','supplement','reserve']); integer(row.score,-1,100); choice(row.confidence,['insufficient','limited','substantial']); integer(row.checkedAt); integer(row.deepAt); text(row.state) }, 10000)
  return dto
}
function alignment(value: ChapterMatch | null, kind: 'novel' | 'comic'): ChapterMatch {
  const dto = required(value), v = object(dto)
  choice(v.kind,[kind]); choice(v.identity,['verified','provisional']); choice(v.status,['matched','ambiguous','unmatched']); integer(v.chapterId); integer(v.targetItemId,1); text(v.evidence)
  each(v.targets, entry => { const row=object(entry); integer(row.id,1); integer(row.ord); text(row.title); text(row.remoteId); boolean(row.available) },8)
  return dto
}
export const listNovelBuiltinSources = () => response(() => NovelSourceDirectory(), value => directory(value,'novel'))
export const listComicBuiltinSources = () => response(() => ComicSourceDirectory(), value => directory(value,'comic'))
export const findNovelSources = (id:number,cursor='') => response(() => NovelSourceAlternatives(id,cursor),value => {const dto=required(value);search(dto);for(const row of dto.results??[])for(const item of row?.items??[])if(item)choice(item.kind,['novel']);return dto})
export const findComicSources = (id:number,cursor='') => response(() => ComicSourceAlternatives(id,cursor),value => {const dto=required(value);search(dto);for(const row of dto.results??[])for(const item of row?.items??[])if(item)choice(item.kind,['comic']);return dto})
export const matchNovelChapter = (id:number,chapter:number,target:number) => response(() => NovelChapterMatch(id,chapter,target),value => alignment(value,'novel'))
export const matchComicEpisode = (id:number,chapter:number,target:number) => response(() => ComicEpisodeMatch(id,chapter,target),value => alignment(value,'comic'))
