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

/** Transient system-bar colours do not overwrite the persisted launch canvas. */
export function useBackdropSystemBars(element:Ref<HTMLElement|undefined>, screen:()=>string):void {
  const bridge=(window as unknown as {wails?:{setSystemBarColor?:(color:string)=>void}}).wails
  if(!bridge?.setSystemBarColor)return
  let generation=0, disposed=false, request:AbortController|undefined
  let cached:{src:string;image:ImagePixels}|undefined
  async function update():Promise<void>{
    const node=element.value
    if(disposed||!node)return
    const token=++generation
    request?.abort();request=new AbortController()
    const style=getComputedStyle(node)
    const fallback=style.getPropertyValue('--route-backdrop-color').trim()
    const match=/url\((?:"([^"]*)"|'([^']*)'|([^)]*))\)/.exec(style.backgroundImage)
    const src=match?.[1]||match?.[2]||match?.[3]
    if(!src){cached=undefined;bridge?.setSystemBarColor?.(/^#[\da-f]{6}$/i.test(fallback)?fallback:'');return}
    const image=cached?.src===src?cached.image:await readImagePixels(src,request.signal)
    if(disposed||token!==generation)return
    if(!image){bridge?.setSystemBarColor?.('');return}
    cached={src,image}
    const translation=/translate3d\(([-\d.]+)%,([-\d.]+)%/.exec(node.style.transform)
    const veil=style.getPropertyValue('--route-backdrop-veil').match(/[\d.]+/g)?.map(Number)||[]
    let color=backdropEdgeColor(image,{width:node.offsetWidth,height:node.offsetHeight,viewWidth:node.parentElement?.clientWidth||innerWidth,x:Number(translation?.[1]||0)/100*node.offsetWidth,y:Number(translation?.[2]||0)/100*node.offsetHeight,background:fallback},veil)
    const scene=node.parentElement?.querySelector('[data-route-active="true"]')
    // The page may add a top-edge wash above the shared wallpaper.
    const wash=scene&&/rgba?\(([^)]+)\)/.exec(getComputedStyle(scene,'::before').backgroundImage)
    if(wash)color=blendColor([0,2,4].map(offset=>parseInt(color.slice(offset+1,offset+3),16)),wash[1].split(',').map(Number))
    bridge?.setSystemBarColor?.(color)
  }
  let observer:MutationObserver|undefined
  const refresh=()=>{void update()}
  watch([element,screen],refresh,{flush:'post'})
  onMounted(()=>{
    observer=new MutationObserver(refresh)
    observer.observe(document.documentElement,{attributes:true,attributeFilter:['style','data-theme']})
    window.addEventListener('resize',refresh)
    refresh()
  })
  onBeforeUnmount(()=>{disposed=true;generation++;request?.abort();observer?.disconnect();window.removeEventListener('resize',refresh);cached=undefined;bridge?.setSystemBarColor?.('')})
}
