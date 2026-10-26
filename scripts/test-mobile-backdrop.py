"""Persistent mobile backdrop through the installed acceptance host and trusted touch."""
import argparse,importlib.util,json,time
from pathlib import Path
from android_webview import AndroidWebView
ROOT=Path(__file__).resolve().parent.parent
spec=importlib.util.spec_from_file_location('compiled',ROOT/'scripts/collect-android-diagnostics.py');compiled=importlib.util.module_from_spec(spec);spec.loader.exec_module(compiled)
parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--device',default='emulator-5554');parser.add_argument('--output',type=Path,default=ROOT/'dist/audit/shared-backdrop');args=parser.parse_args();args.output.mkdir(parents=True,exist_ok=True)
result=dict(passed=False,device=args.device,environment='emulator' if args.device.startswith('emulator-') else 'unclassified',checks=[])
with AndroidWebView(args.device,application_id='com.cczj.video.acceptance') as view:
 def rpc(name,*values):
  path,alias=compiled.compiled_binding(name)
  return view.evaluate('(async()=>{const m=await import('+json.dumps(path)+');return m['+json.dumps(alias)+'](...'+json.dumps(values)+')})()')
 def wait(expression):
  deadline=time.monotonic()+25
  while time.monotonic()<deadline:
   value=view.evaluate(expression)
   if value:return value
   time.sleep(.1)
  raise AssertionError(dict(expression=expression,text=view.evaluate('document.body.innerText.slice(-1200)')))
 def route(path):
  view.evaluate('document.querySelector("#app").__vue_app__.config.globalProperties.$router.push('+json.dumps(path)+')')
  wait('location.hash==='+json.dumps('#'+path)+'&&!document.querySelector("[class*=route-forward-enter-active],[class*=route-back-enter-active]")')
  wait('document.querySelector(".cczj-mobile-route-backdrop").getAnimations().every(animation=>animation.playState!=="running")')
 original_url=view.evaluate('location.href');original_mode=rpc('GetSetting','application_mode')
 try:
  rpc('SetSetting','application_mode','video')
  view.evaluate('location.href='+json.dumps('https://wails.localhost/?application-mode=video&background-audit='+str(time.monotonic_ns())+'#/settings'))
  wait('!!document.querySelector(".cczj-mobile-route-backdrop")&&!document.querySelector(".splash-wrap")')
  view.evaluate('window.__backgroundAudit=document.querySelector(".cczj-mobile-route-backdrop")')
  # Pick an existing bundled background through Vite's production manifest assets;
  # this is only a CSS presentation override and never a source/network fixture.
  assets=list((ROOT/'frontend/dist/assets').glob('china_ink-*.jpg'))
  assert assets
  image_path='/assets/'+assets[0].name
  view.evaluate('document.querySelector(".cczj-mobile-shell").style.setProperty("--bg-image",'+json.dumps('url("'+image_path+'")')+')')
  image=view.evaluate('getComputedStyle(window.__backgroundAudit).backgroundImage')
  result['image']=image
  origin=view.evaluate('window.__backgroundAudit.style.transform')
  for index,path in enumerate(['/','/short-drama','/search','/favorites','/settings']):
   route(path)
   atlas=view.evaluate('''(()=>{const e=window.__backgroundAudit,s=e.parentElement.getBoundingClientRect();return {w:e.getBoundingClientRect().width,stage:s.width,x:new DOMMatrixReadOnly(getComputedStyle(e).transform).m41}})()''')
   assert abs(atlas['w']-5*atlas['stage'])<1 and abs(atlas['x']+index*atlas['stage'])<1,atlas
  result['checks'].append('five primary tabs occupy five equal viewport cells of one panorama')
  for path in ['/settings?group=basic','/settings?group=theme','/settings','/history','/favorites','/settings']:
   route(path)
   assert view.evaluate('document.querySelectorAll(".cczj-mobile-route-backdrop").length===1&&window.__backgroundAudit===document.querySelector(".cczj-mobile-route-backdrop")')
   assert view.evaluate('[...document.querySelectorAll(".cczj-mobile-route-scene")].every(e=>getComputedStyle(e).backgroundImage==="none"&&getComputedStyle(e).backgroundColor==="rgba(0, 0, 0, 0)")')
  assert view.evaluate('window.__backgroundAudit.style.transform')==origin
  view.evaluate('for(const e of document.querySelectorAll(".cczj-mobile-route-stage,.cczj-mobile-route-viewport")){e.scrollLeft=100;e.scrollTop=100}')
  assert view.evaluate('[...document.querySelectorAll(".cczj-mobile-route-stage,.cczj-mobile-route-viewport")].every(e=>e.scrollLeft===0&&e.scrollTop===0)')
  result['checks'].append('oversized backdrop and entering scenes cannot scroll the route containers')
  result['checks'].append('one persistent backdrop across tab/subpage navigation and Back coordinates')
  route('/favorites');before=view.evaluate('window.__backgroundAudit.style.transform')
  geometry=view.evaluate('({width:innerWidth,height:innerHeight})');x=geometry['width']*.7;y=geometry['height']*.48
  view.call('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[dict(x=x,y=y)]))
  for step in range(1,5):view.call('Input.dispatchTouchEvent',dict(type='touchMove',touchPoints=[dict(x=x-geometry['width']*.18*step/4,y=y)]));time.sleep(.025)
  wait('!!document.querySelector(".cczj-mobile-tab-dragging")')
  moved=view.evaluate('window.__backgroundAudit.style.transform');assert moved!=before,(before,moved)
  assert view.evaluate('getComputedStyle(document.querySelector(".cczj-mobile-route-preview")).boxShadow')=='none'
  view.screenshot(args.output/'swipe-midpoint.png')
  view.call('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]))
  wait('!document.querySelector(".cczj-mobile-tab-dragging")');time.sleep(.4)
  assert view.evaluate('location.hash')=='#/favorites'
  assert view.evaluate('window.__backgroundAudit.style.transform')==before
  result['checks'].append('trusted partial swipe moves shared crop and cancellation restores it')
  frames=view.evaluate('''new Promise(resolve=>{const samples=[],start=performance.now();function tick(){const e=document.querySelector(".cczj-mobile-route-backdrop"),r=e.getBoundingClientRect(),stage=e.parentElement.getBoundingClientRect();samples.push({same:e===window.__backgroundAudit,count:document.querySelectorAll(".cczj-mobile-route-backdrop").length,x:new DOMMatrixReadOnly(getComputedStyle(e).transform).m41,covered:r.left<=stage.left+1&&r.right>=stage.right-1&&r.top<=stage.top+1&&r.bottom>=stage.bottom-1});if(performance.now()-start<700)requestAnimationFrame(tick);else resolve(samples)}requestAnimationFrame(tick);document.querySelector("#app").__vue_app__.config.globalProperties.$router.push("/settings")})''')
  assert all(row['same'] and row['count']==1 and row['covered'] for row in frames),frames
  assert len({round(row['x'],1) for row in frames})>2,frames
  result['transitionFrames']=frames
  result['checks'].append('sampled transition keeps the same layer covering every viewport edge')
  view.call('Emulation.setEmulatedMedia',dict(features=[dict(name='prefers-reduced-motion',value='reduce')]))
  route('/settings')
  assert view.evaluate('getComputedStyle(window.__backgroundAudit).transitionDuration')=='0s',view.evaluate('({duration:getComputedStyle(window.__backgroundAudit).transitionDuration,flag:document.documentElement.dataset.motionPages,css:document.querySelector(".cczj-mobile-route-stage").className})')
  assert view.evaluate('Math.abs(new DOMMatrixReadOnly(getComputedStyle(window.__backgroundAudit).transform).m41+innerWidth*4)<1')
  view.call('Emulation.setEmulatedMedia',dict(features=[]))
  time.sleep(.3) # The motion store applies the media-query change asynchronously.
  view.evaluate('document.documentElement.dataset.motionPages="off"');route('/history')
  assert view.evaluate('getComputedStyle(window.__backgroundAudit).transitionDuration')=='0s',view.evaluate('({duration:getComputedStyle(window.__backgroundAudit).transitionDuration,flag:document.documentElement.dataset.motionPages,css:document.querySelector(".cczj-mobile-route-stage").className})')
  view.evaluate('delete document.documentElement.dataset.motionPages')
  result['checks'].append('reduced motion and page animation off are respected')
  route('/settings');view.screenshot(args.output/'video-settings.png')
  for mode in ['novel','comic']:
   rpc('SetSetting','application_mode',mode)
   view.evaluate('location.href='+json.dumps('https://wails.localhost/?application-mode='+mode+'&background-audit='+str(time.monotonic_ns())+'#/'+mode+'/settings'))
   wait('!!document.querySelector(".cczj-mobile-route-backdrop")')
   assert view.evaluate('!getComputedStyle(document.querySelector(".cczj-mobile-route-backdrop")).backgroundImage.includes("url(")')
   assert view.evaluate('document.documentElement.scrollWidth<=innerWidth')
   view.screenshot(args.output/(mode+'-settings.png'))
  result['checks'].append('novel/comic retain mode canvas without video wallpaper leakage')
  result['passed']=True;print('PASS '+ '; '.join(result['checks']),flush=True)
 except Exception as error:result['error']=str(error);view.screenshot(args.output/'failure.png');raise
 finally:
  (args.output/'results.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
  view.call('Emulation.setEmulatedMedia',dict(features=[]))
  rpc('SetSetting','application_mode',original_mode);view.evaluate('location.href='+json.dumps(original_url))
