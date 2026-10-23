"""Comic reader regressions through production Vue views and an isolated RPC seam."""
import argparse
import json
import re
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parent.parent

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port',type=int,default=19320)
    parser.add_argument('--output',type=Path,default=ROOT/'dist/audit/comic-usability/browser')
    args=parser.parse_args()
    args.output.mkdir(parents=True,exist_ok=True)
    calls=[]
    records={}
    item=dict(id=1,kind='comic',sourceKey='fixture',sourceName='测试来源',remoteId='series',title='回归漫画',author='作者',summary='',genres='冒险',status='连载',cover='',latest='',words='',updatedAt='',available=True)
    rows=[dict(id=100+i,remoteId=str(i),ord=i-1,title=f'第{i}话 旅途',isVolume=False,volume='',access='available',available=True) for i in range(1,81)]
    def rpc(method,params):
        calls.append((method,params))
        if method=='ReadingStateLoad':
            if params[0] not in records:records[params[0]]={(r['bucket'],r['key']):r for r in json.loads(params[1])}
            return list(records[params[0]].values())
        if method=='ReadingStateApply':
            for row in json.loads(params[1]):records[params[0]][(row['bucket'],row['key'])]=row
            return None
        if method in ('LibraryItem','LibraryRefreshToc','LibraryRefreshReadingToc'):return dict(item=item,chapters=rows,chapterCount=len(rows))
        if method in ('LibraryComicContext','LibraryRefreshComicContext'):
            pos=params[0]
            current=next((r for r in rows if r['remoteId']==pos['remoteId']),None) if pos['remoteId'] else next((r for r in rows if r['id']==pos['chapterId']),rows[pos['ord']] if 0<=pos['ord']<len(rows) else None)
            at=rows.index(current) if current else -1
            return dict(item=item,chapter=current,previous=rows[at-1] if at>0 else None,next=rows[at+1] if at>=0 and at+1<len(rows) else None,index=at,count=len(rows),cacheIdentity='fixture:'+str(current['id']) if current else '')
        if method in ('LibraryChapterPages','LibraryRefreshChapterPages'):return dict(chapterId=params[0],pages=[dict(index=i,url=f'/__comic_page/{params[0]}/{i}.svg') for i in range(6)],warnings=[],fromCache=True)
        if method=='WindowGetSize':return dict(width=1280,height=900)
        if method.startswith('WindowIs'):return False
        if method=='GetAppVersion':return '2.4.0'
        return [] if method in ('LibrarySources','BackupArchives','BackupExports','ListPlugins','RescanPlugins') else ''
    names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf8'))
    mock="import {CancellablePromise} from '/tests/browser/runtime.ts';\nfunction call(method,args){return new CancellablePromise((resolve,reject)=>{fetch('/__comic_interaction',{method:'POST',body:JSON.stringify({method,args})}).then(r=>r.json()).then(resolve,reject)})}\n"
    mock+='\n'.join('export function '+name+'(...args){return call('+json.dumps(name)+',args)}' for name in names)
    errors=[]
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True,channel='chrome')
        try:
            page=browser.new_page(viewport=dict(width=1280,height=900))
            page.on('pageerror',lambda e:errors.append(str(e)))
            page.set_default_timeout(6000)
            page.route('**/bindings/cczjVideo/app/service/app*',lambda r:r.fulfill(body=mock,content_type='text/javascript'))
            page.route('**/__comic_interaction',lambda r:r.fulfill(body=json.dumps(rpc(r.request.post_data_json['method'],r.request.post_data_json['args']),ensure_ascii=False),content_type='application/json'))
            page.route('**/__comic_page/**',lambda r:r.fulfill(body='<svg xmlns="http://www.w3.org/2000/svg" width="720" height="1100"><rect width="720" height="1100" fill="#786c89"/></svg>',content_type='image/svg+xml'))
            page.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/comic/read/1/0?episodeRemoteId=1')
            expect(page.locator('.comic-reader-indicator')).to_contain_text('第1话')
            viewport=page.locator('.comic-reader-viewport')
            page.wait_for_timeout(450)
            viewport.evaluate('(el)=>el.scrollTop=el.scrollHeight')
            page.wait_for_timeout(350)
            viewport.evaluate('(el)=>el.scrollTop=el.scrollHeight');page.wait_for_timeout(350)
            viewport.hover();page.mouse.wheel(0,120)
            expect(page.locator('.comic-reader-indicator')).to_contain_text('第2话')
            page.wait_for_timeout(400)
            count=lambda method:sum(m==method for m,_ in calls)
            pages_before=count('LibraryChapterPages')
            viewport.evaluate('(el)=>el.scrollTop=0');page.wait_for_timeout(300);page.mouse.wheel(0,-120)
            expect(page.locator('.comic-reader-indicator')).to_contain_text('第1话')
            page.wait_for_timeout(350)
            assert count('LibraryChapterPages')==pages_before,'backtracking fetched cached page table again'
            assert count('LibraryItem')==0,'reader serialized the full directory'
            assert viewport.evaluate('(el)=>el.scrollHeight-el.clientHeight-el.scrollTop')<2,'previous episode did not open at its end'
            print('PASS wheel crosses episodes, backtracks at end from cache without full catalog',flush=True)
            page.keyboard.press('Escape')
            page.locator('.comic-reader-bottom').get_by_role('button',name='目录',exact=True).click()
            expect(page.locator('.comic-reader-episodes li')).to_have_count(80)
            assert count('LibraryItem')==1
            page.get_by_role('button',name='倒序',exact=True).click()
            expect(page.locator('.comic-reader-episodes li').first).to_contain_text('第80话')
            expect(page.locator('.comic-reader-episodes li').first.locator('button > span')).to_have_count(0)
            page.get_by_role('button',name='关闭',exact=True).click()
            print('PASS lazy directory, reverse sort, source title without duplicate numbering',flush=True)
            page.keyboard.press('Escape')
            page.get_by_role('button',name='沉浸阅读（F）',exact=True).click()
            expect(page.locator('.comic-reader')).to_have_class(re.compile('comic-reader-immersive'))
            assert page.evaluate('!!document.fullscreenElement')
            page.keyboard.press('Escape');page.wait_for_timeout(150)
            assert not page.evaluate('!!document.fullscreenElement')
            page.screenshot(path=str(args.output/'reader-desktop.png'))
            page.keyboard.press('Escape')
            page.locator('.comic-reader-bottom').get_by_role('button',name='左右翻页',exact=True).click()
            page.keyboard.press('Escape')
            page.locator('.comic-reader-seek input').evaluate("el=>{el.value='1';el.dispatchEvent(new Event('input',{bubbles:true}))}")
            page.keyboard.press('ArrowRight')
            expect(page.locator('.comic-reader-indicator')).to_contain_text('2 / 6')
            page.mouse.wheel(0,120)
            expect(page.locator('.comic-reader-indicator')).to_contain_text('3 / 6')
            assert page.locator('.comic-panel img').count()<=5
            assert page.locator('.comic-panel img').first.evaluate('(el)=>getComputedStyle(el).animationName')=='none'
            print('PASS fullscreen, keyboard and wheel paging retain a five-image window',flush=True)
            records['comic']={k:v for k,v in records['comic'].items() if k[0]!='preferences'}
            mobile=browser.new_page(viewport=dict(width=375,height=812),is_mobile=True,has_touch=True)
            mobile.on('pageerror',lambda e:errors.append(str(e)))
            mobile.route('**/bindings/cczjVideo/app/service/app*',lambda r:r.fulfill(body=mock,content_type='text/javascript'))
            mobile.route('**/__comic_interaction',lambda r:r.fulfill(body=json.dumps(rpc(r.request.post_data_json['method'],r.request.post_data_json['args']),ensure_ascii=False),content_type='application/json'))
            mobile.route('**/__comic_page/**',lambda r:r.fulfill(body='<svg xmlns="http://www.w3.org/2000/svg" width="720" height="1100"><rect width="720" height="1100" fill="#786c89"/></svg>',content_type='image/svg+xml'))
            mobile.route('**/src/api/runtime.ts',lambda r:r.fulfill(body="export const Window={};export const Dialogs={};export const isMobileRuntime=()=>true;export const isDesktopRuntime=()=>false;export const initializeRuntimeEnvironment=async()=>{};export const reloadApplication=url=>location.replace(url);export const closeMobileApplication=()=>{};",content_type='text/javascript'))
            mobile.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/comic/read/1/0?episodeRemoteId=1')
            expect(mobile.locator('.comic-reader-indicator')).to_contain_text('第1话')
            mobile.wait_for_timeout(450)
            mv=mobile.locator('.comic-reader-viewport')
            for _ in range(2):mv.evaluate('(el)=>el.scrollTop=el.scrollHeight');mobile.wait_for_timeout(350)
            mobile.evaluate('window.__comicBefore=document.querySelector(".comic-reader")')
            cdp=mobile.context.new_cdp_session(mobile)
            def swipe(start,stop):
                cdp.send('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[dict(x=187,y=start)]))
                for n in range(1,9):cdp.send('Input.dispatchTouchEvent',dict(type='touchMove',touchPoints=[dict(x=187,y=start+(stop-start)*n/8)]));mobile.wait_for_timeout(20)
                cdp.send('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]))
            swipe(600,240)
            expect(mobile.locator('.comic-reader-indicator')).to_contain_text('第2话')
            mobile.wait_for_timeout(400);mv.evaluate('(el)=>el.scrollTop=0');mobile.wait_for_timeout(350)
            swipe(200,560)
            expect(mobile.locator('.comic-reader-indicator')).to_contain_text('第1话')
            assert mobile.evaluate('window.__comicBefore===document.querySelector(".comic-reader")')
            assert not mobile.locator('[class*=route-forward-enter],[class*=route-back-enter]').count()
            # Oversized backdrop must not create hidden scroll ranges.
            mobile.evaluate('for(const el of document.querySelectorAll(".cczj-mobile-route-stage,.cczj-mobile-route-viewport")){el.scrollLeft=100;el.scrollTop=100}')
            assert mobile.evaluate('[...document.querySelectorAll(".cczj-mobile-route-stage,.cczj-mobile-route-viewport")].every(el=>el.scrollLeft===0&&el.scrollTop===0)')
            expect(mobile.locator('.comic-reader-immersion')).to_have_count(0)
            mobile.screenshot(path=str(args.output/'reader-mobile.png'))
            print('PASS native touch boundaries preserve reader, avoid route fade and cannot shift background containers',flush=True)
            assert not errors,errors
        finally:browser.close()

if __name__=='__main__':main()
