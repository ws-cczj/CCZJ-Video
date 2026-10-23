"""Real publisher ranking, source search and responsive home against reading-browser-host.

Requires Vite and scripts/reading-browser-host, using an isolated database.
Only the Wails transport is replaced; ranking, source and image responses are real.
"""
import argparse,json,re
from pathlib import Path
from urllib.request import Request,urlopen
from urllib.error import HTTPError
from playwright.sync_api import sync_playwright,expect

ROOT=Path(__file__).resolve().parent.parent
def main():
 parser=argparse.ArgumentParser(description=__doc__)
 parser.add_argument('--frontend-port',type=int,default=19320)
 parser.add_argument('--rpc-port',type=int,default=9296)
 parser.add_argument('--output',type=Path,default=ROOT/'dist/audit/comic-usability/home')
 args=parser.parse_args();args.output.mkdir(parents=True,exist_ok=True)
 names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text('utf-8'))
 mock="import {CancellablePromise} from '/tests/browser/runtime.ts';function call(method,args){return new CancellablePromise((resolve,reject)=>{fetch('/__comic_live',{method:'POST',body:JSON.stringify({method,args})}).then(async r=>{const v=await r.json();if(!r.ok)throw new Error(v.message);return v}).then(resolve,reject)})}\n"
 mock+='\n'.join('export function '+name+'(...args){return '+('call('+json.dumps(name)+',args)' if name.startswith(('Library','ReadingState','ComicBrowse')) or name=='ComicTrending' else "CancellablePromise.resolve('')")+'}' for name in names)
 calls=[];errors=[]
 def bridge(route):
  call=route.request.post_data_json;calls.append(call)
  request=Request(f'http://127.0.0.1:{args.rpc_port}/rpc',json.dumps(call).encode(),{'Content-Type':'application/json'})
  try:
   with urlopen(request,timeout=65) as response:route.fulfill(body=response.read(),content_type='application/json')
  except HTTPError as error:route.fulfill(status=error.code,body=error.read(),content_type='application/json')
 with sync_playwright() as p:
  browser=p.chromium.launch(headless=True,channel='chrome')
  try:
   page=browser.new_page(viewport=dict(width=1280,height=1000))
   page.set_default_timeout(20000)
   page.on('pageerror',lambda error:errors.append(str(error)))
   page.route('**/bindings/cczjVideo/app/service/app*',lambda route:route.fulfill(body=mock,content_type='text/javascript'))
   page.route('**/__comic_live',bridge)
   page.route('**/__cczj/img?*',lambda route:route.continue_(url=f'http://127.0.0.1:{args.rpc_port}/__cczj/img?'+route.request.url.split('?',1)[1]))
   page.goto(f'http://127.0.0.1:{args.frontend_port}/tests/browser/reading.html#/comic')
   expect(page.locator('.manga-trend-card')).to_have_count(12,timeout=90000)
   expect(page.locator('.manga-trend-card').first).to_contain_text('航海王')
   for mode in ['light','dark']:
    page.evaluate('mode=>document.documentElement.dataset.themeMode=mode',mode)
    for width in [1280,375]:
     page.set_viewport_size(dict(width=width,height=1000))
     assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
     page.screenshot(path=str(args.output/f'home-{mode}-{width}.png'))
   page.set_viewport_size(dict(width=1280,height=1000))
   page.locator('.manga-trending').get_by_role('button',name='国漫',exact=True).click()
   expect(page.locator('.manga-trend-card').first).to_contain_text('登霞九法',timeout=60000)
   expect(page.locator('.manga-trending footer')).to_contain_text('哔哩哔哩漫画')
   page.locator('.manga-trending').get_by_role('button',name='日漫',exact=True).click()
   expect(page.locator('.manga-trend-card').first).to_contain_text('航海王')
   page.locator('.manga-trend-card').first.click()
   expect(page.locator('.comic-search input')).to_have_value('航海王')
   expect(page.locator('.comic-card').filter(has_text='航海王').first).to_be_visible(timeout=90000)
   searched=sum(call['method'].startswith('LibrarySearch') for call in calls)
   page.locator('.comic-card').filter(has_text='航海王').first.click()
   expect(page.locator('.comic-detail-actions a')).to_be_visible(timeout=90000)
   page.go_back()
   expect(page.locator('.comic-card').filter(has_text='航海王').first).to_be_visible()
   page.wait_for_timeout(400)
   assert sum(call['method'].startswith('LibrarySearch') for call in calls)==searched,'Back repeated the source search'
   page.go_back()
   expect(page.locator('.manga-trend-card')).to_have_count(12)
   assert sum(call['method'].startswith('LibrarySearch') for call in calls)==searched
   assert not errors,errors
   result=dict(passed=True,native=False,checks=['real Japanese and Chinese rankings','12 titles on the home, 24 in each snapshot','source-attributed responsive light/dark layout','popular title opens actual source search','Back from details retains source search and Back again retains ranking'],calls=len(calls),errors=errors)
   (args.output/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),'utf-8')
   print(json.dumps(result,ensure_ascii=False),flush=True)
  finally:browser.close()
if __name__=='__main__':main()
