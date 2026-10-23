import sys,time,json
from pathlib import Path
sys.path.insert(0,'scripts')
from android_webview import AndroidWebView
out=Path('reports/mumu-hands-on-2026-10-20'); results=[]
with AndroidWebView() as v:
 def wait_path(path):
  for _ in range(80):
   if v.evaluate('location.hash')=='#'+path:return
   time.sleep(.1)
  raise AssertionError((path,v.evaluate('location.hash')))
 def clicklink(path):
  assert v.evaluate('Array.from(document.querySelectorAll("a")).some(e=>e.getAttribute("href")==='+json.dumps('#'+path)+')'),path
  v.evaluate('Array.from(document.querySelectorAll("a")).find(e=>e.getAttribute("href")==='+json.dumps('#'+path)+').click()')
  wait_path(path);time.sleep(.8)
 def exit_reader():
  v.evaluate('document.querySelector("button[aria-label=返回]").click()')
  time.sleep(.8)
 def check(name,path):
  wait_path(path);results.append({'case':name,'route':v.evaluate('location.hash'),'passed':True});v.screenshot(out/(name+'.png'))
 clicklink('/comic/read/58/0')
 exit_reader();check('42-comic-continue-back','/comic')
 clicklink('/comic/series/58');clicklink('/comic/read/58/0')
 exit_reader();check('43-comic-detail-back','/comic/series/58')
 v.evaluate('document.querySelector("button[aria-label=返回]").click()')
 check('44-comic-second-back','/comic')
 clicklink('/comic/settings')
 v.evaluate('Array.from(document.querySelectorAll("button")).find(e=>e.innerText.trim()==="小说模式").click()')
 time.sleep(.3)
 v.evaluate('Array.from(document.querySelectorAll("button")).find(e=>e.innerText.trim()==="暂停任务并切换").click()')
 time.sleep(3)
 print(json.dumps({'results':results,'state':v.evaluate('({url:location.href,links:Array.from(document.querySelectorAll("a")).map(e=>({text:e.innerText,href:e.getAttribute("href")}))})')},ensure_ascii=True))
(out/'native-return-results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2),encoding='utf-8')
