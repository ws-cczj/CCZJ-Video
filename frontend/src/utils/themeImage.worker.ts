/// <reference lib="webworker" />
import { MIN_THEME_CROP_EDGE, type ThemeCrop } from './themeCrop'
// Decode and encode outside the UI thread; terminating the worker cancels its owner operation.
self.onmessage = async (event: MessageEvent<{blob: Blob; crop?: ThemeCrop}>) => {
  let bitmap: ImageBitmap | undefined
  try {
    const header=new DataView(await event.data.blob.slice(0,24).arrayBuffer())
    if(header.byteLength>=24&&header.getUint32(0)===0x89504e47&&header.getUint32(4)===0x0d0a1a0a&&header.getUint32(16)*header.getUint32(20)>80_000_000)throw new Error('dimensions')
    bitmap = await createImageBitmap(event.data.blob)
    if (bitmap.width * bitmap.height > 80_000_000) throw new Error('dimensions')
    if (!event.data.crop) {self.postMessage({blob:event.data.blob});return}
    const c = event.data.crop
    const sx = Math.round(bitmap.width*c.x/100), sy = Math.round(bitmap.height*c.y/100)
    const sw = Math.max(1,Math.min(bitmap.width-sx,Math.round(bitmap.width*c.width/100)))
    const sh = Math.max(1,Math.min(bitmap.height-sy,Math.round(bitmap.height*c.height/100)))
    if(sw<Math.min(MIN_THEME_CROP_EDGE,bitmap.width)||sh<Math.min(MIN_THEME_CROP_EDGE,bitmap.height))throw new Error('crop')
    const canvas = new OffscreenCanvas(sw,sh)
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('canvas')
    ctx.drawImage(bitmap,sx,sy,sw,sh,0,0,sw,sh)
    const blob = await canvas.convertToBlob({type:'image/png'})
    if(blob.size>32*1024*1024)throw new Error('size')
    self.postMessage({blob})
  } catch { self.postMessage({error:true}) }
  finally { bitmap?.close() }
}
