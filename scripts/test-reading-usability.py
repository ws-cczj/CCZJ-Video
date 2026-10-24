"""Source import/home invalidation and actual reader exit clicks on cached mobile views."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import time
from urllib.request import urlopen
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parent.parent

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--channel',default='msedge')
    parser.add_argument('--output',type=Path,default=ROOT/'reports/reading-regression-2026-10-21')
    args=parser.parse_args()
    output=args.output
    output.mkdir(parents=True,exist_ok=True)
    server=subprocess.Popen(['node','node_modules/vite/bin/vite.js','--host','127.0.0.1','--port','9303','--strictPort'],cwd=ROOT/'frontend',stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,creationflags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0)
    records,installed,results,errors={},[],[],[]
    def rpc(method,args):
        if method in ('LibraryNovelContext','LibraryRefreshNovelContext'):
            location=args[0]
            view=rpc('LibraryItem',[location['itemId'],False])
            rows=view['chapters'];current=next((row for row in rows if row['remoteId']==location['remoteId']),None) if location['remoteId'] else next((row for row in rows if row['id']==location['chapterId']),rows[0] if location['first'] and rows else None)
            index=rows.index(current) if current else -1
            return dict(item=view['item'],chapter=current,previous=None,next=None,index=index,count=len(rows),cacheIdentity='fixture:'+str(current['id']) if current else '')
        if method=='ReadingStateLoad':
            if args[0] not in records:records[args[0]]={}
            return list(records[args[0]].values())
        if method=='ReadingStateApply':
            for row in json.loads(args[1]):records[args[0]][(row['bucket'],row['key'])]=row
            return None
        if method=='LibrarySources':return [dict(sourceKey=k+'-1',kind=k,name='测试来源',apiUrl='https://example.org',enabled=True,itemCount=0) for k in installed if k==args[0]]
        if method=='LibraryItems':return []
        if method=='LibraryPreviewSources':return dict(id='preview',origin='',expiresAt=9999999999,candidates=[dict(id='candidate',name='测试来源',apiUrl='https://example.org',kind=args[0],project='Legado',origin='',format='adapted',compatible=True,reason='',summary='',imported=args[0] in installed)])
        if method=='LibraryImportSources':
            kind='comic' if current_kind[0]=='comic' else 'novel'
            if kind not in installed:installed.append(kind)
            return [dict(id='candidate',sourceKey=kind+'-1',name='测试来源',status='imported',message='')]
        if method in ('LibraryItem','LibraryRefreshReadingToc'):
            return dict(item=dict(id=1,kind='novel',sourceKey='novel-1',sourceName='测试来源',remoteId='book/1',title='测试书籍',author='',cover='',summary='',genres='',status='',latest='',words='',updatedAt='',available=True),chapters=[dict(id=101,remoteId='chapter/1',ord=0,title='第一章',isVolume=False,volume='',access='available',available=True)],chapterCount=1)
        if method=='LibraryChapterText':return dict(chapterId=101,paragraphs=['山川与河流之间，旅人开始新的旅途。'*15]*30,warnings=[],fromCache=False)
        return ''
    current_kind=['novel']
    try:
        for _ in range(100):
            try:
                urlopen('http://127.0.0.1:9303/tests/browser/reading.html',timeout=1).close();break
            except OSError:time.sleep(.1)
        names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
        mock="import {CancellablePromise} from '/tests/browser/runtime.ts';\n"
        mock+="function call(method,args){return new CancellablePromise((resolve,reject)=>{fetch('/__usability',{method:'POST',body:JSON.stringify({method,args})}).then(r=>r.json()).then(resolve,reject)})}\n"
        mock+='\n'.join('export function '+name+'(...args){return '+('call('+json.dumps(name)+',args)' if name.startswith(('Library','ReadingState')) else "CancellablePromise.resolve('')")+'}' for name in names)
        with sync_playwright() as p:
            browser=p.chromium.launch(headless=True,channel=args.channel)
            page=browser.new_page(viewport=dict(width=412,height=860))
            page.set_default_timeout(4000)
            page.on('pageerror',lambda e:errors.append(str(e)))
            page.route('**/bindings/cczjVideo/app/service/app*',lambda r:r.fulfill(body=mock,content_type='text/javascript'))
            page.route('**/src/api/runtime.ts',lambda r:r.fulfill(body="export const isMobileRuntime=()=>true;export const isDesktopRuntime=()=>false;export const initializeRuntimeEnvironment=async()=>{};export const reloadApplication=(url)=>location.replace(url);export const closeMobileApplication=()=>{};",content_type='text/javascript'))
            page.route('**/__usability',lambda r:r.fulfill(body=json.dumps(rpc(r.request.post_data_json['method'],r.request.post_data_json['args'])),content_type='application/json'))
            def navigate(path):
                page.evaluate('(path)=>location.hash=path',path)
                expect(page.locator('.cczj-mobile-route-scene[data-route-active=true]').last).to_have_attribute('data-path',path)
            page.goto('http://127.0.0.1:9303/tests/browser/reading.html?legacy-source-import#/novel/shelf');page.wait_for_load_state('networkidle')
            def check(name,work):
                try:work();results.append(dict(name=name,passed=True));print('PASS',name,flush=True)
                except Exception as e:results.append(dict(name=name,passed=False,error=str(e)));page.screenshot(path=str(output/(name+'.png')));print('FAIL',name,str(e)[:300],flush=True)
            def home():
                expect(page.locator('.novel-shelf').get_by_role('link',name='添加源',exact=True)).to_be_visible()
                navigate('/novel/sources')
                page.locator('.library-sources-tabs').get_by_role('button',name='添加源',exact=True).click()
                page.locator('.source-candidate input[type=checkbox]').check()
                page.get_by_role('button',name='导入所选 (1)',exact=True).click()
                expect(page.locator('.library-source')).to_have_count(1)
                navigate('/novel/shelf')
                expect(page.locator('.novel-shelf').get_by_role('link',name='添加源',exact=True)).to_have_count(0)
                expect(page.locator('.novel-shelf a[href="#/novel/search"]').first).to_be_visible()
            check('personal-shelf-refreshes-after-import',home)
            def exit_reader():
                navigate('/novel/read/1/101')
                expect(page.locator('.novel-reading-paragraph')).to_have_count(30)
                page.keyboard.press('ArrowRight')
                page.get_by_role('button',name='返回',exact=True).click(timeout=2000)
                expect(page).to_have_url(re.compile('/novel/book/1$'))
            check('exit-remains-clickable-after-page-turn',exit_reader)
            def simple_add():
                installed.clear();current_kind[0]='comic';navigate('/comic/sources')
                page.locator('.library-sources-tabs').get_by_role('button',name='添加源',exact=True).click()
                page.locator('.source-candidate').get_by_role('button',name='添加',exact=True).click()
                expect(page.locator('.library-source')).to_have_count(1)
            check('one-click-source-add',simple_add)
            def manage_source():
                page.locator('.library-sources-tabs').get_by_role('button',name='我的源 1',exact=True).click()
                expect(page.locator('.library-source')).to_be_visible()
                expect(page.locator('.library-source').get_by_role('button',name='停用',exact=True)).to_be_visible()
                expect(page.locator('.library-source').get_by_role('button',name='编辑',exact=True)).not_to_be_visible()
            check('management-separates-advanced-actions',manage_source)
            (output/'result.json').write_text(json.dumps(dict(passed=all(r['passed'] for r in results) and not errors,results=results,errors=errors),ensure_ascii=False,indent=2),encoding='utf-8')
            browser.close()
            if not all(r['passed'] for r in results) or errors:raise SystemExit(1)
    finally:
        server.terminate();server.wait(timeout=10)

if __name__=='__main__':main()
