"""Real reading views/stores with isolated deterministic Wails transport fixtures.

This checks browser behavior, not native Wails or Android. Persistence adapter
and portable-ID recovery are separately tested against real SQLite in Go.
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
    parser.add_argument('--channel', default=None)
    parser.add_argument('--port', type=int, default=9284)
    parser.add_argument('--output', type=Path, default=ROOT/'reports/reading-regression-2026-10-21')
    args = parser.parse_args()
    origin = f'http://127.0.0.1:{args.port}'
    server = subprocess.Popen(['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', str(args.port), '--strictPort'], cwd=ROOT/'frontend', stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
    records = {}
    fail_write = False
    calls = []

    def item(kind, number):
        return dict(id=number, kind=kind, sourceKey=f'fixture-{kind}', sourceName=f'Fixture {kind}', remoteId=f'book/{number}', title=f'{kind.title()} {number:03}', author='Fixture author', cover='', summary='Local test fixture', genres='rare' if number == 1 else 'adventure', status='Completed', latest='', words='', updatedAt='', available=True)

    items = [item('novel', n) for n in range(1, 122)] + [item('comic', n) for n in range(201, 322)]
    by_id = {row['id']: row for row in items}

    def key(row):
        return '\0'.join((row['kind'], row['sourceKey'], row['remoteId']))

    def rpc(method, params):
        calls.append(method)
        if method in ('LibraryNovelContext','LibraryRefreshNovelContext'):
            location=params[0]
            view=rpc('LibraryItem',[location['itemId'],False])
            rows=view['chapters'];current=next((row for row in rows if row['remoteId']==location['remoteId']),None) if location['remoteId'] else next((row for row in rows if row['id']==location['chapterId']),rows[0] if location['first'] and rows else None)
            index=rows.index(current) if current else -1
            return dict(item=view['item'],chapter=current,previous=None,next=None,index=index,count=len(rows),cacheIdentity='fixture:'+str(current['id']) if current else '')
        if method == 'ReadingStateLoad':
            kind, legacy = params
            if kind not in records:
                records[kind] = {(r['bucket'], r['key']): r for r in json.loads(legacy)}
            return list(records[kind].values())
        if method == 'ReadingStateApply':
            if fail_write:
                raise RuntimeError('fixture storage full')
            kind, changes = params
            for change in json.loads(changes):
                identity = (change['bucket'], change['key'])
                if change.get('delete'):
                    records[kind].pop(identity, None)
                else:
                    records[kind][identity] = change
            return None
        if method == 'LibraryCatalogPage':
            q = params[0]
            found = [row for row in items if row['kind'] == q['kind'] and (not q['genre'] or row['genres'] == q['genre'])]
            found.sort(key=lambda row: row['title'] if q['sort'] == 'title' else -row['id'])
            offset = int(q['cursor'] or 0)
            next_offset = offset + q['limit']
            return dict(items=found[offset:next_offset], genres=['adventure', 'rare'], nextCursor=str(next_offset) if next_offset < len(found) else '')
        if method == 'LibraryResolveRefs':
            return [next((row for row in items if key(row) == '\0'.join((ref['kind'], ref['sourceKey'], ref['remoteId']))), None) for ref in params[0]]
        if method in ('LibraryItem', 'LibraryRefreshToc', 'LibraryRefreshReadingToc'):
            row = by_id[int(params[0])]
            return dict(item=row, chapters=[dict(id=1000+row['id'], remoteId='chapter-one', ord=0, title='Chapter one', volume='', isVolume=False, access='free', available=True)], chapterCount=1)
        if method == 'LibraryChapterText':
            return dict(chapterId=params[0], paragraphs=['Reading fixture paragraph.'] * 12, warnings=[], fromCache=False)
        if method in ('LibraryChapterPages', 'LibraryRefreshChapterPages'):
            return dict(chapterId=params[0], pages=[dict(index=i,url='data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7') for i in range(3)], warnings=[], fromCache=False)
        if method == 'LibrarySources':
            return [dict(sourceKey=f'fixture-{params[0]}', kind=params[0], name='Fixture source', enabled=True, itemCount=121)]
        return ''

    try:
        for _ in range(100):
            if server.poll() is not None:
                raise RuntimeError('Vite server exited')
            try:
                with urlopen(origin+'/tests/browser/reading.html', timeout=1):
                    break
            except OSError:
                time.sleep(.2)
        names = re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
        remote = {'LibraryNovelContext','LibraryRefreshNovelContext','ReadingStateLoad', 'ReadingStateApply', 'LibraryCatalogPage', 'LibraryResolveRefs', 'LibraryItem', 'LibraryRefreshToc', 'LibraryRefreshReadingToc', 'LibraryChapterText', 'LibraryChapterPages', 'LibraryRefreshChapterPages', 'LibrarySources'}
        mock = "import { CancellablePromise } from '/tests/browser/runtime.ts';\n"
        mock += """
        function invoke(method,args){ const controller=new AbortController(); return new CancellablePromise((resolve,reject)=>{
          fetch('/__reading_fixture',{method:'POST',body:JSON.stringify({method,args}),signal:controller.signal}).then(async response=>{const data=await response.json();if(!response.ok)throw Object.assign(new Error(data.message),{code:'STORAGE'});return data}).then(resolve,error=>{if(!controller.signal.aborted)reject(error)});
        },()=>controller.abort()); }
        """
        mock += '\n'.join(f'export function {name}(...args) {{ return '+(f'invoke({json.dumps(name)},args)' if name in remote else "CancellablePromise.resolve('')")+'; }' for name in names)
        output = args.output
        output.mkdir(parents=True, exist_ok=True)
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True, channel=args.channel)
            try:
                page = browser.new_page(viewport={'width':1280, 'height':900})
                errors = []
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))

                def handle(route):
                    data = route.request.post_data_json
                    try:
                        route.fulfill(body=json.dumps(rpc(data['method'], data['args'])), content_type='application/json')
                    except RuntimeError as error:
                        route.fulfill(status=500, json={'message':str(error)})
                page.route('**/__reading_fixture', handle)
                legacy = dict(key=key(items[0]), itemId=1, addedAt=1, available=True, item=items[0])
                page.add_init_script('if(!sessionStorage.seeded){localStorage.setItem("cczj.novel.shelf.v2",'+json.dumps(json.dumps({'version':1,'value':[legacy]}))+');sessionStorage.seeded="1"}')

                def visit(path):
                    page.goto(origin+'/tests/browser/reading.html#'+path)
                    page.wait_for_load_state('networkidle')
                    expect(page.locator('.cczj-mobile-route-scene[data-route-active=true]').last).to_have_attribute('data-path',path.split('?')[0])

                visit('/novel/shelf')
                expect(page.locator('.novel-shelf-item')).to_have_count(1)
                expect(page.locator('.novel-shelf-item')).to_contain_text('Novel 001')
                visit('/novel/categories')
                expect(page.locator('.novel-book-card')).to_have_count(60)
                page.locator('.novel-catalog-results > button').click()
                expect(page.locator('.novel-book-card')).to_have_count(120)
                visit('/novel/categories?genre=rare')
                expect(page.locator('.novel-book-card')).to_have_count(1)
                expect(page.locator('.novel-book-card')).to_contain_text('Novel 001')

                visit('/novel/book/2')
                page.locator('.novel-detail-actions button').click()
                expect(page.locator('.novel-detail-actions button')).to_have_attribute('aria-pressed','true')
                page.evaluate('localStorage.clear()')
                visit('/novel/shelf')
                page.reload()
                expect(page.locator('.novel-shelf-item')).to_have_count(2)

                fail_write = True
                visit('/novel/book/3')
                page.locator('.novel-detail-actions button').click()
                expect(page.locator('.novel-storage-error')).to_be_visible()
                assert ('shelf', key(items[2])) not in records['novel']
                fail_write = False
                page.locator('.novel-storage-error button').click()
                expect(page.locator('.novel-storage-error')).to_have_count(0)
                assert ('shelf', key(items[2])) in records['novel']

                visit('/comic/series/201')
                page.locator('.comic-detail-actions button, .comic-series-actions button, footer button[aria-pressed]').click()
                expect(page.locator('footer button[aria-pressed]')).to_have_attribute('aria-pressed','true')
                visit('/comic/library')
                expect(page.locator('.comic-card')).to_have_count(1)
                page.evaluate('localStorage.clear()')
                page.reload()
                expect(page.locator('.comic-card')).to_have_count(1)
                page.screenshot(path=str(output/'comic-library.png'), full_page=True)
                comic_key = key(by_id[201])
                records['comic'][('progress',comic_key)] = dict(bucket='progress',key=comic_key,value=json.dumps(dict(itemId=201,title='Comic 201',episodeOrd=0,episodeId=1201,episodeRemoteId='chapter-one',page=2,offset=0,updatedAt=5)))
                records['comic'][('preferences','default')] = dict(bucket='preferences',key='default',value=json.dumps(dict(flow='pages')))
                # Hash-only navigation retains the existing store; emulate a
                # process restart so the injected durable snapshot is hydrated.
                page.reload()
                expect(page.locator('.comic-card')).to_have_count(1)
                visit('/comic/read/201/0')
                expect(page.locator('.comic-reader-indicator')).to_contain_text('3 / 3')
                visit('/novel/shelf')
                expect(page.locator('.novel-shelf-item')).to_have_count(3)
                page.screenshot(path=str(output/'novel-shelf.png'), full_page=True)
                assert not errors, errors
                (output/'result.json').write_text(json.dumps({'passed':True,'transport':'deterministic browser fixture','native':False,'calls':calls},indent=2),encoding='utf-8')
                print('Reading browser checks passed: import, paging, whole-catalog filters, reload, storage failure/retry and mode isolation.')
            finally:
                if 'page' in locals() and not page.is_closed():
                    page.screenshot(path=str(output/'last-state.png'), full_page=True)
                browser.close()
    finally:
        server.terminate()
        server.wait(timeout=10)


if __name__ == '__main__':
    main()
