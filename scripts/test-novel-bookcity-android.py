"""Native Android discovery and trusted touch, using the acceptance app only."""
import argparse
import importlib.util
import json
from pathlib import Path
import time
from android_webview import AndroidWebView

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_OUTPUT = ROOT/'reports/novel-bookcity-2026-10-21/android'
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
            rpc('SetSetting', 'application_mode', 'novel')
            view.evaluate('location.href="https://wails.localhost/?application-mode=novel#/novel"')
            wait('document.querySelectorAll(".room-book").length===18')
            wait('document.querySelector(`.room-stack-cover[data-position="0"] img`)?.naturalWidth>20')
            assert view.evaluate('document.documentElement.scrollWidth<=innerWidth')
            assert view.evaluate('document.querySelector(".novel-home").scrollWidth<=document.querySelector(".novel-home").clientWidth+1')
            capture('home.png')
            assert view.evaluate('(()=>{const e=document.querySelector(".room-discovery"),a=e.getBoundingClientRect(),b=e.querySelector(`[data-position="2"]`).getBoundingClientRect(),c=e.querySelector(".room-discovery-info").getBoundingClientRect();return a.width>a.height&&b.right<=c.left})()')
            record('Real native bookcity opens without entering a book title, actual covers decoded')
            first_pick = view.evaluate('document.querySelector(`.room-stack-cover[data-position="0"]`).getAttribute("href")')
            assert view.evaluate('!document.querySelector(".room-next-book") && document.querySelector(".room-next-leaf").getAttribute("aria-label")==="换一本"')
            tap('document.querySelector(".room-next-leaf")')
            wait('document.querySelector(`.room-stack-cover[data-position="0"]`).getAttribute("href")!=='+json.dumps(first_pick))
            wait('document.querySelector(".room-desk-book").getAttribute("href")===document.querySelector(`.room-stack-cover[data-position="0"]`).getAttribute("href")')
            capture('book-swap.png')
            record('Circular arrow is absent; trusted touch on the exposed next cover swaps a real book, keeping title and destination in sync')
            tap(button('历史古代', 'document.querySelector(".room-wayfinding")'))
            wait('document.querySelectorAll("[data-channel=history] .room-book").length===20')
            tap(button('再逛一排'))
            wait('document.querySelectorAll("[data-channel=history] .room-book").length===40')
            view.evaluate('document.querySelector(".novel-main").scrollTop=0')
            time.sleep(.3)
            capture('category.png')
            title = view.evaluate('document.querySelector(".room-book h3").textContent')
            tap('document.querySelector("[data-channel=history] .room-book")')
            wait('!!document.querySelector(".novel-detail-actions a")')
            capture('detail.png')
            tap('document.querySelector(".novel-detail-actions a")')
            wait('document.querySelector(".novel-reading-paper")?.textContent.length>500')
            assert not view.evaluate('/[\uE000-\uF8FF]/.test(document.querySelector(".novel-reading-paper").textContent)')
            capture('reader.png')
            record('Trusted touch selects a category, loads another page and opens actual readable chapter: '+title)
            view.evaluate('history.back()')
            wait('!!document.querySelector(".novel-detail-actions a")')
            view.evaluate('history.back()')
            wait('document.querySelectorAll(".cczj-mobile-route-scene[data-route-active=true] [data-channel=history] .room-book").length===40')
            record('Native Back preserves the loaded 40-book category')
            view.evaluate('document.querySelector(".novel-main").scrollTop=0')
            tap('[...document.querySelectorAll(".reading-bottom-nav a")].find(a=>a.getAttribute("href")==="#/novel/shelf")')
            wait('document.querySelector(".cczj-mobile-route-scene[data-route-active=true]")?.dataset.path==="/novel/shelf"')
            tap('[...document.querySelectorAll(".reading-bottom-nav a")].find(a=>a.getAttribute("href").split("?")[0]==="#/novel")')
            wait('document.querySelectorAll(".cczj-mobile-route-scene[data-route-active=true] [data-channel=history] .room-book").length===40')
            record('Bookcity / personal shelf tabs are independent and retain browsing state')
            # Inspect a night version without changing persisted user preferences.
            view.evaluate('document.documentElement.dataset.themeMode="dark"')
            capture('night.png')
            result = dict(passed=True, native=True, package='com.cczj.video.acceptance', userAgent=view.evaluate('navigator.userAgent'), checks=checks)
            (output/'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
        finally:
            view.screenshot(output/'last-state.png')
            rpc('SetSetting', 'application_mode', old_mode or '')
            view.evaluate('location.href='+json.dumps(old_url))


if __name__ == '__main__': main()
