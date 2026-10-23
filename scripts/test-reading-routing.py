"""Browser-substitute QA: real Go builtin directories; explicit switch UI fixtures.

Servers are supplied by with_server.py. No public source quality is inferred
from the synthetic alternative/chapter responses used for interaction tests.
"""
import json
from pathlib import Path
import re
from urllib.request import Request, urlopen
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parent.parent
OUT=ROOT/'reports/reading-routing-2026-10-22'
OUT.mkdir(exist_ok=True)
names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
mock="import {CancellablePromise} from '/tests/browser/runtime.ts';\n"
mock+="""function invoke(method,args){const controller=new AbortController();return new CancellablePromise((resolve,reject)=>{fetch('/__reading_route_rpc',{method:'POST',body:JSON.stringify({method,args}),signal:controller.signal}).then(async r=>{const data=await r.json();if(!r.ok)throw Object.assign(new Error(data.message),{code:data.code});return data}).then(resolve,reject)},()=>controller.abort())}\n"""
mock+='\n'.join('export function '+name+'(...args){return '+('invoke('+json.dumps(name)+',args)' if name.startswith(('Library','ReadingState','Novel','Comic')) else "CancellablePromise.resolve('')")+';}' for name in names)
calls=[]
def rpc(method,args):
    with urlopen(Request('http://127.0.0.1:9298/rpc',json.dumps(dict(method=method,args=args)).encode(),{'Content-Type':'application/json'}),timeout=40) as r:return json.load(r)
def fixture(method,args):
    kind='novel' if method.startswith('Novel') else 'comic'
    item=dict(id=101 if kind=='novel' else 202,kind=kind,sourceKey='fixture_'+kind,sourceName='备用书源' if kind=='novel' else '备用漫源',remoteId='fixture',title='章节对应验证' if kind=='novel' else '话次对应验证',author='测试作者',available=True)
    if method.endswith('SourceAlternatives'):
        return dict(keyword=item['title'],page=1,total=1,failedCount=0,nextCursor='',pendingSources=0,searchedSources=1,results=[dict(sourceKey=item['sourceKey'],sourceName=item['sourceName'],items=[item],nextUrl='',hasNext=False)])
    return dict(kind=kind,identity='verified',status='matched',chapterId=args[1],targetItemId=item['id'],targets=[dict(id=1011 if kind=='novel' else 2022,ord=4,remoteId='target',title='第五章 落雨' if kind=='novel' else '第4.5话 星光',volume='',isVolume=False,access='available',available=True)],evidence='界面交互测试夹具：非网络章节质量证据',matched=1,ambiguous=0,unmatched=0,checkedAt=1)
with sync_playwright() as p:
    browser=p.chromium.launch(headless=True,channel='chrome')
    try:
        page=browser.new_page(viewport={'width':1360,'height':1000});errors=[];page.on('pageerror',lambda error:errors.append(str(error)))
        page.route('**/bindings/cczjVideo/app/service/app*',lambda route:route.fulfill(body=mock,content_type='text/javascript'))
        def bridge(route):
            call=route.request.post_data_json;calls.append(call);method,args=call['method'],call['args']
            try:value=fixture(method,args) if method in ['NovelSourceAlternatives','ComicSourceAlternatives','NovelChapterMatch','ComicEpisodeMatch'] else rpc(method,args);route.fulfill(body=json.dumps(value,ensure_ascii=False),content_type='application/json')
            except Exception as error:route.fulfill(status=500,body=json.dumps(dict(code='INTERNAL',message=str(error))),content_type='application/json')
        page.route('**/__reading_route_rpc',bridge)
        inventories={}
        for kind,heading,selector in [('novel','书从这里来','.book-source-ledger li'),('comic','漫画补给站','.manga-source-card')]:
            page.goto('http://127.0.0.1:9299/tests/browser/reading.html#/'+kind+'/sources');page.wait_for_load_state('networkidle')
            expect(page.get_by_role('heading',name=heading)).to_be_visible();page.wait_for_selector(selector)
            inventories[kind]=page.locator(selector).count();assert inventories[kind]>0
            assert page.locator('textarea').count()==0
            directory=rpc('NovelSourceDirectory' if kind=='novel' else 'ComicSourceDirectory',[]);assert directory['kind']==kind
            assert all(row['id'] and row['sourceKey'] for row in directory['rows'])
            inventories[kind]={'adapted':inventories[kind],'registered':directory['registered'],'awaitingAdapter':directory['awaitingAdapter']}
            page.screenshot(path=str(OUT/(kind+'-desktop.png')),full_page=True)
            page.set_viewport_size({'width':390,'height':844});page.wait_for_timeout(120)
            assert page.evaluate('document.documentElement.scrollWidth <= innerWidth+1')
            page.screenshot(path=str(OUT/(kind+'-mobile.png')),full_page=True);page.set_viewport_size({'width':1360,'height':1000})
        page.goto('http://127.0.0.1:9299/tests/browser/source-switch.html');page.wait_for_load_state('networkidle')
        page.get_by_role('button',name=re.compile('备用书源')).click();expect(page.get_by_role('button',name='第五章 落雨')).to_be_visible()
        page.get_by_role('button',name=re.compile('备用漫源')).click();expect(page.get_by_role('button',name='第4.5话 星光')).to_be_visible()
        page.get_by_role('button',name='第4.5话 星光').click()
        print('Switch navigation:',page.url,'errors:',errors,flush=True)
        page.wait_for_function("location.hash.includes('/comic/read/202/4')",timeout=5000)
        assert 'episodeRemoteId=target' in page.url and 'sourceSwitch=1' in page.url,page.url
        assert any(c['method']=='NovelChapterMatch' for c in calls) and any(c['method']=='ComicEpisodeMatch' for c in calls)
        assert not errors,errors
        (OUT/'browser-verification.json').write_text(json.dumps(dict(inventories=inventories,calls=calls,errors=errors,scope='real Go directories; synthetic switch UI responses; browser substitute only'),ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
        print(json.dumps(dict(inventories=inventories,errors=errors,checks='desktop/mobile directories and separate source-switch routes passed'),ensure_ascii=False))
    finally:browser.close()
