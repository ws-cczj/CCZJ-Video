/// <reference lib="webworker" />
import { MIN_THEME_CROP_EDGE } from './themeCrop'
import type { ThemeImageOperation } from './themeImage'
// Decode and encode outside the UI thread; terminating the worker cancels its owner operation.
self.onmessage = async (event: MessageEvent<{ blob: Blob } & ThemeImageOperation>) => {
  let bitmap: ImageBitmap | undefined
  try {
    const { blob: source, raster } = event.data
    if (raster?.reuseSmallSource && (typeof createImageBitmap !== 'function' || typeof OffscreenCanvas !== 'function')) {
      self.postMessage({ blob: source })
      return
    }
    const header=new DataView(await source.slice(0,24).arrayBuffer())
    if(header.byteLength>=24&&header.getUint32(0)===0x89504e47&&header.getUint32(4)===0x0d0a1a0a&&header.getUint32(16)*header.getUint32(20)>80_000_000)throw new Error('dimensions')
    bitmap = await createImageBitmap(source)
    if (bitmap.width * bitmap.height > 80_000_000) throw new Error('dimensions')
    if (raster) {
      const scale = Math.min(1, raster.maxEdge / Math.max(bitmap.width, bitmap.height))
      if (scale === 1 && raster.reuseSmallSource) {
        self.postMessage({ blob: source })
        return
      }
      const canvas = new OffscreenCanvas(Math.max(1, Math.round(bitmap.width * scale)), Math.max(1, Math.round(bitmap.height * scale)))
      const context = canvas.getContext('2d')
      if (!context) throw new Error('canvas')
      context.imageSmoothingQuality = 'high'
      context.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
      bitmap.close()
      bitmap = undefined
      self.postMessage({ blob: await canvas.convertToBlob({ type: 'image/webp', quality: raster.reuseSmallSource ? .92 : .8 }) })
      return
    }
    if (!event.data.crop) {self.postMessage({blob:source});return}
    const c = event.data.crop
    const sx = Math.round(bitmap.width*c.x/100), sy = Math.round(bitmap.height*c.y/100)
    const sw = Math.max(1,Math.min(bitmap.width-sx,Math.round(bitmap.width*c.width/100)))
    const sh = Math.max(1,Math.min(bitmap.height-sy,Math.round(bitmap.height*c.height/100)))
    if(sw<Math.min(MIN_THEME_CROP_EDGE,bitmap.width)||sh<Math.min(MIN_THEME_CROP_EDGE,bitmap.height))throw new Error('crop')
    const canvas = new OffscreenCanvas(sw,sh)
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('canvas')
    ctx.drawImage(bitmap,sx,sy,sw,sh,0,0,sw,sh)
    bitmap.close()
    bitmap = undefined
    const blob = await canvas.convertToBlob({type:'image/png'})
    if(blob.size>32*1024*1024)throw new Error('size')
    self.postMessage({blob})
  } catch { self.postMessage({error:true}) }
  finally { bitmap?.close() }
}
