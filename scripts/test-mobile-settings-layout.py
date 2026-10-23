"""Native WebView settings navigation, form bounds and theme gradient checks.
Changes only temporary reactive themes and viewport metrics, never preferences.
"""
import json
import subprocess
import time
from pathlib import Path
from android_webview import AndroidWebView

OUT=Path(__file__).resolve().parent.parent/'dist/audit/mobile-settings-20261018'
OUT.mkdir(parents=True,exist_ok=True)
STORE="document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('theme')"

with AndroidWebView() as p:
 original=p.evaluate('location.hash')
 theme=p.evaluate(STORE+'.currentId')
 results=[]
 def wait(expression):
  end=time.monotonic()+15
  while not p.evaluate(expression):
   assert time.monotonic()<end,expression
   time.sleep(.1)
 def visit(route):
  p.evaluate('location.hash='+json.dumps(route))
  group=route.split('group=')[1].split('&')[0] if 'group=' in route else 'index'
  wait("!!document.querySelector('.cczj-mobile-route-scene[data-screen=\"/settings:"+group+"\"]') && document.querySelectorAll('.cczj-mobile-route-scene').length===1 && !document.querySelector('.cczj-mobile-route-stage [class*=enter-active],.cczj-mobile-route-stage [class*=leave-active]')")
  time.sleep(.1)
  wait("!!document.querySelector('.settings-page') && document.querySelector('.settings-page').getBoundingClientRect().width>0")
 try:
  for width in [320,360,412,768]:
   p.call('Emulation.setDeviceMetricsOverride',{'width':width,'height':760,'deviceScaleFactor':1,'mobile':True})
   for chosen in dict.fromkeys([theme,'d-blue']):
    p.evaluate(STORE+'.currentId='+json.dumps(chosen))
    visit('/settings')
    assert p.evaluate("document.querySelectorAll('.cczj-mobile-settings-copy').length>=5")
    assert p.evaluate("[...document.querySelectorAll('.cczj-mobile-menu-row')].every(e=>e.getBoundingClientRect().height>=44)")
    if chosen==theme:p.screenshot(OUT/f'{width}-index.png')
    for group in ['basic','basic&section=home','basic&section=data','basic&section=maintenance','theme','advanced','motion','about','diagnostics','logs','extensions']:
     visit('/settings?group='+group)
     assert not p.evaluate("!!document.querySelector('.cczj-mobile-settings-back')"),'Duplicate back navigation'
     assert not p.evaluate("!!document.querySelector('.cczj-mobile-navigation')"),'Secondary settings reserve room for content'
     data=p.evaluate('''(()=>{
       const visible=e=>e.getBoundingClientRect().width>0&&e.getBoundingClientRect().height>0;
       const main=document.querySelector('.cczj-mobile-content');
       const clipped=[...document.querySelectorAll('.settings-group-active button,.settings-group-active input,.settings-group-active .select-dropdown')].filter(visible).filter(e=>{const r=e.getBoundingClientRect();return r.left<-1||r.right>innerWidth+1}).map(e=>e.className);
       const overlaps=[...document.querySelectorAll('.settings-group-active .item')].filter(visible).filter(e=>{const a=e.querySelector('.item-text'),b=e.querySelector('.item-control');return a&&b&&visible(b)&&a.getBoundingClientRect().right>b.getBoundingClientRect().left+1}).length;
       return {overflow:main.scrollWidth>main.clientWidth+1,clipped,overlaps};
     })()''')
     results.append({'width':width,'theme':chosen,'group':group,**data})
     assert not data['overflow'] and not data['clipped'] and not data['overlaps'],results[-1]
     if group.startswith('basic'):
      assert p.evaluate("[...document.querySelectorAll('.basic-section')].filter(e=>e.getBoundingClientRect().width>0).length===1"),'More than one basic section visible'
      assert not p.evaluate("document.querySelector('#basic-window')?.getBoundingClientRect().width"),'Desktop window controls on mobile'
     if chosen==theme and group in ['basic','basic&section=home','basic&section=data','advanced','theme']:p.screenshot(OUT/f'{width}-{group.replace("&section=","-")}.png')
    if chosen==theme:
     visit('/settings?group=theme')
     p.evaluate("document.querySelector('.theme-card .mini-btn').click()")
     time.sleep(.4)
     editor=p.evaluate("(()=>{const d=document.querySelector('[role=dialog]');return {overflow:d.scrollWidth>d.clientWidth,small:[...d.querySelectorAll('button,input:not([hidden]):not([type=checkbox])')].filter(e=>e.getBoundingClientRect().width).filter(e=>{const r=e.getBoundingClientRect();return r.width<44||r.height<44||r.left<0||r.right>innerWidth}).length,colors:d.querySelectorAll('input[type=color][aria-label]').length}})()")
     assert not editor['overflow'] and not editor['small'] and editor['colors']>=10,editor
     p.screenshot(OUT/f'{width}-theme-editor.png')
     subprocess.run(p.adb+['shell','input','keyevent','KEYCODE_BACK'],check=True,capture_output=True)
     wait("!document.querySelector('[role=dialog]')")
    # Header Back always returns to the settings index, independent of entry route.
    p.evaluate("document.querySelector('.cczj-mobile-header button').click()")
    wait("location.hash==='#/settings'")
    p.evaluate("location.hash='/'");time.sleep(.8)
    assert p.evaluate("getComputedStyle(document.querySelector('.cczj-mobile-route-scene[data-path=\"/\"]'),'::before').backgroundImage.includes('linear-gradient')")
    assert p.evaluate("getComputedStyle(document.querySelector('.cczj-mobile-shell')).backgroundImage.includes('url')" ) if chosen==theme else True
    if width==360:p.screenshot(OUT/f'{chosen}-home.png')
  (OUT/'results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2),encoding='utf-8')
  print(f'PASS: {len(results)} settings views, no clipped controls/overlap; navigation and home gradient')
 finally:
  p.evaluate(STORE+'.currentId='+json.dumps(theme))
  p.call('Emulation.clearDeviceMetricsOverride')
  p.evaluate('location.hash='+json.dumps(original))
