"""Compatibility source import and live reading against production Go services
in temporary SQLite. The old import panel is mounted explicitly by the harness;
current builtin directory routes are covered by test-reading-routing.py.
Wails transport is replaced; native UI is not tested.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import time
from urllib.request import Request, urlopen
from urllib.error import HTTPError
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parent.parent

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--channel',default='chrome')
    parser.add_argument('--output',type=Path,default=ROOT/'reports/reading-regression-2026-10-21')
    args=parser.parse_args()
    output=args.output
    output.mkdir(parents=True,exist_ok=True)
    flags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0
    binary=ROOT/'dist/reading-browser-host.exe'
    subprocess.run(['go','build','-o',str(binary),'./scripts/reading-browser-host'],cwd=ROOT,check=True,creationflags=flags)
    backend=subprocess.Popen([str(binary),'--port','9296'],cwd=ROOT,creationflags=flags,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    vite=subprocess.Popen(['node','node_modules/vite/bin/vite.js','--host','127.0.0.1','--port','9297','--strictPort'],cwd=ROOT/'frontend',creationflags=flags,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    calls=[]
    failures=[]

    def rpc(method,params):
        calls.append(method)
        req=Request('http://127.0.0.1:9296/rpc',json.dumps(dict(method=method,args=params)).encode(),{'Content-Type':'application/json'})
        with urlopen(req,timeout=100) as response:
            return json.load(response)

    try:
        for endpoint in ['http://127.0.0.1:9296/health','http://127.0.0.1:9297/tests/browser/reading.html']:
            for attempt in range(150):
                try:
                    with urlopen(endpoint,timeout=1):break
                except OSError:time.sleep(.2)
            else:raise RuntimeError('Test server did not start: '+endpoint)
        names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
        mock="import {CancellablePromise} from '/tests/browser/runtime.ts';\n"
        mock+='''function invoke(method,args){const controller=new AbortController();return new CancellablePromise((resolve,reject)=>{
fetch('/__live_reading',{method:'POST',body:JSON.stringify({method,args}),signal:controller.signal}).then(async response=>{const data=await response.json();if(!response.ok)throw Object.assign(new Error(data.message),{code:data.code});return data}).then(resolve,error=>{if(!controller.signal.aborted)reject(error)});
},()=>controller.abort());}\n'''
        mock+='\n'.join(f'export function {name}(...args){{return '+(f'invoke({json.dumps(name)},args)' if name.startswith('Library') or name.startswith('ReadingState') else "CancellablePromise.resolve('')")+';}' for name in names)
        with sync_playwright() as p:
            browser=p.chromium.launch(headless=True,channel=args.channel)
            try:
                page=browser.new_page(viewport={'width':1280,'height':900})
                errors=[]
                page.on('pageerror',lambda error:errors.append(str(error)))
                page.route('**/bindings/cczjVideo/app/service/app*',lambda route:route.fulfill(body=mock,content_type='text/javascript'))
                def bridge(route):
                    call=route.request.post_data_json
                    try:route.fulfill(body=json.dumps(rpc(call['method'],call['args'])),content_type='application/json')
                    except HTTPError as error:
                        body=error.read()
                        failures.append({'method':call['method'],'response':body.decode(errors='replace')})
                        route.fulfill(status=error.code,body=body,content_type='application/json')
                page.route('**/__live_reading',bridge)
                def visit(path):
                    page.goto('http://127.0.0.1:9297/tests/browser/reading.html?legacy-source-import#'+path)
                    page.wait_for_load_state('networkidle')
                    expect(page.locator('.cczj-mobile-route-scene[data-route-active=true]').last).to_have_attribute('data-path',path.split('?')[0],timeout=10000)
                visit('/novel/sources')
                page.locator('.library-sources-tabs').get_by_role('button',name='添加源',exact=True).click()
                expected_novel={row['id'] for row in rpc('LibraryPreviewSources',['novel','',''])['candidates'] if row['compatible']}
                novel_count=len(expected_novel)
                expect(page.locator('.source-candidate')).to_have_count(novel_count)
                assert set(page.locator('.source-candidate').evaluate_all('(rows)=>rows.map(row=>row.dataset.sourceId)'))==expected_novel, 'visible import candidates differ from the compatible backend catalog'
                # Shipped observations expire after 24 hours; browsing them is not a fresh live test.
                shipped=next(row for row in json.loads((ROOT/'app/library/source_catalog.json').read_text(encoding='utf-8')) if row['name']=='酷我小说')
                quality=shipped['quality']
                expected_status='stale' if time.time()-quality['checkedAt']>24*3600 else quality['status']
                expect(page.locator('.source-candidate').filter(has_text='酷我小说')).to_have_attribute('data-status',expected_status)
                fq=page.locator('.source-candidate').filter(has_text='番茄小说')
                expect(fq.locator('input[type=checkbox]')).to_be_enabled()
                fq.get_by_role('button',name='检测详情',exact=True).click()
                expect(fq).to_contain_text('内置适配')
                page.get_by_role('button',name='更多小说源',exact=True).click()
                community_count=len(rpc('LibraryPreviewSources',['novel','community',''])['candidates'])
                expect(page.locator('.source-catalog-selection')).to_contain_text(re.compile(r'\b'+str(community_count)+r'\b'),timeout=15000)
                page.locator('.source-catalog-search input').fill('69书吧')
                blocked=page.locator('.source-candidate').filter(has_text='69书吧')
                expect(blocked.first.locator('input[type=checkbox]')).to_be_disabled()
                page.get_by_role('button',name='已适配源',exact=True).click()
                expect(page.locator('.source-candidate')).to_have_count(novel_count)
                kuwo=page.locator('.source-candidate').filter(has_text='酷我小说')
                kuwo.locator('input[type=checkbox]').check()
                page.get_by_role('button',name='导入所选 (1)',exact=True).click()
                expect(page.locator('.library-sources-list .library-source')).to_have_count(1)
                expect(page.locator('.library-sources-list')).to_contain_text('酷我小说')
                installed=rpc('LibrarySources',['novel'])
                assert len(installed)==1 and installed[0]['name']=='酷我小说',installed
                # A local test supersedes the shipped observation and reranks.
                page.locator('.library-sources-tabs').get_by_role('button',name='添加源',exact=True).click()
                kuwo.get_by_role('button',name='检测详情',exact=True).click()
                kuwo.get_by_role('button',name='检测链路',exact=True).click()
                expect(kuwo).to_contain_text('本机检测结果',timeout=100000)
                page.locator('.source-catalog-heading').scroll_into_view_if_needed()
                page.screenshot(path=str(output/'novel-sources-desktop.png'),full_page=True)
                visit('/novel/search')
                page.locator('.novel-search input[type=search]').fill('三国')
                page.locator('.novel-search input[type=search]').press('Enter')
                expect(page.locator('.novel-book-card').first).to_be_visible(timeout=30000)
                page.locator('.novel-book-card').first.click()
                expect(page.locator('.novel-detail-actions a')).to_be_visible(timeout=100000)
                page.locator('.novel-detail-actions a').click()
                expect(page.locator('.novel-reading-paragraph').first).to_be_visible(timeout=30000)
                assert len(page.locator('.novel-reading-paper').inner_text())>500
                page.wait_for_load_state('networkidle')
                expect(page.locator('.novel-storage-error')).to_have_count(0)
                page.screenshot(path=str(output/'novel-real-reader.png'),full_page=True)
                visit('/novel/sources')
                page.locator('.library-sources-tabs').get_by_role('button',name='添加源',exact=True).click()
                fanqie=page.locator('.source-candidate').filter(has_text='番茄小说')
                fanqie.locator('input[type=checkbox]').check()
                page.get_by_role('button',name='导入所选 (1)',exact=True).click()
                expect(page.locator('.library-sources-list .library-source')).to_have_count(2)
                rpc('LibrarySetSourceEnabled',[installed[0]['sourceKey'],False])
                visit('/novel/search')
                page.locator('.novel-search input[type=search]').fill('部族荣光')
                page.locator('.novel-search input[type=search]').press('Enter')
                expect(page.locator('.novel-book-card').first).to_be_visible(timeout=30000)
                page.locator('.novel-book-card').first.click()
                expect(page.locator('.novel-detail-actions a')).to_be_visible(timeout=30000)
                page.locator('.novel-detail-actions a').click()
                expect(page.locator('.novel-reading-paragraph').first).to_contain_text('大乾皇朝',timeout=30000)
                assert not page.locator('.novel-reading-paper').evaluate('(element)=>/[\uE000-\uF8FF]/.test(element.innerText)'), 'unmapped private-use glyph'
                page.wait_for_load_state('networkidle')
                expect(page.locator('.novel-storage-error')).to_have_count(0)
                page.screenshot(path=str(output/'fanqie-real-reader.png'),full_page=True)
                visit('/comic/sources')
                page.locator('.library-sources-tabs').get_by_role('button',name='添加源',exact=True).click()
                expect(page.locator('.source-candidate')).to_have_count(4)
                guazi=page.locator('.source-candidate').filter(has_text='瓜子漫画')
                guazi.locator('input[type=checkbox]').check()
                page.get_by_role('button',name='导入所选 (1)',exact=True).click()
                expect(page.locator('.library-sources-list .library-source')).to_have_count(1)
                assert len(rpc('LibrarySources',['comic']))==1
                page.set_viewport_size({'width':360,'height':800})
                page.locator('.library-sources-tabs').get_by_role('button',name='添加源',exact=True).click()
                page.locator('.source-catalog-heading').scroll_into_view_if_needed()
                page.screenshot(path=str(output/'comic-sources-mobile.png'),full_page=True)
                assert page.evaluate('document.documentElement.scrollWidth<=innerWidth'), 'horizontal overflow'
                for width in [320,412,768]:
                    page.set_viewport_size({'width':width,'height':900})
                    assert page.evaluate('document.documentElement.scrollWidth<=innerWidth'),f'horizontal overflow at {width}'
                page.set_viewport_size({'width':360,'height':800})
                # Previewing a file does not install its other candidates.
                page.get_by_role('button',name='链接 / 文件导入',exact=True).click()
                page.locator('input[type=file]').set_input_files(str(ROOT/'app/library/source_catalog.json'))
                expect(page.locator('.source-candidate')).to_have_count(4)
                assert len(rpc('LibrarySources',['comic']))==1
                visit('/comic/search?q=斗罗')
                expect(page.locator('.comic-card').first).to_be_visible(timeout=100000)
                page.locator('.comic-card').first.click()
                expect(page.locator('.comic-detail-actions a')).to_be_visible(timeout=100000)
                page.locator('.comic-detail-actions a').click()
                expect(page.locator('.comic-panel img').first).to_be_visible(timeout=100000)
                page.wait_for_function('Array.from(document.querySelectorAll(".comic-panel img")).some(image=>image.naturalWidth>0)',timeout=100000)
                page.wait_for_load_state('networkidle')
                expect(page.locator('.comic-error')).to_have_count(0)
                for attempt in range(100):
                    comic_records=rpc('ReadingStateLoad',['comic','[]'])
                    if any(row['bucket']=='progress' for row in comic_records):break
                    # Pump Playwright events while the reader's 500 ms save
                    # debounce and image layout settle, then inspect actual SQLite.
                    page.wait_for_timeout(100)
                assert any(row['bucket']=='progress' for row in comic_records),comic_records
                page.screenshot(path=str(output/'comic-real-reader.png'),full_page=True)
                assert not errors,errors
                assert not failures,failures
                result={'passed':True,'native':False,'transport':'Wails transport replacement with production Go services','sqlite':'isolated temporary database','calls':calls,'runtime_errors':errors}
                (output/'result.json').write_text(json.dumps(result,indent=2),encoding='utf-8')
                print('Live source browser checks passed: ranking, selection, unsupported states, import, local check, real novel text, real comic decode and mobile layout.')
            finally:
                if 'page' in locals() and not page.is_closed():
                    page.screenshot(path=str(output/'last-state.png'),full_page=True)
                    (output/'last-context.json').write_text(json.dumps({'calls':calls,'failures':failures,'errors':errors,'url':page.url,'text':page.locator('body').inner_text()[:3500]},ensure_ascii=False,indent=2),encoding='utf-8')
                browser.close()
    finally:
        for process in [vite,backend]:
            process.terminate()
            try:process.wait(timeout=10)
            except subprocess.TimeoutExpired:process.kill();process.wait(timeout=10)

if __name__=='__main__':main()
