import type { NovelChapter } from '../domain'

export interface ChapterEntry { chapter:NovelChapter; number:number }

/** Descending order reverses chapters and volumes while keeping each volume
 * heading ahead of its own children. Display ordinals never change. */
export function orderChapterEntries(entries:readonly ChapterEntry[],descending:boolean):readonly ChapterEntry[] {
  if(!descending)return entries
  const groups:Array<{heading?:ChapterEntry;rows:ChapterEntry[]}>=[{rows:[]}]
  for(const entry of entries){
    if(entry.chapter.isVolume)groups.push({heading:entry,rows:[]})
    else groups[groups.length-1]!.rows.push(entry)
  }
  return groups.reverse().flatMap(group=>[...(group.heading?[group.heading]:[]),...group.rows.reverse()])
}
