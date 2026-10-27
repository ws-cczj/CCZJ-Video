/** Restore while the active page's content is settling. Stop on user intent,
 * navigation or the deadline; no observer survives into a dormant mode. */
export function restoreModeScroll(element:HTMLElement,top:number,options:{smooth?:boolean}={}):()=>void {
  let stopped=false,frame=0
  const target=Math.max(0,top)
  const smooth=!!options.smooth&&!matchMedia('(prefers-reduced-motion: reduce)').matches
  const deadline=performance.now()+(smooth?2000:1800)
  let warmup=2,started:number|undefined,origin=0,duration=0
  const observer=typeof ResizeObserver==='undefined'?undefined:new ResizeObserver(schedule)
  const timer=setTimeout(()=>{if(smooth&&!stopped&&element.isConnected)element.scrollTop=target;stop()},smooth?2000:1800)
  function stop(){if(stopped)return;stopped=true;clearTimeout(timer);cancelAnimationFrame(frame);observer?.disconnect();for(const name of ['pointerdown','touchstart','wheel'])element.removeEventListener(name,stop)}
  function apply(now:number){
    frame=0
    if(stopped||!element.isConnected)return stop()
    if(!smooth){element.scrollTop=target;if(Math.abs(element.scrollTop-target)<2)stop();return}
    if(warmup-->0){schedule();return}
    if(started===undefined){
      const entering=element.closest('.cczj-mobile-route-scene')?.getAnimations?.().some(animation=>animation.playState==='running')
      if(entering&&now<deadline-1500){schedule();return}
      started=now;origin=element.scrollTop;duration=Math.max(1,Math.min(1500,deadline-now))
    }
    const progress=Math.max(0,Math.min(1,(now-started)/duration))
    const eased=1-Math.pow(1-progress,3)
    element.scrollTop=origin+(target-origin)*eased
    if(progress>=1)stop();else schedule()
  }
  function schedule(){if(!stopped&&!frame)frame=requestAnimationFrame(apply)}
  for(const name of ['pointerdown','touchstart','wheel'])element.addEventListener(name,stop,{passive:true})
  observer?.observe(element)
  if(element.firstElementChild)observer?.observe(element.firstElementChild)
  schedule()
  return stop
}
