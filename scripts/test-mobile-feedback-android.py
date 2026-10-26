"""Isolated native regression for source XML, theme boundaries and log sharing.

Uses the acceptance package only; temporary source/settings are restored.
"""
import importlib.util
import json
from pathlib import Path
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit, parse_qs

ROOT=Path(__file__).resolve().parent.parent
spec=importlib.util.spec_from_file_location('upgrade',ROOT/'scripts/test-upgrade-android.py')
upgrade=importlib.util.module_from_spec(spec);spec.loader.exec_module(upgrade)
OUT=ROOT/'dist/audit/mobile-feedback';OUT.mkdir(parents=True,exist_ok=True)

class XMLSource(BaseHTTPRequestHandler):
    def log_message(self,*args):pass
    def do_GET(self):
        if self.path.startswith('/broken'):
            self.send_response(200);self.end_headers();self.wfile.write(b'{"code":1,"list":"not an array"}');return
        if self.path.startswith('/refused'):
            self.send_response(200);self.end_headers();self.wfile.write(b'{"code":1002,"msg":"refused","list":"no"}');return
        if self.path.startswith('/forbidden'):
            self.send_response(403);self.end_headers();self.wfile.write(b'<html>denied</html>');return
        body=b'''<?xml version="1.0"?><rss version="5.1"><list page="1" pagecount="1" pagesize="20" recordcount="1"><video><id>feedback-xml-1</id><tid>1</tid><name>Mobile XML regression</name><type>Short drama</type><note>1 episode</note><dl><dd flag="HLS">Episode 1$https://example.test/1.m3u8</dd></dl></video></list></rss>'''
        self.send_response(200);self.send_header('Content-Type','application/xml');self.end_headers();self.wfile.write(body)

def main():
    server=ThreadingHTTPServer(('127.0.0.1',0),XMLSource)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    port=server.server_port
    device=upgrade.Device('adb','emulator-5554')
    device.native('reverse',f'tcp:{port}',f'tcp:{port}')
    device.native('shell','am','start','-W','-n',upgrade.PACKAGE+'/com.wails.app.MainActivity');device.connect()
    store='document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("theme")'
    original={'url':device.js('location.hash'),'theme':device.js(store+'.currentId'),'private':device.rpc('GetAllowPrivateNetwork')}
    key='';extra_keys=[];checks=[]
    def wait(expression):
        end=time.monotonic()+20
        while time.monotonic()<end:
            if device.js(expression):return
            time.sleep(.1)
        raise AssertionError(expression)
    def visit(route,selector):
        parsed=urlsplit(route)
        screen=parsed.path+(':'+parse_qs(parsed.query).get('group',['index'])[0] if parsed.path=='/settings' else '')
        device.js('location.hash='+json.dumps(route))
        wait(f'!!document.querySelector({json.dumps(selector)}) && !!document.querySelector('+json.dumps('.cczj-mobile-route-scene[data-screen="'+screen+'"]')+') && !document.querySelector(".cczj-mobile-route-stage [class*=enter-active],.cczj-mobile-route-stage [class*=leave-active]") && document.querySelector(".cczj-mobile-route-backdrop").getAnimations().every(a=>a.playState!=="running")')
    def shot(name): (OUT/(name+'.png')).write_bytes(device.native('exec-out','screencap','-p',binary=True))
    try:
        device.rpc('SetAllowPrivateNetwork',True)
        source=device.rpc('CreateSource',dict(name='Mobile feedback XML',api_url=f'http://127.0.0.1:{port}/cms',enabled=1,kind='video'));key=source['source_key']
        result=device.rpc('SearchSourceRequest',key,'Mobile',1,20,'mobile-feedback')
        assert len(result['videos'])==1 and result['videos'][0]['vod_id']=='feedback-xml-1',result
        checks.append('real Go search normalizes legacy RSS XML and preserves video identity')
        for path,kind,status in [('broken','decode',200),('refused','source',200),('forbidden','http',403)]:
            row=device.rpc('CreateSource',dict(name='Mobile probe '+path,api_url=f'http://127.0.0.1:{port}/{path}',enabled=1,kind='video'))
            extra_keys.append(row['source_key'])
            probe=device.rpc('ProbeSource',row['source_key'])
            assert probe['failure_kind']==kind and probe['status_code']==status and not probe['ok'],probe
        checks.append('native probe bindings preserve decode/source/HTTP distinctions and actual status codes')
        for theme in ['green','d-blue','china_ink']:
            device.js(store+'.currentId='+json.dumps(theme))
            visit('/settings?group=theme','.theme-edit-fab')
            assert device.js('document.querySelectorAll(".theme-card .mini-btn").length')==0
            assert device.js('document.querySelectorAll(".theme-current-actions").length')==0
            assert device.js('document.querySelectorAll(".theme-edit-fab").length')==1
            assert device.js('document.querySelector(".cczj-mobile-shell").getBoundingClientRect().top')==0
            assert device.js('getComputedStyle(document.querySelector(".cczj-mobile-header")).backgroundColor === "rgba(0, 0, 0, 0)"')
            shot('theme-'+theme)
            for width in [320,390]:
                device.call('Emulation.setDeviceMetricsOverride',dict(width=width,height=740,deviceScaleFactor=2,mobile=True))
                geometry=device.js('''(()=>{const e=document.querySelector('.theme-edit-fab'),r=e.getBoundingClientRect();return {x:r.x,y:r.y,w:r.width,h:r.height,right:innerWidth-r.right,bottom:innerHeight-r.bottom,overflow:document.documentElement.scrollWidth>innerWidth}})()''')
                assert geometry['w']==52 and geometry['h']==52 and geometry['right']>=16 and geometry['bottom']>=16 and not geometry['overflow'],geometry
                device.js("document.querySelector('.cczj-mobile-content').scrollTop=500")
                assert device.js("document.querySelector('.theme-edit-fab').getBoundingClientRect().y")==geometry['y']
                device.js("document.querySelector('.cczj-mobile-content').scrollTop=0")
                shot(f'theme-{theme}-{width}')
            device.call('Emulation.clearDeviceMetricsOverride')
            name=device.js(store+'.current.name')
            device.js('document.querySelector(".theme-edit-fab").click()');wait('!!document.querySelector("[role=dialog]")')
            assert device.js('document.querySelector("[role=dialog] input[type=text]").value')==name
            shot('theme-editor-'+theme)
            device.native('shell','input','keyevent','KEYCODE_BACK');wait('!document.querySelector("[role=dialog]")')
            assert device.js(store+'.currentId')==theme
            for group in ['basic','logs']:
                visit('/settings?group='+group,'.cczj-mobile-header')
                assert device.js('getComputedStyle(document.querySelector(".cczj-mobile-header")).backgroundColor === "rgba(0, 0, 0, 0)"')
                assert device.js('!document.querySelector(".theme-edit-fab")?.getClientRects().length')
                assert device.js('document.querySelector(".cczj-mobile-header").querySelectorAll("a").length')==0
            for route in ['/danmaku-sources','/video-types']:
                visit(route,'.cczj-mobile-header')
                assert device.js('document.querySelector(".cczj-mobile-header").querySelectorAll("a").length')==0
                if route=='/danmaku-sources':
                    assert device.js('!document.querySelector(".cczj-danmaku-source-hero h1")')
                    shot('danmaku-sources-'+theme)
            native_background=device.native('shell','run-as',upgrade.PACKAGE,'cat','shared_prefs/cczj-presentation.xml')
            visit('/short-drama','.short-drama')
            assert device.js('document.querySelectorAll(".app-shell.cczj-short-drama-surface,.cczj-mobile-shell.cczj-short-drama-surface").length')==0
            assert device.js('getComputedStyle(document.querySelector(".cczj-mobile-shell")).getPropertyValue("--cczj-mobile-canvas").trim()===getComputedStyle(document.documentElement).getPropertyValue("--bg-primary").trim()')
            assert device.native('shell','run-as',upgrade.PACKAGE,'cat','shared_prefs/cczj-presentation.xml')==native_background,'short dramas changed native system bar/launch background'
            shot('short-drama-'+theme)
            visit('/short-drama?view=theatre','.drama-theatre')
            assert device.native('shell','run-as',upgrade.PACKAGE,'cat','shared_prefs/cczj-presentation.xml')==native_background
            visit('/settings?group=theme','.theme-edit-fab')
        checks.append('light/dark/wallpaper headers use the shared backdrop; one fixed theme editor action at 320/390px opens the current theme and restores it on Back')
        checks.append('short-drama recommendations and theatre retain the light/dark shell, navigation and native bar theme')
        visit('/settings','.cczj-mobile-settings-index')
        assert device.js('!document.querySelector(".cczj-mobile-settings-index .cczj-mobile-intro")')
        shot('settings-hub')
        visit('/settings?group=theme','.theme-edit-fab')
        device.js('document.querySelector(".theme-edit-fab").click()');wait('!!document.querySelector("[role=dialog]")');shot('theme-editor')
        device.native('shell','input','keyevent','KEYCODE_BACK');wait('!document.querySelector("[role=dialog]")')
        visit('/settings?group=logs','.log-export-actions')
        device.js('document.querySelector(".log-export-actions .ui-btn--primary").click()')
        time.sleep(2)
        resumed=device.native('shell','dumpsys','activity','activities')
        assert 'ChooserActivity' in resumed or 'ResolverActivity' in resumed,'export did not open Android chooser'
        shot('log-share')
        device.native('shell','input','keyevent','KEYCODE_BACK')
        wait('!!document.querySelector(".log-export-actions .ui-btn--secondary")')
        shot('logs-ready')
        checks.append('log export immediately opens system share/save chooser and remains available after cancellation')
        (OUT/'result.json').write_text(json.dumps(checks,ensure_ascii=False,indent=2),encoding='utf-8')
        print(json.dumps(checks))
    finally:
        try:
            device.call('Emulation.clearDeviceMetricsOverride')
            if key:device.rpc('DeleteSource',key)
            for item in extra_keys:device.rpc('DeleteSource',item)
            device.rpc('SetAllowPrivateNetwork',original['private'])
            device.js(store+'.currentId='+json.dumps(original['theme']))
            device.js('location.hash='+json.dumps(original['url']))
        finally:
            device.close();device.native('reverse','--remove',f'tcp:{port}');server.shutdown();server.server_close()

if __name__=='__main__':main()
