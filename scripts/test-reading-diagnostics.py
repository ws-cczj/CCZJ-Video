"""Reading settings/diagnostic layout and actions through real Vue with RPC fixtures."""
import argparse
import json
import importlib.util
import time
import re
from pathlib import Path
from playwright.sync_api import sync_playwright, expect
ROOT=Path(__file__).resolve().parent.parent

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port',type=int,default=19320)
    parser.add_argument('--output',type=Path,default=ROOT/'dist/audit/settings-layout')
    parser.add_argument('--native',action='store_true',help='Use the installed acceptance Android host and real Go services')
    parser.add_argument('--device',default='emulator-5554')
    args=parser.parse_args();args.output.mkdir(parents=True,exist_ok=True)
    if args.native:
        native(args);return
    calls=[];records={};active=[False];checks=[];errors=[]
    status=lambda:dict(active=active[0],path='fixture-recording' if active[0] else '',samples=3,markers=1,started_at_ms=1,ended_at_ms=0,reason='',error='')
    def rpc(name,values):
        calls.append(name)
        if name=='ReadingStateLoad':
            if values[0] not in records:records[values[0]]=json.loads(values[1])
            return records[values[0]]
        if name=='ReadingStateApply':return None
        if name=='LibrarySources':return [dict(sourceKey='fixture',name='测试来源',kind=values[0],apiUrl='https://example.org',enabled=True)]
        if name=='GetRuntimeMetrics':return dict(uptime_seconds=60,goroutines=46,resources=dict(native_available=True,native_scope='tree',tree_private_bytes=438933913,heap_alloc_bytes=27577548),recording=status(),network=dict(categories=[dict(category='novel',bytes=2*1048576,requests=28,fails=2,cancelled=1)]))
        if name=='GetDiagnostics':return dict(storage=dict(database_bytes=9*1048576),tables=[dict(name='library_items',rows=42),dict(name='library_chapters',rows=6800)],notes=[])
        if name=='StartDiagnosticsRecording':active[0]=True;return status()
        if name=='StopDiagnosticsRecording':active[0]=False;return status()
        if name=='AddDiagnosticsRecordingMarker':return True
        if name=='ExportDiagnosticsReport':return dict(bundle_path='C:/fixture/report.zip',path='C:/fixture/report.json')
        if name=='GetSetting':return ''
        return None
    names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
    mock="import {CancellablePromise} from '/tests/browser/runtime.ts';\nfunction call(name,args){const controller=new AbortController();return new CancellablePromise((resolve,reject)=>{fetch('/__settings_diagnostic',{method:'POST',body:JSON.stringify({name,args}),signal:controller.signal}).then(r=>r.json()).then(resolve,e=>{if(!controller.signal.aborted)reject(e)})},()=>controller.abort())}\n"
    mock+='\n'.join('export function '+name+'(...args){return call('+json.dumps(name)+',args)}' for name in names)
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True,channel='chrome')
        try:
            page=browser.new_page(viewport=dict(width=1280,height=900))
            page.on('pageerror',lambda e:errors.append(str(e)))
            page.route('**/bindings/cczjVideo/app/service/app*',lambda r:r.fulfill(body=mock,content_type='text/javascript'))
            page.route('**/__settings_diagnostic',lambda r:r.fulfill(body=json.dumps(rpc(r.request.post_data_json['name'],r.request.post_data_json['args'])),content_type='application/json'))
            page.goto(f'http://127.0.0.1:{args.port}/tests/browser/reading.html#/novel/diagnostics')
            page.wait_for_load_state('networkidle')
            expect(page.locator('.reading-diag-heading h1')).to_have_text('运行诊断')
            expect(page.locator('.reading-diag-cards')).to_contain_text('418.6 MiB')
            expect(page.locator('.cczj-runtime-diagnostics-page > button')).to_have_count(0)
            assert page.locator('.cczj-runtime-diagnostics-page').evaluate('(el)=>el.getBoundingClientRect().left')>50
            assert page.locator('.reading-diag-actions button').evaluate_all('(items)=>items.every(el=>el.clientHeight>=38)')
            assert page.locator('.cczj-diag-coverage').get_attribute('open') is None
            page.screenshot(path=str(args.output/'novel-desktop.png'))
            checks.append('desktop centered frame, heading, consistent actions, folded notes')
            page.get_by_text('数据采集与指标说明',exact=True).click()
            expect(page.locator('.cczj-diag-coverage-content')).to_be_visible()
            expect(page.locator('.cczj-diag-observation-row')).to_have_count(4)
            page.get_by_text('数据采集与指标说明',exact=True).click()
            page.get_by_text('这些数字包含哪些资源？',exact=True).click()
            expect(page.locator('.reading-diag-scope p')).to_be_visible()
            page.get_by_text('这些数字包含哪些资源？',exact=True).click()
            page.get_by_role('button',name='开始记录',exact=True).click()
            expect(page.get_by_role('button',name='结束并导出',exact=True)).to_have_attribute('aria-pressed','true')
            page.get_by_role('button',name='结束并导出',exact=True).click()
            page.get_by_role('button',name='导出诊断报告',exact=True).click()
            expect(page.get_by_role('button',name='打开报告目录',exact=False)).to_be_visible()
            assert 'ExportDiagnosticsReport' in calls and 'StartDiagnosticsRecording' in calls and 'StopDiagnosticsRecording' in calls
            page.get_by_role('button',name='刷新',exact=True).click()
            expect(page.get_by_role('button',name='刷新',exact=True)).to_be_enabled()
            checks.append('notes, recording, export and refresh retain actual handler wiring')
            tabs=page.locator('.reading-diag-tabs button')
            for text in ['启动与过渡','业务活动','网络与缓存','运行环境与存储']:
                tabs.filter(has_text=text).click()
                expect(tabs.filter(has_text=text)).to_have_attribute('aria-pressed','true')
                if text=='网络与缓存':expect(page.locator('.reading-diag-cards')).to_contain_text('2.0 MiB')
                if text=='运行环境与存储':expect(page.locator('.reading-diag-content')).to_contain_text('library_chapters')
            checks.append('all diagnostic sections remain reachable')
            for mode in ['novel','comic']:
                for width,height in [(1280,900),(375,812),(320,740)]:
                    page.set_viewport_size(dict(width=width,height=height))
                    page.evaluate('(path)=>location.hash=path',f'/{mode}/diagnostics')
                    expect(page.locator('.reading-diag-heading h1')).to_be_visible()
                    page.locator('.reading-diag-tabs button').first.click()
                    expect(page.locator('.reading-diag-cards')).to_contain_text('418.6 MiB')
                    assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
                    assert page.locator('.cczj-runtime-diagnostics-page').evaluate('(el)=>el.scrollWidth<=el.clientWidth+1')
                    for theme in ['light','dark']:
                        page.evaluate('(value)=>document.documentElement.dataset.themeMode=value',theme)
                        page.locator('.main-content').last.evaluate('(el)=>el.scrollTop=0')
                        selected=page.locator('.reading-diag-tabs .ui-seg__item--active')
                        selected.hover()
                        page.wait_for_function('!document.querySelector(".reading-diag-tabs .ui-seg__item--active").getAnimations().some(a=>a.playState==="running")')
                        contrast=selected.evaluate('''el=>{const rgb=s=>s.match(/[0-9.]+/g).slice(0,3).map(Number);const lum=c=>c.map(v=>{v/=255;return v<=.04045?v/12.92:((v+.055)/1.055)**2.4}).reduce((a,v,i)=>a+v*[.2126,.7152,.0722][i],0);const a=lum(rgb(getComputedStyle(el).color)),b=lum(rgb(getComputedStyle(el.parentElement.querySelector(".ui-seg__indicator")).backgroundColor));return (Math.max(a,b)+.05)/(Math.min(a,b)+.05)}''')
                        assert contrast>=4.5,(mode,width,theme,contrast)
                        page.locator('.main-content').last.evaluate('(el)=>el.scrollTop=0')
                        page.screenshot(path=str(args.output/f'{mode}-{width}-{theme}.png'))
                    # Settings hub and its reading/mode subpages use the same real route tree.
                    for suffix in ['settings','settings?group=reading','settings?group=mode']:
                        page.evaluate('(path)=>location.hash=path',f'/{mode}/{suffix}')
                        expect(page.locator(f'.{mode}-settings')).to_be_visible()
                        assert page.locator(f'.{mode}-settings').evaluate('(el)=>el.scrollWidth<=el.clientWidth+1')
                    if width==375:page.screenshot(path=str(args.output/f'{mode}-settings-mobile.png'))
            checks.append('novel/comic diagnostics and settings fit 1280, 375, 320 widths and both themes')
            page.evaluate('(async()=>{const m=await import("/src/locales/index.ts");m.default.global.locale.value="en"})()')
            page.set_viewport_size(dict(width=375,height=812))
            for mode in ['novel','comic']:
                page.evaluate('(path)=>location.hash=path',f'/{mode}/diagnostics')
                expect(page.locator('.reading-diag-heading h1')).to_contain_text(re.compile('diagnostic',re.I))
                assert page.locator('.cczj-runtime-diagnostics-page').evaluate('(el)=>el.scrollWidth<=el.clientWidth+1')
                # Compare layout cells; the pressed animation independently scales a button.
                geometry=page.locator('.reading-diag-tabs .ui-seg').evaluate('el=>({indicator:el.querySelector(".ui-seg__indicator").offsetWidth,button:el.querySelector("button").offsetWidth,width:el.offsetWidth,columns:getComputedStyle(el).gridTemplateColumns,buttons:[...el.querySelectorAll("button")].map(b=>({text:b.textContent,width:b.offsetWidth}))})')
                assert abs(geometry['indicator']-geometry['button'])<2,geometry
                page.locator('.main-content').last.evaluate('(el)=>el.scrollTop=0')
                page.screenshot(path=str(args.output/f'{mode}-english-mobile.png'))
            checks.append('long English navigation and actions remain aligned on narrow screens')
            assert not errors,errors
            (args.output/'results.json').write_text(json.dumps(dict(checks=checks,errors=errors,scope='production Vue and shared controls, deterministic RPC; not native host'),ensure_ascii=False,indent=2),encoding='utf-8')
            print('\n'.join('PASS '+name for name in checks),flush=True)
        except Exception:
            page.screenshot(path=str(args.output/'failure.png'))
            (args.output/'failure.html').write_text(page.content(),encoding='utf-8')
            raise
        finally:browser.close()

def native(args):
    from android_webview import AndroidWebView
    spec=importlib.util.spec_from_file_location('compiled',ROOT/'scripts/collect-android-diagnostics.py')
    compiled=importlib.util.module_from_spec(spec);spec.loader.exec_module(compiled)
    output=args.output/'android';output.mkdir(parents=True,exist_ok=True)
    results=dict(package='com.cczj.video.acceptance',device=args.device,environment='emulator' if args.device.startswith('emulator-') else 'unclassified',passed=False,checks=[])
    with AndroidWebView(args.device,application_id=results['package']) as view:
        def rpc(name,*values):
            path,alias=compiled.compiled_binding(name)
            return view.evaluate('(async()=>{const m=await import('+json.dumps(path)+');return m['+json.dumps(alias)+'](...'+json.dumps(values)+')})()')
        def wait(expression):
            deadline=time.monotonic()+20
            while time.monotonic()<deadline:
                value=view.evaluate(expression)
                if value:return value
                time.sleep(.1)
            raise AssertionError(dict(expression=expression,text=view.evaluate('document.body.innerText.slice(-1500)')))
        def tap(expression):
            point=wait('(()=>{const e='+expression+';if(!e)return null;e.scrollIntoView({block:"center"});const r=e.getBoundingClientRect(),x=r.left+r.width/2,y=r.top+r.height/2;return e.contains(document.elementFromPoint(x,y))?{x,y}:null})()')
            view.call('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[point]))
            view.call('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]))
        button=lambda name:'[...document.querySelectorAll(".cczj-reading-diagnostics button")].find(e=>e.textContent.trim()==='+json.dumps(name)+')'
        original_url=view.evaluate('location.href');original_mode=rpc('GetSetting','application_mode')
        try:
            for mode in ['novel','comic']:
                rpc('SetSetting','application_mode',mode)
                view.evaluate('location.href='+json.dumps('https://wails.localhost/?application-mode='+mode+'&settings-audit='+str(time.monotonic_ns())+'#/'+mode+'/diagnostics'))
                wait('!!document.querySelector(".reading-diag-heading h1")')
                wait('document.querySelector(".reading-diag-cards strong")?.textContent!=="—"')
                assert view.evaluate('document.documentElement.dataset.cczjPlatform')=='mobile'
                assert view.evaluate('document.querySelectorAll(".cczj-runtime-diagnostics-page > button").length')==0
                assert view.evaluate('document.querySelector(".cczj-runtime-diagnostics-page").scrollWidth<=innerWidth')
                tap(button('启动与过渡'))
                wait('!!document.querySelector(".diag-table")')
                tap(button('运行环境与存储'))
                wait('!!document.querySelector(".table-row")')
                tap(button('资源概览'))
                tap('document.querySelector(".cczj-diag-coverage-summary")')
                wait('document.querySelector(".cczj-diag-coverage").open')
                tap('document.querySelector(".cczj-diag-coverage-summary")')
                tap(button('刷新'))
                wait('!document.querySelector(".reading-diag-actions button").disabled')
                view.evaluate('document.querySelector(".main-content").scrollTop=0')
                view.screenshot(output/(mode+'.png'))
                results['checks'].append(mode+' actual mobile layout, trusted tab taps, storage, notes and refresh')
            results['passed']=True
            print('PASS native Android settings/diagnostics: '+', '.join(results['checks']),flush=True)
        except Exception as error:
            results['error']=str(error);view.screenshot(output/'failure.png');raise
        finally:
            (output/'results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2),encoding='utf-8')
            rpc('SetSetting','application_mode',original_mode)
            view.evaluate('location.href='+json.dumps(original_url))

if __name__=='__main__':main()
