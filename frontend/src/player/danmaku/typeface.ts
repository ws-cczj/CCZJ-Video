import { shallowRef } from 'vue'
import fontURL from '../../assets/fonts/source-han-sans-cn-variable.woff2?url'

export const danmakuTypefaceStatus = shallowRef<'idle'|'loading'|'ready'|'failed'>('idle')
type FontRegistry = FontFaceSet & { add?: (face:FontFace)=>unknown; delete?: (face:FontFace)=>boolean }
const registry=document.fonts as FontRegistry | undefined
let active: FontFace | undefined, loading: Promise<boolean> | undefined
/** One document-owned font, requested only by the danmaku renderer or preview. */
export function ensureDanmakuTypeface(): Promise<boolean> {
  if (danmakuTypefaceStatus.value === 'ready') return Promise.resolve(true)
  if (loading) return loading
  if (typeof FontFace === 'undefined' || typeof registry?.add !== 'function' || typeof registry?.delete !== 'function') { danmakuTypefaceStatus.value = 'failed'; return Promise.resolve(false) }
  let face: FontFace
  try { face = new FontFace('CCZJ Danmaku Sans', `url("${fontURL}")`, { weight: '100 900', style: 'normal' }); registry.add(face) }
  catch { danmakuTypefaceStatus.value = 'failed'; return Promise.resolve(false) }
  active = face; danmakuTypefaceStatus.value = 'loading'
  const pending: Promise<boolean> = face.load().then(() => {
    if (active !== face) return false
    danmakuTypefaceStatus.value = 'ready'; return true
  }).catch(() => {
    registry?.delete?.(face)
    if (active === face) { active = undefined; danmakuTypefaceStatus.value = 'failed' }
    return false
  }).finally(() => { if (loading === pending) loading = undefined })
  loading = pending
  return pending
}
if (import.meta.hot) import.meta.hot.dispose(() => {
  if (active) registry?.delete?.(active)
  active = undefined; loading = undefined; danmakuTypefaceStatus.value = 'idle'
})
