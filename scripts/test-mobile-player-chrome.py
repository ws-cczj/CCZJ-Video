"""Exercise native playback chrome transitions, including reduced motion."""
import json
import time
from pathlib import Path
from android_webview import AndroidWebView

OUT=Path(__file__).resolve().parent.parent/'dist/audit/mobile-player-chrome-20261018'
OUT.mkdir(parents=True,exist_ok=True)
with AndroidWebView() as p:
 original=p.evaluate('location.hash')
 overlays=p.evaluate("document.documentElement.getAttribute('data-motion-overlays')")
 motion=p.evaluate("document.documentElement.getAttribute('data-cczj-motion')")
 def wait(expr,timeout=20):
  end=time.monotonic()+timeout
  while not p.evaluate(expr):
   assert time.monotonic()<end,expr
   time.sleep(.1)
 def tap(selector):
  point=p.evaluate('(()=>{const r=document.querySelector('+json.dumps(selector)+').getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()')
  p.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[point]})
  p.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
 try:
  p.evaluate("location.hash='/player/yyzy_tv/1/0?vod=56126'")
  wait("document.querySelector('video')?.readyState>=2",110)
  p.evaluate("document.querySelector('video').play()")
  if p.evaluate("!!document.querySelector('.cczj-mobile-resume')"):tap('.cczj-mobile-resume > button:last-child')
  for fullscreen in [False,True]:
   if fullscreen:
    if not p.evaluate("document.querySelector('.player-wrapper').classList.contains('show-controls')"):p.evaluate("document.querySelector('video').click()")
    time.sleep(.4);tap('.cczj-mobile-fullscreen');wait("!!document.fullscreenElement")
   for show in [False,True]:
    if p.evaluate("document.querySelector('.player-wrapper').classList.contains('show-controls')")==show:
     p.evaluate("document.querySelector('video').click()")
     time.sleep(.4)
    state=p.evaluate('''(async()=>{
      document.querySelector('video').click();await new Promise(requestAnimationFrame);await new Promise(requestAnimationFrame);
      const e=document.querySelector('.progress-bar-wrapper'),m=new DOMMatrix(getComputedStyle(e).transform),top=new DOMMatrix(getComputedStyle(document.querySelector('.player-title-bar')).transform);return {classes:e.className,opacity:getComputedStyle(e).opacity,transform:getComputedStyle(e).transform,duration:getComputedStyle(e).transitionDuration,x:m.m41,y:m.m42,topX:top.m41,topY:top.m42};
    })()''')
    assert 'cczj-mobile-player-chrome-'+('enter' if show else 'leave')+'-active' in state['classes'],state
    assert state['duration']!='0s',state
    assert abs(state['x'])<.01 and abs(state['topX'])<.01 and state['y']>=0 and state['topY']<=0,state
    time.sleep(.5)
    assert p.evaluate("getComputedStyle(document.querySelector('.progress-bar-wrapper')).display!=='none'")==show
    if show:p.screenshot(OUT/('fullscreen.png' if fullscreen else 'portrait.png'))
  p.call('Emulation.setEmulatedMedia',{'features':[{'name':'prefers-reduced-motion','value':'reduce'}]})
  state=p.evaluate("(async()=>{document.querySelector('video').click();await new Promise(requestAnimationFrame);return getComputedStyle(document.querySelector('.progress-bar-wrapper')).transitionDuration})()")
  assert all(float(s.strip().removesuffix('s'))==0 for s in state.split(',')),state
  p.call('Emulation.setEmulatedMedia',{'features':[]})
  p.evaluate("document.documentElement.setAttribute('data-cczj-motion','off')")
  state=p.evaluate("getComputedStyle(document.querySelector('.progress-bar-wrapper')).transitionDuration")
  assert all(float(s.strip().removesuffix('s'))==0 for s in state.split(',')),state
  p.evaluate("document.documentElement.setAttribute('data-motion-overlays','off');document.documentElement.removeAttribute('data-cczj-motion')")
  assert p.evaluate("getComputedStyle(document.querySelector('.progress-bar-wrapper')).transitionDuration")== '0s'
  print('PASS: portrait/fullscreen chrome enter+leave, reduced motion and animation-off')
 finally:
  p.call('Emulation.setEmulatedMedia',{'features':[]})
  p.evaluate("document.querySelector('video')?.pause();if(document.fullscreenElement)document.exitFullscreen()")
  p.evaluate("document.documentElement."+('removeAttribute(\'data-cczj-motion\')' if motion is None else 'setAttribute(\'data-cczj-motion\','+json.dumps(motion)+')'))
  p.evaluate("document.documentElement."+('removeAttribute(\'data-motion-overlays\')' if overlays is None else 'setAttribute(\'data-motion-overlays\','+json.dumps(overlays)+')'))
  p.evaluate('location.hash='+json.dumps(original))
