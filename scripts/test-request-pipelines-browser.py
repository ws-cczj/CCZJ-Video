"""Exercise real API policies and the cached Sources view; replace IPC only."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import time
from urllib.request import urlopen
from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent.parent

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--channel', default='msedge')
    parser.add_argument('--port', type=int, default=9321)
    args = parser.parse_args()
    output = ROOT/'reports/request-pipelines-2026-10-20'
    output.mkdir(parents=True, exist_ok=True)
    origin = f'http://127.0.0.1:{args.port}'
    server = subprocess.Popen(['node','node_modules/vite/bin/vite.js','--host','127.0.0.1','--port',str(args.port),'--strictPort'], cwd=ROOT/'frontend', stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0)
    try:
        for _ in range(100):
            if server.poll() is not None: raise RuntimeError('Vite failed to start')
            try:
                urlopen(origin+'/tests/browser/request-pipelines.html', timeout=1).close(); break
            except OSError: time.sleep(.1)
        names = re.findall(r'^export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'), re.M)
        mock = "import { CancellablePromise } from '/tests/browser/pipelineRuntime.ts';\n"
        mock += """
        function invoke(method,args) {
          (window.__calls ||= []).push({method,args});
          if(method==='ProbeSources')return new CancellablePromise(resolve=>{(window.__probes ||= []).push(resolve)},()=>{window.__probeCancelled=(window.__probeCancelled||0)+1});
          if(method==='GetSetting'&&args[0]==='pipeline-burst')return new CancellablePromise(resolve=>window.__releaseBurst=resolve);
          if(method==='SearchSourceRequest')return new CancellablePromise(resolve=>{(window.__searches ||= []).push({resolve,args})},()=>{window.__searchCancelled=(window.__searchCancelled||0)+1});
          if(method==='SearchVideos'&&window.__holdLocalSearch)return new CancellablePromise(resolve=>{(window.__localQueries ||= []).push({resolve,args})},()=>{window.__localCancelled=(window.__localCancelled||0)+1});
          if(method==='GetSetting'||method==='GetAppVersion')return CancellablePromise.resolve('');
          return CancellablePromise.resolve([]);
        }
        """
        mock += '\n'.join(f'export function {name}(...args){{return invoke({json.dumps(name)},args)}}' for name in names)
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True, channel=args.channel)
            try:
                page = browser.new_page(viewport={'width':1280,'height':800})
                errors = []
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.add_init_script("window._wails={environment:{OS:'windows'},flags:{frameless:true}};window.chrome ||= {};window.chrome.webview={postMessage:()=>{}};")
                page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))
                page.goto(origin+'/tests/browser/request-pipelines.html')
                page.wait_for_load_state('networkidle')
                page.locator('.sources-page').wait_for(state='visible')
                page.wait_for_function('window.__probes?.length===1')
                page.evaluate('window.pipelineTest.state.active=false')
                page.wait_for_function('window.__probeCancelled===1')
                page.evaluate('window.__probes[0]([])')
                page.wait_for_function("window.pipelineTest.snapshot().find(row=>row.pipeline==='rpc.ProbeSources')?.active===0")
                assert page.evaluate("window.__calls.filter(call=>call.method==='SourceProbeTimeline').length") == 0, 'leaving view started a late timeline read'
                page.evaluate('window.pipelineTest.state.active=true')
                page.wait_for_function('window.__probes.length===2')
                page.evaluate('window.__probes[1]([])')
                page.wait_for_function("window.__calls.filter(call=>call.method==='SourceProbeTimeline').length===1")
                page.evaluate("() => {window.__burst=Array.from({length:64},()=>window.pipelineTest.api.GetSetting('pipeline-burst'));window.__burstResult=Promise.all(window.__burst)}")
                assert page.evaluate("window.__calls.filter(call=>call.method==='GetSetting'&&call.args[0]==='pipeline-burst').length") == 1
                page.evaluate("window.__releaseBurst('loaded')")
                values = page.evaluate('window.__burstResult')
                assert values == ['loaded']*64
                page.evaluate("""() => {
                  window.pipelineTest.sources.sources=[{source_key:'pipeline_source',name:'Pipeline source',api_url:'https://example.invalid',enabled:1}];
                  window.pipelineTest.sources.currentSourceKey='pipeline_source';window.pipelineTest.sources.loaded=true;
                  localStorage.setItem('source_search_mode','true');window.pipelineTest.state.view='search';
                }""")
                page.locator('.search-input').wait_for(state='visible')
                page.wait_for_function("document.querySelector('.source-search-toggle')?.getAttribute('aria-pressed')==='true'")
                page.locator('.search-input').fill('query')
                page.locator('.search-input').press('Enter')
                page.wait_for_function('window.__searches?.length===1')
                page.locator('.search-input').press('Enter')
                page.wait_for_function('window.__searches.length===2&&window.__searchCancelled===1')
                page.evaluate("""() => {
                  window._wails.dispatchWailsEvent({name:'search:progress',data:{requestId:window.__searches[0].args[4],stage:'fetching_list',message:'OBSOLETE-PIPELINE-MARKER',current:0,total:1}});
                  window._wails.dispatchWailsEvent({name:'search:progress',data:{requestId:window.__searches[1].args[4],stage:'fetching_list',message:'CURRENT-PIPELINE-MARKER',current:0,total:1}});
                }""")
                page.get_by_text('CURRENT-PIPELINE-MARKER',exact=True).wait_for(state='visible')
                assert page.get_by_text('OBSOLETE-PIPELINE-MARKER',exact=True).count()==0
                page.evaluate('window.pipelineTest.state.active=false')
                page.wait_for_function('window.__searchCancelled===2')
                page.evaluate('window.pipelineTest.state.active=true')
                page.wait_for_function('window.__searches.length===3')
                page.evaluate("window.__searches[2].resolve({total:0,page_count:1,videos:[]})")
                page.wait_for_function("window.pipelineTest.snapshot().find(row=>row.pipeline==='rpc.SearchSourceRequest')?.active===0")
                page.locator('.source-search-toggle').click()
                page.evaluate('window.__holdLocalSearch=true')
                page.locator('.search-input').fill('local query')
                page.locator('.search-input').press('Enter')
                page.wait_for_function('window.__localQueries?.length===1')
                page.evaluate('window.pipelineTest.state.active=false')
                page.wait_for_function('window.__localCancelled===1')
                page.evaluate('window.pipelineTest.state.active=true')
                page.wait_for_function('window.__localQueries.length===2')
                page.evaluate("window.__localQueries[1].resolve({total:0,videos:[],local_videos:[]})")
                page.wait_for_function("window.pipelineTest.snapshot().find(row=>row.pipeline==='rpc.SearchVideos')?.active===0")
                stats = page.evaluate('window.pipelineTest.snapshot()')
                assert not errors, errors
                result = {'passed':True, 'checks':['cached view cancellation','no late timeline read','reactivation','64 callers share one IPC','source search cancels superseded request','progress filters request identity','source search resumes after activation','video search cancellation and resumption'], 'pipelines':stats, 'pageErrors':errors}
                (output/'browser-result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
                page.screenshot(path=str(output/'sources-pipeline-browser.png'))
                print('Pipeline browser checks passed: cancellation, reactivation, no late I/O, 64:1 deduplication.')
            finally: browser.close()
    finally:
        server.terminate()
        try: server.wait(timeout=5)
        except subprocess.TimeoutExpired: server.kill(); server.wait()

if __name__ == '__main__': main()
