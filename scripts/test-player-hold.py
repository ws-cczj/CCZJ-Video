"""Real touch input against VideoPlayer with a local looping clip; no source changes.
Use the existing Android HMR helper for current frontend code in a native WebView.
"""
import argparse
import base64
import json
from pathlib import Path
import re
import time
from urllib.parse import quote

from android_webview import AndroidWebView

ROOT = Path(__file__).resolve().parent.parent


def wait(page, expression, seconds=20):
    deadline = time.monotonic()+seconds
    while time.monotonic()<deadline:
        if page.evaluate(expression):
            return
        time.sleep(.05)
    raise AssertionError(expression)


def check(page, origin, out):
    clip = 'data:video/webm;base64,'+base64.b64encode((ROOT/'frontend/tests/browser/sample.webm').read_bytes()).decode()
    page.call('Page.navigate', {'url':origin+'/tests/browser/player.html?resume=0&media='+quote(clip,safe='')})
    wait(page, "!!document.querySelector('.cczj-mobile-video video') && document.querySelector('video').readyState>=2")
    page.evaluate("(async()=>{const v=document.querySelector('video');v.loop=true;v.muted=true;v.playbackRate=1.5;await v.play()})()")
    wait(page, "!document.querySelector('video').paused && !document.querySelector('.pause-overlay:not([style*=\"display: none\"])')")
    time.sleep(.3)
    page.evaluate("window.__holdTrace=[];for(const type of ['pointerdown','pointerup','pointercancel','lostpointercapture','contextmenu'])document.addEventListener(type,e=>{window.__holdTrace.push({type,target:e.target?.className,primary:e.isPrimary,button:e.button});window.__holdTrace=window.__holdTrace.slice(-30)},true)")
    speed_setting = "(()=>{const raw=JSON.parse(localStorage.getItem('vp_settings')||'{}');return (raw.value||raw).speed})()"
    saved = page.evaluate(speed_setting)
    assert saved == '1.5', ('selected rate was not persisted before hold',saved)

    def touch(kind):
        rect = page.evaluate("(()=>{const r=document.querySelector('.player-wrapper').getBoundingClientRect();return{x:r.x+r.width*.72,y:r.y+r.height*.5}})()")
        page.call('Input.dispatchTouchEvent', {'type':kind,'touchPoints':[] if kind in ('touchEnd','touchCancel') else [rect]})

    def hold():
        touch('touchStart')
        wait(page, "document.querySelector('video').playbackRate===2")
        wait(page, "document.querySelector('.cczj-mobile-speed-feedback')?.textContent.includes('2')")

    def restored():
        wait(page, "document.querySelector('video').playbackRate===1.5 && !document.querySelector('.cczj-mobile-speed-feedback')")
        assert page.evaluate(speed_setting)==saved, ('temporary rate changed selected speed',saved,page.evaluate(speed_setting))

    touch('touchStart');time.sleep(.1);touch('touchEnd');time.sleep(.6)
    assert page.evaluate("document.querySelector('video').playbackRate===1.5"), 'tap activated boost'
    hold();time.sleep(1.3)
    assert page.evaluate("document.querySelector('.cczj-mobile-speed-feedback')?.textContent.includes('正在 2 倍速播放')")
    assert page.evaluate(speed_setting)==saved, ('boost was saved as selected speed',saved,page.evaluate(speed_setting))
    hud = page.evaluate("document.querySelector('.cczj-mobile-speed-feedback').textContent")
    page.screenshot(out/'portrait-hold.png')
    touch('touchEnd');restored()
    assert page.evaluate("!document.querySelector('video').paused"), 'release toggled pause'
    hold();touch('touchCancel');restored()
    hold();page.evaluate("document.querySelector('video').pause()");restored();touch('touchCancel')
    page.evaluate("document.querySelector('video').play()")
    wait(page, "!document.querySelector('video').paused")
    rect = page.evaluate("(()=>{const r=document.querySelector('.cczj-mobile-fullscreen').getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()")
    page.call('Input.dispatchTouchEvent', {'type':'touchStart','touchPoints':[rect]})
    page.call('Input.dispatchTouchEvent', {'type':'touchEnd','touchPoints':[]})
    wait(page, '!!document.fullscreenElement')
    hold();page.screenshot(out/'fullscreen-hold.png');touch('touchEnd');restored()
    hold();page.evaluate('document.exitFullscreen()');restored();touch('touchCancel')
    hold();page.evaluate("window.__heldVideo=document.querySelector('video');window.playerTest.state.episode++")
    wait(page, "window.__heldVideo.playbackRate===1.5 && !document.querySelector('.cczj-mobile-speed-feedback')")
    touch('touchCancel')
    result = dict(scope='real touch events; production VideoPlayer; local test clip; host-specific result',hud=hud,
                  checks=['short tap','hold acceleration','persistent HUD','release restores selected rate','release does not pause',
                          'no preference write','touch cancellation','pause cancellation','fullscreen hold','fullscreen exit','episode switch'])
    (out/'results.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf8')
    print(json.dumps(result,ensure_ascii=False),flush=True)


def failure_snapshot(page,out):
    page.screenshot(out/'failure.png')
    state=page.evaluate("(()=>{const v=document.querySelector('video'),r=document.querySelector('.player-wrapper')?.getBoundingClientRect();return{url:location.href.slice(0,140),text:document.body.innerText.slice(0,1000),video:v&&{rate:v.playbackRate,paused:v.paused,ready:v.readyState,ended:v.ended,loop:v.loop,error:v.error?.message},rect:r&&{x:r.x,y:r.y,w:r.width,h:r.height},trace:window.__holdTrace,hit:r&&document.elementsFromPoint(r.x+r.width*.72,r.y+r.height*.5).map(e=>e.tagName+'.'+e.className)}})()")
    (out/'failure.json').write_text(json.dumps(state,ensure_ascii=False,indent=2),encoding='utf8')


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--android',action='store_true')
    parser.add_argument('--device',default='emulator-5554')
    parser.add_argument('--package',default='com.cczj.video.preview')
    parser.add_argument('--url',default='http://127.0.0.1:19555')
    parser.add_argument('--output',type=Path,default=ROOT/'dist/audit/player-hold')
    args=parser.parse_args()
    out=args.output/('android' if args.android else 'browser');out.mkdir(parents=True,exist_ok=True)
    if args.android:
        with AndroidWebView(device=args.device,application_id=args.package) as page:
            original=page.evaluate('({url:location.href,settings:localStorage.getItem("vp_settings")})')
            try:
                check(page,'https://wails.localhost',out)
            except Exception:
                failure_snapshot(page,out)
                raise
            finally:
                page.evaluate("document.querySelector('video')?.pause()")
                page.call('Page.navigate',{'url':original['url']})
                wait(page,'location.href==='+json.dumps(original['url']))
                page.evaluate("localStorage."+('removeItem("vp_settings")' if original['settings'] is None else 'setItem("vp_settings",'+json.dumps(original['settings'])+')'))
        return
    from playwright.sync_api import sync_playwright
    with sync_playwright() as p:
        browser=p.chromium.launch(channel='chrome',headless=True,args=['--autoplay-policy=no-user-gesture-required'])
        try:
            tab=browser.new_page(viewport={'width':390,'height':844},is_mobile=True,has_touch=True)
            tab.add_init_script('window._wails={environment:{OS:"android"}}')
            names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf8'))
            mock='\n'.join(f'export async function {n}(){{return '+('[]' if n in ('ListPlugins','RescanPlugins','GetAllSources') else '""')+';}' for n in names)
            tab.route('**/bindings/cczjVideo/app/service/app*',lambda route:route.fulfill(body=mock,content_type='text/javascript'))
            client=tab.context.new_cdp_session(tab)

            class Target:
                def evaluate(self,expression):return tab.evaluate(expression)
                def call(self,method,params=None):return client.send(method,params or {})
                def screenshot(self,path):tab.screenshot(path=str(path))

            try:
                check(Target(),args.url,out)
            except Exception:
                failure_snapshot(Target(),out)
                raise
        finally:
            browser.close()


if __name__=='__main__':
    main()
