"""Measure retained browser heap and listeners across repeated real player mounts.

Uses a local video and mocked Wails bindings. GC is requested only in this test
browser so retained growth can be distinguished from garbage awaiting collection.
"""
import argparse
import base64
import json
from pathlib import Path
import re
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--url', default='http://127.0.0.1:9248')
    parser.add_argument('--channel', default=None)
    parser.add_argument('--cycles', type=int, default=30)
    args = parser.parse_args()
    bindings = (ROOT / 'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
    names = re.findall(r'export function (\w+)\(', bindings)
    mock = '\n'.join(f'export async function {name}() {{ return {"[]" if name in ("ListPlugins", "RescanPlugins") else chr(34)+chr(34)}; }}' for name in names)
    video_bytes = (ROOT / 'frontend/tests/browser/sample.webm').read_bytes()
    samples = []
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True, channel=args.channel, args=['--autoplay-policy=no-user-gesture-required'])
        try:
            page = browser.new_page()
            errors = []
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))

            def proxy(route):
                encoded = parse_qs(urlparse(route.request.url).query).get('u', [''])[0]
                remote = base64.urlsafe_b64decode(encoded + '=' * (-len(encoded) % 4)).decode()
                assert remote.startswith(args.url + '/tests/browser/sample.webm'), remote
                route.fulfill(body=video_bytes, headers={'Accept-Ranges': 'bytes'}, content_type='video/webm')

            page.route('**/__cczj/hls?*', proxy)
            page.goto(args.url + '/tests/browser/player.html')
            page.wait_for_load_state('networkidle')
            page.wait_for_function("document.querySelector('video')?.readyState >= 2")
            cdp = page.context.new_cdp_session(page)
            cdp.send('Performance.enable')

            def measure(cycle):
                page.wait_for_timeout(450)
                cdp.send('HeapProfiler.collectGarbage')
                metrics = {item['name']: item['value'] for item in cdp.send('Performance.getMetrics')['metrics']}
                samples.append({'cycle': cycle, **{key: metrics[key] for key in ('JSHeapUsedSize', 'Nodes', 'Documents', 'JSEventListeners')}})

            page.evaluate('window.playerTest.state.open = false')
            page.wait_for_function("!document.querySelector('.player-wrapper')")
            page.wait_for_timeout(150)
            measure(0)
            for cycle in range(1, args.cycles + 1):
                page.evaluate('(cycle) => { window.playerTest.state.line = cycle; window.playerTest.state.open = true }', cycle)
                page.wait_for_function("document.querySelector('video')?.readyState >= 2")
                page.evaluate("document.querySelector('video').play()")
                page.wait_for_function("!document.querySelector('video').paused")
                page.evaluate('window.playerTest.state.open = false')
                page.wait_for_function("!document.querySelector('.player-wrapper')")
                page.wait_for_timeout(60)
                if cycle % 10 == 0 or cycle == args.cycles:
                    measure(cycle)
            first, last = samples[0], samples[-1]
            assert last['JSHeapUsedSize'] - first['JSHeapUsedSize'] < 8*1024**2, samples
            assert last['JSEventListeners'] <= first['JSEventListeners'] + 20, samples
            assert last['Nodes'] <= first['Nodes'] + 100, samples
            assert not errors, errors
            output = ROOT / 'dist/audit/browser-memory.json'
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_text(json.dumps({'scenario': 'Local WebM player mount/play/close with test-only GC', 'samples': samples}, indent=2), encoding='utf-8')
            print(json.dumps({'cycles': args.cycles, 'baseline': first, 'final': last, 'retained_heap_delta_mb': (last['JSHeapUsedSize']-first['JSHeapUsedSize'])/1024**2}, ensure_ascii=True))
        finally:
            browser.close()


if __name__ == '__main__':
    main()
