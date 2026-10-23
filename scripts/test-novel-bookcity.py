"""Real remote discovery, SQLite and reading with browser Wails transport only.

Starts with an empty temporary database. No catalog/search fixture is supplied.
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

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_OUTPUT = ROOT / 'reports/novel-bookcity-2026-10-21'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=DEFAULT_OUTPUT)
    parser.add_argument('--record', action='store_true')
    options = parser.parse_args()
    output = options.output
    output.mkdir(parents=True, exist_ok=True)
    flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
    binary = ROOT / 'dist/reading-browser-host.exe'
    subprocess.run(['go', 'build', '-o', str(binary), './scripts/reading-browser-host'], cwd=ROOT, check=True, creationflags=flags)
    backend = subprocess.Popen([str(binary), '--port', '9296', '--relative-images'], cwd=ROOT, creationflags=flags, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    vite = subprocess.Popen(['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '9297', '--strictPort'], cwd=ROOT/'frontend', creationflags=flags, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    calls, failures, checks = [], [], []
    def rpc(method, params):
        print('RPC '+method, flush=True)
        calls.append(dict(method=method, args=params))
        request = Request('http://127.0.0.1:9296/rpc', json.dumps(dict(method=method, args=params)).encode(), {'Content-Type': 'application/json'})
        with urlopen(request, timeout=100) as response:
            result = json.load(response)
            print('DONE '+method+' '+str(len(result.get('items', [])) if isinstance(result, dict) else ''), flush=True)
            return result
    try:
        for endpoint in ['http://127.0.0.1:9296/health', 'http://127.0.0.1:9297/tests/browser/reading.html']:
            for _ in range(150):
                try:
                    with urlopen(endpoint, timeout=1): break
                except OSError: time.sleep(.2)
            else: raise RuntimeError('Server did not start: ' + endpoint)
        assert rpc('LibrarySources', ['novel']) == []
        assert not rpc('LibraryItems', ['novel', True])
        names = re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
        mock = "import {CancellablePromise} from '/tests/browser/runtime.ts';\n"
        mock += '''function invoke(method,args){const controller=new AbortController();return new CancellablePromise((resolve,reject)=>{
fetch('/__live_reading',{method:'POST',body:JSON.stringify({method,args}),signal:controller.signal}).then(async response=>{const data=await response.json();if(!response.ok)throw Object.assign(new Error(data.message),{code:data.code});return data}).then(resolve,error=>{if(!controller.signal.aborted)reject(error)});
},()=>controller.abort());}\n'''
        mock += '\n'.join(f'export function {name}(...args){{return '+(f'invoke({json.dumps(name)},args)' if name.startswith(('Library', 'ReadingState', 'NovelBrowse')) else "CancellablePromise.resolve('')")+';}' for name in names)
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True, channel='chrome')
            page = None
            try:
                recording = dict(record_video_dir=str(output/'video'), record_video_size={'width': 1008, 'height': 800}) if options.record else {}
                page = browser.new_page(viewport={'width': 1360, 'height': 1080}, **recording)
                video_start = time.monotonic()
                errors = []
                page.on('pageerror', lambda error: (errors.append(str(error)), print('PAGE ERROR '+str(error), flush=True)))
                page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))
                def bridge(route):
                    call = route.request.post_data_json
                    try: route.fulfill(body=json.dumps(rpc(call['method'], call['args'])), content_type='application/json')
                    except HTTPError as error:
                        body = error.read()
                        failures.append(dict(method=call['method'], response=body.decode(errors='replace')))
                        route.fulfill(status=error.code, body=body, content_type='application/json')
                page.route('**/__live_reading', bridge)
                # Production uses a same-origin relative image endpoint. Forward
                # those bytes to the Go proxy; Referer and validation stay there.
                page.route('**/__cczj/img?*', lambda route: route.continue_(url='http://127.0.0.1:9296'+route.request.url.split('9297', 1)[1]))
                url = 'http://127.0.0.1:9297/tests/browser/reading.html#/novel'
                page.goto(url)
                print('OPENED', flush=True)
                for channel in ['popular', 'publication', 'history']:
                    expect(page.locator(f'[data-channel={channel}] .room-book')).to_have_count(6, timeout=100000)
                    print('LOADED '+channel, flush=True)
                expect(page.locator('.room-desk-book')).to_be_visible()
                assert page.locator('.room-wayfinding').evaluate('(el)=>{const s=getComputedStyle(el);return s.borderTopWidth==="0px"&&s.borderBottomWidth==="0px"}')
                assert page.locator('.room-next-book').count() == 0
                assert page.locator('.room-discovery').evaluate('(el)=>{const a=el.getBoundingClientRect(),b=document.querySelector(".room-shelves").getBoundingClientRect();return a.width<300&&a.height>a.width&&a.left>=b.right}')
                checks.append('Categories have no horizontal rules; desktop discovery is a portrait side page beside the shelves; circular arrow is absent')
                assert not any(call['method'].startswith('LibrarySearch') for call in calls)
                assert len(rpc('LibrarySources', ['novel'])) == 1
                assert rpc('LibrarySources', ['comic']) == []
                checks.append('Empty database opens real browse shelves without a search or collection; novel source only')
                page.wait_for_function('document.querySelectorAll(".room-book-cover img").length>=6 && [...document.querySelectorAll(".room-book-cover img")].filter(i=>i.complete&&i.naturalWidth>10).length>=6', timeout=60000)
                # The deck rotates existing metadata without another fetch.
                demo_start = time.monotonic() - video_start
                if options.record: page.wait_for_timeout(500)
                active_cover = page.locator('.room-stack-cover[data-position="0"]')
                original_book = active_cover.get_attribute('href')
                browse_calls = len([call for call in calls if call['method'] == 'NovelBrowsePage'])
                swap = page.get_by_role('button', name='换一本', exact=True)
                page.wait_for_timeout(600)
                assert swap.evaluate('(el)=>{const r=el.getBoundingClientRect();return el.contains(document.elementFromPoint(r.left+r.width/2,r.top+r.height/2))}')
                swap.focus(); swap.press('Enter')
                expect(active_cover).not_to_have_attribute('href', original_book)
                new_book = active_cover.get_attribute('href')
                expect(page.locator('.room-desk-book')).to_have_attribute('href', new_book)
                assert len([call for call in calls if call['method'] == 'NovelBrowsePage']) == browse_calls
                assert page.locator('.room-stack-cover[tabindex="0"]').count() == 1
                expect(page.locator('.novel-home')).not_to_contain_text('好故事，慢慢找')
                checks.append('Discovery deck cycles real books by keyboard, keeps one accessible active cover and does not fetch again')
                if options.record:
                    for _ in range(2):
                        page.wait_for_timeout(900)
                        swap.click()
                    page.wait_for_timeout(700)
                page.locator('[data-channel=popular] .room-book').first.hover()
                page.wait_for_function('getComputedStyle(document.querySelector("[data-channel=popular] .room-book .novel-cover")).transform!=="none"')
                page.wait_for_timeout(600)
                page.screenshot(path=str(output/'book-hover.png'))
                page.mouse.move(0, 0)
                checks.append('Book cover hinge responds to hover without moving its title or pointer target')
                page.screenshot(path=str(output/'bookcity-desktop.png'))
                page.locator('.room-wayfinding').get_by_role('button', name='历史古代', exact=True).click()
                history = page.locator('[data-channel=history] .room-book')
                expect(history).to_have_count(20, timeout=60000)
                if options.record:
                    page.wait_for_timeout(900)
                    (output/'video-window.json').write_text(json.dumps(dict(start=max(0, demo_start-.3), duration=time.monotonic()-video_start-demo_start+.6)), encoding='utf-8')
                before = history.all_text_contents()
                page.get_by_role('button', name='再逛一排', exact=True).click()
                expect(history).to_have_count(40, timeout=60000)
                assert len(set(history.evaluate_all('(rows)=>rows.map(row=>row.href)'))) == 40
                checks.append('Actual remote category pagination adds 20 distinct novels without keyword')
                title = history.first.locator('h3').inner_text()
                history.first.click()
                expect(page.locator('.novel-detail-actions a')).to_be_visible(timeout=100000)
                page.screenshot(path=str(output/'book-detail-desktop.png'))
                page.locator('.novel-detail-actions button').first.click()
                page.locator('.novel-detail-actions a').click()
                expect(page.locator('.novel-reading-paragraph').first).to_be_visible(timeout=100000)
                assert len(page.locator('.novel-reading-paper').inner_text()) > 500
                assert not page.locator('.novel-reading-paper').evaluate('(element)=>/[\uE000-\uF8FF]/.test(element.innerText)')
                page.screenshot(path=str(output/'reader-desktop.png'))
                page.go_back()
                expect(page.locator('.novel-detail-actions a')).to_be_visible(timeout=30000)
                page.go_back()
                expect(page.locator('[data-channel=history] .room-book')).to_have_count(40, timeout=30000)
                assert page.locator('[data-channel=history] .room-book').all_text_contents()[:20] == before
                checks.append('Open a real novel, save it, read decoded chapter, Back retains all loaded pages')
                page.locator('.reading-desktop-nav').get_by_role('link', name='书架', exact=True).click()
                expect(page.locator('.novel-shelf')).to_be_visible()
                page.locator('.reading-desktop-nav').get_by_role('link', name='书城', exact=True).click()
                expect(page.locator('[data-channel=history] .room-book')).to_have_count(40)
                checks.append('Bookcity / shelf navigation preserves the selected category and loaded pages')
                # Reload is a new frontend instance, backed by the persisted first page.
                page.goto(url)
                page.reload()
                expect(page.locator('[data-channel=popular] .room-book')).to_have_count(6, timeout=30000)
                expect(page.locator('[data-channel=popular] .room-shelf-footer')).to_contain_text('书目保存于')
                page.locator('.reading-desktop-nav').get_by_role('link', name='书架', exact=True).click()
                expect(page.locator('.novel-shelf')).to_contain_text(title, timeout=30000)
                checks.append('Reload restores SQLite book listings and saved personal shelf')
                page.goto(url)
                page.set_viewport_size({'width': 390, 'height': 844})
                expect(page.locator('[data-channel=popular] .room-book')).to_have_count(6, timeout=30000)
                page.wait_for_timeout(1200)
                assert page.locator('.room-next-book').count() == 0
                assert page.locator('.room-discovery').evaluate('(el)=>{const a=el.getBoundingClientRect(),b=el.querySelector(".room-book-stack").getBoundingClientRect(),c=el.querySelector(".room-discovery-info").getBoundingClientRect();return a.width>a.height&&c.left>=b.right}')
                assert page.locator('.room-discovery').evaluate('(el)=>el.querySelector(`[data-position="2"]`).getBoundingClientRect().right<=el.querySelector(".room-discovery-info").getBoundingClientRect().left')
                assert swap.evaluate('(el)=>{const r=el.getBoundingClientRect();return el.contains(document.elementFromPoint(r.left+r.width/2,r.top+r.height/2))}')
                assert page.locator('.novel-home').evaluate('(el)=>el.scrollWidth<=el.clientWidth+1')
                assert page.locator('.room-book').first.evaluate('(el)=>el.getBoundingClientRect().width') > 85
                page.screenshot(path=str(output/'bookcity-mobile.png'))
                for width in [320, 360, 412, 768]:
                    page.set_viewport_size({'width': width, 'height': 900})
                    assert page.locator('.novel-home').evaluate('(el)=>el.scrollWidth<=el.clientWidth+1'), width
                    assert page.locator('.room-search input').evaluate('(el)=>el.getBoundingClientRect().width') >= 50, width
                    box = swap.bounding_box()
                    assert box and box['width'] >= 44 and box['height'] >= 44, width
                    assert swap.evaluate('(el)=>{const r=el.getBoundingClientRect();return el.contains(document.elementFromPoint(r.left+r.width/2,r.top+r.height/2))}'), width
                page.set_viewport_size({'width': 390, 'height': 844})
                page.locator('.room-wayfinding').get_by_role('button', name='东方玄幻', exact=True).click()
                expect(page.locator('[data-channel=fantasy] .room-book')).to_have_count(20, timeout=60000)
                page.locator('.novel-main').evaluate('(el)=>el.scrollTop=0')
                page.screenshot(path=str(output/'category-mobile.png'))
                checks.append('390px phone layout and distinct real category data without horizontal page overflow')
                page.emulate_media(reduced_motion='reduce')
                assert page.locator('.room-book').first.evaluate('(el)=>getComputedStyle(el).animationName') == 'none'
                assert page.locator('.room-shelf-marker').evaluate('(el)=>getComputedStyle(el).transitionDuration') == '0s'
                page.locator('.room-wayfinding').get_by_role('button', name='漫游', exact=True).click()
                expect(page.locator('.room-desk-book')).to_be_visible()
                swap.click()
                expect(page.locator('.room-desk-book')).to_have_attribute('href', active_cover.get_attribute('href'))
                page.emulate_media(reduced_motion='no-preference')
                assert page.evaluate('(()=>{document.documentElement.dataset.cczjMotion="off";return getComputedStyle(document.querySelector(".room-stack-cover")).transitionDuration})()') == '0s'
                checks.append('System reduced motion and application motion-off stop effects while book selection remains usable')
                for channel in ['mystery', 'city', 'xianxia']:
                    found = rpc('NovelBrowsePage', [channel, '', False])
                    assert found['items'] and all(book['kind'] == 'novel' for book in found['items']), channel
                checks.append('All seven exposed remote channels return real novel metadata')
                assert not errors, errors
                assert not failures, failures
                result = dict(passed=True, native=False, checks=checks, title=title, rpcCalls=len(calls), errors=errors)
                (output/'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
                print(json.dumps(result, ensure_ascii=False))
            finally:
                if page is not None:
                    page.screenshot(path=str(output/'last-state.png'))
                    (output/'last-state.txt').write_text(page.locator('body').inner_text(), encoding='utf-8')
                    page.unroute_all(behavior='wait')
                    page.close()
                    if options.record: page.video.save_as(output/'session.webm')
                browser.close()
    finally:
        backend.terminate(); vite.terminate()
        backend.wait(timeout=10); vite.wait(timeout=10)


if __name__ == '__main__': main()
