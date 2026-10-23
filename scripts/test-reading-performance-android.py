"""Native reading acceptance with real sources and trusted WebView touch input.

Runs only against the separate acceptance package. Does not clear application
data. Records device identity; an emulator is never labelled a physical phone.
No mocked source, chapter, image, network or binding responses are installed.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import subprocess
import time

from android_webview import AndroidWebView

ROOT = Path(__file__).resolve().parent.parent
PACKAGE = 'com.cczj.video.acceptance'
SPEC = importlib.util.spec_from_file_location('compiled', ROOT/'scripts/collect-android-diagnostics.py')
COMPILED = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(COMPILED)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', required=True)
    parser.add_argument('--environment', choices=['physical', 'emulator'], required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--turns', type=int, default=18)
    parser.add_argument('--reader-cycles', type=int, default=3)
    args = parser.parse_args()
    if not 2 <= args.turns <= 100:
        parser.error('--turns must be between 2 and 100')
    if not 1 <= args.reader_cycles <= 10:
        parser.error('--reader-cycles must be between 1 and 10')
    args.output.mkdir(parents=True, exist_ok=True)
    adb = ['adb', '-s', args.device]

    def native(*command, timeout=30):
        return subprocess.check_output(adb+list(command), text=True, encoding='utf-8', errors='replace', timeout=timeout).strip()

    identity = {key: native('shell', 'getprop', key) for key in [
        'ro.product.model', 'ro.product.cpu.abi', 'ro.build.version.release',
        'ro.build.fingerprint', 'ro.kernel.qemu', 'ro.hardware', 'ro.product.manufacturer']}
    emulator_hint = args.device.startswith('emulator-') or identity['ro.kernel.qemu'] == '1' or any(
        s in (identity['ro.hardware']+' '+identity['ro.build.fingerprint']).lower() for s in ['ranchu', 'goldfish', 'vbox', 'mumu'])
    if args.environment == 'physical' and emulator_hint:
        parser.error('Device appears to be an emulator; refusing a physical-phone result')
    started = native('shell', "date '+%m-%d %H:%M:%S.000'")
    result = dict(device=args.device, environment=args.environment, identity=identity, package=PACKAGE,
                  startedDeviceTime=started, passed=False, scenarios=[], phaseMetrics=[], motion=[], javascriptErrors=[], limitations=[
                      'WebView rAF callback gaps are not hardware presentation FPS.',
                      'Network wait is reported separately from cached page-turn interaction.',
                      'Success on these sampled works does not establish all-source quality.'])
    (args.output/'memory-before.txt').write_text(native('shell', 'dumpsys', 'meminfo', PACKAGE), encoding='utf-8')
    with AndroidWebView(args.device, application_id=PACKAGE) as view:
        view.call('Page.enable')
        probe = (ROOT/'scripts/fixtures/reading-performance-probe.js').read_text(encoding='utf-8')
        injection = view.call('Page.addScriptToEvaluateOnNewDocument', {'source': probe})
        view.evaluate(probe)

        def rpc(name, *values):
            path, alias = COMPILED.compiled_binding(name)
            return view.evaluate('(async()=>{const m=await import('+json.dumps(path)+');return await m['+json.dumps(alias)+'](...'+json.dumps(values)+')})()')

        def wait(expression, timeout=45):
            deadline = time.monotonic()+timeout
            while time.monotonic() < deadline:
                value = view.evaluate(expression)
                if value:
                    return value
                time.sleep(.08)
            raise AssertionError({'wait': expression, 'text': view.evaluate('document.body.innerText.slice(-2400)')})

        def target(selector):
            return 'document.querySelector('+json.dumps(selector)+')'

        def button(name, scope='document'):
            return '[...'+scope+'.querySelectorAll("button")].find(e=>e.textContent.trim()==='+json.dumps(name)+'&&!e.closest("[inert]"))'

        def tap(expression):
            point = wait('(()=>{const e='+expression+';if(!e||e.disabled)return null;for(let p=e;p;p=p.parentElement){if(p.inert||p.dataset.routeActive==="false"||p.getAnimations().some(a=>a.playState==="running"&&Number.isFinite(a.effect?.getTiming().iterations)))return null}e.scrollIntoView({block:"center",inline:"center"});const r=e.getBoundingClientRect(),x=r.left+r.width/2,y=r.top+r.height/2;return r.width&&r.height&&e.contains(document.elementFromPoint(x,y))?{x,y}:null})()')
            token = view.evaluate('(()=>{const e='+expression+';const token=(window.__readingTouchToken??0)+1;window.__readingTouchToken=token;e.addEventListener("click",()=>{window.__readingTouchDelivered=token},{once:true,capture:true});return token})()')
            touch(point['x'], point['y'])
            # Native touch dispatch acknowledgement precedes DOM click delivery.
            # Wait for the actual target event before operating another control.
            wait('window.__readingTouchDelivered==='+str(token), 5)

        def touch(x, y):
            view.call('Input.dispatchTouchEvent', dict(type='touchStart', touchPoints=[dict(x=x, y=y)]))
            view.call('Input.dispatchTouchEvent', dict(type='touchEnd', touchPoints=[]))

        def phase(name):
            view.evaluate('window.__readingPerf.begin('+json.dumps(name)+')')

        def end():
            metric = view.evaluate('window.__readingPerf.end()')
            if metric:
                result['phaseMetrics'].append(metric)
                print(json.dumps({'phase': metric['name'], 'ms': round(metric['elapsedMs']), 'rafP95': metric['rafGapP95Ms'], 'maxGap': metric['rafGapMaxMs'], 'longTasks': len(metric['longTasks'])}), flush=True)

        def screenshot(name):
            wait('!document.getAnimations().some(a=>a.playState==="running"&&Number.isFinite(a.effect?.getTiming().iterations))', 10)
            view.screenshot(args.output/(name+'.png'))

        def start_motion(name):
            view.evaluate('window.__readingPerf.beginMotion('+json.dumps(name)+')')

        def end_motion():
            time.sleep(.3)
            sample = view.evaluate('window.__readingPerf.endMotion()')
            result['motion'].append(sample)
            assert sample and sample['samples'], 'No animation frames observed'
            assert sample['maxNoVisibleSceneMs'] < 100, sample

        def leave(kind):
            native('shell', 'input', 'keyevent', 'KEYCODE_BACK')
            time.sleep(.3)
            # Android Back first dismisses reader controls, then leaves.
            # Outgoing reader DOM remains during a route animation. It must not
            # trigger a second Back after the route already reached the detail.
            if view.evaluate('location.hash.startsWith('+json.dumps('#/'+kind+'/read/')+')'):
                native('shell', 'input', 'keyevent', 'KEYCODE_BACK')
            wait('!!document.querySelector(".'+kind+'-detail-actions a")')

        def show_controls(kind):
            selector = '.'+kind+'-reader-top'
            if view.evaluate('getComputedStyle(document.querySelector('+json.dumps(selector)+')).visibility==="hidden"'):
                rect = view.evaluate('(()=>{const r=document.querySelector(".'+kind+'-page-viewport,.'+kind+'-reader-viewport").getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height*.4}})()')
                touch(rect['x'], rect['y'])
            wait('getComputedStyle(document.querySelector('+json.dumps(selector)+')).visibility!=="hidden"')

        def swipe(selector, reverse=False, x_fraction=.5):
            rect = view.evaluate('(()=>{const r=document.querySelector('+json.dumps(selector)+').getBoundingClientRect();return {x:r.left+r.width*'+str(x_fraction)+',top:r.top+r.height*.25,bottom:r.top+r.height*.78}})()')
            start, stop = (rect['top'], rect['bottom']) if reverse else (rect['bottom'], rect['top'])
            view.call('Input.dispatchTouchEvent', dict(type='touchStart', touchPoints=[dict(x=rect['x'], y=start)]))
            for i in range(1, 13):
                view.call('Input.dispatchTouchEvent', dict(type='touchMove', touchPoints=[dict(x=rect['x'], y=start+(stop-start)*i/12)]))
                time.sleep(.016)
            view.call('Input.dispatchTouchEvent', dict(type='touchEnd', touchPoints=[]))
            time.sleep(.2)

        old_url, old_mode = view.evaluate('location.href'), rpc('GetSetting', 'application_mode')
        old_preferences = {kind: [r for r in rpc('ReadingStateLoad', kind, '[]') if r['bucket'] == 'preferences'] for kind in ['novel', 'comic']}
        initial_pid = native('shell', 'pidof', PACKAGE)
        try:
            for kind, keyword, title in [('novel', '部族荣光', '部族荣光'), ('comic', '逃离长空', '逃离长空')]:
                scenario = dict(kind=kind, keyword=keyword, passed=False)
                result['scenarios'].append(scenario)
                try:
                    rpc('SetSetting', 'application_mode', kind)
                    # A changed query guarantees a document navigation even if
                    # an interrupted older test left the same mode query behind.
                    view.evaluate('location.href='+json.dumps('https://wails.localhost/?application-mode='+kind+'&acceptance-run='+str(time.monotonic_ns())+'#/'+kind+'/search'))
                    wait('document.querySelector(".app-shell")?.dataset.applicationMode==='+json.dumps(kind))
                    wait('!!document.querySelector(".'+kind+'-search input")')
                    phase(kind+'.real-search')
                    tap(target('.'+kind+'-search input'))
                    wait('document.activeElement===document.querySelector(".'+kind+'-search input")')
                    view.call('Input.insertText', {'text': keyword})
                    wait('document.querySelector(".'+kind+'-search input")?.value==='+json.dumps(keyword), 3)
                    tap(target('.'+kind+'-search button[type=submit]'))
                    card = '[...document.querySelectorAll(".'+('novel-book-card' if kind == 'novel' else 'comic-card')+'")].find(e=>e.textContent.includes('+json.dumps(title)+'))'
                    began = time.monotonic()
                    while not view.evaluate('!!('+card+')'):
                        if time.monotonic()-began > 100:
                            raise AssertionError('No readable target found through real source search')
                        more = button('继续扩展来源')
                        if view.evaluate('!!('+more+')'):
                            tap(more)
                        time.sleep(.25)
                    scenario['searchFirstTargetMs'] = round((time.monotonic()-began)*1000)
                    end()
                    screenshot(kind+'-search')
                    phase(kind+'.open-detail-and-read-network')
                    tap(card)
                    tap(target('.'+kind+'-detail-actions a'))
                    ready = '!!document.querySelector(".novel-reading-paragraph")' if kind == 'novel' else '[...document.querySelectorAll(".comic-panel img")].some(i=>i.complete&&i.naturalWidth>100&&!i.currentSrc.includes("image_notfound"))'
                    wait(ready, 100)
                    scenario['readerRoute'] = view.evaluate('location.hash')
                    end()
                    screenshot(kind+'-reader')
                    show_controls(kind)
                    toolbar = view.evaluate('([...document.querySelectorAll(".'+kind+'-reader-bottom nav button")].map(e=>{const r=e.getBoundingClientRect();return {text:e.textContent.trim(),top:r.top,left:r.left,right:r.right,width:r.width}}))')
                    scenario['toolbar'] = toolbar
                    scenario['readerGeometry']=view.evaluate('({viewport:{width:innerWidth,visualWidth:visualViewport.width,scale:visualViewport.scale,offset:visualViewport.offsetLeft},ancestors:[...document.querySelectorAll(".app-shell,.reading-shell,.cczj-mobile-route-viewport,.cczj-mobile-route-scene,.main-content,.novel-reader,.comic-reader")].map(e=>({className:e.className,scrollLeft:e.scrollLeft,scrollWidth:e.scrollWidth,clientWidth:e.clientWidth,rect:e.getBoundingClientRect().toJSON(),transform:getComputedStyle(e).transform}))})')
                    screenshot(kind+'-toolbar')
                    assert len(toolbar) == 4 and max(b['top'] for b in toolbar)-min(b['top'] for b in toolbar) <= 1, {'toolbarWrapped': toolbar}
                    assert all(b['width'] >= 44 and b['left'] >= 0 and b['right'] <= view.evaluate('innerWidth')+1 for b in toolbar), {'toolbarHitArea': toolbar}
                    if kind == 'novel':
                        tap(target('.novel-reader-bottom nav button:last-child'))
                        tap(button('左右翻页'))
                        wait('('+button('左右翻页')+').getAttribute("aria-pressed")==="true"', 5)
                        tap(target('.reader-sheet-close'))
                        wait('!!document.querySelector(".novel-reader-paged")')
                    else:
                        if view.evaluate('!!document.querySelector(".comic-strip")'):
                            tap(target('.comic-reader-bottom nav button:nth-child(2)'))
                    time.sleep(.4)
                    show_controls(kind)
                    # Start at the first page through the visible range control.
                    seek = view.evaluate('(()=>{const r=document.querySelector(".'+kind+'-reader-seek input").getBoundingClientRect();return {x:r.left+1,y:r.top+r.height/2}})()')
                    touch(seek['x'], seek['y'])
                    time.sleep(.3)
                    # Hide reader controls with a center touch before page taps.
                    rect = view.evaluate('(()=>{const r=document.querySelector(".'+kind+'-page-viewport,.'+kind+'-reader-viewport").getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height*.4,right:r.right-20}})()')
                    touch(rect['x'], rect['y'])
                    phase(kind+'.consecutive-page-turns')
                    latencies = []
                    status = '.novel-reading-status' if kind == 'novel' else '.comic-reader-indicator'
                    count = view.evaluate('(()=>{const t=document.querySelector('+json.dumps(status)+').textContent;const m=t.match(/(\\d+)\\s*\\/\\s*(\\d+)/);return m?Number(m[2]):0})()')
                    assert count > 1, 'Target chapter has fewer than two readable pages'
                    stride = max(1, min(args.turns//2, count-1))
                    scenario['pageTurnStride'] = stride
                    for i in range(args.turns):
                        before = view.evaluate('document.querySelector('+json.dumps(status)+').textContent')
                        began = time.monotonic()
                        # Read a run of pages, then return; do not just flip the
                        # same pair or cross a chapter during this cached phase.
                        x = rect['right'] if i % (2*stride) < stride else 20
                        touch(x, rect['y'])
                        wait('document.querySelector('+json.dumps(status)+')?.textContent!=='+json.dumps(before), 5)
                        if kind == 'novel':
                            wait('(()=>{const e=document.querySelector(".novel-reading-paper");return e&&Math.abs(new DOMMatrix(getComputedStyle(e).transform).m41-new DOMMatrix(e.style.transform).m41)<.5})()', 5)
                        else:
                            wait('[...document.querySelectorAll(".comic-panel")].filter(e=>getComputedStyle(e).display!=="none").some(e=>{const i=e.querySelector("img");return i&&i.complete&&i.naturalWidth>100&&!i.currentSrc.includes("image_notfound")})', 20)
                        latencies.append(round((time.monotonic()-began)*1000, 1))
                        time.sleep(.12)
                    scenario['pageTurnObservedMs'] = latencies
                    end()
                    screenshot(kind+'-pages')
                    show_controls(kind)
                    phase(kind+'.reader-sheet-open-close')
                    for _ in range(4):
                        tap(target('.'+kind+'-reader-bottom nav button:last-child'))
                        wait('!!document.querySelector(".reader-sheet")')
                        tap(target('.reader-sheet-close'))
                        wait('!document.querySelector(".reader-sheet")')
                    end()
                    if kind == 'novel':
                        tap(target('.novel-reader-bottom nav button:last-child'))
                        tap(button('上下滚动'))
                        wait('('+button('上下滚动')+').getAttribute("aria-pressed")==="true"', 5)
                        tap(target('.reader-sheet-close'))
                    else:
                        tap(target('.comic-reader-bottom nav button:nth-child(2)'))
                    # Gestures must start on artwork, not the open bottom toolbar.
                    if view.evaluate('getComputedStyle(document.querySelector(".'+kind+'-reader-top")).visibility!=="hidden"'):
                        rect=view.evaluate('(()=>{const r=document.querySelector(".'+kind+'-page-viewport,.'+kind+'-reader-viewport").getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height*.4}})()')
                        touch(rect['x'],rect['y'])
                        wait('getComputedStyle(document.querySelector(".'+kind+'-reader-top")).visibility==="hidden"')
                    phase(kind+'.continuous-scroll')
                    viewport = '.novel-page-viewport' if kind == 'novel' else '.comic-reader-viewport'
                    scroll_positions=[]
                    for i in range(10):
                        swipe(viewport, reverse=i >= 5)
                        scroll_positions.append(view.evaluate('document.querySelector('+json.dumps(viewport)+').scrollTop'))
                    scenario['scrollPositions']=scroll_positions
                    assert max(scroll_positions)-min(scroll_positions)>10, {'scrollDidNotMove':scroll_positions}
                    end()
                    screenshot(kind+'-scroll')
                    if kind=='comic':
                        phase('comic.touch-episode-boundaries')
                        heading='.comic-reader-indicator span:first-child'
                        original_title=view.evaluate('document.querySelector('+json.dumps(heading)+').textContent')
                        view.evaluate('window.__comicReaderNode=document.querySelector(".comic-reader")')
                        for _ in range(2):
                            view.evaluate('(()=>{const el=document.querySelector(".comic-reader-viewport");el.scrollTop=el.scrollHeight})()')
                            time.sleep(.3)
                        wait('[...document.querySelectorAll(".comic-panel img")].every(i=>i.complete&&i.naturalWidth>100)',60)
                        view.evaluate('(()=>{const el=document.querySelector(".comic-reader-viewport");el.scrollTop=el.scrollHeight})()')
                        time.sleep(.2)
                        # The centered episode-end button is an explicit action,
                        # so begin the boundary gesture on the artwork margin.
                        swipe('.comic-reader-viewport',x_fraction=.08)
                        wait('document.querySelector('+json.dumps(heading)+')?.textContent!=='+json.dumps(original_title),100)
                        wait(ready,100)
                        view.evaluate('document.querySelector(".comic-reader-viewport").scrollTop=0')
                        time.sleep(.3)
                        swipe('.comic-reader-viewport',reverse=True,x_fraction=.08)
                        wait('document.querySelector('+json.dumps(heading)+')?.textContent==='+json.dumps(original_title),100)
                        wait(ready,100)
                        assert view.evaluate('window.__comicReaderNode===document.querySelector(".comic-reader")')
                        assert view.evaluate('document.querySelectorAll(".comic-panel img").length<=5')
                        assert view.evaluate('!document.querySelector("[class*=route-forward-enter],[class*=route-back-enter]")')
                        end()
                        scenario['touchEpisodeBoundaries']=True
                    show_controls(kind)
                    previous_route = view.evaluate('location.hash')
                    route_identity = '(()=>{const [path,query=""]=location.hash.split("?");const p=new URLSearchParams(query);p.delete("position");return path+"?"+p.toString()})()'
                    previous_identity = view.evaluate(route_identity)
                    heading = '.novel-reading-paper h1' if kind == 'novel' else '.comic-reader-indicator span:first-child'
                    previous_title = view.evaluate('document.querySelector('+json.dumps(heading)+').textContent')
                    next_button = target('.'+kind+'-reader-seek button:last-child')
                    phase(kind+'.next-chapter-network')
                    tap(next_button)
                    wait('location.hash!=='+json.dumps(previous_route))
                    wait('(()=>{const e=document.querySelector('+json.dumps(heading)+');return e&&e.textContent!=='+json.dumps(previous_title)+'})()', 100)
                    wait(ready, 100)
                    end()
                    screenshot(kind+'-next-chapter')
                    scenario['nextChapterRoute'] = view.evaluate('location.hash')
                    assert scenario['nextChapterRoute'] != previous_route
                    phase(kind+'.previous-chapter-cached')
                    show_controls(kind)
                    tap(target('.'+kind+'-reader-seek button:first-child'))
                    # Previous chapter intentionally enters at its end; this
                    # position hint is not a different chapter identity.
                    wait(route_identity+'==='+json.dumps(previous_identity))
                    wait('document.querySelector('+json.dumps(heading)+')?.textContent==='+json.dumps(previous_title), 100)
                    wait(ready, 100)
                    end()
                    phase(kind+'.return-to-detail')
                    start_motion(kind+'.return-to-detail')
                    leave(kind)
                    end_motion()
                    end()
                    scenario['readerCycles'] = []
                    for cycle in range(args.reader_cycles):
                        phase(kind+'.reader-cycle-'+str(cycle+1))
                        start_motion(kind+'.open-reader-'+str(cycle+1))
                        tap(target('.'+kind+'-detail-actions a'))
                        wait(ready, 100)
                        end_motion()
                        start_motion(kind+'.close-reader-'+str(cycle+1))
                        leave(kind)
                        end_motion()
                        scenario['readerCycles'].append(view.call('Memory.getDOMCounters'))
                        end()
                    scenario['passed'] = True
                    print('PASS '+kind+' real search, read, page turns, sheets, scroll, chapters, repeated reader and Back', flush=True)
                except Exception as error:
                    end()
                    scenario['error'] = str(error)
                    screenshot(kind+'-failure')
                    print('FAIL '+kind+': '+str(error), flush=True)
                finally:
                    result['javascriptErrors'].extend(view.evaluate('window.__readingPerf?.errors') or [])
            result['initialPid'] = initial_pid
            result['finalPid'] = native('shell', 'pidof', PACKAGE)
            result['sameProcess'] = initial_pid == result['finalPid']
            result['passed'] = all(s['passed'] for s in result['scenarios']) and not result['javascriptErrors'] and result['sameProcess']
        finally:
            result['javascriptErrorsLastDocument'] = view.evaluate('window.__readingPerf?.errors')
            (args.output/'memory-after.txt').write_text(native('shell', 'dumpsys', 'meminfo', PACKAGE), encoding='utf-8')
            (args.output/'gfxinfo.txt').write_text(native('shell', 'dumpsys', 'gfxinfo', PACKAGE, 'framestats'), encoding='utf-8')
            (args.output/'exit-info.txt').write_text(native('shell', 'dumpsys', 'activity', 'exit-info', PACKAGE), encoding='utf-8')
            (args.output/'crash-buffer.txt').write_text(native('logcat', '-d', '-b', 'crash', '-v', 'threadtime', '-T', started), encoding='utf-8')
            (args.output/'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
            for kind, preferences in old_preferences.items():
                if preferences:
                    rpc('ReadingStateApply', kind, json.dumps(preferences))
            rpc('SetSetting', 'application_mode', old_mode)
            view.call('Page.removeScriptToEvaluateOnNewDocument', injection)
            view.evaluate('location.href='+json.dumps(old_url))
    print(json.dumps({'passed': result['passed'], 'environment': args.environment, 'output': str(args.output)}, ensure_ascii=False), flush=True)
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
