"""Verify portrait player navigation/scrubbing/comments and mobile loading UI in MuMu."""
import argparse,json,time,subprocess
from pathlib import Path
from android_webview import AndroidWebView
parser=argparse.ArgumentParser();parser.add_argument('--width',type=int,default=0);args=parser.parse_args()
output=Path(__file__).resolve().parent.parent/'dist/audit'/('mobile-player-feedback-'+str(args.width or 360));output.mkdir(parents=True,exist_ok=True)
with AndroidWebView() as p:
 original=p.evaluate('location.hash');results={}
 def wait(expr,seconds=25):
  end=time.monotonic()+seconds
  while time.monotonic()<end:
   if p.evaluate(expr):return
   time.sleep(.1)
  raise AssertionError(expr)
 def tap(selector):
  pt=p.evaluate('(()=>{const r=document.querySelector('+json.dumps(selector)+').getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()')
  p.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[pt]});p.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]});time.sleep(.15)
 try:
  p.call('Page.reload');time.sleep(2)
  if args.width:p.call('Emulation.setDeviceMetricsOverride',{'width':args.width,'height':700,'deviceScaleFactor':3,'mobile':True})
  p.evaluate("location.hash='#/history'");wait("!!document.querySelector('.cczj-mobile-history-card')")
  p.evaluate("document.querySelector('.cczj-mobile-history-card').click()");wait("!!document.querySelector('.episodes-grid button')")
  detail=p.evaluate('location.hash');p.evaluate("document.querySelector('.episodes-grid button').click()")
  wait("!!document.querySelector('.player-wrapper')")
  wait("document.querySelector('video').readyState>=2 && document.querySelector('video').videoWidth>0",110)
  p.evaluate("document.querySelector('video').pause()")
  if p.evaluate("!!document.querySelector('.cczj-mobile-resume')"):tap('.cczj-mobile-resume > button:last-child')
  assert p.evaluate("getComputedStyle(document.querySelector('.progress-bar-wrapper')).display!=='none'")
  assert p.evaluate("getComputedStyle(document.querySelector('.player-title-bar')).display!=='none'")
  assert p.evaluate("!document.querySelector('.player-col-side .douban-comments')"),'Comments fetched before opening tab'
  assert p.evaluate("!document.querySelector('.pause-overlay,.loading img')"),'Legacy graphic still mounted on mobile'
  assert p.evaluate("fetch(document.querySelector('video').poster).then(r=>r.text()).then(svg=>svg.includes('fill=\"#000\"'))"),'No explicit black poster'
  current=p.evaluate("document.querySelector('video').currentTime")
  r=p.evaluate("(()=>{const r=document.querySelector('.progress-slider').getBoundingClientRect();return{x:r.left,y:r.top+r.height/2,w:r.width}})()")
  # Touch the actual range control and drag, rather than setting its value from JS.
  p.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[{'x':r['x']+2,'y':r['y']}]})
  for i in range(1,7):
   p.call('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[{'x':r['x']+r['w']*.12*i/6,'y':r['y']}]});time.sleep(.025)
  p.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
  wait('document.querySelector("video").currentTime>'+str(current+10))
  results['portraitRangeTouch']=True
  p.evaluate('document.querySelector("video").currentTime='+str(current));time.sleep(.2)
  p.screenshot(output/'overview.png')
  p.evaluate("(async()=>{const v=document.querySelector('video');await v.play();v.dispatchEvent(new Event('waiting'));await new Promise(requestAnimationFrame)})()")
  wait("!!document.querySelector('.cczj-mobile-video-loading')")
  assert p.evaluate("!document.querySelector('img.loading-spinner')")
  p.screenshot(output/'buffering.png')
  p.evaluate("document.querySelector('video').dispatchEvent(new Event('canplay'));document.querySelector('video').pause()")
  tap('#player-comments-tab');wait("getComputedStyle(document.querySelector('#player-comments-panel')).display!=='none'")
  has_id=p.evaluate("!!document.querySelector('#player-comments-panel .douban-comments')")
  if has_id:
   wait("!document.querySelector('#player-comments-panel .comments-loading')",65)
   state=p.evaluate("(()=>{const panel=document.querySelector('#player-comments-panel');return {items:panel.querySelectorAll('.comment-item').length,error:panel.querySelector('.comments-error')?.textContent.trim(),empty:!!panel.querySelector('.comments-empty')}})()")
   results['comments']=state
   assert not state.get('error') or 'Binding call failed' not in state['error'], 'Internal Wails error exposed to user'
   assert state['items'] or state.get('error') or state['empty'],'Comment request has no visible outcome'
   assert p.evaluate("getComputedStyle(document.querySelector('#player-comments-panel .douban-comments')).paddingTop==='0px'")
  else:assert p.evaluate("!!document.querySelector('.cczj-mobile-comments-unavailable')")
  p.screenshot(output/'comments.png')
  assert p.evaluate("document.querySelector('.cczj-mobile-content').scrollWidth<=innerWidth"),'Portrait page overflows horizontally'
  p.evaluate("document.querySelector('.cczj-mobile-content').scrollTop=120")
  assert p.evaluate("Math.abs(document.querySelector('.player-wrapper').getBoundingClientRect().top)<2"),'Player does not stay visible while reading'
  p.evaluate("document.querySelector('.cczj-mobile-content').scrollTop=0")
  tap('#player-overview-tab');wait("getComputedStyle(document.querySelector('.side-header')).display!=='none'")
  tap('.cczj-mobile-player-back');wait('location.hash==='+json.dumps(detail))
  results.update({'back':True,'commentTab':True,'blackPoster':True,'legacyGraphicsRemoved':True,'portraitTime':True})
  (output/'results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2),encoding='utf-8')
  print('PASS: portrait Back, touch range, comments tab, black poster, compact loading and scroll positioning')
 finally:
  p.evaluate("document.querySelector('video')?.pause();if(document.fullscreenElement)document.exitFullscreen()")
  if args.width:p.call('Emulation.clearDeviceMetricsOverride')
  p.evaluate('location.hash='+json.dumps(original))
