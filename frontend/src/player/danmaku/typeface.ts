import { shallowRef } from 'vue'
import fontURL from '../../assets/fonts/cczj-danmaku-sans.woff2?url'
import { danmakuTypefaceFamily } from './textStyle'

export const danmakuTypefaceStatus = shallowRef<'idle' | 'loading' | 'ready' | 'failed'>('idle')
type FontRegistry = FontFaceSet & { add?: (face: FontFace) => unknown; delete?: (face: FontFace) => boolean }
const registry = document.fonts as FontRegistry | undefined
let active: FontFace | undefined
let loading: Promise<boolean> | undefined

/** The bundled face is document-owned and loaded only when selected. */
export function ensureDanmakuTypeface(): Promise<boolean> {
  if (active) return Promise.resolve(true)
  if (loading) return loading
  if (typeof FontFace === 'undefined' || typeof registry?.add !== 'function' || typeof registry?.delete !== 'function') {
    danmakuTypefaceStatus.value = 'failed'
    return Promise.resolve(false)
  }

  let face: FontFace
  try {
    face = new FontFace(danmakuTypefaceFamily, `url("${fontURL}") format("woff2")`, { weight: '250 900', style: 'normal' })
  } catch {
    danmakuTypefaceStatus.value = 'failed'
    return Promise.resolve(false)
  }
  danmakuTypefaceStatus.value = 'loading'
  const pending: Promise<boolean> = face.load().then(() => {
    if (loading !== pending) return false
    registry.add!(face)
    active = face
    danmakuTypefaceStatus.value = 'ready'
    return true
  }).catch(() => {
    if (loading === pending) danmakuTypefaceStatus.value = 'failed'
    return false
  }).finally(() => {
    if (loading === pending) loading = undefined
  })
  loading = pending
  return pending
}

if (import.meta.hot) import.meta.hot.dispose(() => {
  if (active) registry?.delete?.(active)
  active = undefined
  loading = undefined
  danmakuTypefaceStatus.value = 'idle'
})
