export type VideoFit = 'contain' | 'cover' | 'fill' | 'width' | 'height'
export interface VideoPictureSize {
  width:number; height:number; sourceWidth:number; sourceHeight:number; viewportWidth:number; viewportHeight:number
}

/** Resize the media element itself so native video surfaces and enhancement
 * canvases follow the same geometry, without relying on an object-fit repaint. */
export function sizeVideoPicture(video:HTMLVideoElement,host:HTMLElement,fit:VideoFit,fluid=false):VideoPictureSize|undefined {
  const width=host.clientWidth,height=host.clientHeight
  if(!width||!height||!video.videoWidth||!video.videoHeight)return
  let scale=1,pictureWidth=width,pictureHeight=height
  if(fit!=='fill'){
    const horizontal=width/video.videoWidth,vertical=height/video.videoHeight
    scale=fit==='width'?horizontal:fit==='height'?vertical:fit==='cover'?Math.max(horizontal,vertical):Math.min(horizontal,vertical)
    pictureWidth=video.videoWidth*scale;pictureHeight=video.videoHeight*scale
  }
  if(fluid && CSS.supports('width','1cqw')){
    // Container lengths follow native viewport rotation in the same layout,
    // before ResizeObserver refreshes enhancement canvases and diagnostics.
    const ratio=video.videoWidth/video.videoHeight
    const fittedWidth=`calc(100cqh * ${ratio})`,fittedHeight=`calc(100cqw / ${ratio})`
    video.style.width=fit==='fill'||fit==='width'?'100cqw':fit==='height'?fittedWidth:`${fit==='cover'?'max':'min'}(100cqw,${fittedWidth})`
    video.style.height=fit==='fill'||fit==='height'?'100cqh':fit==='width'?fittedHeight:`${fit==='cover'?'max':'min'}(100cqh,${fittedHeight})`
  }else{
    video.style.width=`${pictureWidth}px`
    video.style.height=`${pictureHeight}px`
  }
  video.style.objectFit='fill'
  return {width:video.clientWidth,height:video.clientHeight,sourceWidth:video.videoWidth,sourceHeight:video.videoHeight,viewportWidth:width,viewportHeight:height}
}

/** Decoded picture geometry shared by enhancement output and page snapshots. */
export function videoPictureBounds(video: HTMLVideoElement, host: HTMLElement) {
  if (!video.videoWidth || !video.videoHeight) return
  const rect = video.getBoundingClientRect(), parent = host.getBoundingClientRect()
  if (rect.width <= 0 || rect.height <= 0) return
  const fit = getComputedStyle(video).objectFit
  let width = rect.width, height = rect.height
  if (fit !== 'fill') {
    const scale = (fit === 'cover' ? Math.max : Math.min)(rect.width / video.videoWidth, rect.height / video.videoHeight)
    width = video.videoWidth * scale; height = video.videoHeight * scale
  }
  return { left: rect.left - parent.left + (rect.width - width) / 2, top: rect.top - parent.top + (rect.height - height) / 2, width, height }
}

/** Match enhancement output to the decoded image, including contain/cover/fill. */
export function layoutCanvasToVideo(video: HTMLVideoElement, canvas: HTMLCanvasElement) {
  const bounds = canvas.parentElement && videoPictureBounds(video, canvas.parentElement)
  if (!bounds) return
  canvas.style.left = `${bounds.left}px`
  canvas.style.top = `${bounds.top}px`
  canvas.style.width = `${bounds.width}px`
  canvas.style.height = `${bounds.height}px`
  return bounds
}

export interface EnhancementBufferBudget { maxEdge: number; maxPixels: number }

/** The output need only cover the displayed image in physical pixels. */
export function sizeEnhancementBuffer(video: HTMLVideoElement, canvas: HTMLCanvasElement, scale: number, budget: EnhancementBufferBudget): boolean {
  const bounds = layoutCanvasToVideo(video, canvas)
  const ratio = window.devicePixelRatio || 1
  const displayWidth = Math.max(1, Math.min(video.videoWidth * scale, Math.ceil((bounds ? bounds.width * ratio : video.videoWidth) || 1)))
  const displayHeight = Math.max(1, Math.min(video.videoHeight * scale, Math.ceil((bounds ? bounds.height * ratio : video.videoHeight) || 1)))
  const fit = Math.min(1, budget.maxEdge / Math.max(displayWidth, displayHeight), Math.sqrt(budget.maxPixels / (displayWidth * displayHeight)))
  const width = Math.max(1, Math.floor(displayWidth * fit))
  const height = Math.max(1, Math.floor(displayHeight * fit))
  if (width === canvas.width && height === canvas.height) return false
  canvas.width = width
  canvas.height = height
  return true
}

/** The source upload and canvas are RGBA8. Every intermediate target is RGBA16F. */
export function enhancementBufferBudget(gl: WebGL2RenderingContext, width: number, height: number, sourceTargets: number, doubleTargets = 0): EnhancementBufferBudget | undefined {
  const limit = Math.min(gl.getParameter(gl.MAX_TEXTURE_SIZE), gl.getParameter(gl.MAX_RENDERBUFFER_SIZE))
  const scale = doubleTargets ? 2 : 1
  if (Math.max(width * scale, height * scale) > limit) return
  const textureBytes = width * height * (4 + 8 * sourceTargets + 32 * doubleTargets)
  const pixels = Math.floor((256 * 1024 * 1024 - textureBytes) / 4)
  if (pixels < 1) return
  return { maxEdge: Math.min(4096, limit), maxPixels: Math.min(3840 * 2160, pixels) }
}
