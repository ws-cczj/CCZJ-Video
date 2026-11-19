export const builtinDanmakuSources = [
  { id: 'bilibili', icon: 'source-bilibili' },
  { id: 'qq', icon: 'source-qq' },
  { id: 'iqiyi', icon: 'source-iqiyi' },
  { id: 'mgtv', icon: 'source-mgtv' },
  { id: 'migu', icon: 'source-migu' },
  { id: 'youku', icon: 'source-youku' },
  { id: 'sohu', icon: 'source-sohu' },
  { id: 'leshi', icon: 'source-leshi' },
  { id: 'xigua', icon: 'source-xigua' },
  { id: 'bahamut', icon: 'source-bahamut' },
] as const

export type DanmakuSource = typeof builtinDanmakuSources[number]
export type DanmakuProvider = DanmakuSource['id']
export const danmakuSourceByID = (id: unknown) => builtinDanmakuSources.find(source => source.id === id)
export const isDanmakuProvider = (id: unknown): id is DanmakuProvider => danmakuSourceByID(id) !== undefined
