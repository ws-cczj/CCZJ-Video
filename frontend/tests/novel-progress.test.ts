import { expect, it } from 'vitest'
import type { NovelChapter, ReadingProgress } from '../src/novel/domain'
import { progressChapter } from '../src/novel/reader/progress'

const chapters:NovelChapter[]=[
  {id:100,remoteId:'volume',ord:0,title:'卷一',volume:'',isVolume:true,available:true,access:'unknown'},
  {id:101,remoteId:'one',ord:1,title:'第一章',volume:'',isVolume:false,available:true,access:'available'},
  {id:102,remoteId:'two',ord:2,title:'第二章',volume:'',isVolume:false,available:true,access:'available'},
]
const saved:ReadingProgress={itemId:1,title:'书籍',chapterId:0,chapterRemoteId:'two',chapterOrd:2,ratio:.45,updatedAt:1}

it('resolves portable progress by remote identity after local IDs have changed',()=>{
  expect(progressChapter(chapters,saved)?.id).toBe(102)
  expect(progressChapter(chapters,{...saved,chapterId:101})?.id).toBe(102)
})
it('supports legacy records with no remote identity and never selects volume headings',()=>{
  expect(progressChapter(chapters,{...saved,chapterRemoteId:'',chapterId:101})?.id).toBe(101)
  expect(progressChapter(chapters,{...saved,chapterRemoteId:''})?.id).toBe(102)
  expect(progressChapter(chapters,{...saved,chapterRemoteId:'',chapterOrd:0})).toBeUndefined()
})
it('keeps a missing saved chapter explicit rather than silently restarting the book',()=>{
  expect(progressChapter(chapters,{...saved,chapterRemoteId:'removed'})).toBeUndefined()
  expect(progressChapter(chapters,{...saved,chapterId:999,chapterRemoteId:''})).toBeUndefined()
  expect(progressChapter(chapters,undefined)).toBeUndefined()
})
