import { ComicBrowseChannels, ComicBrowsePage, ComicTrending } from './app'
import { response } from './response'
import { boolean, choice, integer, list, object, text } from './decode'
import { item } from './libraryDecode'

export function comicBrowseChannels() {
  return response(() => ComicBrowseChannels(), value => list(value, entry => {
    const row = object(entry)
    return { id: text(row.id), title: text(row.title), description: text(row.description) }
  }, 10))
}

export function comicBrowsePage(channel: string, refresh = false) {
  return response(() => ComicBrowsePage(channel, refresh), value => {
    const page = object(value)
    return {
      channelId: text(page.channelId), sourceName: text(page.sourceName), fetchedAt: integer(page.fetchedAt, 1),
      fromCache: boolean(page.fromCache), stale: boolean(page.stale), warning: text(page.warning),
      items: list(page.items, entry => {
        const row = item(entry)
        return { id: integer(row.id, 1), kind: choice(row.kind, ['comic']), sourceKey: text(row.sourceKey), sourceName: text(row.sourceName), remoteId: text(row.remoteId),
          title: text(row.title), author: text(row.author), cover: text(row.cover), summary: text(row.summary), genres: text(row.genres),
          status: text(row.status), latest: text(row.latest), words: text(row.words), updatedAt: text(row.updatedAt), available: boolean(row.available) }
      }, 500),
    }
  })
}

export type ComicBrowseShelf = Awaited<ReturnType<typeof comicBrowsePage>>
export type ComicBrowseChannel = Awaited<ReturnType<typeof comicBrowseChannels>>[number]

export function comicTrending(board:'japan'|'china') {
 return response(()=>ComicTrending(board,false),value=>{
  const page=object(value)
  return {board:choice(page.board,['japan','china']),fetchedAt:integer(page.fetchedAt,1),fromCache:boolean(page.fromCache),stale:boolean(page.stale),warning:text(page.warning),items:list(page.items,entry=>{const row=object(entry);return {title:text(row.title),author:text(row.author),cover:text(row.cover),genres:text(row.genres),url:text(row.url)}},24)}
 })
}
