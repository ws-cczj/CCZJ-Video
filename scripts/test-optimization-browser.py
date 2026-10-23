"""Real RemoteImage and KeepAlive resource lifecycle, with only Wails bindings mocked."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import time
import ctypes
from urllib.parse import quote
from urllib.request import urlopen
from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parent.parent

def browser_memory(browser, page):
    """Read only this isolated browser's PIDs, obtained from its own CDP session."""
    cdp = page.context.new_cdp_session(page)
    cdp.send('Performance.enable')
    cdp.send('HeapProfiler.collectGarbage')  # Test-only retained-memory measurement.
    metrics = {row['name']:row['value'] for row in cdp.send('Performance.getMetrics')['metrics']}
    result = {key:metrics[key] for key in ('JSHeapUsedSize','Nodes','JSEventListeners')}
    cdp.detach()
    if os.name != 'nt': return result
    session = browser.new_browser_cdp_session()
    processes = session.send('SystemInfo.getProcessInfo')['processInfo']
    session.detach()
    class Counters(ctypes.Structure):
        _fields_ = [('cb',ctypes.c_ulong),('faults',ctypes.c_ulong)] + [(name,ctypes.c_size_t) for name in ('peak_working_set','working_set','peak_paged','paged','peak_nonpaged','nonpaged','pagefile','peak_pagefile','private')]
    kernel = ctypes.WinDLL('kernel32',use_last_error=True)
    psapi = ctypes.WinDLL('psapi',use_last_error=True)
    kernel.OpenProcess.argtypes = [ctypes.c_ulong,ctypes.c_int,ctypes.c_ulong]
    kernel.OpenProcess.restype = ctypes.c_void_p
    kernel.CloseHandle.argtypes = [ctypes.c_void_p]
    psapi.GetProcessMemoryInfo.argtypes = [ctypes.c_void_p,ctypes.POINTER(Counters),ctypes.c_ulong]
    rows = []
    for process in processes:
        handle = kernel.OpenProcess(0x410,False,int(process['id']))
        if not handle: raise OSError(ctypes.get_last_error(),'cannot read isolated browser process')
        try:
            memory = Counters(); memory.cb = ctypes.sizeof(memory)
            if not psapi.GetProcessMemoryInfo(handle,ctypes.byref(memory),memory.cb):
                raise OSError(ctypes.get_last_error(),'cannot read browser memory')
            rows.append({'type':process['type'],'private_bytes':memory.private,'working_set_bytes':memory.working_set})
        finally: kernel.CloseHandle(handle)
    result['private_bytes'] = sum(row['private_bytes'] for row in rows)
    result['processes'] = rows
    return result

def prepare_baseline(ref):
    directory = ROOT/'frontend/dist/optimization-baseline'
    directory.mkdir(parents=True,exist_ok=True)
    # Historical files stay in ignored audit output; production files are never reset.
    utils = subprocess.check_output(['git','show',f'{ref}:frontend/src/utils/index.ts'],cwd=ROOT).decode('utf-8')
    utils = utils.replace("'../platform/","'../../src/platform/").replace("'../locales'","'../../src/locales'").replace("'../api/app'","'../../src/api/app'")
    utils += '\n;(window as any).__legacyImageProxyStats = imageProxyStats\n'
    (directory/'utils.ts').write_text(utils,encoding='utf-8')
    image = subprocess.check_output(['git','show',f'{ref}:frontend/src/components/RemoteImage.vue'],cwd=ROOT).decode('utf-8')
    image = image.replace("'../utils'","'./utils'").replace("'../assets/","'../../src/assets/").replace("'../stores/","'../../src/stores/")
    (directory/'RemoteImage.vue').write_text(image,encoding='utf-8')
    return directory/'RemoteImage.vue'

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--channel', default=None)
    parser.add_argument('--baseline', action='store_true')
    parser.add_argument('--baseline-ref', default=None, help='Compare only historical image code from a Git ref, without changing the checkout')
    parser.add_argument('--output-name', default=None, help='Optional audit output name for repeated measurements')
    parser.add_argument('--port', type=int, default=9250)
    args = parser.parse_args()
    legacy = prepare_baseline(args.baseline_ref) if args.baseline_ref else None
    if legacy: args.baseline = True
    flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
    server = subprocess.Popen(['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', str(args.port), '--strictPort'], cwd=ROOT/'frontend', stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=flags)
    url = f'http://127.0.0.1:{args.port}'
    try:
        for _ in range(100):
            try:
                with urlopen(url+'/tests/browser/optimization.html', timeout=1): break
            except OSError: time.sleep(.2)
        else: raise RuntimeError('test server did not become ready')
        bindings = (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
        names = re.findall(r'export function (\w+)\(', bindings)
        special = {'ProxyImage', 'GetRuntimeMetrics', 'GetDiagnostics', 'ListIdentityMergeCandidates'}
        mock = '\n'.join(f'export async function {name}() {{ return {"[]" if name in ("ListPlugins", "RescanPlugins") else chr(34)+chr(34)}; }}' for name in names if name not in special)
        mock += '''
          export async function ProxyImage(url) {
            (window.__imageRequests ||= []).push(url);
            if (window.__delayNextImage) { window.__delayNextImage = false; await new Promise(resolve => window.__finishImage = resolve); }
            const canvas = document.createElement('canvas'); canvas.width = 480; canvas.height = 720;
            const context = canvas.getContext('2d');
            context.fillStyle = `hsl(${Number(url.split('/').pop()) * 7},70%,50%)`;
            context.fillRect(0,0,480,720);
            return canvas.toDataURL();
          }
          export async function GetRuntimeMetrics() {
            window.__metricCalls = (window.__metricCalls || 0) + 1;
            return {goroutines: 17, uptime_seconds: 120, cache: [], network: {}, resources: {sampled_at_ms: Date.now(), heap_alloc_bytes: 1024}};
          }
          export async function GetDiagnostics() { return {env:{},storage:{},douban:{},collect:{},sources:[],tables:[],notes:[]}; }
          export async function ListIdentityMergeCandidates() { return {groups: []}; }
        '''
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(headless=True, channel=args.channel)
            try:
                page = browser.new_page(viewport={'width':1280,'height':800})
                errors = []
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock,content_type='text/javascript'))
                page.add_init_script("""
                  window.__activeIntervals = new Set();
                  const originalSetInterval = window.setInterval.bind(window);
                  const originalClearInterval = window.clearInterval.bind(window);
                  window.setInterval = (...args) => { const id = originalSetInterval(...args); if (args[1] === 100 || args[1] === 200) window.__activeIntervals.add(id); return id; };
                  window.clearInterval = id => { window.__activeIntervals.delete(id); originalClearInterval(id); };
                """)
                if legacy: page.add_init_script('window.__legacyImageURL = '+json.dumps('/@fs/'+quote(legacy.as_posix(),safe='/:')))
                page.goto(url+'/tests/browser/optimization.html'+('?legacy' if legacy else ''))
                page.wait_for_load_state('networkidle')
                page.wait_for_timeout(400)
                initial_requests = page.evaluate('(window.__imageRequests || []).length')
                initial = page.evaluate('window.optimizationTest.imageProxyStats()')
                initial_memory = browser_memory(browser,page)
                page.get_by_role('textbox').fill('retain my draft')
                page.evaluate('window.optimizationTest.state.page = "blank"')
                page.wait_for_timeout(900)
                # Read the retained component's DOM while it is in KeepAlive storage.
                retained = page.evaluate('window.optimizationTest.retainedImages ? window.optimizationTest.retainedImages() : -1')
                hidden_memory = browser_memory(browser,page)
                page.evaluate('window.optimizationTest.state.page = "posters"')
                expect(page.get_by_role('textbox')).to_have_value('retain my draft')
                if not args.baseline:
                    assert page.evaluate('window.__posterMounts') == 1, 'returning recreated cached page state'
                    page.locator('.main-content').evaluate('(el) => el.scrollTop = el.scrollHeight')
                    page.wait_for_function('(window.__imageRequests || []).includes("https://posters.example/119")')
                    page.wait_for_function("""document.querySelector('img[alt="Poster 119"]')?.naturalWidth === 480""")
                    # Backgrounding releases sources too, without discarding cached page state.
                    page.evaluate('Object.defineProperty(document, "hidden", { configurable:true, get:() => window.__simulateHidden || false }); window.__simulateHidden = true; document.dispatchEvent(new Event("visibilitychange"))')
                    page.wait_for_timeout(100)
                    assert page.evaluate('window.optimizationTest.retainedImages()') == 0
                    page.evaluate('window.__simulateHidden = false; document.dispatchEvent(new Event("visibilitychange"))')
                    page.wait_for_function("""document.querySelector('img[alt="Poster 119"]')?.naturalWidth === 480""")
                    # Create a fresh slow poster at the bottom, then deactivate before it returns.
                    page.evaluate('window.__delayNextImage = true; window.optimizationTest.state.count = 121')
                    page.wait_for_function('!!window.__finishImage')
                    page.evaluate('window.optimizationTest.state.page = "blank"')
                    page.wait_for_timeout(100)
                    page.evaluate('window.__finishImage()')
                    page.wait_for_timeout(800)
                    assert page.evaluate('window.optimizationTest.retainedImages()') == 0, 'late image request repopulated a hidden page'
                page.evaluate('window.optimizationTest.state.page = "diagnostics"')
                page.wait_for_function('(window.__metricCalls || 0) >= 1')
                page.evaluate('window.optimizationTest.state.page = "blank"')
                page.wait_for_timeout(100)
                before = page.evaluate('window.__metricCalls')
                page.wait_for_timeout(2400)
                hidden_calls = page.evaluate('window.__metricCalls') - before
                if not args.baseline:
                    page.evaluate('window.optimizationTest.state.page = "diagnostics"')
                    page.wait_for_function('(window.__metricCalls || 0) > '+str(before))
                    # Native minimize and tray events can precede DOM visibility changes.
                    page.evaluate('window._wails.dispatchWailsEvent({name:"common:WindowMinimise"})')
                    page.wait_for_timeout(100)
                    minimized_calls = page.evaluate('window.__metricCalls')
                    page.wait_for_timeout(2200)
                    assert page.evaluate('window.__metricCalls') == minimized_calls
                    page.evaluate('window._wails.dispatchWailsEvent({name:"common:WindowUnMinimise"})')
                    page.wait_for_function('(window.__metricCalls || 0) > '+str(minimized_calls))
                    page.evaluate('window.optimizationTest.state.page = "carousel"')
                    page.wait_for_function('window.__activeIntervals.size === 1')
                    page.evaluate('window._wails.dispatchWailsEvent({name:"common:WindowHide"})')
                    page.wait_for_function('window.__activeIntervals.size === 0')
                    page.evaluate('window.optimizationTest.state.carouselInterval = 100')
                    page.wait_for_timeout(300)
                    assert page.evaluate('window.__activeIntervals.size') == 0, 'hidden carousel restarted after settings changed'
                    page.evaluate('window._wails.dispatchWailsEvent({name:"common:WindowShow"})')
                    page.wait_for_function('window.__activeIntervals.size === 1')
                    page.evaluate('window.optimizationTest.state.page = "blank"')
                    page.wait_for_timeout(100)
                    steady = browser_memory(browser,page)
                    repeated = []
                    for cycle in range(24):
                        page.evaluate('window.optimizationTest.state.page = "posters"')
                        page.wait_for_timeout(50)
                        page.evaluate('window.optimizationTest.state.page = "blank"')
                        page.wait_for_timeout(50)
                        if (cycle + 1) % 12 == 0: repeated.append(browser_memory(browser,page))
                    page.wait_for_timeout(1800)
                    final = browser_memory(browser,page)
                    assert final['JSHeapUsedSize'] - steady['JSHeapUsedSize'] < 8*1024**2, 'repeated image activation retained unbounded JS heap'
                    assert final['JSEventListeners'] <= steady['JSEventListeners'] + 20, 'repeated image activation retained listeners'
                    if all('private_bytes' in row for row in repeated):
                        assert repeated[-1]['private_bytes'] - repeated[-2]['private_bytes'] < 32*1024**2, 'rapid image restoration kept growing native browser allocation after warmup'
                result = {'initial_poster_requests':initial_requests,'image_cache':initial,'retained_active_image_sources':retained,'hidden_diagnostics_calls':hidden_calls,'initial_memory':initial_memory,'hidden_memory':hidden_memory}
                if not args.baseline: result['repeat_activation'] = {'before':steady,'intermediate':repeated,'after':final,'cycles':24}
                if legacy:
                    result['historical_image_ref'] = subprocess.check_output(['git','rev-parse',args.baseline_ref],cwd=ROOT).decode().strip()
                name = args.output_name or ('legacy-images' if legacy else 'before' if args.baseline else 'after')
                output = ROOT/f'dist/audit/optimization-{name}.json'
                output.parent.mkdir(parents=True,exist_ok=True)
                output.write_text(json.dumps(result,indent=2),encoding='utf-8')
                print(json.dumps({key:result[key] for key in ('initial_poster_requests','image_cache','retained_active_image_sources','hidden_diagnostics_calls')} | {'native_private_mb':round(initial_memory['private_bytes']/1024**2,1) if 'private_bytes' in initial_memory else None,'audit':output.name}))
                if not args.baseline:
                    assert initial_requests < 60, 'offscreen posters were fetched before entering viewport'
                    assert retained == 0, 'deactivated page retained active image sources'
                    assert hidden_calls == 0, 'KeepAlive diagnostics kept polling on another route'
                assert not errors, errors
            finally: browser.close()
    finally:
        server.terminate()
        try: server.wait(timeout=10)
        except subprocess.TimeoutExpired: server.kill(); server.wait()

if __name__ == '__main__': main()
