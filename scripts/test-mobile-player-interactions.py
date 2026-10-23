"""Real Android player gestures, fullscreen actions, native device control and PiP.
Does not delete records; restores device audio and app player preferences afterward.
"""
import json, subprocess, time
from pathlib import Path
from android_webview import AndroidWebView
OUT=Path(__file__).resolve().parent.parent/'dist/audit/mobile-player-interactions'
OUT.mkdir(parents=True,exist_ok=True)
with AndroidWebView() as page:
 original=page.evaluate('location.hash')
 settings=page.evaluate("localStorage.getItem('vp_settings')")
 native=page.evaluate('JSON.parse(wails.playerState())')
 def wait(expr,timeout=25):
  until=time.monotonic()+timeout
  while time.monotonic()<until:
   if page.evaluate(expr):return
   time.sleep(.1)
  raise AssertionError(expr)
 def touch(kind,x=0,y=0):page.call('Input.dispatchTouchEvent',{'type':kind,'touchPoints':[] if kind in ('touchEnd','touchCancel') else [{'x':x,'y':y}]})
 def tap(selector):
  r=page.evaluate('(()=>{const e=document.querySelector('+json.dumps(selector)+');e.scrollIntoView({block:"nearest"});const r=e.getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()')
  touch('touchStart',**r);touch('touchEnd');time.sleep(.15)
 def rect():return page.evaluate("(()=>{const r=document.querySelector('.player-wrapper').getBoundingClientRect();return{x:r.x,y:r.y,w:r.width,h:r.height}})()")
 def swipe(x,y,dx,dy,cancel=False):
  r=rect();touch('touchStart',r['x']+r['w']*x,r['y']+r['h']*y)
  for i in range(1,9):
   touch('touchMove',r['x']+r['w']*(x+dx*i/8),r['y']+r['h']*(y+dy*i/8));time.sleep(.035)
  hud=page.evaluate("document.querySelector('.cczj-mobile-gesture-hud')?.textContent")
  touch('touchCancel' if cancel else 'touchEnd');time.sleep(.3)
  return hud
 def back():subprocess.run(page.adb+['shell','input','keyevent','KEYCODE_BACK'],check=True);time.sleep(.5)
 try:
  page.call('Page.reload');time.sleep(3)
  page.evaluate("location.hash='#/history'");wait("!!document.querySelector('.cczj-mobile-history-card')")
  page.evaluate("document.querySelector('.cczj-mobile-history-card').click()");wait("!!document.querySelector('.episodes-grid button')")
  page.evaluate("document.querySelector('.episodes-grid button').click()");wait("!!document.querySelector('.native-video')")
  wait("document.querySelector('video').readyState>=2 && document.querySelector('video').videoWidth>0",110)
  page.evaluate("document.querySelector('video').pause()")
  if page.evaluate("!!document.querySelector('.cczj-mobile-resume')"):tap('.cczj-mobile-resume > button:last-child')
  assert page.evaluate("document.querySelectorAll('.cczj-mobile-player-controls button').length===3")
  assert page.evaluate("getComputedStyle(document.querySelector('.progress-bar-wrapper')).display!=='none'")
  page.screenshot(OUT/'compact.png')
  before=page.evaluate("document.querySelector('video').currentTime")
  hud=swipe(.25,.35,.4,0)
  after=page.evaluate("document.querySelector('video').currentTime")
  assert after>before+50 and hud,('seek failed',before,after,hud)
  before=after;swipe(.6,.4,-.2,0,cancel=True)
  assert abs(page.evaluate("document.querySelector('video').currentTime")-before)<.2,'Cancelled gesture committed seek'
  if page.evaluate("!!document.querySelector('.cczj-mobile-resume')"):tap('.cczj-mobile-resume > button:last-child')
  # Start above the restored portrait progress slider; its own drag seeks.
  swipe(.2,.55,0,-.25)
  brightness=page.evaluate('JSON.parse(wails.playerState()).brightness')
  assert brightness>native['brightness']+.2,('brightness failed',brightness)
  page.evaluate("document.querySelector('video').muted=true;document.querySelector('video').volume=.1")
  swipe(.8,.55,0,-.25)
  assert page.evaluate("!document.querySelector('video').muted && document.querySelector('video').volume===1"),'Native gesture leaves the media element muted/attenuated'
  volume=page.evaluate('JSON.parse(wails.playerState()).volume')
  assert volume>native['volume']+.2,('volume failed',volume)
  page.screenshot(OUT/'gesture-volume.png')
  tap('.cczj-mobile-fullscreen');wait('!!document.fullscreenElement && innerWidth>innerHeight')
  time.sleep(.5)
  assert page.evaluate("document.fullscreenElement.querySelector('.cczj-mobile-fullscreen-controls').getBoundingClientRect().height<65")
  bounds=page.evaluate("Array.from(document.querySelectorAll('.cczj-mobile-fullscreen-controls > button,.cczj-mobile-control-popover > button,.player-title-bar button,.cczj-mobile-player-lock')).filter(e=>e.getBoundingClientRect().width&&getComputedStyle(e).display!=='none').map(e=>{const r=e.getBoundingClientRect();return {name:e.ariaLabel||e.title,w:r.width,h:r.height,l:r.left,r:r.right,t:r.top,b:r.bottom}})")
  assert all(x['l']>=-1 and x['r']<=page.evaluate('innerWidth')+1 and x['b']<=page.evaluate('innerHeight')+1 for x in bounds),bounds
  page.screenshot(OUT/'fullscreen.png')
  tap('.cczj-mobile-player-lock')
  assert page.evaluate("document.querySelector('.cczj-mobile-player-lock').getAttribute('aria-pressed')==='true'")
  before=page.evaluate("document.querySelector('video').currentTime")
  swipe(.3,.4,.3,0)
  assert abs(page.evaluate("document.querySelector('video').currentTime")-before)<.2,'Locked player still seeks'
  back();assert page.evaluate('!!document.fullscreenElement'),'Back should unlock before exiting'
  tap('.cczj-mobile-player-more-button');wait("document.querySelector('.cczj-mobile-player-more').getBoundingClientRect().height>0")
  tap('.cczj-mobile-player-categories button:nth-child(2)');wait("document.querySelector('.playback-settings-popup').classList.contains('show')")
  page.screenshot(OUT/'fullscreen-settings.png');back();assert page.evaluate('!!document.fullscreenElement')
  tap('.cczj-mobile-player-more-button');wait("document.querySelector('.cczj-mobile-player-more').getBoundingClientRect().height>0")
  tap('.cczj-mobile-player-categories button:nth-child(3)');tap('.video-info-group > button');wait("!!document.querySelector('.vp-modal-info')");page.screenshot(OUT/'video-info.png');back()
  assert page.evaluate('!!document.fullscreenElement'),'Info Back exited fullscreen'
  tap('.cczj-mobile-player-cast');wait("!!document.querySelector('.cczj-mobile-cast')")
  wait("!document.querySelector('.cczj-mobile-cast-refresh').disabled",12)
  assert not page.evaluate("!!document.querySelector('.cczj-mobile-cast-error')"),'DLNA scan failed'
  page.screenshot(OUT/'cast.png');back();assert page.evaluate('!!document.fullscreenElement')
  back();wait('!document.fullscreenElement && innerWidth<innerHeight')
  page.evaluate("document.querySelector('video').play()")
  wait("!document.querySelector('video').paused")
  # Native PiP must actually create a system pinned task; a DOM flag is insufficient.
  tap('.cczj-mobile-player-controls button:nth-of-type(2)')
  until=time.monotonic()+8;pinned=False
  while time.monotonic()<until:
   activity=subprocess.check_output(page.adb+['shell','dumpsys','activity','activities'],text=True,encoding='utf-8',errors='replace')
   if 'mode=pinned' in activity or 'windowingMode=2' in activity:pinned=True;break
   time.sleep(.2)
  assert pinned,'No Android PiP task created'
  # Allow the native resize/configuration transition to finish. It must preserve
  # the same WebView and advancing media, rather than recreate the Activity.
  pip_time=page.evaluate("document.querySelector('video').currentTime")
  origin=page.evaluate('performance.timeOrigin')
  time.sleep(2)
  assert page.evaluate('performance.timeOrigin')==origin, 'PiP recreated the WebView'
  assert page.evaluate("document.querySelector('video').currentTime")>pip_time+.5, 'PiP stopped playback'
  page.screenshot(OUT/'pip.png')
  subprocess.run(page.adb+['shell','am','start','-W','--activity-single-top','--activity-clear-top','-n','com.cczj.video.preview/com.wails.app.MainActivity'],check=True,capture_output=True)
  wait("!document.body.hasAttribute('data-player-pip')")
  page.evaluate("location.hash='#/search'");wait("!document.querySelector('.player-wrapper')")
  restored=page.evaluate('JSON.parse(wails.playerState()).brightness')
  assert abs(restored-native['brightness'])<.02,('Brightness not restored',native,restored)
  (OUT/'results.json').write_text(json.dumps({'seek':True,'cancelSeek':True,'brightness':brightness,'volume':volume,'lock':True,'fullscreenBounds':bounds,'settings':True,'videoInfo':True,'dlnaDiscovery':True,'nativePip':pinned,'brightnessRestored':True},ensure_ascii=False,indent=2),encoding='utf-8')
  print('PASS: actual Android seek/cancel, brightness/media volume, lock/Back, fullscreen settings/info, DLNA scan, native PiP and brightness cleanup',flush=True)
 finally:
  page.evaluate("document.querySelector('video')?.pause();if(document.fullscreenElement)document.exitFullscreen();location.hash='#/search';wails.playerRestoreBrightness();wails.playerVolume("+str(native['volume'])+")")
  time.sleep(.6)
  if settings is None:page.evaluate("localStorage.removeItem('vp_settings')")
  else:page.evaluate('localStorage.setItem("vp_settings",'+json.dumps(settings)+')')
  page.evaluate('location.hash='+json.dumps(original))
