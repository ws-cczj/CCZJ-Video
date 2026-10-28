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

function interpolateColor(from:string,to:string,progress:number):string {
  if (!/^#[\da-f]{6}$/i.test(from)) return to
  return '#'+[1,3,5].map(offset=>Math.round(parseInt(from.slice(offset,offset+2),16)*(1-progress)+parseInt(to.slice(offset,offset+2),16)*progress).toString(16).padStart(2,'0')).join('')
}

/** One visual transition owns navigation and native bars. Its progress comes
 * from the same scene transform as the page, including cancellation/settling.
 * Reference: Douyin MPFBottomTabComponent.changeThemeWhenPageScroll (REA report).
 * Transient colours never overwrite the persisted launch canvas. */
export function useBackdropSystemBars(element:Ref<HTMLElement|undefined>, screen:()=>string,cinema:()=>boolean=()=>false) {
  const bridge=(window as unknown as {wails?:{setSystemBarColor?:(color:string)=>void;setSystemBarColors?:(status:string,navigation:string)=>void}}).wails
  let generation=0, disposed=false, request:AbortController|undefined
  let scheduled:number|undefined,lastColor:string|undefined
  function publishColor(color:string,navigation=color):void {
    const identity=color+':'+navigation
    if(lastColor===identity)return
    if(bridge?.setSystemBarColors)bridge.setSystemBarColors(color,navigation)
    else bridge?.setSystemBarColor?.(color)
    lastColor=identity
  }
  let normal={status:'#000000',bottom:'#000000',fill:[0,0,0,0]}, motion:{from:string;to:string;progress:number}|undefined
  let motionFrame:number|undefined, motionRevision=0
  const stage=()=>element.value?.parentElement
  const navElement=()=>stage()?.querySelector<HTMLElement>('.cczj-mobile-navigation')
  function dark(path?:string):boolean {
    const scenes=stage()?.querySelectorAll<HTMLElement>('[data-path]')
    const scene=path?Array.from(scenes||[]).find(node=>node.dataset.path===path):stage()?.querySelector('[data-route-active="true"]')
    return !!scene?.querySelector('[data-cczj-chrome="cinema"],[data-cczj-media-active=true]') || (!path && cinema()) || !!path?.startsWith('/player/')
  }
  function render():void {
    if(disposed)return
    const root=stage(), nav=navElement()
    const amount=motion?Number(dark(motion.from))*(1-motion.progress)+Number(dark(motion.to))*motion.progress:Number(dark())
    if(root)root.dataset.cczjChrome=amount>=.5?'cinema':'theme'
    if(nav){
      if(motion){
        const fill=[24,25,28,1].map((value,index)=>normal.fill[index]*(1-amount)+value*amount)
        nav.style.backgroundColor=`rgba(${fill.slice(0,3).map(Math.round).join(',')},${fill[3]})`
        nav.style.setProperty('--cczj-navigation-veil-opacity','0')
      }else{
        nav.style.removeProperty('background-color')
        nav.style.removeProperty('--cczj-navigation-veil-opacity')
      }
    }
    const splash=document.documentElement.dataset.cczjSplashColor
    if(splash){publishColor(splash);return}
    publishColor(interpolateColor(normal.status,'#000000',amount),interpolateColor(normal.bottom,nav?'#18191c':'#000000',amount))
  }
  function applyProgress(from:string,to:string,progress:number):void {
    motion={from,to,progress:Math.max(0,Math.min(1,progress))};render()
  }
  function move(from:string,to:string,progress:number):void {
    motionRevision++
    if(motionFrame!==undefined)cancelAnimationFrame(motionFrame)
    motionFrame=undefined
    applyProgress(from,to,progress)
  }
  function finish():void {
    motionRevision++
    if(motionFrame!==undefined)cancelAnimationFrame(motionFrame)
    motionFrame=undefined;motion=undefined;render();refresh()
  }
  function settle(from:string,to:string,scene:HTMLElement,entering=false,hold=false):void {
    const token=++motionRevision,started=performance.now()
    if(motionFrame!==undefined)cancelAnimationFrame(motionFrame)
    const tick=()=>{
      if(disposed||token!==motionRevision)return
      const width=Math.max(1,scene.offsetWidth)
      const transform=getComputedStyle(scene).transform
      const distance=transform==='none'?0:Math.abs(new DOMMatrixReadOnly(transform).m41)
      applyProgress(from,to,entering?1-distance/width:distance/width)
      // Click transitions have no settle Promise owner. Stop on completion;
      // the deadline also bounds CSS overrides and hidden-window animations.
      if(scene.getAnimations().some(animation=>animation.playState==='running') && performance.now()-started<2000)motionFrame=requestAnimationFrame(tick)
      else {motionFrame=undefined;if(!hold)motion=undefined;render();refresh()}
    }
    motionFrame=requestAnimationFrame(tick)
  }
  let cached:{src:string;image:ImagePixels}|undefined
  async function update():Promise<void>{
    const node=element.value
    if(disposed||!node)return
    const token=++generation
    request?.abort();request=new AbortController()
    const splash=document.documentElement.dataset.cczjSplashColor
    if(splash){publishColor(splash);return}
    const style=getComputedStyle(node)
    const fallback=style.getPropertyValue('--route-backdrop-color').trim()
    const root=stage()
    const previousChrome=root?.dataset.cczjChrome
    if(root)root.dataset.cczjChrome='theme'
    const nav=navElement()
    const previousBackground=nav?.style.backgroundColor
    const previousOpacity=nav?.style.getPropertyValue('--cczj-navigation-veil-opacity')
    nav?.style.removeProperty('background-color')
    nav?.style.removeProperty('--cczj-navigation-veil-opacity')
    const navigation=node.parentElement?.querySelector('.cczj-mobile-navigation,.reading-bottom-nav')
    const bottomStyle=navigation&&getComputedStyle(navigation,'::before')
    const bottomSurface=bottomStyle?.backgroundColor
    const bottomColor=bottomSurface&&/rgba?\(([^)]+)\)/.exec(bottomSurface)
    const bottomChannels=bottomColor?bottomColor[1].split(',').map(Number):[]
    if(bottomChannels.length>=3)bottomChannels[3]=(bottomChannels[3]??1)*Number(bottomStyle?.opacity??1)
    const backing=navigation&&/rgba?\(([^)]+)\)/.exec(getComputedStyle(navigation).backgroundColor)
    const backingChannels=backing?backing[1].split(',').map(Number):[]
    const bottomBase=backingChannels.length>=3&&(backingChannels[3]??1)===1?blendColor(backingChannels.slice(0,3),[]):fallback
    const bottom=systemBarSurface(bottomBase,bottomChannels)
    // Preserve the normal bar's translucency at progress zero. Replacing it
    // with an opaque sampled colour would flash when a wallpaper is present.
    const fill=backingChannels.length>=3&&(backingChannels[3]??1)===1
      ? [1,3,5].map(offset=>parseInt(bottom.slice(offset,offset+2),16)).concat(1)
      : bottomChannels.length>=3 ? [...bottomChannels.slice(0,3),bottomChannels[3]??1] : [0,0,0,0]
    if(root){if(previousChrome)root.dataset.cczjChrome=previousChrome;else delete root.dataset.cczjChrome}
    if(nav&&previousBackground)nav.style.backgroundColor=previousBackground
    if(nav&&previousOpacity)nav.style.setProperty('--cczj-navigation-veil-opacity',previousOpacity)
    const publish=(base:string)=>{
      const surface=node.parentElement?.querySelector('.cczj-mobile-route-viewport')
      const wash=surface&&/rgba?\(([^)]+)\)/.exec(getComputedStyle(surface,'::before').backgroundImage)
      normal={status:systemBarSurface(base,wash?wash[1].split(',').map(Number):[]),bottom,fill};render()
    }
    const match=/url\((?:"([^"]*)"|'([^']*)'|([^)]*))\)/.exec(style.backgroundImage)
    const src=match?.[1]||match?.[2]||match?.[3]
    if(!src){cached=undefined;publish(fallback);return}
    if(cached?.src!==src)publish(fallback)
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
    observer=new MutationObserver(records=>{if(records.some(record=>record.target!==stage()))refresh()})
    observer.observe(document.documentElement,{attributes:true,attributeFilter:['style','data-theme','data-cczj-splash-color']})
    if(element.value?.parentElement)observer.observe(element.value.parentElement,{subtree:true,attributes:true,attributeFilter:['data-cczj-media-active','data-cczj-chrome','data-route-active']})
    window.addEventListener('resize',refresh)
    refresh()
  })
  onBeforeUnmount(()=>{disposed=true;motionRevision++;if(motionFrame!==undefined)cancelAnimationFrame(motionFrame);generation++;if(scheduled!==undefined)cancelAnimationFrame(scheduled);request?.abort();observer?.disconnect();window.removeEventListener('resize',refresh);cached=undefined;delete stage()?.dataset.cczjChrome;bridge?.setSystemBarColor?.('')})
  return { move, settle, finish, refresh }
}
