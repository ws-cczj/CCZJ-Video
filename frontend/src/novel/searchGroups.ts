import { refKey, refOf, type ItemDTO } from '../api/library'
import type { BuiltinSourceRow } from '../api/readingSources'
import { searchKey } from '../reading/searchRelevance'
import type { ReadingProgress } from './domain'

export interface NovelSearchGroup { key:string; book:ItemDTO; sources:ItemDTO[]; continuing:boolean }

/** Display-only grouping. Unknown language/author stays independent; edition
 * words/numbers remain in the title key. Storage and progress keep source IDs. */
export function groupNovelSearch(items:readonly ItemDTO[],sources:readonly BuiltinSourceRow[],progress:Readonly<Record<string,ReadingProgress>>):NovelSearchGroup[] {
  const directory=new Map(sources.map(row=>[row.sourceKey,row]))
  const groups=new Map<string,NovelSearchGroup>()
  for(const book of items){
    const source=directory.get(book.sourceKey),title=searchKey(book.title),author=searchKey(book.author)
    const language=source?.language
    const key=author&&title&&language&&language!=='unknown'?JSON.stringify([title,author,language]):`item:${book.id}`
    const group=groups.get(key)
    if(group){if(!group.sources.some(row=>row.id===book.id))group.sources.push(book)}
    else groups.set(key,{key,book,sources:[book],continuing:false})
  }
  for(const group of groups.values()){
    group.sources.sort((a,b)=>{
      const savedA=progress[refKey(refOf(a))]?.updatedAt??0,savedB=progress[refKey(refOf(b))]?.updatedAt??0
      if(savedA!==savedB)return savedB-savedA
      return Number(directory.get(b.sourceKey)?.supply==='maintained')-Number(directory.get(a.sourceKey)?.supply==='maintained')
    })
    group.book=group.sources[0]!
    group.continuing=!!progress[refKey(refOf(group.book))]
  }
  return [...groups.values()]
}
