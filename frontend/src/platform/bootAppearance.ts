import { CCZJ } from '../constants/cczj'
/** Disposable first-paint cache, shared with index.html and the native launch
 * surface. Authoritative theme preferences stay in SQLite. */
export function cacheLaunchBackground(color: string,mode?:'video'|'novel'|'comic'): void {
  if (!/^#[\da-f]{6}$/i.test(color)) return
  try { localStorage.setItem(CCZJ.Storage.bootBackground, color) } catch { /* WebView storage can be disabled. */ }
  if(mode)try{localStorage.setItem(CCZJ.Keys.bootBackground(mode),color)}catch{}
  if (typeof document !== 'undefined') document.documentElement.style.setProperty('--cczj-boot-bg',color)
  try { (window as unknown as {wails?:{cacheLaunchBackground?:(color:string)=>void}}).wails?.cacheLaunchBackground?.(color) } catch { /* Older native host. */ }
}

export function prepareModeBackground(mode:'video'|'novel'|'comic'):void {
  let color=mode==='comic'?'#fffaf5':'#faf9f6'
  try{const saved=localStorage.getItem(CCZJ.Keys.bootBackground(mode));if(/^#[\da-f]{6}$/i.test(saved||''))color=saved!}catch{}
  cacheLaunchBackground(color)
}
