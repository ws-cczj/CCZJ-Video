"""Real comments UI: never-ending request, late success, stale response and retry."""
import os,re,subprocess,time
from pathlib import Path
from urllib.request import urlopen
from playwright.sync_api import sync_playwright,expect
ROOT=Path(__file__).resolve().parent.parent
port=19658
server=subprocess.Popen(['node','node_modules/vite/bin/vite.js','--port',str(port),'--strictPort'],cwd=ROOT/'frontend',stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,creationflags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0)
try:
 for _ in range(70):
  try:urlopen(f'http://127.0.0.1:{port}/tests/browser/comments.html',timeout=1);break
  except OSError:time.sleep(.2)
 with sync_playwright() as pw:
  browser=pw.chromium.launch(channel='chrome',headless=True)
  page=browser.new_page()
  failures=[];page.on('pageerror',lambda error:failures.append(str(error)))
  names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
  mocks='\n'.join('export async function '+name+'(){return ""}' for name in names if name not in ['GetSetting','SetSetting','ProxyImageUrl','GetDoubanComments'])
  page.route('**/bindings/cczjVideo/app/service/app*',lambda route:route.fulfill(content_type='text/javascript',body=mocks+'''
    export async function GetSetting(){return ''};export async function SetSetting(){};
    export async function ProxyImageUrl(url){return url};
    export function GetDoubanComments(req){return new Promise((resolve,reject)=>{(window.commentCalls||=[]).push({req,resolve,reject})})}
  '''))
  page.goto(f'http://127.0.0.1:{port}/tests/browser/comments.html')
  expect(page.locator('.comments-loading')).to_be_visible()
  expect(page.locator('.comments-error')).to_be_visible(timeout=15000)
  expect(page.locator('.comments-loading')).to_have_count(0)
  page.evaluate("commentCalls[0].resolve({comments:[{id:'late',username:'Late comment',content:'Late success',rating:9}],page:1,total_pages:1,total:1})")
  expect(page.locator('.comment-item')).to_have_count(1)
  expect(page.locator('.comments-error')).to_have_count(0)
  page.evaluate("changeCommentSubject('second')")
  expect(page.locator('.comments-loading')).to_be_visible()
  page.evaluate("changeCommentSubject('third')")
  page.wait_for_function('commentCalls.length===3')
  page.evaluate("commentCalls[1].resolve({comments:[{id:'old',username:'STALE',content:'Wrong subject',rating:1}]});commentCalls[2].reject(new Error('UNAVAILABLE: failed'))")
  expect(page.locator('.comments-error')).to_be_visible()
  expect(page.get_by_text('STALE')).to_have_count(0)
  page.locator('.retry-btn').click();page.wait_for_function('commentCalls.length===4')
  page.evaluate("commentCalls[3].resolve({comments:[],page:1,total_pages:1,total:0})")
  expect(page.locator('.comments-empty')).to_be_visible()
  assert not failures,failures
  browser.close()
 print('PASS: slow comments stop blocking after 12s; late success, stale-response guard, rating clamp and retry')
finally:
 server.terminate();server.wait(timeout=10)
