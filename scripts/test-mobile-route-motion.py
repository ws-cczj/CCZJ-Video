"""Native navigation: direct Settings entry and continuous horizontal motion."""
import argparse,json,time
from pathlib import Path
from android_webview import AndroidWebView
parser=argparse.ArgumentParser();parser.add_argument('--baseline',action='store_true');args=parser.parse_args()
OUT=Path(__file__).resolve().parent.parent/'dist/audit/mobile-route-motion';OUT.mkdir(exist_ok=True)
PINIA="document.querySelector('#app').__vue_app__.config.globalProperties.$pinia"
with AndroidWebView() as p:
 def wait(expr):
  for _ in range(300):
   try:
    if p.evaluate(expr):return
   except RuntimeError:pass
   time.sleep(.1)
  raise AssertionError(expr)
 mode=p.evaluate(PINIA+"._s.get('applicationMode').mode")
 if mode!='video':
  p.evaluate('location.hash='+json.dumps('/'+mode+'/settings'));wait("!!document.querySelector('.mode-switch-options')")
  p.evaluate("document.querySelector('.mode-switch-options button:first-child').click()")
  wait("!!document.querySelector('.cczj-mobile-navigation') && !document.querySelector('.splash-wrap')")
 p.evaluate("location.hash='/'");wait("!!document.querySelector('.cczj-mobile-navigation')")
 time.sleep(.6)
 last=p.evaluate("(()=>{const e=[...document.querySelectorAll('.cczj-mobile-navigation a')].at(-1);return{path:e.hash,label:e.textContent.trim()}})()")
 frames=p.evaluate(r'''new Promise(resolve=>{const data=[],start=performance.now();function sample(){const main=document.querySelector('.cczj-mobile-content');const children=[...main.children].filter(e=>e.getBoundingClientRect().width>0);data.push({t:Math.round(performance.now()-start),count:children.length,roots:children.map(e=>({class:e.className,transform:getComputedStyle(e).transform,x:new DOMMatrixReadOnly(getComputedStyle(e).transform).m41,opacity:getComputedStyle(e).opacity}))});if(performance.now()-start<650)requestAnimationFrame(sample);else resolve(data)}requestAnimationFrame(sample);[...document.querySelectorAll('.cczj-mobile-navigation a')].at(-1).click()})''')
 settings_direct=p.evaluate("location.hash==='#/settings' && !!document.querySelector('.cczj-mobile-settings-index')")
 p.evaluate("location.hash='/settings'");wait("!!document.querySelector('.cczj-mobile-settings-index')")
 detail=p.evaluate(r'''new Promise(resolve=>{const data=[],start=performance.now();function sample(){const nodes=[...document.querySelectorAll('.cczj-mobile-route-scene')];data.push({t:Math.round(performance.now()-start),nodes:nodes.map(e=>({class:e.className,transform:getComputedStyle(e).transform,x:new DOMMatrixReadOnly(getComputedStyle(e).transform).m41,opacity:getComputedStyle(e).opacity}))});if(performance.now()-start<650)requestAnimationFrame(sample);else resolve(data)}requestAnimationFrame(sample);document.querySelector('.cczj-mobile-settings-index .cczj-mobile-menu-row').click()})''')
 result={'bottom':last,'directSettings':settings_direct,'motion':p.evaluate("({global:document.documentElement.dataset.cczjMotion,normal:getComputedStyle(document.documentElement).getPropertyValue('--cczj-motion-normal')})"),'tabFrames':frames,'groupFrames':detail}
 (OUT/('baseline.json' if args.baseline else 'result.json')).write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
 horizontal=any(any(abs(n['x'])>1 for n in f['nodes']) for f in detail)
 print(json.dumps({'directSettings':settings_direct,'settingsGroupHorizontalMotion':horizontal,'bottom':last,'motion':result['motion']},ensure_ascii=False))
 if not args.baseline:
  assert settings_direct,'Bottom navigation still requires the extra Manage-to-Settings step'
  assert horizontal,'Settings group changes instantly instead of moving horizontally'
  assert all(f['nodes'] for f in detail),'A blank frame separates the outgoing and incoming screens'
  assert all(float(n['opacity'])>.99 for f in detail for n in f['nodes']),'Navigation still fades to transparent'
  p.evaluate("document.querySelector('[data-route-active=true] .cczj-mobile-header > button').click()")
  wait("location.hash==='#/settings' && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  p.evaluate("document.querySelector('.main-content').scrollTop=170")
  p.evaluate("document.querySelector('.cczj-mobile-tools a[href=\"#/sources\"]').click()")
  wait("location.hash==='#/sources' && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  p.evaluate("document.querySelector('.cczj-mobile-header > button').click()")
  wait("location.hash==='#/settings' && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  assert p.evaluate("Math.abs(document.querySelector('.main-content').scrollTop-170)<2"),'Settings scroll position was lost on return'
  # Same-group query changes update the current view without remounting it.
  p.evaluate("location.hash='/settings?group=basic&section=data'")
  wait("!!document.querySelector('.settings-group-active') && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  p.evaluate("location.hash='/settings?group=basic&section=appearance'")
  wait("!!document.querySelector('.mode-switch-options')")
  p.evaluate("document.querySelector('.cczj-mobile-header > button').click()")
  wait("location.hash==='#/settings' && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  p.evaluate("document.querySelector('#app').__vue_app__.config.globalProperties.$router.replace('/')")
  wait("location.hash==='#/' && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  p.evaluate("document.querySelector('.cczj-mobile-navigation a:last-child').click()")
  wait("location.hash==='#/settings' && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  p.evaluate("document.querySelector('.cczj-mobile-settings-index [data-group=basic]').click()")
  wait("location.hash.includes('group=basic') && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  p.evaluate("document.querySelector('.cczj-mobile-header > button').click()")
  wait("location.hash==='#/settings' && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  import subprocess
  subprocess.run(p.adb+['shell','input','keyevent','KEYCODE_BACK'],check=True,capture_output=True)
  wait("location.hash==='#/'")
  for target in ['/settings','/settings?group=theme','/history','/search','/settings?group=basic']:
   p.evaluate('location.hash='+json.dumps(target));time.sleep(.07)
  wait("location.hash.includes('group=basic') && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  assert p.evaluate("document.querySelectorAll('.main-content').length===1 && !document.querySelector('.cczj-mobile-route-scene[inert]')"),'Rapid navigation left an inactive screen attached'
  p.screenshot(OUT/'settings-basic.png')
  p.evaluate("document.querySelector('.cczj-mobile-header > button').click()")
  wait("location.hash==='#/settings' && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  p.screenshot(OUT/'settings-index.png')
  flag=p.evaluate("document.documentElement.getAttribute('data-motion-pages')")
  try:
   p.evaluate("document.documentElement.setAttribute('data-motion-pages','off')")
   stopped=p.evaluate(r'''new Promise(resolve=>{const frames=[],start=performance.now();function sample(){frames.push([...document.querySelectorAll('.cczj-mobile-route-scene')].map(e=>({x:new DOMMatrixReadOnly(getComputedStyle(e).transform).m41,duration:getComputedStyle(e).transitionDuration})));if(performance.now()-start<220)requestAnimationFrame(sample);else resolve(frames)}requestAnimationFrame(sample);document.querySelector('.cczj-mobile-settings-index [data-group=basic]').click()})''')
   assert all(abs(n['x'])<1 and all(float(d.strip().rstrip('s'))==0 for d in n['duration'].split(',')) for frame in stopped for n in frame),'Page motion-off did not disable the new transition'
  finally:
   p.evaluate('document.documentElement.'+('removeAttribute(\'data-motion-pages\')' if flag is None else 'setAttribute(\'data-motion-pages\','+json.dumps(flag)+')'))
  try:
   p.call('Emulation.setEmulatedMedia',{'features':[{'name':'prefers-reduced-motion','value':'reduce'}]})
   p.evaluate("document.querySelector('#app').__vue_app__.config.globalProperties.$router.push('/settings?group=theme')")
   wait("!!document.querySelector('.cczj-mobile-route-scene[data-screen=\"/settings:theme\"]') && document.querySelectorAll('.cczj-mobile-route-scene').length===1 && !document.querySelector('.cczj-mobile-route-stage [class*=enter-active]')")
   assert p.evaluate("matchMedia('(prefers-reduced-motion: reduce)').matches && getComputedStyle(document.querySelector('.cczj-mobile-route-scene')).transitionDuration==='0s'")
  finally:p.call('Emulation.setEmulatedMedia',{'features':[]})
  p.evaluate("document.querySelector('.cczj-mobile-header > button').click()")
  wait("location.hash==='#/settings' && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  print('PASS: direct Settings, simultaneous opaque horizontal motion, return/scroll restoration, inline tabs, native Back without loops and rapid navigation cleanup')
  print('PASS: page-animation off and system reduced motion; user preferences restored')


