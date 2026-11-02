import { isMobileRuntime } from './runtime'

interface RevealBridge {
  themeRevealPrepare(id:string,x:number,y:number,radius:number,left:number,top:number,width:number,height:number):boolean
  themeRevealPlay(id:string,duration:number):void
  themeRevealCancel(id:string):void
}
let sequence=0
/** Native snapshot identity owns preparation, animation and bounded cleanup. */
export function prepareThemeReveal(x:number,y:number,radius:number,bounds?:DOMRect) {
  const host=(window as unknown as {wails?:RevealBridge}).wails
  if(!isMobileRuntime()||!host?.themeRevealPrepare||!host.themeRevealPlay||!host.themeRevealCancel)return undefined
  const id=`${Date.now()}-${++sequence}`
  let started=false,closed=false,timer:ReturnType<typeof setTimeout>
  let readyResolve!:(value:boolean)=>void,doneResolve!:()=>void
  const ready=new Promise<boolean>(resolve=>{readyResolve=resolve})
  const done=new Promise<void>(resolve=>{doneResolve=resolve})
  function close():void {
    if(closed)return
    closed=true;clearTimeout(timer);window.removeEventListener('cczj:theme-reveal',changed)
    readyResolve(false);doneResolve();host!.themeRevealCancel(id)
  }
  function changed(event:Event):void {
    const detail:unknown=(event as CustomEvent).detail
    if(!detail||typeof detail!=='object'||!('id' in detail)||detail.id!==id||!('state' in detail))return
    if(detail.state==='ready'){started=true;readyResolve(true)}
    else if(detail.state==='done')close()
  }
  window.addEventListener('cczj:theme-reveal',changed)
  timer=setTimeout(close,4000)
  try{
    const left=Math.max(0,bounds?.left??0),top=Math.max(0,bounds?.top??0)
    const width=Math.min(bounds?.width??innerWidth,innerWidth-left),height=Math.min(bounds?.height??innerHeight,innerHeight-top)
    if(!host.themeRevealPrepare(id,x/innerWidth,y/innerHeight,radius/innerWidth,left/innerWidth,top/innerHeight,width/innerWidth,height/innerHeight))close()
  }catch{close()}
  return {ready,done,cancel:close,play:(duration:number)=>{if(started&&!closed)host.themeRevealPlay(id,duration)}}
}
