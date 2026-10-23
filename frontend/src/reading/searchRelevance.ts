/** Typography-only normalization: edition numbers and subtitles keep their meaning. */
export function searchKey(value: string): string {
  return value.normalize('NFKC').toLowerCase().replace(/[\p{White_Space}\p{P}\p{S}]/gu, '')
}

export function searchRelevance(item: { title: string; author?: string }, keyword: string): number {
  const query = searchKey(keyword)
  if (!query) return 0
  const title = searchKey(item.title), author = searchKey(item.author??'')
  if (title === query) return 4
  if (author === query) return 3
  if (title.includes(query)) return 2
  if (author.includes(query)) return 1
  return 0
}

export function rankSearchItems<T extends { title: string; author: string }>(items: readonly T[], keyword: string): T[] {
  // Decorate once so normalization is O(text size), rather than repeated by sort.
  return items.map((item, index) => ({ item, index, score: searchRelevance(item, keyword) }))
    .sort((a, b) => b.score - a.score || a.index - b.index).map(row => row.item)
}
