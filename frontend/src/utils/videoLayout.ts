export type VideoFit = 'contain' | 'cover' | 'fill' | 'width' | 'height'
export interface VideoPictureSize {
  width:number; height:number; sourceWidth:number; sourceHeight:number; viewportWidth:number; viewportHeight:number
}

/** Resize the media element itself so native video surfaces and enhancement
 * canvases follow the same geometry, without relying on an object-fit repaint. */
export function sizeVideoPicture(video:HTMLVideoElement,host:HTMLElement,fit:VideoFit):VideoPictureSize|undefined {
  const width=host.clientWidth,height=host.clientHeight
  if(!width||!height||!video.videoWidth||!video.videoHeight)return
  let scale=1,pictureWidth=width,pictureHeight=height
  if(fit!=='fill'){
    const horizontal=width/video.videoWidth,vertical=height/video.videoHeight
    scale=fit==='width'?horizontal:fit==='height'?vertical:fit==='cover'?Math.max(horizontal,vertical):Math.min(horizontal,vertical)
    pictureWidth=video.videoWidth*scale;pictureHeight=video.videoHeight*scale
  }
  video.style.width=`${pictureWidth}px`
  video.style.height=`${pictureHeight}px`
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
export function layoutCanvasToVideo(video: HTMLVideoElement, canvas: HTMLCanvasElement): void {
  const bounds = canvas.parentElement && videoPictureBounds(video, canvas.parentElement)
  if (!bounds) return
  canvas.style.left = `${bounds.left}px`
  canvas.style.top = `${bounds.top}px`
  canvas.style.width = `${bounds.width}px`
  canvas.style.height = `${bounds.height}px`
}
