"""Check that a real player close releases its retained segment cache while idle.

Uses a disposable headless browser and local WebM; only Wails bindings and the
media proxy are simulated. Browser clock advancement makes the idle check fast.
"""
import argparse
import base64
import json
import os
from pathlib import Path
import re
import subprocess
import time
from urllib.parse import parse_qs, urlparse
from urllib.request import urlopen

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--channel', default=None)
    parser.add_argument('--port', type=int, default=9276)
    args = parser.parse_args()
    url = f'http://127.0.0.1:{args.port}'
    flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
    server = subprocess.Popen(['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', str(args.port), '--strictPort'], cwd=ROOT/'frontend', stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=flags)
    try:
        for _ in range(100):
            if server.poll() is not None:
                raise RuntimeError('Isolated Vite server exited')
            try:
                with urlopen(url+'/tests/browser/player.html', timeout=1):
                    break
            except OSError:
                time.sleep(.2)
        else:
            raise RuntimeError('Isolated Vite server did not become ready')
        bindings = (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
        names = re.findall(r'export function (\w+)\(', bindings)
        mock = '\n'.join(f'export async function {name}() {{ return {json.dumps([] if name in ("ListPlugins", "RescanPlugins") else False if name.startswith("WindowIs") else "")}; }}' for name in names)
        video = (ROOT/'frontend/tests/browser/sample.webm').read_bytes()
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(headless=True, channel=args.channel, args=['--autoplay-policy=no-user-gesture-required'])
            try:
                page = browser.new_page()
                errors = []
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))

                def proxy(route):
                    encoded = parse_qs(urlparse(route.request.url).query).get('u', [''])[0]
                    remote = base64.urlsafe_b64decode(encoded+'='*(-len(encoded)%4)).decode()
                    assert remote.startswith(url+'/tests/browser/sample.webm'), remote
                    route.fulfill(body=video, content_type='video/webm')

                page.route('**/__cczj/hls?*', proxy)
                page.goto(url+'/tests/browser/player.html')
                page.wait_for_load_state('networkidle')
                page.wait_for_function("document.querySelector('video')?.readyState >= 2")
                page.evaluate('window.playerTest.seedSegmentCache()')
                playing = page.evaluate('window.playerTest.segmentStats().bytes')
                assert playing >= 70*1024**2, playing
                page.clock.install()
                page.evaluate('window.playerTest.state.open = false')
                page.wait_for_function("!document.querySelector('.player-wrapper')")
                immediate = page.evaluate('window.playerTest.segmentStats().bytes')
                assert immediate == playing, 'closing should preserve the short reopen grace period'
                page.clock.fast_forward(10_000)
                page.evaluate('window.playerTest.state.open = true')
                page.wait_for_function("!!document.querySelector('.player-wrapper')")
                page.clock.fast_forward(25_000)
                reopened = page.evaluate('window.playerTest.segmentStats().bytes')
                assert reopened == playing, 'an old idle deadline trimmed a reopened player'
                page.evaluate('window.playerTest.state.open = false')
                page.wait_for_function("!document.querySelector('.player-wrapper')")
                page.clock.fast_forward(31_000)
                idle = page.evaluate('window.playerTest.segmentStats().bytes')
                print(json.dumps({'playing_mib': playing/1024**2, 'immediate_close_mib': immediate/1024**2, 'reopened_mib': reopened/1024**2, 'idle_mib': idle/1024**2}))
                assert idle <= 8*1024**2, f'player idle cache retained {idle/1024**2:.1f} MiB after closing'
                assert not errors, errors
            finally:
                browser.close()
    finally:
        server.terminate()
        server.wait(timeout=10)


if __name__ == '__main__':
    main()
