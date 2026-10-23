export type NovelBookTab = 'intro' | 'chapters'

// The detail view can be recreated when the reader is left. Keep only the
// lightweight task state in memory; chapter data remains owned by the loader.
const tabs = new Map<number, NovelBookTab>()

export function readNovelBookTab(bookId: number): NovelBookTab {
  return tabs.get(bookId) ?? 'intro'
}

export function rememberNovelBookTab(bookId: number, tab: NovelBookTab): void {
  if (Number.isInteger(bookId) && bookId > 0) tabs.set(bookId, tab)
}
