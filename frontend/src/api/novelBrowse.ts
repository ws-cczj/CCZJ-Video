import { NovelBrowseChannels, NovelBrowsePage } from './app'
import { response } from './response'
import { boolean, integer, list, object, required, text } from './decode'
import { item } from './libraryDecode'

export function novelBrowseChannels() {
  return response(() => NovelBrowseChannels(), value => list(value, entry => {
    const row = object(entry)
    return { id: text(row.id), title: text(row.title), description: text(row.description) }
  }, 20))
}

export function novelBrowsePage(channel: string, cursor = '', refresh = false) {
  return response(() => NovelBrowsePage(channel, cursor, refresh), value => {
    const page = required(value)
    text(page.channelId); text(page.sourceName); text(page.nextCursor)
    integer(page.fetchedAt, 1); boolean(page.fromCache); boolean(page.stale); text(page.warning)
    const items = list(page.items, entry => {
      const book = item(entry)
      if (book.kind !== 'novel') throw new TypeError('Invalid novel browse item')
      return {
        id: integer(book.id, 1), kind: 'novel', sourceKey: text(book.sourceKey), sourceName: text(book.sourceName),
        remoteId: text(book.remoteId), title: text(book.title), author: text(book.author), cover: text(book.cover),
        summary: text(book.summary), genres: text(book.genres), status: text(book.status), words: text(book.words),
        latest: text(book.latest), updatedAt: text(book.updatedAt), available: boolean(book.available),
      }
    }, 200)
    return { ...page, items }
  })
}

export type BrowsePage = Awaited<ReturnType<typeof novelBrowsePage>>
export type BrowseChannel = Awaited<ReturnType<typeof novelBrowseChannels>>[number]
