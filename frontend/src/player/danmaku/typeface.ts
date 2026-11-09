import { shallowRef, watch } from 'vue'
import { useInstalledResources, installedResourcesRevision } from '../../resources/store'

export const danmakuTypefaceStatus = shallowRef<'idle'|'loading'|'ready'|'failed'|'absent'>('idle')
type FontRegistry = FontFaceSet & { add?: (face:FontFace)=>unknown; delete?: (face:FontFace)=>boolean }
const registry=document.fonts as FontRegistry | undefined
let generation=0
let active: FontFace | undefined, loading: Promise<boolean> | undefined
/** One document-owned font, requested only by the danmaku renderer or preview. */
async function loadInstalledFont():Promise<boolean> {
  const token=generation,store=useInstalledResources()
  const available=store.ready||await store.load()
  if(token!==generation)return false
  if(!available){danmakuTypefaceStatus.value='failed';return false}
  const fontURL=store.font?.installed?store.font.url:''
  if(!fontURL){danmakuTypefaceStatus.value='absent';return false}
  return loadFace(fontURL)
}
export function ensureDanmakuTypeface(): Promise<boolean> {
  if(danmakuTypefaceStatus.value==='ready')return Promise.resolve(true)
  if(loading)return loading
  const pending=loadInstalledFont().finally(()=>{if(loading===pending)loading=undefined})
  loading=pending
  return pending
}
function loadFace(fontURL:string): Promise<boolean> {
  if (danmakuTypefaceStatus.value === 'ready') return Promise.resolve(true)
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
  })
  return pending
}
function reset():void {
  generation++
  if (active) registry?.delete?.(active)
  active = undefined; loading = undefined; danmakuTypefaceStatus.value = 'idle'
}
const stopWatching=watch(installedResourcesRevision,reset,{flush:'sync'})
if(import.meta.hot)import.meta.hot.dispose(()=>{stopWatching();reset()})
