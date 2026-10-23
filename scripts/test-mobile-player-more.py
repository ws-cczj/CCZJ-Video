"""Native portrait/fullscreen primary controls and the auxiliary More drawer."""
import json
import subprocess
import time
from pathlib import Path
from android_webview import AndroidWebView

OUT=Path(__file__).resolve().parent.parent/'dist/audit/player-more'
OUT.mkdir(parents=True,exist_ok=True)
with AndroidWebView() as p:
 original=p.evaluate('location.hash')
 settings=p.evaluate("localStorage.getItem('vp_settings')")
 native=p.evaluate('JSON.parse(wails.playerState())')
 def wait(expr,timeout=25):
  until=time.monotonic()+timeout
  while time.monotonic()<until:
   if p.evaluate(expr):return
   time.sleep(.1)
  raise AssertionError(expr)
 def tap(selector):
  point=p.evaluate('(()=>{const el=document.querySelector('+json.dumps(selector)+');el.scrollIntoView({block:"nearest"});const r=el.getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()')
  p.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[point]});p.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]});time.sleep(.2)
 def back():subprocess.run(p.adb+['shell','input','keyevent','KEYCODE_BACK'],check=True,capture_output=True);time.sleep(.3)
 def open_more():
  if p.evaluate("getComputedStyle(document.querySelector('.cczj-mobile-player-more-mask')).display!=='none'"):return
  tap('.cczj-mobile-player-more-button')
  wait("document.querySelector('.cczj-mobile-player-more-mask').getBoundingClientRect().height>0")
 try:
  p.evaluate("location.hash='/player/yyzy_tv/1/0?vod=56126'")
  wait("document.querySelector('video')?.readyState>=2",110)
  p.evaluate("document.querySelector('video').pause()")
  if p.evaluate("!!document.querySelector('.cczj-mobile-resume')"):tap('.cczj-mobile-resume > button:last-child')
  for fullscreen in [False,True]:
   if fullscreen:
    tap('.cczj-mobile-fullscreen');wait("!!document.fullscreenElement && innerWidth>innerHeight && document.querySelector('.player-wrapper').classList.contains('fullscreen')");time.sleep(.4)
   open_more()
   tap('.cczj-mobile-player-categories button:first-child')
   data=p.evaluate('''(()=>{const drawer=document.querySelector('.cczj-mobile-player-more');const r=drawer.getBoundingClientRect();return {left:r.left,right:r.right,top:r.top,bottom:r.bottom,width:innerWidth,height:innerHeight,labels:[...drawer.querySelectorAll('.ctrl-btn')].map(e=>e.getAttribute('aria-label')),closed:[...drawer.querySelectorAll('.volume-popup,.speed-popup,.playback-settings-popup,.subtitle-popup,.report-ad-popup')].every(e=>e.getBoundingClientRect().height===0)};})()''')
   assert data['left']>=0 and data['right']<=data['width']+1 and data['top']>=0 and data['bottom']<=data['height']+1,data
   assert data['closed'] and all(data['labels']),data
   p.screenshot(OUT/('fullscreen.png' if fullscreen else 'portrait.png'))
   if fullscreen:assert abs((data['right']-data['left'])/data['width']-1/3)<.02,data
   tap('.cczj-mobile-player-categories button:nth-child(2)')
   assert p.evaluate("getComputedStyle(document.querySelector('.cczj-mobile-more-actions .playback-settings-popup')).display!=='none'")
   assert p.evaluate("document.querySelector('.cczj-mobile-more-actions .ps-toggle').getBoundingClientRect().height>=44")
   back();wait("getComputedStyle(document.querySelector('.cczj-mobile-player-more-mask')).display==='none'")
   if fullscreen:assert p.evaluate("!!document.fullscreenElement")
   open_more()
   assert p.evaluate("!document.querySelector('.cczj-mobile-more-actions .speed-group,.cczj-mobile-more-actions .volume-group,.cczj-mobile-more-actions [data-player-action=pip]')"),'Primary controls are duplicated in More'
   assert p.evaluate("document.querySelectorAll('.cczj-mobile-quality-options button').length>0 && !document.querySelector('.cczj-mobile-more-quality .select-dropdown')"),'Quality still uses the desktop select'
   back()
   open_more();tap('.cczj-mobile-player-categories button:nth-child(3)');tap('.cczj-mobile-more-actions .video-info-group > button')
   wait("!!document.querySelector('.vp-modal-info')")
   wait("getComputedStyle(document.querySelector('.cczj-mobile-player-more-mask')).display==='none'")
   back()
   if fullscreen:
    visible=p.evaluate("[...document.querySelector('.cczj-mobile-fullscreen-controls').children].filter(e=>e.getBoundingClientRect().width>0).map(e=>({c:e.className,y:e.getBoundingClientRect().top,b:e.getBoundingClientRect().bottom}))")
    assert max(e['b'] for e in visible)-min(e['y'] for e in visible)<65,visible
    p.evaluate("document.querySelector('video').play()")
    tap('.cczj-mobile-fullscreen-controls .cczj-mobile-control-popover > button')
    wait("document.querySelectorAll('.cczj-mobile-speed-options button').length===6")
    time.sleep(3.5)
    assert p.evaluate("['.player-title-bar','.progress-bar-wrapper','.cczj-mobile-fullscreen-controls'].every(s=>getComputedStyle(document.querySelector(s)).display!=='none')"),'Control chrome hid while speed panel was open'
    p.screenshot(OUT/'fullscreen-speed.png')
    back()
    tap('.cczj-mobile-fullscreen-controls .cczj-mobile-control-popover:nth-of-type(2) > button')
    wait("!!document.querySelector('.cczj-mobile-volume-options input')")
    assert p.evaluate("(()=>{const r=document.querySelector('.cczj-mobile-volume-options input').getBoundingClientRect();return r.height>r.width*2})()"),'Mobile volume control is not vertical'
    time.sleep(3.5)
    assert p.evaluate("getComputedStyle(document.querySelector('.progress-bar-wrapper')).display!=='none'"),'Progress hid while volume was open'
    p.screenshot(OUT/'fullscreen-volume.png')
    p.evaluate("document.querySelector('video').pause()")
    slider=p.evaluate("(()=>{const r=document.querySelector('.cczj-mobile-volume-vertical').getBoundingClientRect();return {x:r.x+r.width/2,top:r.top,bottom:r.bottom,h:r.height}})()")
    p.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[{'x':slider['x'],'y':slider['bottom']-4}]})
    for step in range(1,9):p.call('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[{'x':slider['x'],'y':slider['bottom']-4-slider['h']*.7*step/8}]});time.sleep(.025)
    p.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
    assert p.evaluate("JSON.parse(wails.playerState()).volume")>.4,'Vertical volume drag did not change device volume'
    back()
    p.evaluate("document.querySelector('video').pause()")
    color=p.evaluate("getComputedStyle(document.querySelector('.cczj-mobile-player-lock')).color")
    tap('.cczj-mobile-player-lock')
    assert p.evaluate("document.querySelector('.cczj-mobile-player-lock').getAttribute('aria-pressed')==='true' && getComputedStyle(document.querySelector('.cczj-mobile-player-lock')).backgroundColor==='rgba(0, 0, 0, 0)'")
    assert p.evaluate("getComputedStyle(document.querySelector('.cczj-mobile-player-lock')).color")!=color,'Locked state color did not change'
    back()
    back();wait("!document.fullscreenElement")
  print('PASS: portrait/fullscreen More, touch quality, primary row, speed/volume stay visible, lock color and native Back')
 finally:
  p.evaluate("document.querySelector('video')?.pause();if(document.fullscreenElement)document.exitFullscreen()")
  p.evaluate("wails.playerVolume("+str(native['volume'])+");"+ ("localStorage.removeItem('vp_settings')" if settings is None else "localStorage.setItem('vp_settings',"+json.dumps(settings)+")"))
  p.evaluate('location.hash='+json.dumps(original))
