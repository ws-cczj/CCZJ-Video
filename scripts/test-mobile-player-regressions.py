"""Native fullscreen gutter, side speed drawer and Android system PiP regression.

Uses the acceptance package and its existing controlled media source, never the
user's preview database. Restores mode, route and player preferences afterward.
"""
import argparse
import importlib.util
import io
import json
import re
from pathlib import Path
import subprocess
import threading
import time
from http.server import ThreadingHTTPServer
from urllib.parse import urlparse
from PIL import Image
from android_webview import AndroidWebView

ROOT = Path(__file__).resolve().parent.parent
def module(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT/path)
    value = importlib.util.module_from_spec(spec); spec.loader.exec_module(value)
    return value
COMPILED = module('compiled', 'scripts/collect-android-diagnostics.py')
MEDIA = module('media', 'scripts/test-upgrade-player-android.py')

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=ROOT/'reports/mobile-player-fixes-2026-10-21/before')
    output = parser.parse_args().output
    output.mkdir(parents=True, exist_ok=True)
    checks = []
    with AndroidWebView(application_id='com.cczj.video.acceptance') as view:
        def rpc(name, *args):
            path, alias = COMPILED.compiled_binding(name)
            return view.evaluate('(async()=>{const m=await import('+json.dumps(path)+');return m['+json.dumps(alias)+'](...'+json.dumps(args)+')})()')
        def wait(expression, seconds=25):
            deadline = time.monotonic()+seconds
            while time.monotonic() < deadline:
                value = view.evaluate(expression)
                if value: return value
                time.sleep(.1)
            raise AssertionError(expression)
        def tap(expression):
            point = wait('(()=>{const e='+expression+';if(!e)return null;const r=e.getBoundingClientRect();return r.width&&r.height?{x:r.left+r.width/2,y:r.top+r.height/2}:null})()')
            view.call('Input.dispatchTouchEvent', dict(type='touchStart', touchPoints=[point]))
            view.call('Input.dispatchTouchEvent', dict(type='touchEnd', touchPoints=[]))
            time.sleep(.3)
        def check(name, work):
            try: row = dict(name=name, passed=True, detail=work())
            except Exception as error: row = dict(name=name, passed=False, error=str(error))
            checks.append(row); print(json.dumps(row, ensure_ascii=False), flush=True)
            (output/'checks.json').write_text(json.dumps(checks, ensure_ascii=False, indent=2), encoding='utf-8')
        old_mode, old_url = rpc('GetSetting','application_mode'), view.evaluate('location.href')
        old_settings = view.evaluate('localStorage.getItem("vp_settings")')
        history = rpc('GetRecentHistory', 30)
        entry = next(row for row in history if row['source_key'].startswith('acceptance_video'))
        source = next(row for row in rpc('GetAllSources') if row['source_key'] == entry['source_key'])
        port = urlparse(source['api_url']).port
        MEDIA.MediaFixture.directory = ROOT/'dist/audit/upgrade-native-media'
        assert (MEDIA.MediaFixture.directory/'movie.mp4').exists(), 'Generate controlled media with test-upgrade-player-android.py first'
        server = ThreadingHTTPServer(('127.0.0.1', port), MEDIA.MediaFixture)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        subprocess.run(view.adb+['reverse',f'tcp:{port}',f'tcp:{port}'],check=True,capture_output=True)
        old_operation = subprocess.check_output(view.adb+['shell','appops','get','com.cczj.video.acceptance','PICTURE_IN_PICTURE'],text=True,encoding='utf-8',errors='replace')
        matched = re.search(r'PICTURE_IN_PICTURE:\s*(\w+)',old_operation)
        old_permission = matched.group(1) if matched else 'allow'
        try:
            subprocess.run(view.adb+['shell','appops','set','com.cczj.video.acceptance','PICTURE_IN_PICTURE','allow'],check=True,capture_output=True)
            rpc('SetSetting','application_mode','video')
            view.evaluate('location.href="https://wails.localhost/?application-mode=video#/"')
            wait('!!document.querySelector(".cczj-mobile-shell")')
            rpc('GetVideoDetail',dict(source_key=entry['source_key'],vod_id=entry['vod_id'],refresh=True))
            view.evaluate('location.hash='+json.dumps(f"/player/{entry['source_key']}/{entry['global_id']}/0?vod={entry['vod_id']}"))
            wait('document.querySelector("video")?.readyState>=2', 60)
            view.evaluate('window.__regressionVideo=document.querySelector("video");__regressionVideo.muted=true;__regressionVideo.play()')
            if view.evaluate('!!document.querySelector(".cczj-mobile-resume")'):
                tap('document.querySelector(".cczj-mobile-resume > button:last-child")')
            if not view.evaluate('document.querySelector(".player-wrapper").classList.contains("show-controls")'):
                tap('document.querySelector("video")')
            tap('document.querySelector(".cczj-mobile-fullscreen")')
            wait('!!document.fullscreenElement && innerWidth>innerHeight')
            time.sleep(.7)
            def gutter():
                view.screenshot(output/'fullscreen.png')
                picture = Image.open(output/'fullscreen.png').convert('RGB')
                width,height = picture.size
                white = [x for x in range(width//6) if sum(min(picture.getpixel((x,y)))>235 for y in range(height//10,9*height//10,3)) / len(range(height//10,9*height//10,3)) > .95]
                geometry=view.evaluate('(()=>{const r=document.querySelector(".player-wrapper").getBoundingClientRect();return {viewport:[innerWidth,innerHeight],rect:[r.left,r.top,r.width,r.height],safeLeft:parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--cczj-player-safe-left"))||0,lockLeft:document.querySelector(".cczj-mobile-player-lock").getBoundingClientRect().left,native:JSON.parse(wails.playerState())}})()')
                assert len(white)<3, dict(whiteColumns=len(white),geometry=geometry)
                assert geometry['lockLeft']>=geometry['safeLeft']-1,geometry
                return geometry
            check('fullscreen-has-no-white-left-strip', gutter)
            if not view.evaluate('document.querySelector(".player-wrapper").classList.contains("show-controls")'):
                tap('document.querySelector("video")')
            tap('document.querySelector(".cczj-mobile-control-popover > button")')
            def speed():
                wait('document.querySelector(".cczj-mobile-speed-options")?.getBoundingClientRect().height>0')
                time.sleep(.4)
                view.screenshot(output/'speed.png')
                data=view.evaluate('(()=>{const d=document.querySelector(".cczj-mobile-speed-options").closest("[role=dialog]"),r=d.getBoundingClientRect();return {left:r.left,top:r.top,right:r.right,bottom:r.bottom,width:innerWidth,height:innerHeight,inFullscreen:document.fullscreenElement.contains(d)}})()')
                assert data['inFullscreen'] and data['left']>data['width']*.4 and abs(data['right']-data['width'])<2 and data['top']<2 and abs(data['bottom']-data['height'])<2,data
                tap('[...document.querySelectorAll(".cczj-mobile-speed-options button")].find(b=>b.textContent.trim()==="1.5x")')
                wait('document.querySelector("video").playbackRate===1.5')
                assert view.evaluate('window.__regressionVideo===document.querySelector("video")')
                return data
            check('fullscreen-speed-is-right-sidebar', speed)
            view.evaluate('[...document.querySelectorAll(".cczj-mobile-speed-options")].forEach(e=>e.closest("[role=dialog]").querySelector("header button").click())')
            time.sleep(.4)
            if not view.evaluate('document.querySelector(".player-wrapper").classList.contains("show-controls")'):
                tap('document.querySelector("video")')
            def pip():
                view.evaluate('window.__pipEvents=[];window.addEventListener("cczj:player-pip",e=>__pipEvents.push(e.detail))')
                capability=view.evaluate('JSON.parse(wails.playerState())')
                assert capability['pip'], 'Test device does not advertise Android PiP'
                tap('document.querySelector(".cczj-mobile-fullscreen-controls > button[aria-pressed]")')
                deadline=time.monotonic()+8
                while time.monotonic()<deadline:
                    state=subprocess.check_output(view.adb+['shell','dumpsys','activity','activities'],text=True,encoding='utf-8',errors='replace')
                    if 'mode=pinned' in state or 'windowingMode=2' in state: break
                    time.sleep(.2)
                else:
                    (output/'pip-log.txt').write_text(subprocess.check_output(view.adb+['logcat','-d','-t','250'],text=True,encoding='utf-8',errors='replace'),encoding='utf-8')
                    raise AssertionError(view.evaluate('({capability:JSON.parse(wails.playerState()),events:__pipEvents,text:document.body.innerText.slice(-700),pip:document.body.dataset.playerPip})'))
                view.screenshot(output/'pip.png')
                before=view.evaluate('document.querySelector("video").currentTime')
                time.sleep(1.5)
                assert view.evaluate('document.querySelector("video").currentTime')>before+.4, 'PiP paused playback'
                return dict(nativePinned=True,videoRetained=view.evaluate('window.__regressionVideo===document.querySelector("video")'))
            check('pip-enters-android-pinned-task',pip)
            subprocess.run(view.adb+['shell','am','start','-W','--activity-single-top','--activity-clear-top','-n','com.cczj.video.acceptance/com.wails.app.MainActivity'],check=True,capture_output=True)
            wait('!document.body.hasAttribute("data-player-pip") && !document.fullscreenElement')
            for permission_mode in ('ignore','default'):
                subprocess.run(view.adb+['shell','appops','set','com.cczj.video.acceptance','PICTURE_IN_PICTURE',permission_mode],check=True,capture_output=True)
                def permission():
                    capability=view.evaluate('JSON.parse(wails.playerState())')
                    assert capability['pipAllowed'] is False, capability
                    view.evaluate('document.querySelector("video").pause()')
                    if not view.evaluate('document.querySelector(".player-wrapper").classList.contains("show-controls")'):
                        tap('document.querySelector("video")')
                    tap('document.querySelector(".cczj-mobile-player-controls > button[aria-pressed]")')
                    wait('document.body.innerText.includes("画中画权限未开启")',3)
                    assert view.evaluate('!document.body.hasAttribute("data-player-pip")')
                    view.screenshot(output/'pip-permission.png')
                    return dict(mode=permission_mode,permissionDeniedShown=True,playerRestored=True)
                check(f'{permission_mode}-pip-permission-is-not-misreported-as-unsupported',permission)
        finally:
            subprocess.run(view.adb+['shell','appops','set','com.cczj.video.acceptance','PICTURE_IN_PICTURE',old_permission],check=True,capture_output=True)
            subprocess.run(view.adb+['shell','am','start','-W','--activity-single-top','--activity-clear-top','-n','com.cczj.video.acceptance/com.wails.app.MainActivity'],capture_output=True)
            view.evaluate('document.querySelector("video")?.pause();if(document.fullscreenElement)document.exitFullscreen()')
            rpc('SetSetting','application_mode',old_mode or '')
            view.evaluate('localStorage.'+('removeItem("vp_settings")' if old_settings is None else 'setItem("vp_settings",'+json.dumps(old_settings)+')'))
            view.evaluate('location.href='+json.dumps(old_url))
            server.shutdown(); server.server_close()
            subprocess.run(view.adb+['reverse','--remove',f'tcp:{port}'],capture_output=True)
    assert all(row['passed'] for row in checks), checks

if __name__=='__main__': main()
