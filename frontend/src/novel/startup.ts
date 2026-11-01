import { novelBrowseChannels, novelBrowsePage } from '../api/novelBrowse'

let prepared: { channels: ReturnType<typeof novelBrowseChannels>; pages: Map<string, ReturnType<typeof novelBrowsePage>> } | undefined

export function prepareNovelHome(channel: string): void {
  if (prepared) return
  const channels = novelBrowseChannels()
  const pages = new Map<string, ReturnType<typeof novelBrowsePage>>()
  prepared = { channels, pages }
  void channels.catch(() => {})
  for (const id of channel ? [channel] : ['popular', 'publication', 'history']) {
    const request = novelBrowsePage(id)
    pages.set(id, request)
    void request.catch(() => {})
  }
}

export function takeNovelHomePreparation() {
  const result = prepared
  prepared = undefined
  return result
}

export function cancelNovelHomePreparation(): void {
  prepared?.channels.cancel()
  for (const request of prepared?.pages.values() ?? []) request.cancel()
  prepared = undefined
}
