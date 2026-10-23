"""Exercise production HLS/player code in an isolated hidden Wails 3 WebView2.

Only bindings and the local media proxy are simulated. No production app,
database, user profile, or browser session is opened or closed.
"""
import argparse
import base64
import json
import os
from pathlib import Path
import re
import subprocess
import shutil
import tempfile
import time
from urllib.parse import parse_qs, urlparse, urlencode
from urllib.request import urlopen

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', type=int, default=9252)
    parser.add_argument('--debug-port', type=int, default=9253)
    parser.add_argument('--cycles', type=int, default=8)
    parser.add_argument('--idle-seconds', type=int, default=10)
    parser.add_argument('--legacy-thumbnails', action='store_true')
    parser.add_argument('--seed-cache', action='store_true', help='Seed the real 70 MiB multi-episode cache before closing')
    parser.add_argument('--output-tag', help='Keep separate measurement results for comparisons')
    parser.add_argument('--cold-baseline', action='store_true', help='Measure before the first video/decoder is ever created')
    parser.add_argument('--natural-gc', action='store_true', help='Do not force GC at playback/idle measurement checkpoints')
    parser.add_argument('--render-offscreen', action='store_true', help='Render in an offscreen, taskbar-hidden native window rather than a hidden WebView')
    parser.add_argument('--memory-trace', action='store_true', help='Capture native allocator dumps and browser process roles through CDP')
    parser.add_argument('--play-seconds', type=float, default=2.1)
    parser.add_argument('--max-retained-mib', type=float, help='Fail if idle private commitment exceeds the cold baseline by this amount')
    parser.add_argument('--reclaim-at-end', action='store_true', help='Record an explicit test-only GC comparison after the natural idle samples')
    parser.add_argument('--no-preview', action='store_true', help='Minimize to the primary video decoder without requesting thumbnails')
    parser.add_argument('--bare-video', action='store_true', help='Use a native MP4 video without the application player/HLS/cache')
    parser.add_argument('--extra-browser-arg', action='append', default=[])
    parser.add_argument('--pressure-at-end', action='store_true', help='Measure an explicit test-only memory pressure notification')
    parser.add_argument('--blank-at-end', action='store_true', help='Compare against unloading the whole document after idle')
    parser.add_argument('--production', action='store_true', help='Compile the fixture with production Vue, keeping only the API/proxy seams simulated')
    args = parser.parse_args()
    if os.name != 'nt':
        raise RuntimeError('This probe requires Windows and installed WebView2')
    url = f'http://127.0.0.1:{args.port}'
    media_root = ROOT / 'frontend/dist/resource-probe-media'
    (ROOT/'frontend/dist/native-blank.html').write_text('<!doctype html><meta charset="utf-8"><title>Isolated playback probe</title>',encoding='utf-8')
    flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
    probe = ROOT/f'dist/audit/.native-probe/probe-{args.debug_port}.exe'
    probe.parent.mkdir(parents=True,exist_ok=True)
    subprocess.run(['go','build','-ldflags=-H windowsgui','-o',str(probe),'./scripts/fixtures/native-playback-probe'],cwd=ROOT,check=True,creationflags=flags)
    if not (media_root/'index.m3u8').exists():
        media_root.mkdir(parents=True,exist_ok=True)
        ffmpeg = shutil.which('ffmpeg')
        if not ffmpeg:
            raise RuntimeError('ffmpeg is required to generate the local H264 fixture')
        subprocess.run([ffmpeg,'-hide_banner','-loglevel','error','-f','lavfi','-i','testsrc2=size=1080x608:rate=24','-t','90','-c:v','libx264','-preset','ultrafast','-crf','28','-g','96','-keyint_min','96','-sc_threshold','0','-pix_fmt','yuv420p','-f','hls','-hls_time','4','-hls_list_size','0',str(media_root/'index.m3u8')],check=True,creationflags=flags)
    # Vite production builds clean dist/. Keep this small fixture in the test
    # runner so parallel verification cannot remove a segment during playback.
    media_data = {path.name:path.read_bytes() for path in media_root.iterdir() if path.is_file()}
    if args.bare_video and 'sample.mp4' not in media_data:
        subprocess.run([shutil.which('ffmpeg'),'-hide_banner','-loglevel','error','-i','index.m3u8','-c','copy','-movflags','+faststart','sample.mp4'],cwd=media_root,check=True,creationflags=flags)
        media_data['sample.mp4'] = (media_root/'sample.mp4').read_bytes()
    server = subprocess.Popen(['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', str(args.port), '--strictPort'], cwd=ROOT/'frontend', stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=flags)
    native = None
    browser = None
    temp_profile = None
    samples = []
    if args.production and args.legacy_thumbnails:
        raise RuntimeError('Legacy thumbnail comparison uses the development fixture')
    tag = 'production' if args.production else 'legacy' if args.legacy_thumbnails else 'fixed'
    if args.output_tag:
        if not re.fullmatch(r'[a-zA-Z0-9_-]{1,64}', args.output_tag):
            raise ValueError('Output tag must contain 1-64 letters, digits, underscores or hyphens')
        tag = args.output_tag
    output = ROOT / f'dist/audit/native-playback-{tag}.json'
    native_log = ROOT / f'dist/audit/native-playback-{tag}-resources.jsonl'
    stop_path = Path(str(native_log)+'.stop')
    stop_path.unlink(missing_ok=True)
    def stop_native():
        if native is None or native.poll() is not None:
            return
        stop_path.write_text('stop',encoding='ascii')
        try:
            native.wait(timeout=5)
        except subprocess.TimeoutExpired:
            native.terminate()  # Only this runner's isolated helper.
            native.wait(timeout=10)
    try:
        for _ in range(100):
            try:
                with urlopen(url+'/tests/browser/player.html', timeout=1):
                    break
            except OSError:
                if server.poll() is not None:
                    raise RuntimeError('Vite server exited')
                time.sleep(.2)
        else:
            raise RuntimeError('Vite did not become ready')
        bindings = (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
        names = re.findall(r'export function (\w+)\(', bindings)
        mock = '\n'.join(f'export async function {name}() {{ return {json.dumps([] if name in ("ListPlugins", "RescanPlugins") else False if name.startswith("WindowIs") else "")}; }}' for name in names)
        fixture = '/tests/browser/player.html'
        if args.production:
            prod_mock = mock+'\nexport function normalizeApiError(error) { return {code:"INTERNAL",message:String(error)}; }'
            site = f'native-probe-site-{args.debug_port}'
            config = '''import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import path from 'path';
const mock = MOCK_SOURCE;
export default defineConfig({
  base:'/dist/SITE_NAME/',
  plugins:[vue(),{name:'native-probe-bindings',enforce:'pre',load(id){if(id.replaceAll('\\\\','/').endsWith('/src/api/app.ts'))return mock;}}],
  build:{outDir:'dist/SITE_NAME',emptyOutDir:true,rollupOptions:{input:path.resolve('tests/browser/player.html')}}
});'''.replace('MOCK_SOURCE',json.dumps(prod_mock)).replace('SITE_NAME',site)
            config_path = ROOT/f'frontend/.native-probe-vite-{args.debug_port}.mjs'
            config_path.write_text(config,encoding='utf-8')
            try:
                subprocess.run(['node','node_modules/vite/bin/vite.js','build','--config',str(config_path)],cwd=ROOT/'frontend',creationflags=flags,check=True,stdout=subprocess.DEVNULL)
            finally:
                config_path.unlink(missing_ok=True)
            fixture = f'/dist/{site}/tests/browser/player.html'
        temp_profile = tempfile.TemporaryDirectory(prefix='cczj-native-playback-', ignore_cleanup_errors=True)
        profile = temp_profile.name
        with sync_playwright() as playwright:
            probe_env = os.environ.copy()
            if args.render_offscreen:
                probe_env['CCZJ_PROBE_RENDER_OFFSCREEN'] = '1'
            probe_env['CCZJ_PROBE_EXTRA_BROWSER_ARGS'] = json.dumps(args.extra_browser_arg)
            native = subprocess.Popen([str(probe), url+'/dist/native-blank.html', profile, str(args.debug_port), str(native_log)], creationflags=flags, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, env=probe_env)
            debug = f'http://127.0.0.1:{args.debug_port}'
            for _ in range(100):
                try:
                    with urlopen(debug+'/json/version', timeout=1):
                        break
                except OSError:
                    if native.poll() is not None:
                        raise RuntimeError('Native probe exited before CDP became ready')
                    time.sleep(.2)
            else:
                raise RuntimeError('Native WebView2 debug endpoint did not become ready')
            browser = playwright.chromium.connect_over_cdp(debug)
            page = browser.contexts[0].pages[0]
            # The CDP target exists before Wails issues its initial navigation.
            # Wait for it rather than racing our fixture against that navigation.
            page.wait_for_url(url+'/dist/native-blank.html', timeout=20000)
            page.wait_for_load_state('networkidle')
            errors = []
            console = []
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.on('console', lambda message: console.append(message.text[:350]))
            page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))
            if args.legacy_thumbnails:
                baseline = subprocess.check_output(['git','show','HEAD:frontend/src/player/useThumbnailScrubber.ts'], cwd=ROOT).decode('utf-8')
                baseline = baseline.replace("'../utils/tsCache'", "'/src/utils/tsCache'").replace("'./hls/proxy'", "'/src/player/hls/proxy'")
                baseline = baseline.replace('    onProgressHover,\n', '    onProgressHover,\n    onProgressLeave: () => { progressHoverPct.value = -1; thumbPreviewVisible.value = false },\n    snapshot: () => ({active: !!thumbVideoRef.value}),\n')
                baseline_path = ROOT/'frontend/dist/thumbnail-probe-baseline.ts'
                baseline_path.write_text(baseline, encoding='utf-8')
                def legacy(route):
                    response = route.fetch(url=url+'/dist/thumbnail-probe-baseline.ts')
                    route.fulfill(response=response)
                page.route('**/src/player/useThumbnailScrubber.ts*', legacy)

            def proxy(route):
                encoded = parse_qs(urlparse(route.request.url).query).get('u', [''])[0]
                remote = base64.urlsafe_b64decode(encoded+'='*(-len(encoded)%4)).decode()
                parsed = urlparse(remote)
                assert remote.startswith(url+'/dist/resource-probe-media/'), remote
                source = media_root/Path(parsed.path).name
                if source.suffix == '.m3u8':
                    lines = media_data[source.name].decode().splitlines()
                    rewritten = []
                    for line in lines:
                        if line and not line.startswith('#'):
                            upstream = url+'/dist/resource-probe-media/'+line
                            value = base64.urlsafe_b64encode(upstream.encode()).decode().rstrip('=')
                            line = '/__cczj/hls?u='+value
                        rewritten.append(line)
                    route.fulfill(body='\n'.join(rewritten), content_type='application/vnd.apple.mpegurl')
                else:
                    route.fulfill(body=media_data[source.name], content_type='video/mp4' if source.suffix == '.mp4' else 'video/mp2t')
            page.route('**/__cczj/hls?*', proxy)
            query = {'media':url+'/dist/resource-probe-media/'+('sample.mp4' if args.bare_video else 'index.m3u8')}
            if args.bare_video:
                query['bare'] = '1'
            if args.cold_baseline:
                query['closed'] = '1'
            page.goto(url+fixture+'?'+urlencode(query))
            try:
                page.wait_for_function("!!window.playerTest" if args.cold_baseline else "document.querySelector('video')?.readyState >= 2", timeout=30000, polling=100)
            except Exception:
                print(json.dumps({'errors':errors,'console':console[-15:],'page':page.evaluate('({url:location.href,text:document.body.innerText.slice(0,800),videos:[...document.querySelectorAll("video")].map(v=>({ready:v.readyState,error:v.error?.message,source:v.currentSrc}))})')},ensure_ascii=True))
                raise
            cdp = page.context.new_cdp_session(page)
            cdp.send('Performance.enable')
            media_players = {}
            def media_properties(event):
                values = media_players.setdefault(event['playerId'], {})
                values.update({prop['name']:prop['value'] for prop in event['properties']})
            cdp.on('Media.playerPropertiesChanged', media_properties)
            cdp.send('Media.enable')
            browser_cdp = browser.new_browser_cdp_session()
            gpu_info = browser_cdp.send('SystemInfo.getInfo')
            trace_complete = []
            if args.memory_trace:
                browser_cdp.on('Tracing.tracingComplete', lambda value: trace_complete.append(value))
                browser_cdp.send('Tracing.start', {'categories':'-*,disabled-by-default-memory-infra', 'transferMode':'ReturnAsStream', 'streamFormat':'json'})

            def measure(label, gc=False, force=False):
                gc = gc and (not args.natural_gc or force)
                if gc:
                    cdp.send('HeapProfiler.collectGarbage')
                page.wait_for_timeout(1100)
                stats = {item['name']:item['value'] for item in cdp.send('Performance.getMetrics')['metrics']}
                native_rows = native_log.read_text(encoding='utf-8').splitlines()
                resource = next(json.loads(line) for line in reversed(native_rows) if line.endswith('}'))
                roles = browser_cdp.send('SystemInfo.getProcessInfo')['processInfo']
                dump = browser_cdp.send('Tracing.requestMemoryDump', {'deterministic':False, 'levelOfDetail':'detailed'}) if args.memory_trace else None
                samples.append({'label':label,'test_gc':gc,'native':resource,'process_roles':roles,'memory_dump':dump,
                    'browser':{key:stats[key] for key in ('JSHeapUsedSize','Nodes','JSEventListeners')},
                    'media_properties':json.loads(json.dumps(media_players)),
                    'cache':page.evaluate('window.playerTest?.segmentStats() || {}'),
                    'resources':page.evaluate('window.playerTest?.resources() || null')})

            page.evaluate('window.playerTest.state.open = false')
            page.wait_for_function("!document.querySelector('.player-wrapper')", polling=100)
            measure('cold-baseline' if args.cold_baseline else 'closed-baseline', True)
            for cycle in range(args.cycles):
                page.evaluate('(cycle) => { window.playerTest.state.line = cycle+2; window.playerTest.state.open = true }',cycle)
                try:
                    page.wait_for_function("document.querySelector('video')?.readyState >= 2", timeout=30000, polling=100)
                except Exception:
                    print(json.dumps({'errors':errors,'console':console[-8:],'media':page.evaluate('([...document.querySelectorAll("video")].map(v=>({source:v.currentSrc,error:v.error?.message,ready:v.readyState})))')},ensure_ascii=True))
                    raise
                page.evaluate("document.querySelector('video').play()")
                page.wait_for_timeout(args.play_seconds * 1000)
                if cycle == 0:
                    if args.seed_cache:
                        page.evaluate('window.playerTest.seedSegmentCache()')
                    measure('playing-no-preview')
                    if not args.legacy_thumbnails:
                        assert page.locator('video').count() == 1, 'an unused preview decoder was eagerly created'
                if not args.no_preview and not args.bare_video:
                    page.locator('.progress-container').dispatch_event('mousemove', {'clientX': 300})
                    page.wait_for_timeout(1800)
                    if cycle == 0:
                        measure('playing-preview')
                        assert page.locator('video').count() == 2, 'preview was not created on demand'
                    page.locator('.progress-container').dispatch_event('mouseleave')
                page.evaluate('window.playerTest.state.open = false')
                page.wait_for_function("!document.querySelector('.player-wrapper')", polling=100)
                assert page.locator('video').count() == 0
                measure(f'closed-cycle-{cycle+1}', True)
            for elapsed in range(0, args.idle_seconds, 30):
                page.wait_for_timeout(min(30,args.idle_seconds-elapsed)*1000)
                measure(f'closed-idle-{min(elapsed+30,args.idle_seconds)}s')
            measure('closed-idle', True)
            final, first = samples[-1], samples[0]
            if args.pressure_at_end:
                cdp.send('Memory.simulatePressureNotification', {'level':'critical'})
                page.wait_for_timeout(2000)
                measure('test-only-critical-pressure')
            if args.blank_at_end:
                page.goto(url+'/dist/native-blank.html')
                page.wait_for_timeout(2000)
                measure('test-only-unload-document')
            if args.reclaim_at_end:
                measure('test-only-gc-after-idle', True, force=True)
            if args.memory_trace:
                browser_cdp.send('Tracing.end')
                for _ in range(300):
                    if trace_complete:
                        break
                    page.wait_for_timeout(100)
                if not trace_complete:
                    raise RuntimeError('Memory trace did not finish')
                handle = trace_complete[-1]['stream']
                try:
                    with (ROOT/f'dist/audit/native-playback-{tag}.trace.json').open('wb') as trace_file:
                        while True:
                            chunk = browser_cdp.send('IO.read', {'handle':handle, 'size':1024*1024})
                            trace_file.write(base64.b64decode(chunk['data']) if chunk.get('base64Encoded') else chunk['data'].encode())
                            if chunk['eof']:
                                break
                finally:
                    browser_cdp.send('IO.close', {'handle':handle})
            scenario = {'production':args.production,'cold_baseline':args.cold_baseline,'natural_gc':args.natural_gc,'render_offscreen':args.render_offscreen,'play_seconds':args.play_seconds,'cycles':args.cycles,'idle_seconds':args.idle_seconds,'extra_browser_args':args.extra_browser_arg,'bare_video':args.bare_video,'no_preview':args.no_preview}
            output.write_text(json.dumps({'scenario':scenario,'gpu_info':gpu_info,'samples':samples},indent=2),encoding='utf-8')
            if not args.natural_gc and (final['browser']['JSEventListeners'] > first['browser']['JSEventListeners']+20 or final['browser']['Nodes'] > first['browser']['Nodes']+100):
                heap_path = ROOT/f'dist/audit/native-playback-{tag}.heapsnapshot'
                with heap_path.open('w',encoding='utf-8') as heap_file:
                    def chunk(event):
                        heap_file.write(event['chunk'])
                    cdp.on('HeapProfiler.addHeapSnapshotChunk',chunk)
                    cdp.send('HeapProfiler.takeHeapSnapshot',{'reportProgress':False})
                    cdp.remove_listener('HeapProfiler.addHeapSnapshotChunk',chunk)
            if not args.natural_gc:
                assert final['browser']['JSHeapUsedSize']-first['browser']['JSHeapUsedSize'] < 8*1024**2, samples
                assert final['browser']['JSEventListeners'] <= first['browser']['JSEventListeners']+20, {'first':first['browser'],'final':final['browser'],'report':str(output)}
                assert final['browser']['Nodes'] <= first['browser']['Nodes']+100, {'first':first['browser'],'final':final['browser'],'report':str(output)}
            assert final['resources']['player'] is None
            if args.seed_cache and args.idle_seconds >= 30:
                assert final['cache']['bytes'] <= 8*1024**2, 'closed player cache did not shrink to the idle budget'
            assert not errors, errors
            retained = (final['native']['tree_private_bytes']-first['native']['tree_private_bytes'])/1024**2
            if args.max_retained_mib is not None:
                assert retained <= args.max_retained_mib, f'Closed player retained {retained:.1f} MiB above baseline (budget {args.max_retained_mib:.1f})'
            print(json.dumps({'scenario':tag,'cycles':args.cycles,'baseline_private_mib':first['native']['tree_private_bytes']/1024**2,'final_private_mib':final['native']['tree_private_bytes']/1024**2,'retained_js_delta_mib':(final['browser']['JSHeapUsedSize']-first['browser']['JSHeapUsedSize'])/1024**2,'report':str(output)},ensure_ascii=True))
            cdp.detach()
            browser.close()
            browser = None
            stop_native()
            native = None
    finally:
        stop_native()
        server.terminate()
        server.wait(timeout=10)
        if temp_profile is not None:
            profile_path = Path(temp_profile.name).resolve()
            if profile_path.parent == Path(tempfile.gettempdir()).resolve() and profile_path.name.startswith('cczj-native-playback-'):
                temp_profile.cleanup()


if __name__ == '__main__':
    main()
