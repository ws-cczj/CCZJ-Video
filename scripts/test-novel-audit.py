"""Novel interaction regression checks using real Vue views and deterministic I/O.

No user's database is opened. Network source tests are run separately.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import time
from urllib.request import urlopen

from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--channel', default='msedge')
    parser.add_argument('--port', type=int, default=9299)
    args = parser.parse_args()
    output = ROOT / 'reports/novel-audit-2026-10-19'
    output.mkdir(parents=True, exist_ok=True)
    origin = f'http://127.0.0.1:{args.port}'
    flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
    server = subprocess.Popen(['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', str(args.port), '--strictPort'], cwd=ROOT/'frontend', stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=flags)
    records, calls, results, errors, held_search = {}, [], [], [], []
    books = [dict(id=n, kind='novel', sourceKey='audit', sourceName='Audit source', remoteId=f'book/{n}', title=f'审查书籍 {n}', author='作者', cover='', summary='用于复现交互问题的确定性多章长正文。', genres='玄幻', status='连载', latest='', words='', updatedAt='', available=True) for n in [1, 2]]
    paragraphs = [f'第 {n} 段。山川与河流之间，旅人开始新的旅途。' * 6 for n in range(1, 71)]

    def chapters(book):
        return [dict(id=book*100+n, remoteId=f'chapter/{n}', ord=n-1, title=f'第{n}章 正文', volume='', isVolume=False, access='available', available=True) for n in range(1, 5)]

    def rpc(method, params):
        calls.append(dict(method=method, params=params))
        if method == 'ReadingStateLoad':
            kind, legacy = params
            if kind not in records:
                records[kind] = {(r['bucket'], r['key']): r for r in json.loads(legacy)}
            return list(records[kind].values())
        if method == 'ReadingStateApply':
            kind, changes = params
            for row in json.loads(changes):
                key = (row['bucket'], row['key'])
                if row.get('delete'):
                    records[kind].pop(key, None)
                else:
                    records[kind][key] = row
            return None
        if method in ('LibraryItem', 'LibraryRefreshToc', 'LibraryRefreshReadingToc'):
            book = int(params[0])
            return dict(item=books[book-1], chapters=chapters(book), chapterCount=4)
        if method == 'LibraryChapterText':
            return dict(chapterId=params[0], paragraphs=paragraphs, warnings=[], fromCache=False)
        if method == 'LibrarySources':
            return [dict(sourceKey='audit', kind='novel', name='Audit source', apiUrl='https://example.org', enabled=True, itemCount=2)]
        if method == 'LibraryCatalogPage':
            return dict(items=books, genres=['玄幻'], nextCursor='')
        if method == 'LibraryResolveRefs':
            return [next((book for book in books if book['remoteId'] == ref['remoteId']), None) for ref in params[0]]
        if method in ('LibrarySearchStream', 'LibrarySearchPage', 'LibrarySearch'):
            keyword = params[1]
            if params[2] and params[2] != 'audit:' + keyword:
                raise RuntimeError('搜索条件已变化，请重新搜索')
            return dict(keyword=keyword, page=1, results=[dict(sourceKey='audit', sourceName='Audit source', items=books[:1] if not params[2] else books, nextUrl='', hasNext=not bool(params[2]), errorCode='', errorMessage='')], total=1, failedCount=0, nextCursor='' if params[2] else 'audit:'+keyword)
        return ''

    try:
        for _ in range(100):
            try:
                with urlopen(origin+'/tests/browser/reading.html', timeout=1):
                    break
            except OSError:
                time.sleep(.1)
        names = re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
        mock = "import {CancellablePromise} from '/tests/browser/runtime.ts';\n"
        mock += """function invoke(method,args){const controller=new AbortController();return new CancellablePromise((resolve,reject)=>{fetch('/__novel_audit',{method:'POST',body:JSON.stringify({method,args}),signal:controller.signal}).then(async r=>{const data=await r.json();if(!r.ok)throw Object.assign(new Error(data.message),{code:'CONFLICT'});return data}).then(resolve,error=>{if(!controller.signal.aborted)reject(error)})},()=>controller.abort())}\n"""
        mock += '\n'.join(f'export function {name}(...args){{return '+(f'invoke({json.dumps(name)},args)' if name.startswith(('Library', 'ReadingState')) else "CancellablePromise.resolve('')")+';}' for name in names)
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True, channel=args.channel)
            page = browser.new_page(viewport=dict(width=412, height=860))
            page.set_default_timeout(5000)
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))

            def handle(route):
                data = route.request.post_data_json
                if data['method'].startswith('LibrarySearch') and data['args'][1]=='迟到搜索':
                    held_search.append((route,data))
                    return
                try:
                    route.fulfill(body=json.dumps(rpc(data['method'], data['args'])), content_type='application/json')
                except RuntimeError as error:
                    route.fulfill(status=409, json=dict(message=str(error)))
            page.route('**/__novel_audit', handle)

            def visit(path, expected=None):
                page.goto(origin+'/tests/browser/reading.html#'+path)
                page.wait_for_load_state('networkidle')
                expect(page.locator('.cczj-mobile-route-scene[data-route-active=true]').last).to_have_attribute('data-path', expected or path.split('?')[0])

            def reader(chapter=101):
                visit(f'/novel/read/1/{chapter}')
                expect(page.locator('.novel-reading-paragraph')).to_have_count(70)
                page.wait_for_timeout(100)

            def progress():
                row = records.get('novel', {}).get(('progress', 'novel\0audit\0book/1'))
                return json.loads(row['value']) if row else None

            def check(name, test):
                try:
                    detail = test()
                    results.append(dict(name=name, passed=True, detail=detail))
                    print('PASS', name, detail or '', flush=True)
                except Exception as error:
                    results.append(dict(name=name, passed=False, error=str(error)))
                    page.screenshot(path=str(output/(name+'.png')), full_page=True)
                    print('FAIL', name, str(error)[:700], flush=True)

            def resize():
                reader()
                def geometry():
                    return page.evaluate('''()=>{const v=document.querySelector('.novel-page-viewport'),b=document.querySelector('.novel-pagination-track');return {actual:v.clientWidth,column:parseFloat(b.style.columnWidth),height:v.clientHeight,count:Math.round((b.scrollWidth+40)/(v.clientWidth+40))}}''')
                before = geometry()
                page.set_viewport_size(dict(width=860, height=412))
                page.wait_for_timeout(300)
                after = geometry()
                assert abs(after['actual']-after['column']) <= 1, dict(before=before, after=after)
                page.set_viewport_size(dict(width=412, height=860))
                return dict(before=before, after=after)
            check('resize-repaginates', resize)

            def paged_preferences():
                page.set_viewport_size(dict(width=412, height=860))
                reader()
                page.locator('.novel-page-viewport').click(position=dict(x=180,y=140))
                page.locator('.novel-reader-seek input').evaluate("(el)=>{el.value='550';el.dispatchEvent(new Event('input',{bubbles:true}))}")
                page.wait_for_timeout(700)
                before=progress()['ratio']
                page.locator('.novel-reader-bottom nav button').last.click()
                for _ in range(4):
                    page.get_by_role('button',name='放大正文字号',exact=True).click()
                page.wait_for_timeout(150)
                page.get_by_role('button',name='关闭',exact=True).click()
                page.wait_for_timeout(600)
                after=progress()['ratio']
                assert abs(before-after)<.06,dict(before=before,after=after)
                page.screenshot(path=str(output/'reader-paged.png'))
                page.reload()
                expect(page.locator('.novel-reading-paragraph')).to_have_count(70)
                page.wait_for_timeout(100)
                assert abs(progress()['ratio']-after)<.03,progress()
                return dict(before=before,after=after)
            check('font-change-and-paged-resume', paged_preferences)

            def switch_flow():
                page.set_viewport_size(dict(width=412, height=860))
                reader()
                page.locator('.novel-page-viewport').click(position=dict(x=180,y=140))
                page.locator('.novel-reader-bottom nav button').last.click()
                page.get_by_role('button',name='上下滚动',exact=True).click()
                page.get_by_role('button',name='关闭',exact=True).click()
                page.wait_for_timeout(100)
                width = page.locator('.novel-reading-paper').evaluate('(el)=>getComputedStyle(el).columnWidth')
                assert width=='auto', width
            check('scroll-flow-clears-columns', switch_flow)

            def restore_scroll():
                records['novel'][('preferences','default')] = dict(bucket='preferences',key='default',value=json.dumps(dict(flow='scroll')))
                records['novel'][('progress','novel\0audit\0book/1')] = dict(bucket='progress',key='novel\0audit\0book/1',value=json.dumps(dict(itemId=1,title='审查书籍 1',chapterId=101,chapterRemoteId='chapter/1',chapterOrd=0,ratio=.65,updatedAt=1)))
                page.reload()
                reader()
                ratio = page.locator('.novel-page-viewport').evaluate('(v)=>v.scrollTop/(v.scrollHeight-v.clientHeight)')
                assert abs(ratio-.65) < .02, ratio
                return ratio
            check('scroll-progress-restores', restore_scroll)

            def scroll_chapter():
                reader()
                page.locator('.novel-page-viewport').evaluate('(v)=>{v.scrollTop=v.scrollHeight}')
                page.wait_for_timeout(80)
                page.keyboard.press('ArrowRight')
                expect(page).to_have_url(re.compile(r'/novel/read/1/102$'), timeout=2500)
            check('scroll-next-chapter', scroll_chapter)

            def scroll_resize():
                reader()
                page.locator('.novel-page-viewport').evaluate('(v)=>{v.scrollTop=(v.scrollHeight-v.clientHeight)*.6}')
                page.wait_for_timeout(100)
                page.set_viewport_size(dict(width=320, height=640))
                page.wait_for_timeout(300)
                ratio = page.locator('.novel-page-viewport').evaluate('(v)=>v.scrollTop/(v.scrollHeight-v.clientHeight)')
                assert abs(ratio-.6)<.03, ratio
                page.set_viewport_size(dict(width=412, height=860))
                return ratio
            check('scroll-resize-keeps-place', scroll_resize)

            def bookmark():
                reader(102)
                page.locator('.novel-page-viewport').evaluate('(v)=>{v.scrollTop=(v.scrollHeight-v.clientHeight)*.4}')
                page.wait_for_timeout(700)
                visit('/novel/book/1')
                link = page.locator('.novel-detail-actions a')
                expect(link).to_have_attribute('href', '#/novel/read/1/102')
                link.click()
                expect(page.locator('.novel-reading-paper h1')).to_have_text('第2章 正文')
                page.wait_for_timeout(100)
                ratio = page.locator('.novel-page-viewport').evaluate('(v)=>v.scrollTop/(v.scrollHeight-v.clientHeight)')
                assert abs(ratio-.4)<.03, dict(ratio=ratio, saved=progress())
            check('book-continues-chapter-and-position', bookmark)

            def one_shot_end():
                visit('/novel/read/1/101?position=end')
                expect(page.locator('.novel-reading-paragraph')).to_have_count(70)
                page.wait_for_timeout(100)
                page.locator('.novel-page-viewport').evaluate('(v)=>{v.scrollTop=(v.scrollHeight-v.clientHeight)*.3}')
                page.wait_for_timeout(700)
                assert abs(progress()['ratio']-.3)<.02, progress()
                page.reload()
                expect(page.locator('.novel-reading-paragraph')).to_have_count(70)
                page.wait_for_timeout(100)
                ratio = page.locator('.novel-page-viewport').evaluate('(v)=>v.scrollTop/(v.scrollHeight-v.clientHeight)')
                assert abs(ratio-.3)<.03, ratio
            check('previous-chapter-end-is-one-shot', one_shot_end)

            def portable_progress():
                records['novel'][('progress','novel\0audit\0book/1')] = dict(bucket='progress',key='novel\0audit\0book/1',value=json.dumps(dict(itemId=1,title='审查书籍 1',chapterId=0,chapterRemoteId='chapter/2',chapterOrd=1,ratio=.45,updatedAt=1)))
                page.reload()
                visit('/novel/book/1')
                expect(page.locator('.novel-detail-actions a')).to_have_attribute('href','#/novel/read/1/102')
                visit('/novel/read/1/0',expected='/novel/read/1/102')
                expect(page.locator('.novel-reading-paper h1')).to_have_text('第2章 正文')
                page.wait_for_timeout(100)
                ratio = page.locator('.novel-page-viewport').evaluate('(v)=>v.scrollTop/(v.scrollHeight-v.clientHeight)')
                assert abs(ratio-.45)<.03, ratio
                expect(page).to_have_url(re.compile(r'/novel/read/1/102$'))
                page.reload()
                expect(page.locator('.novel-reading-paper h1')).to_have_text('第2章 正文')
            check('portable-progress-resolves-chapter', portable_progress)

            def missing_progress():
                key='novel\0audit\0book/1'
                records['novel'][('progress',key)]=dict(bucket='progress',key=key,value=json.dumps(dict(itemId=1,title='审查书籍 1',chapterId=101,chapterRemoteId='removed',chapterOrd=0,ratio=.45,updatedAt=1)))
                page.reload()
                visit('/novel/book/1')
                page.locator('.novel-detail-actions a').click()
                expect(page.locator('.novel-reading-paragraph')).to_have_count(0)
                expect(page.locator('.novel-empty h1')).to_have_text('这一章暂时找不到了')
                assert progress()['chapterRemoteId']=='removed',progress()
            check('missing-saved-chapter-never-restarts', missing_progress)

            def search_query():
                visit('/novel/search')
                field = page.locator('.novel-search input')
                field.fill('山川');field.press('Enter')
                expect(page.locator('.novel-book-card')).to_have_count(1)
                field.fill('河流')
                page.locator('.novel-load-more button').click()
                expect(page.locator('.novel-book-card')).to_have_count(2)
                assert calls[-1]['params'][1]=='山川', calls[-1]
            check('search-continuation-uses-submitted-query', search_query)
            def supersede_search():
                visit('/novel/search')
                field=page.locator('.novel-search input')
                field.fill('迟到搜索');field.press('Enter')
                page.wait_for_timeout(100)
                assert held_search,'slow search was not started'
                field.fill('新搜索');field.press('Enter')
                expect(page.locator('.novel-book-card')).to_have_count(1)
                assert calls[-1]['params'][1]=='新搜索',calls[-1]
                old_route,data=held_search.pop()
                old_route.fulfill(body=json.dumps(rpc(data['method'],data['args'])),content_type='application/json')
                page.wait_for_timeout(100)
                expect(page.locator('.novel-book-card')).to_have_count(1)
                assert field.input_value()=='新搜索'
            check('new-query-cancels-slow-search', supersede_search)
            check('no-unhandled-errors', lambda: (_ for _ in ()).throw(AssertionError(errors)) if errors else None)
            (output/'result.json').write_text(json.dumps(dict(passed=all(r['passed'] for r in results) and not errors, results=results, errors=errors, native=False), ensure_ascii=False, indent=2), encoding='utf-8')
            browser.close()
            if not all(r['passed'] for r in results) or errors:
                raise SystemExit(1)
    finally:
        server.terminate()
        server.wait(timeout=10)


if __name__ == '__main__':
    main()
