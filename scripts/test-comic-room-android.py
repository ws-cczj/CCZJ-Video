"""Native Android comic exploration and trusted touch, using the acceptance app only."""
import argparse
import importlib.util
import json
from pathlib import Path
import time
from android_webview import AndroidWebView

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_OUTPUT = ROOT/'reports/comic-room-2026-10-21/android'
SPEC = importlib.util.spec_from_file_location('compiled', ROOT/'scripts/collect-android-diagnostics.py')
COMPILED = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(COMPILED)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=DEFAULT_OUTPUT)
    output = parser.parse_args().output
    output.mkdir(parents=True, exist_ok=True)
    checks = []
    with AndroidWebView(application_id='com.cczj.video.acceptance') as view:
        def rpc(name, *args):
            path, alias = COMPILED.compiled_binding(name)
            return view.evaluate('(async()=>{const m=await import('+json.dumps(path)+');return await m['+json.dumps(alias)+'](...'+json.dumps(args)+')})()')
        def wait(expression, timeout=60):
            deadline = time.monotonic()+timeout
            while time.monotonic() < deadline:
                value = view.evaluate(expression)
                if value: return value
                time.sleep(.15)
            raise AssertionError(dict(wait=expression, text=view.evaluate('document.body.innerText.slice(-1800)')))
        def tap(expression):
            point = wait('(()=>{const e='+expression+';if(!e)return null;for(let p=e;p;p=p.parentElement){if(p.inert||p.dataset.routeActive==="false"||p.getAnimations().some(a=>a.playState==="running"&&Number.isFinite(a.effect?.getTiming().iterations)))return null}e.scrollIntoView({block:"center",inline:"center"});const r=e.getBoundingClientRect();const x=r.left+r.width/2,y=r.top+r.height/2;return e.contains(document.elementFromPoint(x,y))?{x,y}:null})()')
            view.call('Input.dispatchTouchEvent', dict(type='touchStart', touchPoints=[point]))
            view.call('Input.dispatchTouchEvent', dict(type='touchEnd', touchPoints=[]))
        def button(name, scope='document'):
            return '[...'+scope+'.querySelectorAll("button")].find(b=>!b.closest("[inert]")&&b.textContent.trim()==='+json.dumps(name)+')'
        def record(text):
            checks.append(text); print('PASS '+text, flush=True)
        def capture(name):
            wait('!document.querySelector(".splash-wrap") && !document.getAnimations().some(a=>a.playState==="running"&&Number.isFinite(a.effect?.getComputedTiming().endTime))')
            time.sleep(.2)
            view.screenshot(output/name)
        old_mode, old_url = rpc('GetSetting', 'application_mode'), view.evaluate('location.href')
        try:
            rpc('SetSetting', 'application_mode', 'comic')
            view.evaluate('location.href="https://wails.localhost/?application-mode=comic#/comic"')
            wait('document.querySelectorAll(".manga-grid .comic-card").length===24',100)
            wait('document.querySelector(".manga-spotlight-cover img")?.naturalWidth>100')
            assert view.evaluate('document.querySelector(".comic-home").scrollWidth<=document.querySelector(".comic-home").clientWidth+1')
            capture('home.png')
            record('Native comic exploration opens four real bundled catalogs, with decoded covers and no search term')
            first=view.evaluate('document.querySelector(".manga-spotlight-cover").getAttribute("href")')
            tap('document.querySelectorAll(".manga-contact-sheet button")[1]')
            wait('document.querySelector(".manga-spotlight-cover").getAttribute("href")!=='+json.dumps(first))
            assert view.evaluate('document.querySelector(".manga-open").getAttribute("href")===document.querySelector(".manga-spotlight-cover").getAttribute("href")')
            capture('cover-selected.png')
            record('Actual touch changes the featured cover and matching detail destination')
            tap('[...document.querySelectorAll(".manga-channels button")].find(b=>b.textContent.trim().startsWith("人气国漫"))')
            total=wait('document.querySelectorAll("[data-channel=guazi] .comic-card").length>6&&document.querySelectorAll("[data-channel=guazi] .comic-card").length')
            title=view.evaluate('document.querySelector("[data-channel=guazi] .comic-card h3").textContent')
            tap('document.querySelector("[data-channel=guazi] .comic-card")')
            wait('!!document.querySelector(".comic-detail-actions a")')
            capture('detail.png')
            if view.evaluate('document.querySelector(".comic-detail-actions button").getAttribute("aria-pressed")!=="true"'):
                tap('document.querySelector(".comic-detail-actions button")')
            tap('document.querySelector(".comic-detail-actions a")')
            wait('[...document.querySelectorAll(".comic-panel img")].some(i=>i.complete&&i.naturalWidth>100)',100)
            capture('reader.png')
            record('Actual bundled-source chapter images load through native Go bindings and the image proxy: '+title)
            view.evaluate('history.back()')
            wait('!!document.querySelector(".cczj-mobile-route-scene[data-route-active=true] .comic-detail-actions a")')
            view.evaluate('history.back()')
            wait('document.querySelectorAll(".cczj-mobile-route-scene[data-route-active=true] [data-channel=guazi] .comic-card").length==='+str(total))
            tap('[...document.querySelectorAll(".reading-bottom-nav a")].find(a=>a.getAttribute("href")==="#/comic/library")')
            wait('!!document.querySelector(".cczj-mobile-route-scene[data-route-active=true] .comic-library")')
            assert view.evaluate('document.querySelector(".cczj-mobile-route-scene[data-route-active=true] .comic-library").textContent.includes('+json.dumps(title)+')')
            tap('[...document.querySelectorAll(".reading-bottom-nav a")].find(a=>a.getAttribute("href")==="#/comic")')
            wait('document.querySelectorAll(".cczj-mobile-route-scene[data-route-active=true] [data-channel=guazi] .comic-card").length==='+str(total))
            record('Native Back and Following tab retain the expanded collection and saved comic')
            tap(button('全部作品','document.querySelector(".cczj-mobile-route-scene[data-route-active=true] .manga-channels")'))
            view.evaluate('document.documentElement.dataset.themeMode="dark"')
            capture('night.png')
            result=dict(passed=True,native=True,package='com.cczj.video.acceptance',userAgent=view.evaluate('navigator.userAgent'),checks=checks)
            (output/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
        finally:
            view.screenshot(output/'last-state.png')
            rpc('SetSetting', 'application_mode', old_mode or '')
            view.evaluate('location.href='+json.dumps(old_url))


if __name__ == '__main__': main()
