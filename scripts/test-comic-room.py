"""Real bundled comic catalogs, SQLite, following and image reading.

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
DEFAULT_OUTPUT = ROOT / 'reports/comic-room-2026-10-21'


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
        mock += '\n'.join(f'export function {name}(...args){{return '+(f'invoke({json.dumps(name)},args)' if name.startswith(('Library', 'ReadingState', 'NovelBrowse', 'ComicBrowse', 'ComicTrending')) else "CancellablePromise.resolve('')")+';}' for name in names)
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
                url = 'http://127.0.0.1:9297/tests/browser/reading.html#/comic'
                page.goto(url)
                for channel in ['guazi', 'dongman', 'honkai', 'terra']:
                    expect(page.locator(f'[data-channel={channel}] .comic-card')).to_have_count(6, timeout=100000)
                assert not any(call['method'].startswith('LibrarySearch') for call in calls)
                assert len(rpc('LibrarySources', ['comic'])) == 4
                assert rpc('LibrarySources', ['novel']) == []
                checks.append('Empty SQLite opens four actual bundled comic catalogs without keyword; novel/video sources stay separate')
                page.wait_for_function('document.querySelector(".manga-spotlight-cover img")?.naturalWidth>100 && [...document.querySelectorAll(".manga-grid img")].filter(i=>i.complete&&i.naturalWidth>10).length>=6', timeout=60000)
                page.wait_for_timeout(800)
                demo_start = time.monotonic()-video_start
                page.screenshot(path=str(output/'comic-desktop.png'))
                featured = page.locator('.manga-spotlight-cover')
                first = featured.get_attribute('href')
                before_rpc = len(calls)
                page.get_by_role('button', name='随便翻翻', exact=True).focus()
                page.get_by_role('button', name='随便翻翻', exact=True).press('Enter')
                expect(featured).not_to_have_attribute('href', first)
                expect(page.locator('.manga-open')).to_have_attribute('href', featured.get_attribute('href'))
                page.locator('.manga-contact-sheet button').nth(1).click()
                expect(page.locator('.manga-contact-sheet button').nth(1)).to_have_attribute('aria-pressed','true')
                assert len(calls)==before_rpc
                checks.append('Keyboard shuffle and contact-sheet cover selection agree with the detail link and perform no extra requests')
                if options.record:
                    page.wait_for_timeout(1200)
                    page.locator('.manga-contact-sheet button').nth(2).click()
                    page.wait_for_timeout(1000)
                page.locator('[data-channel=guazi] .comic-card').first.hover()
                page.wait_for_timeout(450)
                page.screenshot(path=str(output/'comic-hover.png'))
                if options.record:
                    (output/'video-window.json').write_text(json.dumps(dict(start=demo_start,duration=time.monotonic()-video_start-demo_start+.5)),encoding='utf-8')
                page.locator('.manga-channels').get_by_role('button',name='人气国漫').click()
                total = page.locator('[data-channel=guazi] .comic-card').count()
                assert total>6
                selected_title=page.locator('[data-channel=guazi] .comic-card h3').first.inner_text()
                page.locator('[data-channel=guazi] .comic-card').first.click()
                expect(page.locator('.comic-detail-actions a')).to_be_visible(timeout=90000)
                page.screenshot(path=str(output/'comic-detail.png'))
                page.locator('.comic-detail-actions button').first.click()
                expect(page.locator('.comic-detail-actions button').first).to_have_attribute('aria-pressed','true')
                page.locator('.comic-detail-actions a').click()
                page.wait_for_function('[...document.querySelectorAll(".comic-panel img")].some(i=>i.complete&&i.naturalWidth>100)',timeout=90000)
                page.screenshot(path=str(output/'comic-reader.png'))
                page.go_back()
                expect(page.locator('.comic-detail-actions a')).to_be_visible(timeout=30000)
                page.go_back()
                expect(page.locator('[data-channel=guazi] .comic-card')).to_have_count(total)
                expect(page.locator('.manga-channels').get_by_role('button',name='人气国漫')).to_have_attribute('aria-pressed','true')
                page.locator('.reading-desktop-nav').get_by_role('link',name='追漫',exact=True).click()
                expect(page.locator('.comic-library')).to_contain_text(selected_title)
                page.locator('.reading-desktop-nav').get_by_role('link',name='漫游',exact=True).click()
                expect(page.locator('[data-channel=guazi] .comic-card')).to_have_count(total)
                checks.append('Actual bundled-source comic can be followed and read; Back and Following tab retain the selected catalog')
                page.reload()
                expect(page.locator('[data-channel=guazi] .manga-source-caption')).to_contain_text('书目保存于',timeout=30000)
                page.locator('.reading-desktop-nav').get_by_role('link',name='追漫',exact=True).click()
                expect(page.locator('.comic-library')).to_contain_text(selected_title)
                page.goto(url)
                page.set_viewport_size({'width':390,'height':844})
                expect(page.locator('.manga-spotlight-cover')).to_be_visible()
                page.wait_for_timeout(1200)
                page.screenshot(path=str(output/'comic-mobile.png'))
                for width in [320,360,390,412,768,1024,1360]:
                    page.set_viewport_size({'width':width,'height':900})
                    assert page.locator('.comic-home').evaluate('(e)=>e.scrollWidth<=e.clientWidth+1'),width
                    assert page.locator('.manga-search input').evaluate('(e)=>e.getBoundingClientRect().width>=50'),width
                    for control in page.locator('.manga-contact-sheet button').all():
                        assert control.evaluate('(e)=>{const r=e.getBoundingClientRect();return r.width>=44&&r.height>=44&&e.contains(document.elementFromPoint(r.left+r.width/2,r.top+r.height/2))}'),width
                page.set_viewport_size({'width':390,'height':844})
                page.locator('.manga-contact-sheet button').nth(1).click()
                expect(page.locator('.manga-open')).to_have_attribute('href',featured.get_attribute('href'))
                page.evaluate('document.documentElement.dataset.themeMode="dark"')
                page.screenshot(path=str(output/'comic-mobile-dark.png'))
                page.evaluate('document.documentElement.dataset.themeMode="light"')
                page.emulate_media(reduced_motion='reduce')
                assert featured.evaluate('(e)=>getComputedStyle(e).animationName')=='none'
                assert page.locator('.manga-contact-sheet button').first.evaluate('(e)=>getComputedStyle(e).transitionDuration')=='0s'
                page.locator('.manga-contact-sheet button').first.click()
                expect(page.locator('.manga-open')).to_have_attribute('href',featured.get_attribute('href'))
                checks.append('320–1360px responsive layouts, reachable 44px cover controls, night palette, and functional reduced-motion selection')
                assert not errors,errors
                assert not failures,failures
                result=dict(passed=True,native=False,checks=checks,rpcCalls=len(calls),errors=errors)
                (output/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
                print(json.dumps(result,ensure_ascii=False))
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
