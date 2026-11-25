import { tr } from '../locales'
import { ProxyImage } from '../api/app'
import type { ThemeCrop } from './themeCrop'
export type { ThemeCrop } from './themeCrop'
export const THEME_IMAGE_MAX_BYTES=32*1024*1024
export interface ThemeImageOperation {
  crop?: ThemeCrop
  raster?: { maxEdge: number; reuseSmallSource?: true }
}

export async function readThemeImageSource(source: string, signal: AbortSignal): Promise<Blob> {
  try {
    const response = await fetch(source, { signal })
    if (!response.ok) throw new Error(tr('settings.imageLoadFailed'))
    return await response.blob()
  } catch (error) {
    if (signal.aborted || !/^https?:\/\//i.test(source)) throw error
    const request = ProxyImage(source)
    const cancel = () => { void request.cancel() }
    signal.addEventListener('abort', cancel, { once: true })
    try {
      const data = await request
      if (!data.startsWith('data:image/')) throw error
      return await (await fetch(data, { signal })).blob()
    } finally { signal.removeEventListener('abort', cancel) }
  }
}

/** Each invocation owns a worker and releases it on success, failure, timeout or cancellation. */
function processThemeImage(blob: Blob, signal: AbortSignal, operation: ThemeImageOperation): Promise<Blob> {
  if(blob.size>THEME_IMAGE_MAX_BYTES)return Promise.reject(new Error(tr('common.fileTooLarge',{max:32})))
  return new Promise((resolve,reject)=>{
    if(signal.aborted){reject(new DOMException('Cancelled','AbortError'));return}
    const worker=new Worker(new URL('./themeImage.worker.ts',import.meta.url),{type:'module'})
    const cleanup=()=>{clearTimeout(timeout);signal.removeEventListener('abort',abort);worker.terminate()}
    const fail=()=>{cleanup();reject(new Error(tr('settings.imageProcessFailed')))}
    const abort=()=>{cleanup();reject(new DOMException('Cancelled','AbortError'))}
    const timeout=setTimeout(fail,30000)
    signal.addEventListener('abort',abort,{once:true})
    worker.onmessage=event=>{if(event.data.blob instanceof Blob){cleanup();resolve(event.data.blob)}else fail()}
    worker.onerror=fail
    worker.postMessage({ blob, ...operation })
  })
}
export function prepareThemeImage(blob: Blob, signal: AbortSignal, crop?: ThemeCrop): Promise<Blob> {
  return processThemeImage(blob, signal, { crop })
}
export function prepareThemeThumbnail(blob: Blob, signal: AbortSignal): Promise<Blob> {
  return processThemeImage(blob, signal, { raster: { maxEdge: 320 } })
}
export function prepareWallpaperImage(blob: Blob, signal: AbortSignal, maxEdge: number): Promise<Blob> {
  if (typeof Worker !== 'function' || typeof OffscreenCanvas !== 'function' || blob.type === 'image/svg+xml') {
    return Promise.resolve(blob)
  }
  return processThemeImage(blob, signal, { raster: { maxEdge, reuseSmallSource: true } })
}
