import { response } from './response'
import { boolean, choice, each, integer, object, required, text } from './decode'
import { search, item, chapter, chapterText, readingEvidence } from './libraryDecode'
import { ComicEpisodeMatch, ComicSourceAlternatives, ComicSourceDirectory, NovelChapterMatch, NovelSourceAlternatives, NovelSourceDirectory, NovelPrepareSourceSwitch, NovelBookSources } from './app'
import type { BuiltinSourceRow, ChapterMatch, SourceDirectory } from '../../bindings/cczjVideo/app/library/models'

export type { BuiltinSourceRow, ChapterMatch, SourceDirectory }
export const novelBookSources=(id:number,refresh=false)=>response(()=>NovelBookSources(id,refresh),value=>{
  const dto=required(value),v=object(dto)
  integer(v.itemId,1);integer(v.checkedAt);integer(v.nextCheckAt);integer(v.available);integer(v.target,1)
  each(v.candidates,value=>{const row=object(value);item(row.item);choice(object(row.item).kind,['novel']);choice(row.identity,['verified','provisional']);choice(row.state,['candidate','sample_verified','readable','limited','temporary','invalid','stale','paused','unavailable']);integer(row.checkedAt);text(row.errorCode);text(row.revision);text(row.family)},12)
  if(dto.itemId!==id)throw new TypeError('Invalid book source cache identity')
  return dto
})
function directory(value: SourceDirectory | null, kind: 'novel' | 'comic'): SourceDirectory {
  const dto = required(value), v = object(dto)
  choice(v.kind, [kind]); integer(v.registered); integer(v.awaitingAdapter); boolean(v.auditRunning)
  each(v.rows, entry => { const row = object(entry); text(row.id); text(row.sourceKey); text(row.name); boolean(row.enabled); choice(row.pool, ['known','preferred','supplement','reserve']); integer(row.score,-1,100); choice(row.confidence,['insufficient','limited','substantial']); integer(row.checkedAt); integer(row.deepAt); text(row.state) }, 10000)
  each(v.rows, entry => {
    const row=object(entry)
    choice(row.supply,['maintained','supplement','custom']);choice(row.section,['active','reserve','attention'])
    for(const key of ['site','origin','project','protocol','revision','family','language'])text(row[key])
    each(row.capabilities,text,16)
    each(row.operations,entry=>{const operation=object(entry);text(operation.operation);choice(operation.status,['ok','failed','skipped','stale','empty']);integer(operation.checkedAt);integer(operation.count);integer(operation.millis);text(operation.code)},8)
    readingEvidence(row.reading)
  },10000)
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
export const prepareNovelSwitch = (id:number,fromChapter:number,target:number,targetChapter:number) => response(()=>NovelPrepareSourceSwitch(id,fromChapter,target,targetChapter),value=>{
  const dto=required(value),v=object(dto)
  item(v.item);choice(object(v.item).kind,['novel']);chapter(v.chapter);chapterText(v.text);text(v.revision);integer(v.checkedAt,1)
  if(integer(object(v.chapter).id,1)!==targetChapter || integer(object(v.text).chapterId,1)!==targetChapter || integer(object(v.item).id,1)!==target)throw new TypeError('Invalid prepared source identity')
  return dto
})
