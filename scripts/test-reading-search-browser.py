"""Production reading views with deterministic transport responses; not native QA."""
import argparse
import json
from pathlib import Path
import re
from urllib.request import Request, urlopen
from urllib.parse import urlsplit

from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--port', type=int, default=9310)
    parser.add_argument('--output', type=Path, default=ROOT/'dist/audit/reading-search/browser')
    parser.add_argument('--live-rpc', help='Opt in to real upstream requests through an isolated reading-browser-host')
    parser.add_argument('--live-novel-title', default='鬼吹灯')
    parser.add_argument('--live-novel-author', default='天下霸唱')
    parser.add_argument('--live-comic-title', help='Also search, open and render a real comic')
    parser.add_argument('--live-comic-author', default='')
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    calls = []

    def book(kind, number, title, author='测试作者'):
        return dict(id=number, kind=kind, sourceKey='fixture-'+kind, sourceName='搜索回归夹具', remoteId=str(number), title=title, author=author, summary='简介提及目标作品', genres='', status='', cover='', latest='', words='', updatedAt='', available=True)

    def rpc(method, params):
        calls.append(dict(method=method, args=params))
        if args.live_rpc:
            with urlopen(Request(args.live_rpc,json.dumps(dict(method=method,args=params),ensure_ascii=False).encode(),{'Content-Type':'application/json'}),timeout=50) as response:
                return json.load(response)
        if method in ('LibraryNovelContext','LibraryRefreshNovelContext'):
            location=params[0]
            view=rpc('LibraryItem',[location['itemId'],False])
            rows=view['chapters'];current=next((row for row in rows if row['remoteId']==location['remoteId']),None) if location['remoteId'] else next((row for row in rows if row['id']==location['chapterId']),rows[0] if location['first'] and rows else None)
            index=rows.index(current) if current else -1
            return dict(item=view['item'],chapter=current,previous=None,next=None,index=index,count=len(rows),cacheIdentity='fixture:'+str(current['id']) if current else '')
        if method == 'ReadingStateLoad': return []
        if method == 'ReadingStateApply': return None
        if method == 'LibrarySources': return [dict(kind=params[0], sourceKey='fixture-'+params[0], name='搜索回归夹具', apiUrl='https://example.org', enabled=True)]
        if method in ('NovelSourceDirectory', 'ComicSourceDirectory'):
            kind = 'novel' if method.startswith('Novel') else 'comic'
            rows = [dict(id=str(i), sourceKey=str(i), name=name, enabled=True, pool='reserve', score=-1, confidence='insufficient', checkedAt=1700000000, deepAt=0, state=state) for i, (name, state) in enumerate([('可检索来源', 'search_ready'), ('连接超时来源', 'search_cooldown'), ('新来源', 'unmeasured')])]
            return dict(kind=kind, registered=3, awaitingAdapter=0, rows=rows, auditRunning=False)
        if method == 'LibrarySearchStream':
            kind, query, cursor = params[:3]
            failures = [dict(sourceKey='failed-'+str(i), sourceName='超时来源 '+str(i), items=[], errorCode='TIMEOUT', errorMessage='连接超时', nextUrl='', hasNext=False) for i in range(13)]
            first = not cursor
            title = '鬼吹灯' if kind == 'novel' else '海贼王'
            items = [book(kind, 1, '王锋锋的第9本书')]
            if not first: items.append(book(kind, 2, title, '天下霸唱' if kind == 'novel' else '尾田荣一郎'))
            return dict(keyword=query, page=1 if first else 2, total=len(items), failedCount=13, searchedSources=15 if first else 5, pendingSources=5 if first else 0, nextCursor='remaining' if first else '', results=[dict(sourceKey='fixture-'+kind, sourceName='搜索回归夹具', items=items, nextUrl='', hasNext=False)]+failures)
        if method in ('LibraryItem', 'LibraryRefreshToc'):
            return dict(item=book('novel', 2, '鬼吹灯', '天下霸唱'), chapters=[dict(id=21, remoteId='chapter', ord=0, title='第一章 阅读回归', volume='', isVolume=False, access='free', available=True)], chapterCount=1)
        if method == 'LibraryChapterText': return dict(chapterId=21, paragraphs=['这是用于验证阅读入口的测试正文。']*15, warnings=[], fromCache=False)
        return ''

    names = re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
    mock = "import {CancellablePromise} from '/tests/browser/runtime.ts';\n"
    mock += """function invoke(method,args){
      if(method==='LibrarySearchStream'&&args[1]==='慢查询')return new CancellablePromise(()=>{},()=>{window.__searchCancelled=true});
      const controller=new AbortController();return new CancellablePromise((resolve,reject)=>{
        fetch('/__search_fixture',{method:'POST',body:JSON.stringify({method,args}),signal:controller.signal}).then(r=>r.json()).then(resolve,reject)
      },()=>controller.abort());
    }\n"""
    mock += '\n'.join('export function '+name+'(...args){return invoke('+json.dumps(name)+',args);}' for name in names)
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True, channel='chrome')
        try:
            page = browser.new_page(viewport={'width':1280, 'height':900})
            errors = []
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))
            page.route('**/__search_fixture', lambda route: route.fulfill(body=json.dumps(rpc(route.request.post_data_json['method'], route.request.post_data_json['args']), ensure_ascii=False), content_type='application/json'))
            if args.live_rpc:
                def image_proxy(route):
                    target=urlsplit(route.request.url)
                    backend=urlsplit(args.live_rpc)
                    response=route.fetch(url=f'{backend.scheme}://{backend.netloc}{target.path}?{target.query}')
                    route.fulfill(response=response)
                page.route('**/__cczj/img?*',image_proxy)
                for width in (1280,375):
                    page.set_viewport_size({'width':width,'height':900 if width>600 else 812})
                    page.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/novel/search?q={args.live_novel_title}')
                    target=page.locator('.novel-book-card').filter(has=page.get_by_role('heading',name=args.live_novel_title,exact=True)).filter(has_text=args.live_novel_author)
                    expect(target.first).to_be_visible(timeout=50000)
                    expect(page.locator('.reading-search-status button')).to_have_count(0,timeout=50000)
                    assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
                    page.wait_for_load_state('networkidle')
                    assert page.locator('.novel-book-card img').evaluate_all("images=>images.every(img=>img.getAttribute('src')||getComputedStyle(img).opacity==='0')"), 'empty image fallback exposes a broken-image icon over the book cover'
                    page.screenshot(path=str(args.output/f'live-novel-{width}.png'),full_page=True)
                target.first.click()
                expect(page.locator('.novel-book-hero')).to_contain_text(args.live_novel_title,timeout=30000)
                page.locator('.novel-detail-actions a').click()
                expect(page.locator('.novel-reading-paragraph').first).to_be_visible(timeout=30000)
                paragraphs=page.locator('.novel-reading-paragraph').all_text_contents()
                assert len(paragraphs)>1 and sum(map(len,paragraphs))>300
                assert not any(word in '\n'.join(paragraphs) for word in ['站长推荐','adsbygoogle'])
                page.screenshot(path=str(args.output/'live-novel-reader-mobile.png'))
                page.keyboard.press('ArrowRight')
                expect(page.locator('.novel-reader')).to_be_visible()
                comic_images = []
                if args.live_comic_title:
                    for width in (1280,375):
                        page.set_viewport_size({'width':width,'height':900 if width>600 else 812})
                        page.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/comic/search?q={args.live_comic_title}')
                        target=page.locator('.comic-card').filter(has_text=args.live_comic_title).filter(has_text=args.live_comic_author)
                        expect(target.first).to_be_visible(timeout=50000)
                        expect(page.locator('.reading-search-status button')).to_have_count(0,timeout=50000)
                        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
                        page.screenshot(path=str(args.output/f'live-comic-{width}.png'),full_page=True)
                    target.first.click()
                    expect(page.locator('.comic-detail-hero')).to_contain_text(args.live_comic_title,timeout=30000)
                    expect(page.locator('.comic-author')).to_contain_text(args.live_comic_author)
                    expect(page.locator('.comic-detail-actions a')).to_be_visible(timeout=30000)
                    page.locator('.comic-detail-actions a').click()
                    expect(page.locator('.comic-reader')).to_be_visible(timeout=30000)
                    page.wait_for_function("(()=>{const img=document.querySelector('.comic-reader img');return img&&img.complete&&img.naturalWidth>0})()",timeout=60000)
                    comic_images=page.locator('.comic-reader img').evaluate_all("imgs=>imgs.filter(img=>img.complete&&img.naturalWidth>0).map(img=>({width:img.naturalWidth,height:img.naturalHeight}))")
                    page.screenshot(path=str(args.output/'live-comic-reader-mobile.png'))
                assert not errors,errors
                report=dict(scope='real upstream + production Go service + browser transport substitute; not native QA',errors=errors,calls=calls,novelTitle=args.live_novel_title,novelAuthor=args.live_novel_author,comicTitle=args.live_comic_title,comicImages=comic_images,paragraphCount=len(paragraphs),characters=sum(map(len,paragraphs)),checks='title/author match, desktop/mobile search, open book, real reading; novel turn page; optional comic image rendering')
                (args.output/'live-verification.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
                print(report['checks'])
                return
            for kind, query, card in [('novel','鬼吹灯','.novel-book-card'), ('comic','海贼王','.comic-card')]:
                for width in (1280,375):
                    page.set_viewport_size({'width':width, 'height':900 if width>600 else 812})
                    page.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/{kind}/search?q={query}')
                    page.wait_for_load_state('networkidle')
                    expect(page.locator('.reading-search-status')).to_contain_text('已搜索 20 个来源')
                    expect(page.locator(card).first).to_contain_text(query)
                    expect(page.locator('.reading-search-issues summary')).to_contain_text('13 个来源')
                    assert page.locator('.reading-search-issues').get_attribute('open') is None
                    assert page.locator('.reading-search-related').get_attribute('open') is None
                    assert not page.locator('.reading-search-issues li').first.is_visible()
                    assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
                    page.screenshot(path=str(args.output/f'{kind}-{width}.png'), full_page=True)
                    page.locator('.reading-search-issues summary').click()
                    assert page.locator('.reading-search-issues ul').evaluate('(e)=>e.clientHeight<=256')
                    page.locator('.reading-search-issues summary').click()
                    page.locator('.reading-search-related summary').click()
                    expect(page.locator('.reading-search-related')).to_contain_text('王锋锋的第9本书')
                    page.locator('.reading-search-related summary').click()
                page.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/{kind}/sources')
                page.wait_for_load_state('networkidle')
                expect(page.get_by_text('最近搜索成功', exact=False)).to_be_visible()
                expect(page.get_by_text('暂缓搜索 · 扩展时仍会尝试', exact=False)).to_be_visible()
                assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
            page.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/novel/search?q=慢查询')
            page.wait_for_load_state('networkidle')
            page.get_by_role('button',name='停止查找').click()
            expect(page.get_by_text('已停止查找，已找到的结果仍可打开。')).to_be_visible()
            assert page.evaluate('window.__searchCancelled===true')
            page.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/novel/search?q=鬼吹')
            expect(page.get_by_text('尚无精确匹配，以下为书名或作者相关的作品。',exact=True)).to_be_visible()
            page.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/novel/search?q=鬼吹灯')
            page.wait_for_load_state('networkidle')
            target=page.locator('.novel-book-card').filter(has=page.get_by_role('heading',name='鬼吹灯',exact=True))
            expect(target).to_be_visible()
            target.click()
            page.wait_for_load_state('networkidle')
            expect(page.locator('.novel-book-hero')).to_contain_text('鬼吹灯')
            assert not errors, errors
            report=dict(scope='browser substitute; deterministic transport; no upstream quality claim',errors=errors,calls=calls,checks='separate modes; desktop/mobile; automatic expansion; relevance; folded errors; source feedback; cancellation; book navigation')
            (args.output/'verification.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
            print(report['checks'])
        finally:
            page.unroute_all(behavior='wait')
            browser.close()


if __name__ == '__main__': main()
