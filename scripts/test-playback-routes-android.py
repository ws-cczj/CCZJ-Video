"""Actual Android route-quality discovery, switching and loading/resume layout.

Uses the isolated acceptance package and the existing media/CDP helpers. The
synthetic masters deliberately declare 720/1080 metadata; this checks playlist
declarations, rather than claiming these short fixture clips have that size.
"""
import argparse, importlib.util, json, threading, time
from pathlib import Path
from http.server import ThreadingHTTPServer
from urllib.parse import urlparse

ROOT=Path(__file__).resolve().parent.parent
def module(name,file):
    spec=importlib.util.spec_from_file_location(name,ROOT/'scripts'/file);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m
w=module('weak','test-playback-weak-network.py');u=module('upgrade','test-upgrade-android.py')

class Fixture(w.Media):
    gate=threading.Event()
    identity=str(int(time.time()))
    def do_GET(self):
        path=urlparse(self.path).path;base=f'http://127.0.0.1:{self.server.server_port}'
        if path=='/cms':
            urls=[f'{base}/media/low/index.m3u8',f'{base}/quality/720.m3u8',f'{base}/quality/1080.m3u8']
            row=dict(vod_id=self.identity,vod_name='Playlist quality acceptance '+self.identity,type_id='1',type_name='剧情片',vod_play_from='Unknown$$$720$$$1080',vod_play_url='$$$'.join('第一集$'+url for url in urls))
            body=json.dumps(dict(code=1,page=1,pagecount=1,total=1,list=[row]),ensure_ascii=False).encode();kind='application/json'
        elif path.startswith('/quality/'):
            height=1080 if '1080' in path else 720;width=1920 if height==1080 else 1280
            body=f'#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=350000,RESOLUTION={width}x{height}\n{base}/media/low/index.m3u8\n'.encode();kind='application/vnd.apple.mpegurl'
        else:
            if path.endswith('.ts'):self.gate.wait(15)
            super().do_GET();return
        self.send_response(200);self.send_header('Content-Type',kind);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--device',default='127.0.0.1:16384');args=parser.parse_args()
    out=ROOT/'dist/audit/playback-routes';out.mkdir(parents=True,exist_ok=True)
    (out/'android.json').unlink(missing_ok=True)
    Fixture.directory=ROOT/'.cache/weak-network/media';w.prepare_media(Fixture.directory);Fixture.link=w.Link();Fixture.link.configure(10_000_000,.02)
    server=ThreadingHTTPServer(('127.0.0.1',0),Fixture);threading.Thread(target=server.serve_forever,daemon=True).start();port=server.server_port
    d=u.Device('adb',args.device);d.native('reverse',f'tcp:{port}',f'tcp:{port}');d.connect();keys=[];checks=[];evidence={}
    original={'url':d.js('location.href'),'private':d.rpc('GetAllowPrivateNetwork'),'default':d.rpc('GetSetting','default_source_key'),'settings':d.js('localStorage.getItem("vp_settings")')}
    def wait(expression,seconds=20):
        end=time.monotonic()+seconds
        while time.monotonic()<end:
            if d.js(expression):return
            time.sleep(.08)
        raise AssertionError('timeout: '+expression)
    def tap(expression):
        d.js('('+expression+').scrollIntoView({block:"center",behavior:"instant"})')
        time.sleep(.15)
        point=d.js('(()=>{const r=('+expression+').getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()')
        d.call('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[point]));time.sleep(.08)
        d.call('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]))
    def shot(name):(out/(name+'.png')).write_bytes(d.native('exec-out','screencap','-p',binary=True))
    def layout():return d.js('(()=>{const root=document.querySelector(".player-wrapper"),prep=root.querySelector(".cczj-mobile-video-loading > span"),resume=root.querySelector(".cczj-mobile-resume"),controls=root.querySelector(".cczj-mobile-control-stack");const rect=e=>e?{x:e.getBoundingClientRect().x,y:e.getBoundingClientRect().y,bottom:e.getBoundingClientRect().bottom,height:e.getBoundingClientRect().height}:null;return{root:rect(root),prep:rect(prep),resume:rect(resume),controls:rect(controls)}})()')
    try:
        d.rpc('SetAllowPrivateNetwork',True)
        # Previous acceptance runs may leave sources pointing at stopped local
        # servers. Keep their discovery work out of this fixture's budget.
        original['sources']=[source for source in d.rpc('GetAllSources') if source['enabled']]
        for source in original['sources']:
            d.rpc('UpdateSource',dict(source,enabled=0))
        for index in range(2):
            source=d.rpc('CreateSource',dict(name=f'Quality audit {index} '+str(int(time.time())),api_url=f'http://127.0.0.1:{port}/cms?source={index}',enabled=1,kind='video'))
            keys.append(source['source_key'])
        quality=d.rpc('ProbePlayLines',dict(source_key=keys[1],vod_id=Fixture.identity,global_id=0,ep_num=1));assert {r['height'] for r in quality}=={0,720,1080},quality
        try:d.rpc('GetGlobalIdForVideo',keys[1],Fixture.identity)
        except RuntimeError:pass
        else:raise AssertionError('quality preview imported an unselected remote source')
        checks.append('native Go metadata preview reads declared resolutions without importing the source match')
        result=d.rpc('SearchSourceRequest',keys[0],'Playlist',1,20,'quality-audit');d.rpc('ImportSourceVideos',keys[0],result['videos']);identity=d.rpc('GetGlobalIdForVideo',keys[0],Fixture.identity)
        d.js('(async()=>{const s=document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("source");if(s)await s.loadSources(true)})()')
        d.visit(f'/detail/{keys[0]}/{identity}?vod={Fixture.identity}')
        wait('document.querySelector(".line-picker")?.textContent.includes("1080P")')
        detail_order=d.js('[...document.querySelectorAll(".line-picker .route-item")].map(e=>e.dataset.line)')
        assert detail_order==['2','1','0'],detail_order
        tap('[...document.querySelectorAll(".line-picker .route-item")].find(e=>e.dataset.line==="1")')
        wait('document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("video").activeLineIndex===1')
        checks.append('native detail page automatically probes quality and chooses the correct original line')
        d.rpc('SaveWatchHistory',dict(source_key=keys[0],vod_id=Fixture.identity,ep_num=1,position=20))
        # Seed the existing persisted resume API's key, independently of old
        # cross-source episode history retained in the acceptance application.
        resume_key='vp_t_vp_t_player_'+Fixture.identity+'_0'
        d.js('localStorage.setItem('+json.dumps(resume_key)+',JSON.stringify({version:1,value:20}))')
        d.js('(()=>{const raw=JSON.parse(localStorage.getItem("vp_settings")||"{}");localStorage.setItem("vp_settings",JSON.stringify({version:1,value:{...(raw.value||raw),auto_resume_jump:"0"}}))})()')
        d.js('(async()=>{const s=document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("source");if(s)await s.loadSources(true)})()')
        d.visit(f'/player/{keys[0]}/{identity}/0?vod={Fixture.identity}')
        # The current player presents preparation in the central loading
        # status, replacing the historical bottom preparation-tip element.
        wait('!!document.querySelector(".cczj-mobile-resume") && !!document.querySelector(".cczj-mobile-video-loading > span")',10)
        initial=layout();assert initial['prep']['bottom']<=initial['resume']['y']-3,initial
        evidence['hints']={'initial':initial}
        assert initial['root']['bottom']-initial['resume']['bottom']<20,initial
        tap('document.querySelector("video")');wait('!!document.querySelector(".cczj-mobile-resume:not(.cczj-mobile-resume-lowered)")');time.sleep(.3)
        raised=layout();assert raised['resume']['bottom']<=raised['controls']['y']-3,raised
        assert raised['resume']['y']<initial['resume']['y']-30,dict(initial=initial,raised=raised)
        evidence['hints']={'initial':initial,'raised':raised};shot('android-stacked-hints');checks.append('native loading text stays above the initial resume pill; revealing controls raises the pill above controls')
        Fixture.gate.set();wait('document.querySelector("video")?.currentTime>.1',30)
        wait('!document.querySelector(".cczj-mobile-video-loading")')
        first_playing=layout();assert first_playing['prep'] is None,first_playing
        evidence['hints']['first_playing']=first_playing
        tap('[...document.querySelectorAll(".side-section button")].find(e=>e.textContent.trim()==="切换线路")')
        wait('!![...document.querySelectorAll("[role=dialog]")].find(e=>e.getAttribute("aria-label")==="切换线路"&&e.getBoundingClientRect().height>0)')
        row_selector='.playback-routes-mobile [data-source="'+keys[0]+'"]'
        wait('document.querySelectorAll('+json.dumps(row_selector)+').length===3 && document.querySelector(".playback-routes-mobile").textContent.includes("1080P")')
        rows=d.js('[...document.querySelectorAll(".playback-routes-mobile .route-item")].filter(e=>e.dataset.source==='+json.dumps(keys[0])+').map(e=>({line:e.dataset.line,text:e.textContent}))')
        assert [r['line'] for r in rows]==['2','1','0'],rows;evidence['routes']=rows;time.sleep(.4);shot('android-lines')
        tap('document.querySelector('+json.dumps(row_selector+'[data-line="2"]')+')')
        wait('document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("video").activeLineIndex===2')
        wait('(()=>{const url=document.querySelector("video")?.__hls?.url;if(!url)return false;try{return atob(new URL(url,location.href).searchParams.get("u").replace(/-/g,"+").replace(/_/g,"/")).includes("/quality/1080.m3u8")}catch{return false}})()')
        wait('document.querySelector("video").currentTime>.2')
        wait('![...document.querySelectorAll(".cczj-mobile-quick-mask")].some(e=>e.getBoundingClientRect().height>0)')
        time.sleep(.25)
        checks.append('native line drawer sorts 1080P, 720P, unknown and selects the original line identity')
        if not d.js('document.querySelector(".player-wrapper").classList.contains("show-controls")'):tap('document.querySelector("video")')
        wait('document.querySelector(".cczj-mobile-fullscreen").getBoundingClientRect().height>0')
        tap('document.querySelector(".cczj-mobile-fullscreen")');wait('!!document.fullscreenElement&&innerWidth>innerHeight')
        if not d.js('document.querySelector(".player-wrapper").classList.contains("show-controls")'):tap('document.querySelector("video")')
        tap('document.querySelector(".cczj-mobile-player-lines")')
        wait('!!document.fullscreenElement.querySelector('+json.dumps('[role=dialog][aria-label="切换线路"] .route-item')+')')
        time.sleep(.4);shot('android-fullscreen-lines');checks.append('native fullscreen line drawer remains inside fullscreen host')
        (out/'android.json').write_text(json.dumps(dict(checks=checks,**evidence),ensure_ascii=False,indent=2),encoding='utf8');print(json.dumps(checks,ensure_ascii=False),flush=True)
    except Exception as error:
        shot('android-failure')
        evidence['failureState']=d.js('({url:location.href,storage:Object.fromEntries(Object.entries(localStorage).filter(([k])=>k.startsWith("vp_t_")||k==="vp_settings")),video:[...document.querySelectorAll("video")].map(v=>({time:v.currentTime,ready:v.readyState,url:v.__hls?.url})),classes:document.querySelector(".player-wrapper")?.className})')
        (out/'android-failure.json').write_text(json.dumps(dict(error=str(error),checks=checks,evidence=evidence,body=d.js('document.body.innerText')),ensure_ascii=False,indent=2),encoding='utf8');raise
    finally:
        Fixture.gate.set()
        try:
            if d.js('!!document.fullscreenElement'):d.native('shell','input','keyevent','KEYCODE_BACK');time.sleep(.3)
            d.visit('/sources')
            for key in keys:d.rpc('DeleteSource',key)
            for source in original.get('sources',[]):d.rpc('UpdateSource',source)
            d.js('localStorage.removeItem('+json.dumps('vp_t_vp_t_player_'+Fixture.identity+'_0')+')')
            d.rpc('SetSetting','default_source_key',original['default'] or '');d.rpc('SetAllowPrivateNetwork',original['private'])
            d.js('localStorage.'+('removeItem("vp_settings")' if original['settings'] is None else 'setItem("vp_settings",'+json.dumps(original['settings'])+')'));d.js('location.href='+json.dumps(original['url']))
        finally:d.close();d.native('reverse','--remove',f'tcp:{port}');Fixture.link.configure(10_000_000);server.shutdown();server.server_close()

if __name__=='__main__':main()
