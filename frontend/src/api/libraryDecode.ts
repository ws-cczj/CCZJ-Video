import { object,text,integer,boolean,each,choice } from './decode'

export function item(value:unknown){const v=object(value);integer(v.id,1);choice(v.kind,['novel','comic']);text(v.sourceKey);text(v.remoteId);text(v.title);boolean(v.available);return v}
export function chapter(value:unknown){const v=object(value);integer(v.id,1);text(v.remoteId);text(v.title);integer(v.ord);boolean(v.available);return v}
export function detail(value:unknown){const v=object(value);item(v.item);each(v.chapters,chapter);integer(v.chapterCount);return v}
export function chapterText(value:unknown){const v=object(value);integer(v.chapterId,1);each(v.paragraphs,text);each(v.warnings,text);boolean(v.fromCache);return v}
export function novelContext(value:unknown){const v=object(value);item(v.item);for(const key of ['chapter','previous','next'])if(v[key]!==null)chapter(v[key]);integer(v.index,-1);integer(v.count);text(v.cacheIdentity);return v}
export function chapterPages(value:unknown){const v=object(value);integer(v.chapterId,1);each(v.pages,value=>{const page=object(value);integer(page.index);text(page.url);return page});each(v.warnings,text);boolean(v.fromCache);return v}
export function search(value:unknown){const v=object(value);text(v.keyword);integer(v.page,1);integer(v.total);integer(v.failedCount);text(v.nextCursor);if(v.searchedSources!==undefined)integer(v.searchedSources);if(v.pendingSources!==undefined)integer(v.pendingSources);each(v.results,value=>{const row=object(value);text(row.sourceKey);text(row.sourceName);each(row.items,item);text(row.nextUrl);boolean(row.hasNext);if(row.errorCode!==undefined)text(row.errorCode);if(row.errorMessage!==undefined)text(row.errorMessage);return row});return v}
export function source(value:unknown){const v=object(value);text(v.sourceKey);text(v.name);text(v.kind);text(v.apiUrl);boolean(v.enabled);return v}
export function outcome(value:unknown){const v=object(value);text(v.sourceKey);boolean(v.ok);each(v.stages,value=>{const row=object(value);text(row.name);choice(row.status,['ok','skipped','failed']);return row});return v}
export function catalog(value:unknown){const v=object(value);each(v.items,value=>{if(value!==null)item(value)});each(v.genres,text);text(v.nextCursor);return v}

// Validate the received DTO in place. Avoid allocating a second large catalog
// tree; generated bindings retain their existing shape and optional fields.
const decoders:Record<string,(value:unknown)=>unknown>={LibraryCatalogPage:catalog,LibrarySearch:search,LibrarySearchPage:search,LibrarySearchStream:search,LibraryItem:detail,LibraryRefreshToc:detail,LibraryRefreshReadingToc:detail,LibraryChapterText:chapterText,LibraryChapterPages:chapterPages,LibraryRefreshChapterPages:chapterPages,LibraryResolveRef:item,LibraryAddSource:source,LibraryTestSource:outcome}
function comicContext(value:unknown){const v=novelContext(value);choice(object(v.item).kind,['comic']);return v}
decoders.LibraryComicContext=comicContext
decoders.LibraryRefreshComicContext=comicContext
decoders.LibraryNovelContext=novelContext
decoders.LibraryRefreshNovelContext=novelContext
export function validateLibrary<T>(name:string,value:T):T{decoders[name]?.(value);return value}
