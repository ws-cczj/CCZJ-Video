"""Read UI/layout and exercise the shipped HLS runtime in an installed debug preview.

No sources, history, settings or licence acceptance are written. The temporary
muted test video is removed before disconnecting. Uses websocket-client 1.8.0.
"""
import argparse
import json
from pathlib import Path
import subprocess
import time
from urllib.request import urlopen

import websocket

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', required=True)
    parser.add_argument('--playback', action='store_true')
    parser.add_argument('--package', default='com.cczj.video.preview')
    args = parser.parse_args()
    (ROOT / 'dist/audit').mkdir(parents=True, exist_ok=True)
    adb = ['adb', '-s', args.device]
    subprocess.run(adb + ['shell', 'am', 'start', '-n', args.package+'/com.wails.app.MainActivity'], check=True, capture_output=True)
    time.sleep(1)
    pid = subprocess.check_output(adb + ['shell', 'pidof', args.package], text=True).strip()
    assert pid and ' ' not in pid, 'preview app must already be running'
    port = subprocess.check_output(adb + ['forward', 'tcp:0', f'localabstract:webview_devtools_remote_{pid}'], text=True).strip()
    socket = None
    identifier = 0
    network = []
    def call(method, params=None):
        nonlocal identifier
        identifier += 1
        socket.send(json.dumps({'id': identifier, 'method': method, 'params': params or {}}))
        while True:
            result = json.loads(socket.recv())
            if result.get('method') == 'Network.responseReceived':
                response = result['params']['response']
                if '/__cczj/hls' in response.get('url', ''):
                    network.append({'status': response['status'], 'mime': response.get('mimeType')})
            if result.get('method') == 'Network.loadingFailed':
                network.append({'error': result['params'].get('errorText')})
            if result.get('id') == identifier:
                if 'error' in result:
                    raise RuntimeError(result['error'])
                return result.get('result', {})
    def evaluate(expression):
        result = call('Runtime.evaluate', {'expression': expression, 'awaitPromise': True, 'returnByValue': True})
        if 'exceptionDetails' in result:
            raise RuntimeError(result['exceptionDetails'])
        return result.get('result', {}).get('value')
    try:
        with urlopen(f'http://127.0.0.1:{port}/json/list', timeout=10) as response:
            targets = json.load(response)
        target = next(row for row in targets if row.get('url', '').startswith('https://wails.localhost'))
        socket = websocket.create_connection(target['webSocketDebuggerUrl'], timeout=20, origin='http://localhost')
        call('Network.enable')
        state = evaluate('''({platform:document.documentElement.dataset.cczjPlatform,
          viewport:[innerWidth,innerHeight], bodyWidth:document.body.scrollWidth,
          titlebar:document.querySelector('.titlebar') ? getComputedStyle(document.querySelector('.titlebar')).display : 'none',
          dialogs:[...document.querySelectorAll('[role=dialog]')].map(x=>x.getAttribute('aria-label'))})''')
        print(json.dumps({'layout': state}), flush=True)
        assert state['platform'] == 'mobile' and state['titlebar'] == 'none', state
        assert state['bodyWidth'] <= state['viewport'][0], state
        assert evaluate("!!document.querySelector('.cczj-mobile-shell') && !!document.querySelector('.cczj-mobile-navigation')"), 'isolated mobile shell missing'
        (ROOT / 'dist/audit/android-preview-mobile.png').write_bytes(subprocess.check_output(adb + ['exec-out', 'screencap', '-p']))
        if args.playback:
            assert not state['dialogs'], 'dismiss first-run dialogs yourself before testing playback'
            evaluate('''(async () => {
              const entry = document.querySelector('script[type=module][src]').src;
              const references = (text, base) => [...text.matchAll(/["']((?:\.{0,2}\/)?(?:assets\/)?[A-Za-z0-9_./-]+\.js)["']/g)].map(match => new URL(match[1], match[1].startsWith('assets/') ? document.baseURI : base).href);
              const entryText = await (await fetch(entry)).text();
              const entryImports = references(entryText, entry);
              const player = entryImports.find(url => /\/Player[-.][^/]+\.js$/.test(url));
              const playerImports = player ? references(await (await fetch(player)).text(), player) : [];
              const hlsURL = [...entryImports,...playerImports].find(url => /\/hls[-.][^/]+\.js$/.test(url));
              if (!hlsURL) throw Error('Cannot locate HLS in the shipped module references');
              const {default:Hls} = await import(hlsURL);
              if (!Hls.isSupported()) throw Error('MSE not supported by this Android WebView');
              const host = document.createElement('div'); host.id='cczj-android-smoke';
              host.style.cssText='position:fixed;inset:0;z-index:2147483647;background:#111;color:white;display:flex;flex-direction:column;justify-content:center';
              const label=document.createElement('p'); label.textContent='Android HLS playback verification';
              const video=document.createElement('video'); video.muted=true;video.playsInline=true;video.controls=true;video.style.width='100%';
              const button=document.createElement('button');button.textContent='Fullscreen test';button.style.minHeight='48px';
              button.onclick=()=>host.requestFullscreen();
              host.append(label,video,button);document.body.append(host);
              const hls=new Hls({enableWorker:false,maxBufferLength:8,maxMaxBufferLength:12,backBufferLength:0,maxBufferSize:4*1024**2,startLevel:0});
              const errors=[],events=[];hls.on(Hls.Events.ERROR,(_event,data)=>{events.push({event:'error',detail:data.details,fatal:data.fatal});if(data.fatal)errors.push(data.details)});
              for(const name of ['MANIFEST_PARSED','LEVEL_LOADED','FRAG_LOADING','FRAG_LOADED','BUFFER_APPENDED'])hls.on(Hls.Events[name],()=>{events.push({event:name});if(events.length>30)events.shift()});
              window.__cczjAndroidSmoke={hls,host,video,errors,events};
              hls.on(Hls.Events.MANIFEST_PARSED,()=>{hls.currentLevel=0;video.play().catch(error=>errors.push(String(error)))});
              const source='https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8';
              const encoded=btoa(source).replace(/\+/g,'-').replace(/\//g,'_').replace(/=+$/,'');
              hls.loadSource('/__cczj/hls?u='+encoded);hls.attachMedia(video);
            })()''')
            media = None
            deadline = time.monotonic() + 120
            while time.monotonic() < deadline:
                media = evaluate('''(() => {const s=window.__cczjAndroidSmoke;return {time:s.video.currentTime,width:s.video.videoWidth,height:s.video.videoHeight,paused:s.video.paused,ready:s.video.readyState,buffered:Array.from({length:s.video.buffered.length},(_,i)=>[s.video.buffered.start(i),s.video.buffered.end(i)]),errors:s.errors,events:s.events}})()''')
                if media['errors'] or media['time'] > 3:
                    break
                time.sleep(.5)
            print(json.dumps({'media': media, 'network': network[-15:]}), flush=True)
            assert media and media['time'] > 3 and media['width'] > 0 and not media['errors'], media
            (ROOT / 'dist/audit/android-preview-playback.png').write_bytes(subprocess.check_output(adb + ['exec-out', 'screencap', '-p']))
            box = evaluate('''(() => {const r=window.__cczjAndroidSmoke.host.querySelector('button').getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()''')
            for action in ('mousePressed', 'mouseReleased'):
                call('Input.dispatchMouseEvent', {'type':action, 'x':box['x'], 'y':box['y'], 'button':'left', 'clickCount':1})
            time.sleep(1)
            assert evaluate('!!document.fullscreenElement'), 'Android fullscreen request failed'
            dimensions = evaluate('({width:innerWidth,height:innerHeight})')
            assert dimensions['width'] > dimensions['height'], ('fullscreen did not rotate to landscape', dimensions)
            subprocess.run(adb + ['shell', 'input', 'keyevent', 'KEYCODE_BACK'], check=True)
            time.sleep(1)
            assert evaluate('!document.fullscreenElement'), 'Android Back did not exit fullscreen'
            restored = evaluate('[innerWidth,innerHeight]')
            assert (restored[0] > restored[1]) == (state['viewport'][0] > state['viewport'][1]), ('orientation not restored', restored)
            print('PASS: Android HLS playback via the Go proxy and fullscreen')
        print('PASS: Android native UI and mobile layout')
    finally:
        try:
            if socket is not None:
                try:
                    evaluate('''(async () => {if(document.fullscreenElement)await document.exitFullscreen();const s=window.__cczjAndroidSmoke;if(s){s.hls.destroy();s.video.pause();s.video.removeAttribute('src');s.video.load();s.host.remove();delete window.__cczjAndroidSmoke}})()''')
                finally:
                    socket.close()
        finally:
            subprocess.run(adb + ['forward', '--remove', f'tcp:{port}'], check=False, capture_output=True)


if __name__ == '__main__':
    main()
