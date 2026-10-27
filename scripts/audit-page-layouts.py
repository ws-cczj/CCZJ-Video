"""Review real routed views against the isolated Android acceptance backend.

Only Wails transport and desktop window controls are substituted in Chrome.
Video catalog comes from a read-only snapshot; reading uses a loopback fixture.
Creates and removes its own sources, restores preferences, never opens preview.
"""
import argparse
import base64
import html
import importlib.util
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import threading
import time
import uuid
from http.server import ThreadingHTTPServer
from urllib.request import urlopen
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright

ROOT=Path(__file__).resolve().parent.parent
SPEC=importlib.util.spec_from_file_location('native_acceptance',ROOT/'scripts/test-upgrade-android.py')
NATIVE=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(NATIVE)

METRICS=r'''()=>{
 const visible=e=>{const r=e.getBoundingClientRect(),s=getComputedStyle(e),slide=e.closest('.carousel-slide');if(slide){const sr=slide.getBoundingClientRect(),vr=slide.parentElement.getBoundingClientRect();if(sr.right<=vr.left+1||sr.left>=vr.right-1)return false}return r.width>0&&r.height>0&&s.visibility!=='hidden'&&Number(s.opacity)>.01&&!e.closest('[data-route-active="false"],[aria-hidden="true"]')};
 const root=document.querySelector('[data-route-active="true"] .main-content')||document.querySelector('.main-content')||document.querySelector('.app-shell');
 const controls=[...document.querySelectorAll('button,input:not([type=hidden]),textarea,select,[role=combobox]')].filter(visible);
 const inHorizontalScroller=e=>{for(let p=e.parentElement;p&&p!==root;p=p.parentElement){if(['auto','scroll'].includes(getComputedStyle(p).overflowX)&&p.scrollWidth>p.clientWidth+1&&p.getBoundingClientRect().right<=innerWidth+1)return true}return false};
 const outside=controls.filter(e=>{const r=e.getBoundingClientRect();return r.left< -1||r.right>innerWidth+1});
 const describe=e=>({text:(e.getAttribute('aria-label')||e.textContent||e.getAttribute('placeholder')||'').trim().slice(0,80),class:e.className,rect:[e.getBoundingClientRect().x,e.getBoundingClientRect().width]});
 const bad=outside.filter(e=>!inHorizontalScroller(e)).map(describe),scrollable=outside.filter(inHorizontalScroller).map(describe);
 const overlap=[];
 for(const e of document.querySelectorAll('.item')){const a=e.querySelector('.item-text'),b=e.querySelector('.item-control');if(a&&b&&visible(a)&&visible(b)){const ar=a.getBoundingClientRect(),br=b.getBoundingClientRect();if(Math.min(ar.right,br.right)-Math.max(ar.left,br.left)>2&&Math.min(ar.bottom,br.bottom)-Math.max(ar.top,br.top)>2)overlap.push(a.textContent.trim().slice(0,60))}}
 const duplicates=[...document.querySelectorAll('.empty-state')].filter(visible).filter(e=>e.querySelector('.empty-title')?.textContent.trim()===e.querySelector('.empty-desc')?.textContent.trim()).map(e=>e.textContent.trim().slice(0,100));
 const headings=[...document.querySelectorAll('h1')].filter(visible).map(e=>e.textContent.trim());
 const repeatedHeadings=headings.filter((value,index)=>headings.indexOf(value)!==index);
 const clippedText=controls.filter(e=>e.classList.contains('ui-btn')&&e.querySelector('.ui-btn__content')?.scrollWidth>e.clientWidth+1).map(e=>e.textContent.trim().slice(0,60));
 return {overflow:!!root&&root.scrollWidth>root.clientWidth+1,dialogOverflow:[...document.querySelectorAll('[role=dialog]')].filter(visible).some(e=>e.scrollWidth>e.clientWidth+1),clipped:bad,scrollable,overlap,duplicates,repeatedHeadings,clippedText,headings,rootFont:getComputedStyle(document.documentElement).fontSize,readerFont:document.querySelector('.novel-reading-paragraph')?getComputedStyle(document.querySelector('.novel-reading-paragraph')).fontSize:null,buttons:controls.length,body:document.body.innerText.slice(0,450)};
}'''


def main():
 p=argparse.ArgumentParser(description=__doc__)
 p.add_argument('--device',default='emulator-5554')
 p.add_argument('--channel',default='chrome')
 p.add_argument('--port',type=int,default=9268)
 p.add_argument('--quick',action='store_true',help='Only default-size desktop/mobile inventory')
 p.add_argument('--mode',choices=['video','novel','comic','all'],default='all')
 p.add_argument('--mobile-only',action='store_true',help='Audit mobile layouts only, with a separate screenshot gallery')
 p.add_argument('--snapshot',type=Path,default=ROOT/'dist/audit/upgrade-244-245/final-preserved-245/cczj_video.db')
 args=p.parse_args();out=ROOT/'dist/audit/page-polish'
 if args.mobile_only:out=out/'mobile'
 out.mkdir(parents=True,exist_ok=True)
 d=NATIVE.Device('adb',args.device);d.connect()
 original={key:d.rpc('GetSetting',key) for key in ['application_mode','font_size_key','language','default_source_key','outbound_proxy_pool_v1']}
 private=d.rpc('GetAllowPrivateNetwork');original_url=d.js('location.href')
 sources=[];paused_sources=[];rows=[];api_errors=[]
 fixture=ThreadingHTTPServer(('127.0.0.1',0),NATIVE.Fixture)
 threading.Thread(target=fixture.serve_forever,daemon=True).start();fixture_port=fixture.server_port
 d.native('reverse',f'tcp:{fixture_port}',f'tcp:{fixture_port}')
 server=None
 try:
  d.visit('/settings');d.rpc('SetAllowPrivateNetwork',True)
  d.rpc('SetSetting','outbound_proxy_pool_v1','{"enabled":false}')
  d.restart()
  token=uuid.uuid4().hex[:10];key='layout_audit_'+token
  with sqlite3.connect('file:'+str(args.snapshot)+'?mode=ro',uri=True) as con:
   con.row_factory=sqlite3.Row
   src=dict(con.execute('SELECT * FROM sources LIMIT 1').fetchone())
   videos=[dict(row) for row in con.execute('SELECT * FROM source_videos WHERE source_key=?',(src['source_key'],))]
  payload=dict(version=2,source=dict(source_key=key,name='真实目录排版验收',api_url=src['api_url'],enabled=1,kind='video'),videos=videos)
  d.rpc('ImportSourceFromBase64','layout-source.json',base64.b64encode(json.dumps(payload,ensure_ascii=False).encode()).decode());sources.append(('video',key))
  d.rpc('SetSetting','default_source_key',key)
  catalog=d.rpc('GetVideoList',dict(source_key=key,page=1,page_size=30))
  video=next((item for item in catalog['videos'] if item['vod_id']=='108493'),catalog['videos'][0])
  library={kind:dict(item=dict(id=1)) for kind in ['novel','comic']}
  for kind,route in [('novel','nA'),('comic','cA')]:
   if args.mode not in ['all',kind]:continue
   d.visit('/'+kind+'/settings')
   for installed in d.rpc('LibrarySources',kind):
    if installed['enabled']:
     d.rpc('LibrarySetSourceEnabled',installed['sourceKey'],False);paused_sources.append(installed['sourceKey'])
   source=d.rpc('LibraryAddSource',kind,'排版验收'+kind+token,f'http://127.0.0.1:{fixture_port}/{route}',NATIVE.document(kind))
   source_key=source['sourceKey'];sources.append((kind,source_key))
   d.visit('/'+kind+'/settings')
   search=d.rpc('LibrarySearch',kind,'同名',1)
   item=d.rpc('LibraryResolveRefs',[dict(kind=kind,sourceKey=source_key,remoteId=f'/{route}/book/001')])[0]
   if not item:
    (out/('reading-seed-'+kind+'.json')).write_text(json.dumps(dict(source=source,result=search),ensure_ascii=False,indent=2),encoding='utf-8')
    raise AssertionError('Reading fixture not found: '+kind)
   detail=d.rpc('LibraryItem',item['id'],True);library[kind]=dict(item=item,detail=detail)
  # Actual backend methods remain in use for every routed view.
  names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
  bindings='\n'.join('export async function '+name+'(...args){const r=await fetch("/__layout_rpc",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({method:"'+name+'",args})});const value=await r.json();if(value.error)throw value.error;return value.data;}' for name in names)
  def rpc_route(route):
   call=route.request.post_data_json;name=call['method']
   try:
    if name=='WindowGetSize': value=dict(width=1280,height=800)
    elif name=='WindowGetResizable':value=True
    elif name.startswith('Window'):value=False if name.startswith('WindowIs') else None
    else:value=d.rpc(name,*call['args'])
    route.fulfill(json=dict(data=value))
   except Exception as error:
    api_errors.append(dict(method=name,error=str(error)[:500]))
    failure=dict(code='INTERNAL',message='Layout audit request failed')
    try:
     description=json.loads(str(error))['exception']['description'].splitlines()[0]
     decoded=json.loads(description[description.index('{'):])
     if isinstance(decoded.get('code'),str) and isinstance(decoded.get('message'),str):failure=decoded
    except (ValueError,KeyError,TypeError):pass
    route.fulfill(json=dict(error=failure))
  def asset_route(route):
   address=urlsplit(route.request.url)
   target='https://wails.localhost'+address.path+('?' + address.query if address.query else '')
   headers={key:value for key,value in route.request.headers.items() if key.lower()=='range'}
   asset=d.js('(async()=>{const r=await fetch('+json.dumps(target)+',{headers:'+json.dumps(headers)+'});const data=new Uint8Array(await r.arrayBuffer());let text="";for(let i=0;i<data.length;i+=8192)text+=String.fromCharCode(...data.subarray(i,i+8192));return {status:r.status,headers:Object.fromEntries([...r.headers].filter(([key])=>["content-type","content-range","accept-ranges"].includes(key))),data:btoa(text)}})()')
   route.fulfill(status=asset['status'],body=base64.b64decode(asset['data']),headers=asset['headers'])
  server=subprocess.Popen(['node','node_modules/vite/bin/vite.js','--host','127.0.0.1','--port',str(args.port),'--strictPort'],cwd=ROOT/'frontend',stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,creationflags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0)
  origin=f'http://127.0.0.1:{args.port}'
  for attempt in range(80):
   try:
    with urlopen(origin+'/tests/browser/page-audit.html',timeout=1):break
   except OSError:time.sleep(.15)
  vid=video['global_id'];vod=video['vod_id']
  routes={
   'video':['/','/search','/short-drama','/short-drama?view=theatre','/recent',f'/detail/{key}/{vid}?vod={vod}',f'/player/{key}/{vid}/0?vod={vod}','/sources','/danmaku-sources','/downloads','/recycle','/video-types','/favorites','/history','/recommendations','/settings']+['/settings?group='+group for group in ['basic','basic&section=home','basic&section=data','basic&section=maintenance','theme','advanced','motion','about','diagnostics','logs','extensions']],
   'novel':['/novel','/novel/categories','/novel/search','/novel/shelf','/novel/settings','/novel/settings?group=reading','/novel/settings?group=mode','/novel/sources','/novel/diagnostics',f'/novel/book/{library["novel"]["item"]["id"]}',f'/novel/read/{library["novel"]["item"]["id"]}/first'],
   'comic':['/comic','/comic/categories','/comic/search','/comic/library','/comic/settings','/comic/settings?group=reading','/comic/settings?group=mode','/comic/sources','/comic/diagnostics',f'/comic/series/{library["comic"]["item"]["id"]}',f'/comic/read/{library["comic"]["item"]["id"]}/0'],
  }
  profiles=[('desktop',1280,800,'md'),('mobile',390,844,'md')]
  if not args.quick:profiles += [('desktop-large',960,800,'xxl'),('mobile-large',320,844,'xxl')]
  if args.mobile_only:profiles=[profile for profile in profiles if profile[0].startswith('mobile')]
  with sync_playwright() as playwright:
   browser=playwright.chromium.launch(headless=True,channel=args.channel)
   try:
    for mode,paths in routes.items():
     if args.mode!='all' and mode!=args.mode:continue
     for profile,width,height,size in profiles:
      d.visit('/settings' if mode=='video' else '/'+mode+'/settings')
      d.rpc('SetSetting','font_size_key',size)
      context=browser.new_context(viewport=dict(width=width,height=height));page=context.new_page();errors=[]
      page.on('pageerror',lambda error:errors.append(str(error)))
      context.route('**/bindings/cczjVideo/app/service/app*',lambda route:route.fulfill(body=bindings,content_type='text/javascript'))
      context.route('**/__layout_rpc',rpc_route)
      context.route('**/__cczj/img?*',asset_route)
      context.route('**/__cczj/hls?*',asset_route)
      page.add_init_script('window._wails={environment:{OS:'+json.dumps('android' if profile.startswith('mobile') else 'windows')+'}}')
      page.goto(origin+'/tests/browser/page-audit.html#'+paths[0],wait_until='domcontentloaded');page.wait_for_function('!!window.pageAudit',timeout=30000)
      page.wait_for_function('!document.querySelector(".splash-wrap")')
      for index,path in enumerate(paths):
       page.evaluate('(path)=>window.pageAudit.router.push(path)',path)
       page.wait_for_timeout(380)
       media=None
       if path.startswith('/player/'):
        # Geometry and absence of JS exceptions cannot establish playback.
        # Require real decoded frames and advancing time before taking a shot.
        try:
         page.wait_for_function('document.querySelector(".player-wrapper video")?.readyState>=2 || !!document.querySelector(".player-error")',timeout=45000)
         page.evaluate('document.querySelector(".player-wrapper video")?.play().catch(()=>{})')
         page.wait_for_function('(()=>{const v=document.querySelector(".player-wrapper video");return v?.readyState>=2 && v.videoWidth>0 && v.currentTime>.2 && !v.paused})()',timeout=20000)
        except Exception as error:
         media=dict(failed=True,reason=str(error).splitlines()[0])
        state=page.evaluate('(()=>{const v=document.querySelector(".player-wrapper video");return {ready:v?.readyState||0,width:v?.videoWidth||0,height:v?.videoHeight||0,time:v?.currentTime||0,paused:v?.paused??true,error:document.querySelector(".player-error")?.textContent.trim()||v?.error?.message||""}})()')
        media=dict(media or {},**state)
       if '/novel/read/' in path:page.wait_for_selector('.novel-reading-paragraph',timeout=15000)
       page.evaluate('document.querySelectorAll(".ui-toast__close").forEach(e=>e.click())')
       metrics=page.evaluate(METRICS)
       filename=f'{mode}-{profile}-{index:02}.png'
       page.screenshot(path=str(out/filename),animations='disabled')
       if 'large' not in profile and path.startswith('/settings'):
        scrolled=page.evaluate('(()=>{const e=document.querySelector(".main-content");if(!e||e.scrollHeight<=e.clientHeight+100)return false;e.scrollTop=e.scrollHeight;return true})()')
        if scrolled:
         page.wait_for_timeout(100)
         page.screenshot(path=str(out/filename.replace('.png','-bottom.png')),animations='disabled')
       rows.append(dict(mode=mode,profile=profile,route=path,screenshot=filename,media=media,**metrics,errors=list(errors)+page.evaluate('window.pageAudit.errors')))
       (out/'page-inventory.json').write_text(json.dumps(dict(views=rows,apiErrors=api_errors),ensure_ascii=False,indent=2),encoding='utf-8')
       print(json.dumps(dict(mode=mode,profile=profile,route=path,issues={key:value for key,value in metrics.items() if key in ['overflow','clipped','overlap','duplicates','repeatedHeadings','clippedText'] and value}),ensure_ascii=True),flush=True)
       if mode=='video' and profile in ['desktop','mobile-large']:
        trigger='.source-card .edit-btn' if path=='/sources' else '.theme-edit-fab' if path=='/settings?group=theme' and profile.startswith('mobile') else None
        if trigger and page.locator(trigger).count():
         page.locator(trigger).first.click();page.wait_for_selector('[role=dialog]')
         page.wait_for_timeout(200)
         dialog=page.evaluate(METRICS);shot=filename.replace('.png','-dialog.png')
         page.screenshot(path=str(out/shot),animations='disabled')
         rows.append(dict(mode=mode,profile=profile,route=path,scenario='dialog',screenshot=shot,**dialog,errors=list(errors)))
         page.locator('.ui-modal__close').last.click()
      context.close()
   finally:browser.close()
  cards=''.join('<article><h2>'+html.escape(row['mode']+' '+row['profile']+' '+row['route'])+'</h2><a href="'+row['screenshot']+'"><img loading="lazy" src="'+row['screenshot']+'"></a></article>' for row in rows)
  (out/'index.html').write_text('<!doctype html><meta charset="utf-8"><title>Page layout audit</title><style>body{font:14px system-ui;background:#eee}main{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px}article{background:white;padding:8px}h2{font-size:12px;overflow-wrap:anywhere}img{width:100%;height:440px;object-fit:contain;object-position:top}</style><main>'+cards+'</main>',encoding='utf-8')
  issues=[dict(mode=row['mode'],profile=row['profile'],route=row['route'],scenario=row.get('scenario','page')) for row in rows if any(row[key] for key in ['overflow','dialogOverflow','clipped','overlap','duplicates','repeatedHeadings','clippedText','errors']) or row.get('media',{} ) and (row['media'].get('failed') or row['media'].get('error'))]
  reader_sizes={row['readerFont'] for row in rows if row['readerFont']}
  summary=dict(views=len(rows),issues=issues,readerSizes=sorted(reader_sizes),scope='Chrome routed UI with native acceptance RPC; desktop window controls substituted; read-only real video snapshot and loopback reading fixtures')
  (out/'summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2),encoding='utf-8')
  if issues:raise AssertionError('Layout issues: '+json.dumps(issues))
  if args.mode in ['all','novel']:assert len(reader_sizes)==1,reader_sizes
 finally:
  if server:server.terminate();server.wait(timeout=15)
  d.visit('/settings')
  for kind,key in reversed(sources):
   d.rpc('DeleteSource' if kind=='video' else 'LibraryDeleteSource',key)
  for key in paused_sources:d.rpc('LibrarySetSourceEnabled',key,True)
  for key,value in original.items():d.rpc('SetSetting',key,value)
  d.rpc('SetAllowPrivateNetwork',private)
  d.restart();d.js('location.href='+json.dumps(original_url));d.close()
  d.native('reverse','--remove',f'tcp:{fixture_port}');fixture.shutdown();fixture.server_close()


if __name__=='__main__':main()
