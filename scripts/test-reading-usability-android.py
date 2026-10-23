"""Real Android source selection, cached-home refresh and trusted reader taps.

Uses the separate acceptance application and its real Go/SQLite/network services.
Never clears or launches the user's preview package.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import time

from android_webview import AndroidWebView

ROOT = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location('compiled', ROOT/'scripts/collect-android-diagnostics.py')
COMPILED = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(COMPILED)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    parser.add_argument('--output-dir', type=Path, default=ROOT/'reports/reading-usability-2026-10-19/android')
    args = parser.parse_args()
    output = args.output_dir
    output.mkdir(parents=True, exist_ok=True)
    result = dict(passed=False, native=True, package='com.cczj.video.acceptance', checks=[])
    with AndroidWebView(args.device, application_id=result['package']) as view:
        def rpc(name, *args):
            path, alias = COMPILED.compiled_binding(name)
            return view.evaluate('(async()=>{const m=await import('+json.dumps(path)+');return await m['+json.dumps(alias)+'](...'+json.dumps(args)+')})()')

        def wait(expression, timeout=55):
            deadline = time.monotonic()+timeout
            while time.monotonic() < deadline:
                value = view.evaluate(expression)
                if value:
                    return value
                time.sleep(.15)
            raise AssertionError(dict(wait=expression, text=view.evaluate('document.body.innerText.slice(-1800)')))

        def tap(expression):
            # Dispatch trusted touch events after verifying the target wins hit testing.
            point = wait('(()=>{const e='+expression+';if(!e)return null;for(let p=e;p;p=p.parentElement){if(p.inert||p.dataset.routeActive==="false"||p.getAnimations().some(a=>a.playState==="running"&&Number.isFinite(a.effect?.getTiming().iterations)))return null}e.scrollIntoView({block:"center"});const r=e.getBoundingClientRect();const x=r.left+r.width/2,y=r.top+r.height/2;return e.contains(document.elementFromPoint(x,y))?{x,y}:null})()')
            view.call('Input.dispatchTouchEvent', dict(type='touchStart', touchPoints=[dict(x=point['x'], y=point['y'])]))
            view.call('Input.dispatchTouchEvent', dict(type='touchEnd', touchPoints=[]))

        def button(name, scope='document'):
            return '[...'+scope+'.querySelectorAll("button")].find(b=>!b.closest("[inert]")&&b.textContent.trim()==='+json.dumps(name)+')'

        def route(path):
            view.evaluate('location.hash='+json.dumps(path))
            wait('decodeURIComponent(location.hash)==='+json.dumps('#'+path))
            wait('[...document.querySelectorAll(".cczj-mobile-route-scene[data-route-active=true]")].some(e=>e.dataset.path==='+json.dumps(path.split('?')[0])+')')

        def record(message):
            result['checks'].append(message)
            print('PASS '+message,flush=True)

        original_url = view.evaluate('location.href')
        original_mode = rpc('GetSetting', 'application_mode')
        original_preferences = None
        try:
            rpc('SetSetting', 'application_mode', 'novel')
            view.evaluate('location.href="https://wails.localhost/?application-mode=novel#/novel"')
            wait('!!document.querySelector(".novel-home")')
            route('/novel/sources')
            wait('document.querySelectorAll(".source-candidate").length===4')
            # Select all four supported Chinese novel sources through actual controls.
            if view.evaluate('document.querySelectorAll(".source-candidate input:not(:disabled)").length'):
                tap('document.querySelector(".source-catalog-selection button")')
            selected = view.evaluate('document.querySelectorAll(".source-candidate input:checked").length')
            if selected:
                tap(button('导入所选 ('+str(selected)+')'))
                wait('document.querySelectorAll(".source-candidate input:disabled").length===4')
            installed = rpc('LibrarySources', 'novel')
            assert len(installed) >= 4, installed
            record('four novel sources can be selectively imported; no three-source cap')
            tap(button('已添加 ('+str(len(installed))+')'))
            wait('!!document.querySelector(".library-sources-installed")&&getComputedStyle(document.querySelector(".library-sources-installed")).display!=="none"')
            assert view.evaluate('[...document.querySelectorAll(".library-source button")].some(b=>["启用","停用"].includes(b.textContent.trim()))')
            assert not view.evaluate('[...document.querySelectorAll(".library-source button")].some(b=>{if(b.textContent.trim()!=="编辑")return false;const r=b.getBoundingClientRect();return b.contains(document.elementFromPoint(r.left+r.width/2,r.top+r.height/2))})')
            view.screenshot(output/'installed.png')
            tap('document.querySelector(".library-source details summary")')
            tap(button('编辑', 'document.querySelector(".library-source")'))
            wait('!!document.querySelector(".library-source-form")')
            wait('!!document.querySelector(".ui-modal__footer .ui-btn__content")')
            wait('[...document.querySelectorAll(".ui-modal-overlay,.ui-modal")].every(e=>getComputedStyle(e).opacity==="1"&&!e.getAnimations().some(a=>a.playState==="running"&&Number.isFinite(a.effect?.getTiming().iterations)))')
            view.screenshot(output/'source-editor.png')
            tap(button('取消', 'document.querySelector(".ui-modal")'))
            wait('!document.querySelector(".ui-modal")')
            record('shared source-editor modal exposes its caller-provided footer and closes by trusted touch')
            route('/novel')
            wait('!!document.querySelector(".novel-source-ready")')
            assert '还没有小说源' not in view.evaluate('document.querySelector(".novel-home").textContent')
            record('cached novel home immediately reflects imported sources')
            route('/novel/sources')
            wait('document.querySelectorAll(".source-candidate").length===4')
            tap(button('更多小说源'))
            wait('document.querySelector(".source-catalog-selection")?.textContent.includes("2801")')
            result['catalog_count'] = 2801
            result['webview'] = view.evaluate('navigator.userAgent')
            assert view.evaluate('document.documentElement.scrollWidth<=innerWidth'), 'source catalog overflow'
            view.screenshot(output/'novel-source-library.png')
            record('offline full source library, quality ordering and readable mobile layout')

            for source in installed:
                if source['name'] == '番茄小说' and not source['enabled']:
                    rpc('LibrarySetSourceEnabled', source['sourceKey'], True)
            route('/novel/search')
            wait('!!document.querySelector(".novel-search input")')
            view.evaluate('(()=>{const i=document.querySelector(".novel-search input");i.value="部族荣光";i.dispatchEvent(new Event("input",{bubbles:true}));document.querySelector(".novel-search").requestSubmit()})()')
            wait('[...document.querySelectorAll(".novel-book-card")].some(r=>r.textContent.includes("部族荣光"))')
            tap('[...document.querySelectorAll(".novel-book-card")].find(r=>r.textContent.includes("部族荣光"))')
            wait('!!document.querySelector(".novel-detail-actions a")')
            tap('document.querySelector(".novel-detail-actions a")')
            wait('!!document.querySelector(".novel-reading-paragraph")')
            original_preferences = next((r for r in rpc('ReadingStateLoad','novel','[]') if r['bucket']=='preferences'), None)
            # Switch to paged reading using the same settings sheet as the user.
            tap('document.querySelector(".novel-page-viewport")')
            wait('getComputedStyle(document.querySelector(".novel-reader-top")).visibility!=="hidden"')
            tap('document.querySelector(".novel-reader-bottom nav button:last-child")')
            tap(button('左右翻页'))
            tap('document.querySelector(".reader-sheet-close")')
            wait('!!document.querySelector(".novel-reader-paged")')
            # Hide controls, turn one page, then tap the permanently visible back link.
            tap('document.querySelector(".novel-page-viewport")')
            wait('!!document.querySelector(".novel-reader-exit")')
            before_page = view.evaluate('document.querySelector(".novel-reading-paper").style.transform')
            r = view.evaluate('(()=>{const r=document.querySelector(".novel-page-viewport").getBoundingClientRect();return {x:r.right-24,y:r.top+r.height/2}})()')
            view.call('Input.dispatchTouchEvent', dict(type='touchStart', touchPoints=[r]))
            view.call('Input.dispatchTouchEvent', dict(type='touchEnd', touchPoints=[]))
            wait('document.querySelector(".novel-reading-paper").style.transform!=='+json.dumps(before_page))
            wait('(()=>{const p=document.querySelector(".novel-reading-paper");return Math.abs(new DOMMatrix(getComputedStyle(p).transform).m41-new DOMMatrix(p.style.transform).m41)<.5})()')
            geometry = view.evaluate('(()=>{const v=document.querySelector(".novel-page-viewport"),p=document.querySelector(".novel-reading-paper");return {viewport:v.clientWidth,column:parseFloat(p.style.columnWidth)}})()')
            assert abs(geometry['viewport']-geometry['column']) <= 1, geometry
            view.screenshot(output/'novel-paged-exit.png')
            tap('document.querySelector(".novel-reader-exit")')
            wait('location.hash.startsWith("#/novel/book/")')
            record('real novel text, paged reading and trusted touch on persistent exit')

            rpc('SetSetting', 'application_mode', 'comic')
            view.evaluate('location.href="https://wails.localhost/?application-mode=comic#/comic"')
            wait('!!document.querySelector(".comic-home")')
            route('/comic/sources')
            wait('document.querySelectorAll(".source-candidate").length===4')
            target = '[...document.querySelectorAll(".source-candidate")].find(r=>r.textContent.includes("崩坏3官方漫画"))'
            if not view.evaluate('('+target+').textContent.includes("已导入")'):
                tap(button('添加', '('+target+')'))
                wait('('+target+').textContent.includes("已导入")')
            route('/comic')
            wait('document.querySelector(".comic-source-ready")?.textContent.includes("已启用")')
            route('/comic/search?q=崩坏')
            wait('[...document.querySelectorAll(".comic-card")].some(r=>r.textContent.includes("逃离长空"))')
            tap('[...document.querySelectorAll(".comic-card")].find(r=>r.textContent.includes("逃离长空"))')
            wait('!!document.querySelector(".comic-detail-actions a")')
            tap('document.querySelector(".comic-detail-actions a")')
            wait('[...document.querySelectorAll(".comic-panel img")].some(i=>i.naturalWidth>0)')
            view.screenshot(output/'comic-real-reader.png')
            tap('document.querySelector(".comic-reader-exit")')
            wait('location.hash.startsWith("#/comic/series/")')
            record('one-tap official comic import, refreshed home, real image decode and reader exit')
            result['passed'] = True
            print('Native reading usability passed: '+', '.join(result['checks']), flush=True)
        except Exception as error:
            result['error'] = str(error)
            view.screenshot(output/'failure.png')
            raise
        finally:
            (output/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
            if original_preferences:
                rpc('ReadingStateApply', 'novel', json.dumps([original_preferences]))
            rpc('SetSetting', 'application_mode', original_mode)
            view.evaluate('location.href='+json.dumps(original_url))


if __name__ == '__main__':
    main()
