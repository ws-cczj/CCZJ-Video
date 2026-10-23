"""Hold a native finger between pages, then exercise distance-based commit/cancel."""
import json,time
from pathlib import Path
from android_webview import AndroidWebView
OUT=Path(__file__).resolve().parent.parent/'dist/audit/interactive-tabs';OUT.mkdir(parents=True,exist_ok=True)
with AndroidWebView() as p:
 original=p.evaluate('location.hash')
 def wait(s):
  end=time.monotonic()+25
  while time.monotonic()<end:
   if p.evaluate(s):return
   time.sleep(.05)
  raise AssertionError(s)
 def touch(t,x=0,y=0):p.call('Input.dispatchTouchEvent',{'type':t,'touchPoints':[] if t in ('touchEnd','touchCancel') else [{'x':x,'y':y}]})
 def drag(distance,cancel=False):
  wait("document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  r=p.evaluate("(()=>{const r=document.querySelector('.cczj-mobile-navigation').getBoundingClientRect();return{w:r.width,y:r.y+r.height*.5}})()")
  x=r['w']*(.25 if distance>0 else .75);touch('touchStart',x,r['y'])
  for i in range(1,9):touch('touchMove',x+r['w']*distance*i/8,r['y']);time.sleep(.04)
  wait("!!document.querySelector('.cczj-mobile-route-preview')")
  state=p.evaluate("({hash:location.hash,x:document.querySelector('.cczj-mobile-route-current').getBoundingClientRect().x,previewX:document.querySelector('.cczj-mobile-route-preview').getBoundingClientRect().x,mains:document.querySelectorAll('.main-content').length,width:innerWidth})")
  assert abs(state['x']-r['w']*distance)<2 and abs(state['previewX'])<r['w'] and state['mains']==1,state
  p.evaluate("window.__heldPreview=document.querySelector('.cczj-mobile-route-preview');window.__heldPage=window.__heldPreview.querySelector('[data-cczj-scroll-host]')?.firstElementChild")
  nav=p.evaluate("document.querySelector('.cczj-mobile-navigation').getBoundingClientRect().x")
  assert abs(nav)<1,'The bottom navigation moved with the pages'
  p.screenshot(OUT/('held-'+str(distance)+('-cancel' if cancel else '')+'.png'))
  touch('touchCancel' if cancel else 'touchEnd')
  wait("!document.querySelector('.cczj-mobile-route-preview')")
  if not cancel and abs(distance)>=.3:
   assert p.evaluate("document.querySelector('.cczj-mobile-route-current')===window.__heldPreview && window.__heldPage?.isConnected"),'The preview was replaced/remounted on release'
  assert abs(p.evaluate("document.querySelector('.cczj-mobile-route-current').getBoundingClientRect().x"))<1
  return state
 try:
  p.evaluate("location.hash='/'");p.call('Page.reload');wait("!!document.querySelector('.cczj-mobile-navigation')");time.sleep(.7)
  frames=[drag(-.4)];assert frames[-1]['hash']=='#/' and p.evaluate('location.hash')=='#/search'
  frames.append(drag(.18));assert p.evaluate('location.hash')=='#/search','Short drag committed'
  frames.append(drag(.4,cancel=True));assert p.evaluate('location.hash')=='#/search','Cancelled drag committed'
  frames.append(drag(.4));assert p.evaluate('location.hash')=='#/','Long reverse drag did not commit'
  (OUT/'results.json').write_text(json.dumps(frames,indent=2),encoding='utf-8')
  print('PASS: two visible pages track the held finger; distance commits, short and cancelled drags return')
 finally:
  try:touch('touchEnd')
  except RuntimeError:pass
  p.evaluate('location.hash='+json.dumps(original))
