"""Exercise the real Vue player with a local video and mocked Wails bindings.

Requires Python Playwright. Uses installed Chrome with --channel chrome locally,
or Playwright Chromium in CI. Starts/stops its own isolated Vite server.
"""
import argparse
import base64
import json
import os
from pathlib import Path
import re
import subprocess
import time
from urllib.request import urlopen
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--channel', default=None)
    parser.add_argument('--port', type=int, default=9246)
    args = parser.parse_args()
    url = f'http://127.0.0.1:{args.port}'
    flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
    server = subprocess.Popen(
        ['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', str(args.port), '--strictPort'],
        cwd=ROOT / 'frontend', stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=flags,
    )
    try:
        for _ in range(100):
            if server.poll() is not None:
                raise RuntimeError('Vite test server exited before becoming ready')
            try:
                with urlopen(url + '/tests/browser/player.html', timeout=1):
                    break
            except OSError:
                time.sleep(0.2)
        else:
            raise RuntimeError('Vite test server did not become ready')

        # Stub only the binding seam: the Vue component, media elements, HLS,
        # local storage and WebGL code all run unchanged in the browser.
        bindings = (ROOT / 'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
        names = re.findall(r'export function (\w+)\(', bindings)
        mock = '\n'.join(
            f'export async function {name}(...args) {{ return {json.dumps([] if name in ("ListPlugins", "RescanPlugins") else False if name.startswith("WindowIs") else "")}; }}'
            for name in names
            if name not in ('ListPlugins', 'RescanPlugins', 'ReadPluginFile', 'GetSetting', 'SetSetting', 'SetPluginEnabled', 'GetAllSources', 'GetSourceStats', 'GetRecentHistory', 'WindowIsMax', 'WindowSetFullscreen')
        )
        mock += '''
          export async function WindowIsMax() {
            if (window.__delayMax) return await new Promise(resolve => { window.__releaseMax = resolve; });
            return false;
          }
          export async function WindowSetFullscreen(enter) { (window.__fullscreenCalls ||= []).push(enter); }
          export async function GetAllSources() {
            if (window.__delaySources) await new Promise(resolve => { window.__releaseSources = resolve; });
            return [];
          }
          export async function GetSourceStats() { return []; }
          export async function GetRecentHistory() { return []; }
          export async function SetPluginEnabled(id, enabled) {
            const pack = window.__testPacks?.find(pack => pack.id === id);
            if (pack) pack.status = enabled ? 'ready' : 'disabled';
          }
          export async function ListPlugins() { return window.__testPacks || []; }
          export async function RescanPlugins() { return window.__testPacks || []; }
          export async function ReadPluginFile(id, path) { return window.__testFiles?.[path] || ''; }
          export async function GetSetting(key) { return window.__testSettings?.[key] || ''; }
          export async function SetSetting(key, value) { (window.__testSettings ||= {})[key] = value; }
        '''
        video_bytes = (ROOT / 'frontend/tests/browser/sample.webm').read_bytes()

        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(headless=True, channel=args.channel, args=[
                '--autoplay-policy=no-user-gesture-required', '--enable-unsafe-swiftshader',
            ])
            try:
                page = browser.new_page(viewport={'width': 1280, 'height': 800})
                page.add_init_script('if(window.name === "native-player-profile") window._wails = {environment:{OS:"windows"}}')
                errors = []
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))

                def proxy(route):
                    encoded = parse_qs(urlparse(route.request.url).query).get('u', [''])[0]
                    remote = base64.urlsafe_b64decode(encoded + '=' * (-len(encoded) % 4)).decode()
                    assert remote.startswith(url + '/tests/browser/sample.webm'), remote
                    headers = {'Accept-Ranges': 'bytes'}
                    range_header = route.request.headers.get('range', '')
                    if range_header.startswith('bytes='):
                        start_text, end_text = range_header[6:].split('-', 1)
                        start = int(start_text or 0)
                        end = min(int(end_text) if end_text else len(video_bytes) - 1, len(video_bytes) - 1)
                        headers['Content-Range'] = f'bytes {start}-{end}/{len(video_bytes)}'
                        route.fulfill(status=206, body=video_bytes[start:end + 1], headers=headers, content_type='video/webm')
                    else:
                        route.fulfill(body=video_bytes, headers=headers, content_type='video/webm')

                page.route('**/__cczj/hls?*', proxy)
                # A disposed page must not bind progress to another page's video.
                page.goto(url + '/tests/browser/player-page.html')
                page.wait_for_function('!!window.pageTest && window.pageTest.listenerCount() === 2')
                page.evaluate('window.pageTest.close()')
                page.wait_for_timeout(700)
                assert page.evaluate('window.pageTest.replacementBindings()') == 0, 'disposed page bound progress listeners to replacement video'
                assert page.evaluate('window.pageTest.listenerCount()') == 0

                # Slow startup must not install document listeners after unmount.
                page.add_init_script('window.__delaySources = true')
                page.goto(url + '/tests/browser/player-page.html')
                page.wait_for_function('!!window.pageTest && !!window.__releaseSources')
                page.evaluate('window.pageTest.close()')
                page.evaluate('window.__releaseSources()')
                page.wait_for_timeout(700)
                assert page.evaluate('window.pageTest.listenerCount()') == 0, 'late page startup installed document listeners'
                assert page.evaluate('window.pageTest.replacementBindings()') == 0
                page.goto(url + '/tests/browser/player.html')
                page.wait_for_load_state('networkidle')
                page.wait_for_function("document.querySelector('video')?.readyState >= 2")
                video = page.locator('video').first
                expect(page.locator('.player-title')).to_have_text('Episode 1')
                assert page.evaluate("document.querySelector('.player-wrapper').style.getPropertyPriority('animation')") != 'important', 'live player animations were disabled by teardown cleanup'
                # Wails 3 exposes its environment without the old window.go global.
                # Host identity is fixed for the document; choose the fixture
                # before mounting, just as the application composition root does.
                page.evaluate('window.name = "native-player-profile"')
                page.reload()
                page.wait_for_function("document.querySelector('video')?.readyState >= 2")
                page.evaluate('window.__fullscreenCalls = []')
                page.get_by_role('button', name='全屏（F）', exact=True).dispatch_event('click')
                page.wait_for_timeout(200)
                assert page.evaluate('window.__fullscreenCalls') == [True], 'Wails 3 player did not use native fullscreen'
                page.evaluate('window.playerTest.state.line = 8')
                page.wait_for_function("document.querySelector('video')?.readyState >= 2")
                assert page.evaluate("document.body.hasAttribute('data-player-fullscreen')") is True
                page.get_by_role('button', name='退出全屏', exact=True).dispatch_event('click')
                page.wait_for_timeout(200)
                assert page.evaluate('window.__fullscreenCalls') == [True, False]
                page.evaluate('window.__delayMax = true; window.__fullscreenCalls = []')
                page.get_by_role('button', name='全屏（F）', exact=True).dispatch_event('click')
                page.wait_for_function('!!window.__releaseMax')
                page.evaluate('window.playerTest.state.open = false')
                page.wait_for_function('!document.querySelector(".player-wrapper")')
                page.evaluate('window.__releaseMax(false)')
                page.wait_for_timeout(200)
                assert True not in page.evaluate('window.__fullscreenCalls'), 'closed player entered native fullscreen after a late max-state reply'
                assert page.evaluate("document.body.hasAttribute('data-player-fullscreen')") is False
                page.evaluate('window.name = ""')
                page.reload()
                page.wait_for_function("document.querySelector('video')?.readyState >= 2")
                assert page.locator('video').count() == 1, 'watching allocated an unused preview decoder'
                bounds = page.locator('.progress-container').bounding_box()
                page.locator('.progress-container').dispatch_event('mousemove', {'clientX': bounds['x']+bounds['width']*.5})
                page.wait_for_function('window.playerTest.resources().player.thumbnail.active')
                page.wait_for_function('!!document.querySelector(".progress-time-preview img")')
                assert page.locator('video').count() == 2
                page.locator('.progress-container').dispatch_event('mouseleave')
                page.wait_for_function('document.querySelectorAll("video").length === 1', timeout=12000)
                assert page.evaluate('window.playerTest.resources().player.thumbnail.active') is False

                # User play/pause must synchronize the visible state.
                page.evaluate("document.querySelector('video').play()")
                page.wait_for_function("!document.querySelector('video').paused")
                expect(page.locator('.pause-overlay')).to_be_hidden()
                # Background resource release must not pause the active media.
                page.evaluate('window._wails.dispatchWailsEvent({name:"common:WindowHide"})')
                page.wait_for_timeout(350)
                assert page.evaluate("!document.querySelector('video').paused && document.querySelector('video').currentTime > 0")
                page.evaluate('window._wails.dispatchWailsEvent({name:"common:WindowShow"})')

                page.evaluate("document.querySelector('video').pause()")
                expect(page.locator('.pause-overlay')).to_be_visible()

                # Line changes retain this episode's progress; a new episode
                # starts fresh even when it uses the same test media.
                # Disabled history auto-resume must not reset a live line switch.
                page.goto(url + '/tests/browser/player.html?resume=0')
                page.wait_for_function("document.querySelector('video')?.readyState >= 2")
                page.evaluate("document.querySelector('video').currentTime = 7")
                page.wait_for_function("document.querySelector('video').currentTime > 6.9")
                page.evaluate("window.playerTest.state.line = 2")
                page.wait_for_function("document.querySelector('video')?.readyState >= 2 && document.querySelector('video').currentTime >= 5.5")
                page.evaluate("window.playerTest.state.episode = 2")
                page.wait_for_function("document.querySelector('video')?.readyState >= 2 && document.querySelector('video').currentTime < 1")
                expect(page.locator('.player-title')).to_have_text('Episode 2')

                # Rapid changes and close during pending setup must not bring
                # back a player or a late fullscreen/body state.
                page.evaluate("window.playerTest.state.line = 4; window.playerTest.state.line = 5; window.playerTest.state.open = false")
                expect(page.locator('.player-wrapper')).to_have_count(0)
                page.wait_for_timeout(250)
                assert page.evaluate("document.body.hasAttribute('data-player-fullscreen')") is False
                page.evaluate("window.playerTest.state.open = true")
                page.wait_for_function("document.querySelector('video')?.readyState >= 2")
                bounds = page.locator('.progress-container').bounding_box()
                page.locator('.progress-container').dispatch_event('mousemove', {'clientX': bounds['x']+bounds['width']*.4})
                page.wait_for_function('document.querySelectorAll("video").length === 2')
                page.evaluate('window.__previewProbe = [...document.querySelectorAll("video")][1]; window.playerTest.state.open = false')
                page.wait_for_function('document.querySelectorAll("video").length === 0')
                assert page.evaluate('!window.__previewProbe.isConnected && !window.__previewProbe.hasAttribute("src") && window.__previewProbe.paused')
                assert page.evaluate('window.playerTest.resources().player') is None
                page.evaluate('window.__previewProbe = null; window.playerTest.state.open = true')
                page.wait_for_function("document.querySelector('video')?.readyState >= 2")
                expect(page.locator('.pause-overlay')).to_be_visible()
                # Exercise actual GLSL compilation and GPU resource replacement.
                page.evaluate("window.playerTest.enableFilm()")
                page.wait_for_function("[...document.querySelectorAll('.player-wrapper > canvas')].some(c => c.style.zIndex === '2' && c.width === 320 && c.height === 180)")
                page.evaluate("window.playerTest.state.line = 6")
                page.wait_for_function("document.querySelector('video')?.readyState >= 2 && [...document.querySelectorAll('.player-wrapper > canvas')].filter(c => c.style.zIndex === '2').length === 1")
                assert page.evaluate("[...document.querySelectorAll('.player-wrapper > canvas')].find(c => c.style.zIndex === '2').getContext('webgl2').getError()") == 0
                # Trust must precede even a module's top-level side effects.
                page.evaluate("void window.playerTest.seedScript()")
                page.wait_for_function('window.__scriptSyncDone === true')
                expect(page.get_by_role('button', name='暂不运行', exact=True)).to_have_count(0)
                assert page.evaluate("window.__testPacks[0].status") == 'disabled'
                assert page.evaluate("window.__scriptExecutions || 0") == 0
                page.evaluate("window.__scriptSyncDone = false; void window.playerTest.seedScript(1, false, true)")
                expect(page.get_by_role('button', name='暂不运行', exact=True)).to_be_visible()
                assert page.evaluate("window.__scriptExecutions || 0") == 0
                page.get_by_role('button', name='暂不运行', exact=True).click()
                page.wait_for_function('window.__scriptSyncDone === true')
                assert page.evaluate("window.__scriptExecutions || 0") == 0
                page.evaluate("window.__scriptSyncDone = false; void window.playerTest.seedScript(1, true, true)")
                page.get_by_role('button', name='信任并运行', exact=True).click()
                page.wait_for_function('window.__scriptSyncDone === true')
                assert page.evaluate('window.__scriptExecutions') == 1
                page.evaluate("window.__scriptSyncDone = false; void window.playerTest.seedScript(2)")
                page.wait_for_function('window.__scriptSyncDone === true')
                assert page.evaluate('window.__scriptExecutions') == 2
                expect(page.get_by_role('button', name='信任并运行', exact=True)).to_have_count(0)
                assert not errors, errors
                print('Browser regressions passed: page disposal and late startup, Wails 3 fullscreen and stale replies, live animations, lazy thumbnail capture, idle/close decoder release, playback, line resume, episode reset, rapid switch, Film WebGL rebuild and script consent')
            finally:
                browser.close()
    finally:
        server.terminate()
        try:
            server.wait(timeout=10)
        except subprocess.TimeoutExpired:
            server.kill()
            server.wait()


if __name__ == '__main__':
    main()
