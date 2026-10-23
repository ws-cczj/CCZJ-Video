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
 clicklink('/novel/read/5/1')
 exit_reader();check('45-novel-continue-back','/novel')
 clicklink('/novel/categories');clicklink('/novel/book/5');clicklink('/novel/read/5/1')
 exit_reader();check('46-novel-detail-back','/novel/book/5')
 v.evaluate('document.querySelector("button[aria-label=返回]").click()')
 check('47-novel-second-back','/novel/categories')
 print(json.dumps(results,ensure_ascii=True))
old=json.loads((out/'native-return-results.json').read_text(encoding='utf-8'))
(out/'native-return-results.json').write_text(json.dumps(old+results,ensure_ascii=False,indent=2),encoding='utf-8')
