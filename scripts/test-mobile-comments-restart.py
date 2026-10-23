"""Confirm a real successful comment page survives an Android process restart."""
import json
import subprocess
import time
from pathlib import Path
from android_webview import AndroidWebView

api="await import('/src/api/app.ts')"
with AndroidWebView() as p:
 original=p.evaluate('location.hash')
 snapshot=p.evaluate("(async()=>{const api="+api+";return JSON.parse(await api.GetSetting('douban_comments_snapshot')||'[]').map(p=>({key:p.key,fetchedAt:p.fetched_at,count:p.data.comments?.length||0}))})()")
 assert snapshot and snapshot[0]['count']>0,'First load a real comment page before restart testing'
 key=snapshot[0]['key']
 subject,page,sort=key.split('_',2)
 request={'douban_id':subject,'page':int(page),'sort':sort}
subprocess.run(['adb','-s','emulator-5554','shell','am','force-stop','com.cczj.video.preview'],check=True)
subprocess.run(['adb','-s','emulator-5554','shell','am','start','-W','--activity-single-top','--activity-clear-top','-n','com.cczj.video.preview/com.wails.app.MainActivity','--ei','cczjFrontendPort','19545'],check=True,capture_output=True)
time.sleep(1)
with AndroidWebView() as p:
 deadline=time.monotonic()+20
 while not p.evaluate("!!document.querySelector('.cczj-mobile-shell')"):
  assert time.monotonic()<deadline,'App did not restart'
  time.sleep(.1)
 result=p.evaluate("(async()=>{const api="+api+";const start=performance.now();const response=await api.GetDoubanComments("+json.dumps(request)+");const elapsed=performance.now()-start;const diag=await api.GetDiagnostics();return {elapsed,count:response.comments?.length||0,cache:diag.cache.find(r=>r.key==='comments')}})()")
 assert result['count']==snapshot[0]['count'],result
 assert result['cache']['hits']>=1 and result['cache']['fetch_ok']==0 and result['cache']['fetch_fail']==0,result
 assert result['elapsed']<1000,result
 p.evaluate('location.hash='+json.dumps(original))
 Path('dist/audit/mobile-comments-restart.json').write_text(json.dumps(result,indent=2),encoding='utf-8')
 print('PASS: real comments restored after process restart; no network fetch, %.1f ms'%result['elapsed'])
