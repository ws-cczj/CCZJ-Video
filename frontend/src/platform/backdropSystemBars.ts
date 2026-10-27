import { onBeforeUnmount, onMounted, watch, type Ref } from 'vue'
import { readImagePixels, type ImagePixels } from '../utils/imagePalette'

type Frame = { width:number; height:number; viewWidth:number; x:number; y:number; background?:string }
function blendColor(rgb:number[],veil:number[]):string {
  const alpha=veil.length>=3?(veil[3]??1):0
  return '#'+rgb.map((value,index)=>Math.round(value*(1-alpha)+(veil[index]||0)*alpha).toString(16).padStart(2,'0')).join('')
}

/** Sample the visible wallpaper's top edge, including CSS cover and its veil. */
export function backdropEdgeColor(image:ImagePixels, frame:Frame, veil:number[]):string {
  const scale=Math.max(frame.width/image.width,frame.height/image.height)
  const left=(frame.width-image.width*scale)/2, top=(frame.height-image.height*scale)/2
  const sum=[0,0,0]
  const background=frame.background||'#ffffff'
  const hex=/^#[\da-f]{6}$/i.test(background)?background.slice(1):'ffffff'
  const base=[0,2,4].map(offset=>parseInt(hex.slice(offset,offset+2),16))
  for(let i=0;i<16;i++){
    const x=Math.max(0,Math.min(image.width-1,Math.floor(((i+.5)*frame.viewWidth/16-frame.x-left)/scale)))
    const y=Math.max(0,Math.min(image.height-1,Math.floor((-frame.y-top)/scale)))
    const index=(y*image.width+x)*4
    const opacity=image.data[index+3]/255
    for(let channel=0;channel<3;channel++)sum[channel]+=(image.data[index+channel]*opacity+base[channel]*(1-opacity))/16
  }
  return blendColor(sum,veil)
}

/** The top wash also exists on plain themes, without a wallpaper to sample. */
export function systemBarSurface(base:string,wash:number[],cinema=false):string {
  if(cinema)return '#000000'
  if(!/^#[\da-f]{6}$/i.test(base))return ''
  return blendColor([0,2,4].map(offset=>parseInt(base.slice(offset+1,offset+3),16)),wash)
}

/** Transient system-bar colours do not overwrite the persisted launch canvas. */
export function useBackdropSystemBars(element:Ref<HTMLElement|undefined>, screen:()=>string,cinema:()=>boolean=()=>false):void {
  const bridge=(window as unknown as {wails?:{setSystemBarColor?:(color:string)=>void}}).wails
  if(!bridge?.setSystemBarColor)return
  let generation=0, disposed=false, request:AbortController|undefined
  let scheduled:number|undefined,lastColor:string|undefined
  function publishColor(color:string):void {
    if(lastColor===color)return
    bridge?.setSystemBarColor?.(color)
    lastColor=color
  }
  let cached:{src:string;image:ImagePixels}|undefined
  async function update():Promise<void>{
    const node=element.value
    if(disposed||!node)return
    const token=++generation
    request?.abort();request=new AbortController()
    const splash=document.documentElement.dataset.cczjSplashColor
    if(splash){publishColor(splash);return}
    const scene=node.parentElement?.querySelector('[data-route-active="true"]')
    if(cinema()||scene?.querySelector('[data-cczj-media-active=true]')){publishColor('#000000');return}
    const style=getComputedStyle(node)
    const fallback=style.getPropertyValue('--route-backdrop-color').trim()
    const publish=(base:string)=>{
      const wash=scene&&/rgba?\(([^)]+)\)/.exec(getComputedStyle(scene,'::before').backgroundImage)
      publishColor(systemBarSurface(base,wash?wash[1].split(',').map(Number):[]))
    }
    const match=/url\((?:"([^"]*)"|'([^']*)'|([^)]*))\)/.exec(style.backgroundImage)
    const src=match?.[1]||match?.[2]||match?.[3]
    if(!src){cached=undefined;publish(fallback);return}
    const image=cached?.src===src?cached.image:await readImagePixels(src,request.signal)
    if(disposed||token!==generation)return
    if(!image){publish(fallback);return}
    cached={src,image}
    const translation=/translate3d\(([-\d.]+)%,([-\d.]+)%/.exec(node.style.transform)
    const veil=style.getPropertyValue('--route-backdrop-veil').match(/[\d.]+/g)?.map(Number)||[]
    publish(backdropEdgeColor(image,{width:node.offsetWidth,height:node.offsetHeight,viewWidth:node.parentElement?.clientWidth||innerWidth,x:Number(translation?.[1]||0)/100*node.offsetWidth,y:Number(translation?.[2]||0)/100*node.offsetHeight,background:fallback},veil))
  }
  let observer:MutationObserver|undefined
  // Route hooks and mutation records can describe the same commit. Read layout
  // once after it, rather than forcing layout and native work from each record.
  const refresh=()=>{
    if(disposed)return
    generation++;request?.abort()
    if(scheduled!==undefined)return
    scheduled=requestAnimationFrame(()=>{scheduled=undefined;void update()})
  }
  watch([element,screen,cinema],refresh,{flush:'post'})
  onMounted(()=>{
    observer=new MutationObserver(refresh)
    observer.observe(document.documentElement,{attributes:true,attributeFilter:['style','data-theme','data-cczj-splash-color']})
    if(element.value?.parentElement)observer.observe(element.value.parentElement,{subtree:true,attributes:true,attributeFilter:['data-cczj-media-active','data-route-active']})
    window.addEventListener('resize',refresh)
    refresh()
  })
  onBeforeUnmount(()=>{disposed=true;generation++;if(scheduled!==undefined)cancelAnimationFrame(scheduled);request?.abort();observer?.disconnect();window.removeEventListener('resize',refresh);cached=undefined;bridge?.setSystemBarColor?.('')})
}
