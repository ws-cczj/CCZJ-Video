"""HMR-only temporary resume state plus real controls and actual cached comments."""
import json,time
from pathlib import Path
from android_webview import AndroidWebView
OUT=Path(__file__).resolve().parent.parent/'dist/audit/player-polish';OUT.mkdir(parents=True,exist_ok=True)
with AndroidWebView() as p:
 original=p.evaluate('location.hash')
 def wait(expr,seconds=30):
  end=time.monotonic()+seconds
  while time.monotonic()<end:
   if p.evaluate(expr):return
   time.sleep(.05)
  raise AssertionError(expr)
 def tap(selector):
  point=p.evaluate('(()=>{const e=document.querySelector('+json.dumps(selector)+');e.scrollIntoView({block:"nearest"});const r=e.getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()')
  p.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[point]});p.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]});time.sleep(.25)
 try:
  p.evaluate("location.hash='/player/yyzy_tv/0/0?vod=3267'")
  wait("document.querySelector('video')?.readyState>=2",110)
  p.evaluate("document.querySelector('video').pause()")
  gap=p.evaluate("(()=>{const t=document.querySelector('.progress-track-bg').getBoundingClientRect(),b=document.querySelector('.cczj-mobile-play').getBoundingClientRect();return b.y+b.height/2-t.y-t.height/2})()")
  assert 5<gap<26,gap
  color=p.evaluate("(()=>{const e=document.querySelector('.ep-item.active');return e?{text:getComputedStyle(e).color,background:getComputedStyle(e).backgroundColor}:null})()")
  assert color and color['text']=='rgb(255, 255, 255)' and color['background']=='rgb(32, 35, 41)',color
  p.evaluate("window.__resumeState=document.querySelector('.player-wrapper').__vueParentComponent.setupState;window.__resumeSnapshot={show:__resumeState.showResumePrompt,saved:__resumeState.savedTime};__resumeState.showResumePrompt=true;__resumeState.savedTime=120;document.querySelector('video').play()")
  wait("!!document.querySelector('.cczj-mobile-resume')")
  # The native tap uses the normal visibility handler; no DOM class is forced.
  if p.evaluate("document.querySelector('.cczj-mobile-resume').classList.contains('cczj-mobile-resume-lowered')"):tap('video')
  tap('video')
  wait("document.querySelector('.cczj-mobile-resume')?.classList.contains('cczj-mobile-resume-lowered')")
  time.sleep(.3)
  inset=p.evaluate("document.querySelector('.player-wrapper').getBoundingClientRect().bottom-document.querySelector('.cczj-mobile-resume').getBoundingClientRect().bottom")
  assert abs(inset-8)<1,inset
  p.screenshot(OUT/'resume-lowered.png')
  p.evaluate("__resumeState.showResumePrompt=__resumeSnapshot.show;__resumeState.savedTime=__resumeSnapshot.saved;document.querySelector('video').pause()")
  start=time.monotonic();tap('#player-comments-tab')
  wait("!!document.querySelector('.comment-item,.comments-error,.comments-empty,.cczj-mobile-comments-unavailable')",16)
  result=p.evaluate("({items:document.querySelectorAll('.comment-item').length,error:document.querySelector('.comments-error')?.innerText,loading:!!document.querySelector('.comments-loading')})")
  assert not result['loading'] and result['items']>0,result
  result.update({'seconds':round(time.monotonic()-start,2),'progressGap':gap,'episode':color,'resumeInset':inset})
  p.screenshot(OUT/'actual-comments.png');(OUT/'results.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
  print('PASS: tight chrome, lowered resume, white-on-dark episode and actual cached comments')
 finally:
  p.evaluate("if(window.__resumeState&&window.__resumeSnapshot){__resumeState.showResumePrompt=__resumeSnapshot.show;__resumeState.savedTime=__resumeSnapshot.saved};document.querySelector('video')?.pause();location.hash="+json.dumps(original))
