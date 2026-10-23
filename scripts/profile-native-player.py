"""Attribute player retention using a disposable real WebView2 and raw CDP.

No Playwright Network/Fetch agent, response-body capture, Vite development client,
production database or user profile is involved. Only the Wails API seam is
simulated; production player code, HLS and native decoders run unchanged.
"""
import argparse
import base64
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import mimetypes
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import threading
import time
from urllib.parse import parse_qs, urlparse
from urllib.request import urlopen

import websocket

ROOT = Path(__file__).resolve().parent.parent
MIB = 1024**2


class CDP:
    def __init__(self, endpoint):
        self.socket = websocket.create_connection(endpoint, timeout=60, suppress_origin=True)
        self.seq = 0
        self.media = {}
        self.trace = None

    def send(self, method, params=None, session=None):
        self.seq += 1
        request = {'id': self.seq, 'method': method, 'params': params or {}}
        if session:
            request['sessionId'] = session
        self.socket.send(json.dumps(request))
        while True:
            message = json.loads(self.socket.recv())
            if message.get('method') == 'Media.playerPropertiesChanged':
                values = self.media.setdefault(message['params']['playerId'], {})
                values.update({p['name']: p['value'] for p in message['params']['properties']})
            if message.get('method') == 'Tracing.tracingComplete':
                self.trace = message['params']
            if message.get('id') == self.seq:
                if 'error' in message:
                    raise RuntimeError(f'{method}: {message["error"]}')
                return message.get('result', {})

    def evaluate(self, expression, session):
        result = self.send('Runtime.evaluate', {'expression': expression, 'awaitPromise': True, 'returnByValue': True}, session)
        if 'exceptionDetails' in result:
            raise RuntimeError(str(result['exceptionDetails']))
        return result.get('result', {}).get('value')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', type=int, default=9280)
    parser.add_argument('--debug-port', type=int, default=9281)
    parser.add_argument('--tag', required=True)
    parser.add_argument('--bare', action='store_true')
    parser.add_argument('--flag', action='append', default=[])
    parser.add_argument('--cycles', type=int, default=1)
    parser.add_argument('--play-seconds', type=int, default=5)
    parser.add_argument('--idle-seconds', type=int, default=35)
    parser.add_argument('--trace', action='store_true')
    parser.add_argument('--controls', action='store_true', help='Compare test-only memory pressure, GC and document unload after natural idle')
    parser.add_argument('--no-media-agent', action='store_true', help='Control for Media inspector instrumentation')
    parser.add_argument('--max-retained-mib', type=float)
    args = parser.parse_args()
    if os.name != 'nt':
        raise RuntimeError('This profile requires Windows and installed WebView2')
    if not re.fullmatch(r'[a-zA-Z0-9_-]{1,64}', args.tag):
        raise ValueError('Use a simple output tag')
    output = ROOT/'dist/audit'/f'native-profile-{args.tag}'
    output.mkdir(parents=True, exist_ok=True)
    site = output/'site'
    site.mkdir(exist_ok=True)
    flags = subprocess.CREATE_NO_WINDOW
    media = ROOT/'dist/audit/.native-probe-media'
    media.mkdir(parents=True, exist_ok=True)
    ffmpeg = shutil.which('ffmpeg')
    if not ffmpeg:
        raise RuntimeError('ffmpeg is required to generate the local H264 fixture')
    if not (media/'index.m3u8').exists():
        subprocess.run([ffmpeg,'-hide_banner','-loglevel','error','-f','lavfi','-i','testsrc2=size=1080x608:rate=24','-t','90','-c:v','libx264','-preset','ultrafast','-crf','28','-g','96','-keyint_min','96','-sc_threshold','0','-pix_fmt','yuv420p','-f','hls','-hls_time','4','-hls_list_size','0','index.m3u8'],cwd=media,check=True,creationflags=flags)
    if not (media/'sample.mp4').exists():
        subprocess.run([ffmpeg,'-hide_banner','-loglevel','error','-i','index.m3u8','-c','copy','-movflags','+faststart','sample.mp4'],cwd=media,check=True,creationflags=flags)
    media_data = {p.name: p.read_bytes() for p in media.iterdir() if p.is_file()}
    exe = output/'probe.exe'
    subprocess.run(['go', 'build', '-ldflags=-H windowsgui', '-o', str(exe), './scripts/fixtures/native-playback-probe'], cwd=ROOT, check=True, creationflags=flags)
    if not args.bare:
        bindings = (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
        names = re.findall(r'export function (\w+)\(', bindings)
        mock = '\n'.join(f'export async function {name}() {{ return {json.dumps([] if name in ("ListPlugins", "RescanPlugins") else False if name.startswith("WindowIs") else "")}; }}' for name in names)
        mock += '\nexport function normalizeApiError(error) { return {code:"INTERNAL",message:String(error)}; }'
        config = """import {defineConfig} from 'vite';import vue from '@vitejs/plugin-vue';import path from 'path';
const mock=MOCK;export default defineConfig({base:'/',plugins:[vue(),{name:'profile-api',enforce:'pre',load(id){if(id.replaceAll('\\\\','/').endsWith('/src/api/app.ts'))return mock;}}],build:{outDir:OUT,emptyOutDir:true,rollupOptions:{input:path.resolve('tests/browser/player.html')}}});""".replace('MOCK', json.dumps(mock)).replace('OUT', json.dumps(site.as_posix()))
        config_path = ROOT/f'frontend/.native-profile-{args.debug_port}.mjs'
        config_path.write_text(config, encoding='utf-8')
        try:
            subprocess.run(['node', 'node_modules/vite/bin/vite.js', 'build', '--config', str(config_path)], cwd=ROOT/'frontend', check=True, creationflags=flags, stdout=subprocess.DEVNULL)
        finally:
            config_path.unlink(missing_ok=True)
    url = f'http://127.0.0.1:{args.port}'

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def do_GET(self):
            parsed = urlparse(self.path)
            path = parsed.path
            if path == '/__cczj/hls':
                value = parse_qs(parsed.query)['u'][0]
                upstream = urlparse(base64.urlsafe_b64decode(value+'='*(-len(value)%4)).decode())
                if upstream.netloc != f'127.0.0.1:{args.port}' or not upstream.path.startswith('/media/'):
                    self.send_error(403)
                    return
                path = upstream.path
            if path == '/blank.html':
                body, mime = b'<!doctype html><title>Native resource probe</title>', 'text/html'
            elif path.startswith('/media/'):
                name = path.removeprefix('/media/')
                if name not in media_data:
                    self.send_error(404)
                    return
                body = media_data[name]
                mime = 'video/mp4' if name.endswith('.mp4') else 'application/vnd.apple.mpegurl' if name.endswith('.m3u8') else 'video/mp2t'
                if name.endswith('.m3u8'):
                    body = '\n'.join(url+'/media/'+line if line and not line.startswith('#') else line for line in body.decode().splitlines()).encode()
            else:
                file = (site/path.lstrip('/')).resolve()
                if not file.is_relative_to(site.resolve()) or not file.is_file():
                    self.send_error(404)
                    return
                body = file.read_bytes()
                mime = 'application/javascript' if file.suffix == '.js' else mimetypes.guess_type(file)[0] or 'application/octet-stream'
            total = len(body)
            requested = re.fullmatch(r'bytes=(\d+)-(\d*)', self.headers.get('Range', ''))
            if requested:
                start, end = int(requested[1]), min(int(requested[2]) if requested[2] else total-1, total-1)
                if start >= total:
                    self.send_error(416)
                    return
                body = body[start:end+1]
                self.send_response(206)
                self.send_header('Content-Range', f'bytes {start}-{end}/{total}')
            else:
                self.send_response(200)
            self.send_header('Content-Type', mime)
            self.send_header('Content-Length', str(len(body)))
            self.send_header('Accept-Ranges', 'bytes')
            self.end_headers()
            try:
                self.wfile.write(body)
            except (ConnectionResetError, BrokenPipeError):
                pass

    server = ThreadingHTTPServer(('127.0.0.1', args.port), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    native = None
    cdp = None
    log = output/'resources.jsonl'
    stop = Path(str(log)+'.stop')
    stop.unlink(missing_ok=True)
    samples = []
    with tempfile.TemporaryDirectory(prefix='cczj-native-profile-', ignore_cleanup_errors=True) as profile:
        try:
            env = {**os.environ, 'CCZJ_PROBE_RENDER_OFFSCREEN': '1', 'CCZJ_PROBE_EXTRA_BROWSER_ARGS': json.dumps(args.flag)}
            native = subprocess.Popen([str(exe), url+'/blank.html', profile, str(args.debug_port), str(log)], env=env, creationflags=flags, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            for _ in range(100):
                try:
                    with urlopen(f'http://127.0.0.1:{args.debug_port}/json/version', timeout=1) as response:
                        version = json.load(response)
                    break
                except OSError:
                    if native.poll() is not None:
                        raise RuntimeError('Isolated native host exited')
                    time.sleep(.2)
            else:
                raise RuntimeError('CDP did not start')
            cdp = CDP(version['webSocketDebuggerUrl'])
            for _ in range(100):
                target = next((t for t in cdp.send('Target.getTargets')['targetInfos'] if t['type'] == 'page' and t['url'] == url+'/blank.html'), None)
                if target:
                    break
                time.sleep(.1)
            else:
                raise RuntimeError('Native initial navigation did not finish')
            session = cdp.send('Target.attachToTarget', {'targetId':target['targetId'], 'flatten':True})['sessionId']
            cdp.send('Page.enable', session=session)
            cdp.send('Performance.enable', session=session)
            if not args.no_media_agent:
                cdp.send('Media.enable', session=session)
            gpu = cdp.send('SystemInfo.getInfo')
            if args.trace:
                cdp.send('Tracing.start', {'categories':'-*,disabled-by-default-memory-infra', 'transferMode':'ReturnAsStream', 'streamFormat':'json'})
            if not args.bare:
                cdp.send('Page.navigate', {'url':url+'/tests/browser/player.html?closed=1&media='+url+'/media/index.m3u8'}, session)
                for _ in range(200):
                    if cdp.evaluate('!!window.playerTest', session):
                        break
                    time.sleep(.1)
                else:
                    raise RuntimeError('Production fixture did not initialize')

            def measure(label):
                time.sleep(1.1)
                snapshot = json.loads(log.read_text(encoding='utf-8').splitlines()[-1])
                metrics = {p['name']:p['value'] for p in cdp.send('Performance.getMetrics', session=session)['metrics']}
                dump = cdp.send('Tracing.requestMemoryDump', {'deterministic':False,'levelOfDetail':'detailed'}) if args.trace else None
                samples.append({'label':label, 'native':snapshot, 'metrics':metrics, 'roles':cdp.send('SystemInfo.getProcessInfo')['processInfo'], 'media':json.loads(json.dumps(cdp.media)), 'dump':dump,
                    'page':cdp.evaluate('({videos:document.querySelectorAll("video").length,cache:window.playerTest?.segmentStats(),resources:window.playerTest?.resources()})', session)})
                print(json.dumps({'stage':label,'private_mib':round(snapshot['tree_private_bytes']/MIB,1)}), flush=True)

            measure('before-first-play')
            for cycle in range(args.cycles):
                if args.bare:
                    cdp.evaluate("{let v=document.createElement('video');v.muted=true;v.preload='auto';v.style.width='1080px';v.src='/media/sample.mp4';document.body.append(v);}", session)
                else:
                    cdp.evaluate('window.playerTest.state.open=true', session)
                for _ in range(200):
                    if cdp.evaluate("document.querySelector('video')?.readyState>=2", session):
                        break
                    time.sleep(.1)
                else:
                    raise RuntimeError(str(cdp.evaluate("({text:document.body.innerText.slice(0,200),video:[...document.querySelectorAll('video')].map(v=>({src:v.currentSrc,error:v.error?.message,ready:v.readyState}))})", session)))
                cdp.evaluate("document.querySelector('video').play().then(()=>true)", session)
                time.sleep(args.play_seconds)
                measure(f'playing-{cycle+1}')
                if args.bare:
                    cdp.evaluate("{let v=document.querySelector('video');v.pause();v.removeAttribute('src');v.load();v.remove();}", session)
                else:
                    cdp.evaluate('window.playerTest.state.open=false', session)
                measure(f'closed-{cycle+1}')
            time.sleep(args.idle_seconds)
            measure('natural-idle')
            natural = samples[-1]
            if args.controls:
                cdp.send('HeapProfiler.collectGarbage', session=session)
                measure('test-only-gc')
                cdp.send('Memory.simulatePressureNotification', {'level':'critical'}, session)
                time.sleep(2)
                measure('test-only-pressure')
                cdp.send('Page.navigate', {'url':url+'/blank.html'}, session)
                time.sleep(2)
                cdp.send('HeapProfiler.collectGarbage', session=session)
                measure('test-only-unload-gc')
            if args.trace:
                cdp.send('Tracing.end')
                for _ in range(100):
                    cdp.send('Browser.getVersion')
                    if cdp.trace:
                        break
                    time.sleep(.1)
                if not cdp.trace:
                    raise RuntimeError('Memory trace did not finish')
                handle = cdp.trace['stream']
                try:
                    with (output/'memory.trace.json').open('wb') as trace:
                        while True:
                            chunk = cdp.send('IO.read', {'handle':handle, 'size':MIB})
                            trace.write(base64.b64decode(chunk['data']) if chunk.get('base64Encoded') else chunk['data'].encode())
                            if chunk['eof']:
                                break
                finally:
                    cdp.send('IO.close', {'handle':handle})
            retained = (natural['native']['tree_private_bytes']-samples[0]['native']['tree_private_bytes'])/MIB
            (output/'summary.json').write_text(json.dumps({'options':vars(args), 'browser_version':cdp.send('Browser.getVersion'), 'gpu':gpu, 'samples':samples, 'natural_retained_mib':retained}, indent=2), encoding='utf-8')
            print(json.dumps({'retained_mib':round(retained,1),'output':str(output)}), flush=True)
            if args.max_retained_mib is not None:
                assert retained <= args.max_retained_mib, f'Native retention {retained:.1f} MiB exceeds {args.max_retained_mib} MiB'
        finally:
            if cdp:
                cdp.socket.close()
            if native and native.poll() is None:
                stop.write_text('stop', encoding='ascii')
                try:
                    native.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    native.terminate()
                    native.wait(timeout=10)
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)


if __name__ == '__main__':
    main()
