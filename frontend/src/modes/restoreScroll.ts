/** Restore while the active page's content is settling. Stop on user intent,
 * navigation or the deadline; no observer survives into a dormant mode. */
export function restoreModeScroll(element:HTMLElement,top:number):()=>void {
  let stopped=false,frame=0
  const target=Math.max(0,top)
  const observer=typeof ResizeObserver==='undefined'?undefined:new ResizeObserver(schedule)
  const timer=setTimeout(stop,1800)
  function stop(){if(stopped)return;stopped=true;clearTimeout(timer);cancelAnimationFrame(frame);observer?.disconnect();for(const name of ['pointerdown','touchstart','wheel'])element.removeEventListener(name,stop)}
  function apply(){frame=0;if(stopped||!element.isConnected)return stop();element.scrollTop=target;if(Math.abs(element.scrollTop-target)<2)stop()}
  function schedule(){if(!stopped&&!frame)frame=requestAnimationFrame(apply)}
  for(const name of ['pointerdown','touchstart','wheel'])element.addEventListener(name,stop,{passive:true})
  observer?.observe(element)
  if(element.firstElementChild)observer?.observe(element.firstElementChild)
  schedule()
  return stop
}
