"""Novel search/reader/catalog regressions through production Vue views and a deterministic RPC seam."""
import argparse
import json
import re
from pathlib import Path
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parent.parent

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port',type=int,default=19320)
    parser.add_argument('--output',type=Path,default=ROOT/'dist/audit/novel-usability/browser')
    args=parser.parse_args()
    args.output.mkdir(parents=True,exist_ok=True)
    calls=[]
    records={}
    pending=[]
    hold=[False]
    def item():
        return dict(id=1,kind='novel',sourceKey='fixture',sourceName='回归来源',remoteId='book',title='西游记',author='吴承恩',summary='目录与阅读交互验证',genres='古典文学',status='完结',cover='',latest='第80回 归途',words='',updatedAt='',available=True)
    def chapters():
        return [dict(id=100+i,remoteId=str(i),ord=i-1,title=f'第{i}回 山川行旅',isVolume=False,volume='',access='available',available=True) for i in range(1,81)]
    def rpc(method,params):
        calls.append(dict(method=method,args=params))
        if method=='ReadingStateLoad':
            if params[0] not in records:records[params[0]]={(r['bucket'],r['key']):r for r in json.loads(params[1])}
            return list(records[params[0]].values())
        if method=='ReadingStateApply':
            for row in json.loads(params[1]):records[params[0]][(row['bucket'],row['key'])]=row
            return None
        if method=='LibrarySources':return [dict(sourceKey='fixture',kind='novel',name='回归来源',apiUrl='https://example.org',enabled=True)]
        if method=='LibrarySearchStream':return dict(keyword='西游记',page=1,total=1,failedCount=0,searchedSources=5,pendingSources=0,nextCursor='',results=[dict(sourceKey='fixture',sourceName='回归来源',items=[item()],nextUrl='',hasNext=False)])
        if method in ('LibraryItem','LibraryRefreshToc','LibraryRefreshReadingToc'):return dict(item=item(),chapters=chapters(),chapterCount=80)
        if method in ('LibraryNovelContext','LibraryRefreshNovelContext'):
            pos=params[0];rows=chapters()
            current=next((row for row in rows if row['remoteId']==pos['remoteId']),None) if pos['remoteId'] else next((row for row in rows if row['id']==pos['chapterId']),rows[0] if pos['first'] else None)
            at=rows.index(current) if current else -1
            return dict(item=item(),chapter=current,previous=rows[at-1] if at>0 else None,next=rows[at+1] if at>=0 and at+1<len(rows) else None,index=at,count=80,cacheIdentity='fixture:'+str(current['id']) if current else '')
        if method=='LibraryChapterText':return dict(chapterId=params[0],paragraphs=[f'行旅第{i}段。'+('山川与河流之间，旅人走向远方，望见村庄与灯火。'*10) for i in range(30)],warnings=[],fromCache=True)
        if method in ('BackupArchives','BackupExports','DoubanGetAll','ListPlugins','RescanPlugins'):return []
        if method=='DoubanStatus':return dict(running=False)
        if method=='LegacyDataNotice':return {}
        if method=='GetAppVersion':return '2.4.0'
        if method=='WindowGetSize':return dict(width=1280,height=900)
        if method.startswith('WindowIs'):return False
        return ''
    names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf8'))
    mock="import {CancellablePromise} from '/tests/browser/runtime.ts';\nfunction call(method,args){return new CancellablePromise((resolve,reject)=>{fetch('/__novel_interaction',{method:'POST',body:JSON.stringify({method,args})}).then(r=>r.json()).then(resolve,reject)})}\n"
    mock+='\n'.join('export function '+name+'(...args){return call('+json.dumps(name)+',args)}' for name in names)
    errors=[]
    checks=[]
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True,channel='chrome')
        try:
            page=browser.new_page(viewport=dict(width=1280,height=900))
            page.on('pageerror',lambda e:errors.append(str(e)))
            page.set_default_timeout(6000)
            page.route('**/bindings/cczjVideo/app/service/app*',lambda r:r.fulfill(body=mock,content_type='text/javascript'))
            def handle(r):
                data=r.request.post_data_json
                if hold[0] and data['method']=='LibraryRefreshToc':pending.append(r);return
                r.fulfill(body=json.dumps(rpc(data['method'],data['args']),ensure_ascii=False),content_type='application/json')
            page.route('**/__novel_interaction',handle)
            def route(path):
                page.evaluate('(path)=>location.hash=path',path)
                expect(page.locator('.cczj-mobile-route-scene[data-route-active=true]').last).to_have_attribute('data-path',path)
            def check(name):checks.append(name);print('PASS',name,flush=True)
            page.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/novel/search?q=xiyouji')
            page.wait_for_load_state('networkidle')
            expect(page.locator('.novel-book-card')).to_contain_text('西游记')
            expect(page.locator('.novel-search-resolved')).to_contain_text('西游记')
            field=page.locator('.novel-search input')
            field.focus()
            assert field.evaluate('(el)=>getComputedStyle(el).outlineStyle')=='none'
            check('pinyin result relevance and single focus ring')
            count=lambda:len([c for c in calls if c['method']=='LibrarySearchStream'])
            searched=count()
            page.locator('.novel-book-card').click()
            expect(page.locator('.novel-book-hero')).to_be_visible()
            page.go_back()
            expect(page.locator('.novel-book-card')).to_be_visible()
            page.wait_for_timeout(450)
            assert count()==searched,'Back issued a second multi-source search'
            check('Back preserves the search result without network search')
            page.locator('.novel-book-card').click()
            page.get_by_role('tab',name='目录',exact=False).click()
            links=page.locator('.novel-chapter-list a')
            expect(links.first).to_contain_text('第1回')
            expect(links.first.locator('span')).to_have_count(0)
            page.get_by_role('button',name='倒序',exact=True).click()
            expect(links.first).to_contain_text('第80回')
            page.get_by_role('button',name='正序',exact=True).click()
            check('catalog sorts in both directions without duplicate chapter numbers')
            hold[0]=True
            page.get_by_role('button',name='刷新',exact=True).click()
            page.wait_for_timeout(120)
            expect(page.locator('.novel-book-hero')).to_be_visible()
            expect(links).to_have_count(80)
            assert pending,'refresh did not start'
            hold[0]=False
            for r in pending:r.fulfill(body=json.dumps(rpc('LibraryRefreshToc',[1]),ensure_ascii=False),content_type='application/json')
            pending.clear()
            expect(page.get_by_role('button',name='刷新',exact=True)).to_be_enabled()
            check('refresh leaves catalog and page mounted')
            main=page.locator('.main-content').last
            main.evaluate('(el)=>el.scrollTop=800')
            top=page.locator('.vine-wrap .flower-click-target')
            expect(top).to_be_visible()
            top.click()
            page.wait_for_function("document.querySelector('.main-content').scrollTop<2")
            check('shared BackToTop scrolls the active catalog')
            page.locator('.novel-detail-actions a').click()
            expect(page.locator('.novel-reading-paper h1')).to_contain_text('第1回')
            page.wait_for_timeout(350)
            assert 'novel-reader-paged' not in page.locator('.novel-reader').get_attribute('class')
            viewport=page.locator('.novel-page-viewport')
            full_count=lambda:sum(c['method']=='LibraryItem' for c in calls)
            full_before=full_count()
            viewport.hover();page.mouse.wheel(0,220)
            page.wait_for_function("document.querySelector('.novel-page-viewport').scrollTop>0")
            viewport.evaluate('(el)=>el.scrollTop=el.scrollHeight')
            page.wait_for_timeout(250);page.mouse.wheel(0,120)
            expect(page.locator('.novel-reading-paper h1')).to_contain_text('第2回')
            page.wait_for_timeout(350)
            text_count=lambda:sum(c['method']=='LibraryChapterText' and c['args'][0]==101 for c in calls)
            read_before=text_count()
            viewport.evaluate('(el)=>el.scrollTop=0');page.wait_for_timeout(250);page.mouse.wheel(0,-120)
            expect(page.locator('.novel-reading-paper h1')).to_contain_text('第1回')
            page.wait_for_timeout(350)
            assert text_count()==read_before,'backtracking re-requested cached text'
            assert full_count()==full_before,'chapter navigation requested the full catalog'
            check('default vertical scroll, wheel crosses chapters and backtracks from hot cache')
            page.keyboard.press('Escape')
            page.locator('.novel-reader-bottom').get_by_role('button',name='目录',exact=True).click()
            expect(page.locator('.novel-chapter-list li')).to_have_count(80)
            assert full_count()==full_before+1
            page.get_by_role('button',name='关闭',exact=True).click()
            page.locator('.novel-reader-bottom').get_by_role('button',name='目录',exact=True).click()
            expect(page.locator('.novel-chapter-list li')).to_have_count(80)
            assert full_count()==full_before+1
            page.get_by_role('button',name='关闭',exact=True).click()
            page.locator('.novel-reader-top').get_by_role('button',name='阅读设置').click()
            page.get_by_role('button',name='左右翻页',exact=True).click()
            page.get_by_role('button',name='关闭',exact=True).click()
            page.locator('.novel-reader-seek input').evaluate("el=>{el.value='0';el.dispatchEvent(new Event('input',{bubbles:true}))}")
            page.keyboard.press('Escape')
            check('directory is lazy and reused while horizontal paging remains selectable')
            status=page.locator('.novel-reading-status')
            def first():return status.inner_text().splitlines()[1].strip().startswith('1 /')
            def state():return page.locator('.novel-pagination-track').get_attribute('style')
            before=state()
            page.keyboard.press('ArrowRight')
            page.wait_for_function("document.querySelector('.novel-pagination-track').style.transform!=='translate3d(0px, 0px, 0px)'")
            assert state()!=before
            assert page.locator('.novel-pagination-track').evaluate('(el)=>getComputedStyle(el).transitionDuration')=='0s'
            page.keyboard.press('ArrowLeft')
            page.wait_for_timeout(100)
            assert first(),status.inner_text()
            page.locator('.novel-page-viewport').hover()
            page.mouse.wheel(0,120)
            page.wait_for_timeout(100)
            assert not first(),status.inner_text()
            check('keyboard and wheel turn real measured pages without animation')
            page.keyboard.press('Escape')
            page.locator('.novel-reader-top').get_by_role('button',name='阅读设置').click()
            expect(page.locator('.reader-sheet')).to_be_visible()
            page.get_by_role('button',name='下一页按键',exact=True).click()
            page.keyboard.press('KeyD')
            expect(page.get_by_role('button',name='下一页按键',exact=True)).to_contain_text('D')
            page.get_by_role('button',name='关闭',exact=True).click()
            before=state()
            page.keyboard.press('KeyD')
            page.wait_for_timeout(100)
            assert state()!=before
            check('custom key binding persists and turns pages')
            page.get_by_role('button',name='沉浸阅读（F）',exact=True).click()
            expect(page.locator('.novel-reader')).to_have_class(re.compile('novel-reader-immersive'))
            assert page.evaluate('!!document.fullscreenElement')
            page.keyboard.press('Escape')
            page.wait_for_timeout(100)
            assert not page.evaluate('!!document.fullscreenElement')
            check('immersive reading enters and exits actual browser fullscreen')
            page.evaluate('window.__readerBefore=document.querySelector(".novel-reader")')
            route('/novel/read/1/102')
            expect(page.locator('.novel-reading-paper h1')).to_contain_text('第2回')
            assert page.evaluate('window.__readerBefore===document.querySelector(".novel-reader")'),'chapter change recreated reader'
            assert not page.locator('[class*=cczj-motion-page-enter]').count()
            check('chapter change preserves reader node and skips route fade')
            page.screenshot(path=str(args.output/'reader-desktop.png'))
            # A narrow viewport exercises long toolbar labels and pagination geometry.
            page.set_viewport_size(dict(width=375,height=812))
            route('/novel/book/1')
            expect(page.locator('.novel-directory-tools')).to_be_visible()
            assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
            page.screenshot(path=str(args.output/'catalog-mobile.png'))
            page.set_viewport_size(dict(width=1280,height=900))
            page.screenshot(path=str(args.output/'catalog-desktop.png'))
            check('desktop and narrow catalog stay within viewport')
            # Native-mobile presentation is chosen by runtime, independently of width.
            records['novel']={k:v for k,v in records['novel'].items() if k[0]!='preferences'}
            mobile=browser.new_page(viewport=dict(width=375,height=812),is_mobile=True,has_touch=True)
            mobile.on('pageerror',lambda e:errors.append(str(e)))
            mobile.route('**/bindings/cczjVideo/app/service/app*',lambda r:r.fulfill(body=mock,content_type='text/javascript'))
            mobile.route('**/__novel_interaction',handle)
            mobile.route('**/src/api/runtime.ts',lambda r:r.fulfill(body="export const Window={};export const Dialogs={};export const isMobileRuntime=()=>true;export const isDesktopRuntime=()=>false;export const initializeRuntimeEnvironment=async()=>{};export const reloadApplication=url=>location.replace(url);export const closeMobileApplication=()=>{};",content_type='text/javascript'))
            mobile.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/novel/search?q=xiyouji')
            expect(mobile.locator('.novel-book-card')).to_be_visible()
            searched=count()
            mobile.locator('.novel-book-card').click()
            expect(mobile.locator('.novel-book-hero')).to_be_visible()
            mobile.go_back()
            expect(mobile.locator('.novel-book-card')).to_be_visible()
            mobile.wait_for_timeout(400)
            assert count()==searched
            mobile.locator('.novel-book-card').click()
            mobile.locator('.novel-detail-actions a').click()
            expect(mobile.locator('.novel-reader')).to_be_visible()
            expect(mobile.locator('.novel-reader-immersion')).to_have_count(0)
            mobile.wait_for_timeout(500)
            viewport=mobile.locator('.novel-page-viewport')
            assert 'novel-reader-paged' not in mobile.locator('.novel-reader').get_attribute('class')
            def swipe(y1,y2):
                bounds=viewport.bounding_box();x=bounds['x']+bounds['width']/2
                client=mobile.context.new_cdp_session(mobile)
                client.send('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[dict(x=x,y=y1)]))
                for step in range(1,9):client.send('Input.dispatchTouchEvent',dict(type='touchMove',touchPoints=[dict(x=x,y=y1+(y2-y1)*step/8)]))
                client.send('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]));client.detach()
            mobile.evaluate('location.hash="/novel/read/1/101"')
            expect(mobile.locator('.novel-reading-paper h1')).to_contain_text('第1回')
            viewport.evaluate('(el)=>el.scrollTop=el.scrollHeight');mobile.wait_for_timeout(300)
            swipe(650,350)
            expect(mobile.locator('.novel-reading-paper h1')).to_contain_text('第2回')
            mobile.wait_for_timeout(300);viewport.evaluate('(el)=>el.scrollTop=0')
            swipe(350,650)
            expect(mobile.locator('.novel-reading-paper h1')).to_contain_text('第1回')
            mobile.wait_for_timeout(300)
            check('native touch events cross both chapter boundaries in the mobile browser')
            box=viewport.bounding_box()
            viewport.tap(position=dict(x=box['width']/2,y=box['height']/2))
            mobile.locator('.novel-reader-top').get_by_role('button',name='阅读设置').click()
            expect(mobile.get_by_role('button',name='下一页按键',exact=True)).to_have_count(0)
            mobile.screenshot(path=str(args.output/'reader-mobile-preferences.png'))
            mobile.get_by_role('button',name='关闭',exact=True).click()
            mobile.evaluate('location.hash="/novel/read/1/103"')
            expect(mobile.locator('.novel-reading-paper h1')).to_contain_text('第3回')
            assert mobile.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
            mobile.screenshot(path=str(args.output/'reader-mobile.png'))
            check('mobile presentation preserves search and excludes desktop-only controls')
            mobile.close()
            # The actual advanced settings group uses the same shared API seam.
            page.goto(f'http://127.0.0.1:{args.port}/tests/browser/settings.html')
            page.get_by_role('tab',name='高级',exact=True).click()
            expect(page.locator('.cczj-network-policy')).to_be_visible()
            assert page.locator('.cczj-network-policy select').count()==0
            expect(page.locator('.cczj-network-policy .select-dropdown')).to_be_visible()
            page.screenshot(path=str(args.output/'advanced-desktop.png'))
            page.set_viewport_size(dict(width=375,height=812))
            page.route('**/src/api/runtime.ts',lambda r:r.fulfill(body="export const Window={};export const Dialogs={};export const isMobileRuntime=()=>true;export const isDesktopRuntime=()=>false;export const initializeRuntimeEnvironment=async()=>{};export const reloadApplication=url=>location.replace(url);export const closeMobileApplication=()=>{};",content_type='text/javascript'))
            page.goto(f'http://127.0.0.1:{args.port}/tests/browser/settings.html?mobile')
            expect(page.locator('.cczj-network-policy')).to_be_visible()
            page.wait_for_function("!document.querySelector('.cczj-mobile-route-stage [class*=enter-active],.cczj-mobile-route-stage [class*=leave-active]')")
            page.wait_for_timeout(150)
            assert page.locator('.cczj-mobile-route-viewport').evaluate('(el)=>el.clientHeight>500')
            assert page.locator('.cczj-network-policy').evaluate('(el)=>el.getBoundingClientRect().width>250')
            assert page.locator('.cczj-network-policy').evaluate('(el)=>{const r=el.getBoundingClientRect();return r.top>=0&&r.top<innerHeight&&r.bottom>0}')
            assert page.locator('.advanced-settings').evaluate('(el)=>el.scrollWidth<=el.clientWidth+1')
            page.screenshot(path=str(args.output/'advanced-narrow.png'))
            check('advanced group uses opaque cards and shared Select at both widths')
            assert not errors,errors
            (args.output/'results.json').write_text(json.dumps(dict(checks=checks,errors=errors,scope='production Vue, deterministic RPC; browser fullscreen; not native host'),ensure_ascii=False,indent=2),encoding='utf8')
        except Exception:
            for index,active in enumerate(browser.contexts):
                for n,tab in enumerate(active.pages):
                    try:
                        tab.screenshot(path=str(args.output/f'failure-{index}-{n}.png'))
                        (args.output/f'failure-{index}-{n}.html').write_text(tab.content(),encoding='utf8')
                    except Exception:pass
            raise
        finally:
            for r in pending:
                try:r.abort()
                except Exception:pass
            browser.close()

if __name__=='__main__':main()
