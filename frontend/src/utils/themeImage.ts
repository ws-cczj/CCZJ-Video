import { tr } from '../locales'
import type { ThemeCrop } from './themeCrop'
export type { ThemeCrop } from './themeCrop'
export const THEME_IMAGE_MAX_BYTES=32*1024*1024
/** Each invocation owns a worker and releases it on success, failure, timeout or cancellation. */
export function prepareThemeImage(blob:Blob,signal:AbortSignal,crop?:ThemeCrop):Promise<Blob> {
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
    worker.postMessage({blob,crop})
  })
}
