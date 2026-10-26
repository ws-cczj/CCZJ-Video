"""Verify top-right lines and bottom danmaku in the isolated Android player.

Uses existing native bindings/media helpers; restores settings and temporary data.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import subprocess
import threading
import time
from http.server import ThreadingHTTPServer
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parent.parent


def module(name, file):
    spec = importlib.util.spec_from_file_location(name, ROOT/'scripts'/file)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    args = parser.parse_args()
    upgrade = module('upgrade', 'test-upgrade-android.py')
    media = module('player_media', 'test-upgrade-player-android.py')
    out = ROOT/'dist/audit/mobile-player-layout'
    clips = out/'media'
    clips.mkdir(parents=True, exist_ok=True)
    # UI evidence uses an unmarked picture; weak-network clips intentionally
    # contain a source timecode and must not be mistaken for player chrome.
    if not (clips/'movie.mp4').exists():
        subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-loop', '1', '-i',
                        str(ROOT/'frontend/src/assets/theme_images/china_ink.jpg'), '-vf',
                        'scale=640:360:force_original_aspect_ratio=increase,crop=640:360',
                        '-t', '30', '-r', '15', '-an', '-c:v', 'libx264', '-preset', 'ultrafast',
                        '-pix_fmt', 'yuv420p', '-movflags', '+faststart', str(clips/'movie.mp4')], check=True)
        subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-i', str(clips/'movie.mp4'),
                        '-c', 'copy', '-hls_time', '2', '-hls_list_size', '0', str(clips/'movie.m3u8')], check=True)
    media.MediaFixture.directory = clips
    identity = 'layout-'+str(time.time_ns())

    class PlayerFixture(media.MediaFixture):
        def do_GET(self, head=False):
            if urlparse(self.path).path != '/video-api':
                return super().do_GET(head)
            base = f'http://127.0.0.1:{self.server.server_port}/media/'
            row = {'vod_id': identity, 'vod_name': 'Player layout '+identity, 'type_id': '1',
                   'type_name': '测试', 'vod_play_from': 'MP4$$$HLS',
                   'vod_play_url': f'第一集${base}movie.mp4#第二集${base}movie.mp4?episode=2$$$第一集${base}movie.m3u8#第二集${base}movie.m3u8?episode=2'}
            body = json.dumps({'code': 1, 'page': 1, 'pagecount': 1, 'total': 1, 'list': [row]}).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            if not head:
                self.wfile.write(body)

    server = ThreadingHTTPServer(('127.0.0.1', 0), PlayerFixture)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    port = server.server_port
    device = upgrade.Device('adb', args.device)
    device.native('reverse', f'tcp:{port}', f'tcp:{port}')
    device.native('shell', 'am', 'start', '-W', '-n', upgrade.PACKAGE+'/com.wails.app.MainActivity')
    device.connect()
    keys = ['default_source_key', 'danmaku_sources_v1', 'danmaku_preferences_v1']
    settings = {key: device.rpc('GetSetting', key) for key in keys}
    original = {'url': device.js('location.href'), 'private': device.rpc('GetAllowPrivateNetwork')}
    source_key = ''
    checks = []

    def wait(expression):
        until = time.monotonic()+30
        while time.monotonic() < until:
            if device.js(expression):
                return
            time.sleep(.15)
        raise AssertionError(expression)

    def tap(selector):
        point = device.js('(()=>{const r=document.querySelector('+json.dumps(selector)+').getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()')
        device.call('Input.dispatchTouchEvent', {'type': 'touchStart', 'touchPoints': [point]})
        time.sleep(.08)
        device.call('Input.dispatchTouchEvent', {'type': 'touchEnd', 'touchPoints': []})
        time.sleep(.7)

    def reveal():
        if not device.js('document.querySelector(".player-wrapper").classList.contains("show-controls")'):
            tap('video')
        wait('document.querySelector(".cczj-mobile-player-lines")?.getBoundingClientRect().height>0')

    def inspect(label):
        reveal()
        state = device.js('''(()=>{
          const rect=s=>document.querySelector(s).getBoundingClientRect().toJSON();
          return {root:rect('.player-wrapper'),header:rect('.player-title-bar'),lines:rect('.cczj-mobile-player-lines'),
            danmaku:rect('.cczj-danmaku-actions'),bottom:rect('.cczj-mobile-player-controls'),
            topDanmaku:document.querySelectorAll('.player-title-bar .cczj-danmaku-actions').length,
            bottomLines:document.querySelectorAll('.cczj-mobile-player-controls .cczj-mobile-player-lines').length,
            items:[...document.querySelector('.cczj-mobile-player-controls').children].map(e=>({rect:e.getBoundingClientRect().toJSON(),scroll:e.scrollWidth,width:e.clientWidth}))};
        })()''')
        assert state['topDanmaku'] == 0 and state['bottomLines'] == 0, state
        assert state['lines']['x'] > state['root']['x']+state['root']['width']/2, state
        assert state['lines']['bottom'] <= state['header']['bottom']+1, state
        assert state['danmaku']['y'] >= state['bottom']['y']-1, state
        assert state['danmaku']['bottom'] <= state['bottom']['bottom']+1, state
        previous_right = state['bottom']['x']
        for item in state['items']:
            assert item['rect']['left'] >= previous_right-1, state
            assert item['rect']['right'] <= state['bottom']['right']+1, state
            assert item['scroll'] <= item['width']+1, state
            previous_right = item['rect']['right']
        (out/(label+'.png')).write_bytes(device.native('exec-out', 'screencap', '-p', binary=True))
        checks.append({'layout': label, 'geometry': state})

    try:
        device.rpc('SetAllowPrivateNetwork', True)
        device.rpc('SetSetting', 'danmaku_sources_v1', json.dumps({'defaultProvider': '', 'disabled': ['qq', 'bilibili', 'iqiyi']}))
        source = device.rpc('CreateSource', {'name': 'Mobile player layout', 'api_url': f'http://127.0.0.1:{port}/video-api', 'enabled': 1, 'kind': 'video'})
        source_key = source['source_key']
        device.rpc('SetSetting', 'default_source_key', source_key)
        result = device.rpc('SearchSourceRequest', source_key, 'Android', 1, 20, 'mobile-player-layout')
        device.rpc('ImportSourceVideos', source_key, result['videos'])
        global_id = device.rpc('GetGlobalIdForVideo', source_key, identity)
        device.js('(async()=>{await document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("source").loadSources(true)})()')
        device.visit(f'/player/{source_key}/{global_id}/0?vod={identity}')
        wait('document.querySelector("video")?.readyState>=2')
        wait('document.querySelector(".cczj-danmaku-toggle") && !document.querySelector(".cczj-danmaku-toggle").disabled')
        assert device.js('![...document.querySelectorAll(".side-section button")].some(e=>e.textContent.trim()==="切换线路")')
        assert device.js('!document.querySelector(".side-section").textContent.includes("播放源")')
        if device.js('!!document.querySelector(".cczj-mobile-resume")'):
            tap('.cczj-mobile-resume > button:last-child')
        inspect('portrait-360')
        before = device.js('document.querySelector(".cczj-danmaku-toggle").getAttribute("aria-pressed")')
        tap('.cczj-danmaku-toggle')
        assert device.js('document.querySelector(".cczj-danmaku-toggle").getAttribute("aria-pressed")') != before
        tap('.cczj-danmaku-toggle')
        tap('.cczj-mobile-player-lines')
        wait('document.querySelector("[role=dialog][aria-label=切换线路]")?.getBoundingClientRect().height>0')
        device.native('shell', 'input', 'keyevent', 'KEYCODE_BACK')
        wait('!document.querySelector(".cczj-mobile-player-lines").matches("[aria-expanded=true]")')
        device.call('Emulation.setDeviceMetricsOverride', {'width': 320, 'height': 700, 'deviceScaleFactor': 3, 'mobile': True})
        inspect('portrait-320')
        device.call('Emulation.clearDeviceMetricsOverride')
        reveal()
        tap('.cczj-mobile-fullscreen')
        wait('!!document.fullscreenElement && innerWidth>innerHeight')
        inspect('fullscreen')
        tap('.cczj-mobile-player-lines')
        wait('document.fullscreenElement.querySelector("[role=dialog][aria-label=切换线路]")?.getBoundingClientRect().height>0')
        print('PASS: right-top lines, bottom danmaku toggle, 320/360px non-overlap and native fullscreen line drawer')
        (out/'result.json').write_text(json.dumps(checks, ensure_ascii=False, indent=2), encoding='utf-8')
    except Exception as error:
        (out/'failure.png').write_bytes(device.native('exec-out', 'screencap', '-p', binary=True))
        state = device.js('({url:location.href,body:document.body.innerText,media:[...document.querySelectorAll("video")].map(v=>({src:v.currentSrc,ready:v.readyState,error:v.error?.message}))})')
        (out/'failure.json').write_text(json.dumps({'error': str(error), 'state': state}, ensure_ascii=False, indent=2), encoding='utf-8')
        raise
    finally:
        try:
            device.call('Emulation.clearDeviceMetricsOverride')
            if device.js('!!document.fullscreenElement'):
                device.js('document.exitFullscreen()')
                wait('!document.fullscreenElement')
            device.visit('/sources')
            if source_key:
                device.rpc('DeleteSource', source_key)
            for key, value in settings.items():
                device.rpc('SetSetting', key, value or '')
            device.rpc('SetAllowPrivateNetwork', original['private'])
            device.js('location.href='+json.dumps(original['url']))
        finally:
            device.close()
            device.native('reverse', '--remove', f'tcp:{port}')
            server.shutdown()
            server.server_close()


if __name__ == '__main__':
    main()
