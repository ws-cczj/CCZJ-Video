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
            row=dict(vod_id=self.identity,vod_name='Playlist quality acceptance '+self.identity,type_id='1',type_name='剧情片',vod_play_from='Unknown$$$720$$$1080',vod_play_url='$$$'.join('#'.join(f'第{episode:02}集$'+url for episode in range(1,14)) for url in urls))
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
    theme_store='document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("theme")'
    original={'url':d.js('location.href'),'private':d.rpc('GetAllowPrivateNetwork'),'default':d.rpc('GetSetting','default_source_key'),'settings':d.js('localStorage.getItem("vp_settings")'),'autoplay':d.js('localStorage.getItem("cczj_auto_play")'),'theme':d.js(theme_store+'.currentId')}
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
    def reveal_controls():
        if not d.js('document.querySelector(".player-wrapper").classList.contains("show-controls")'):tap('document.querySelector("video")')
        wait('document.querySelector(".player-wrapper").classList.contains("show-controls")')
        time.sleep(.35) # Let the production touch delay and control transition settle.
    def theme(mode):
        d.js(theme_store+'.currentId='+json.dumps('china_ink' if mode=='light' else 'd-blue'))
        wait('document.documentElement.dataset.themeMode==='+json.dumps(mode))
        time.sleep(.15)
    def action_colors(selector,mode):
        values=d.js('''(()=>{const lum=colour=>{const rgb=colour.match(/[\\d.]+/g).slice(0,3).map(Number).map(v=>{v/=255;return v<=.04045?v/12.92:((v+.055)/1.055)**2.4});return rgb.reduce((s,v,i)=>s+v*[.2126,.7152,.0722][i],0)};return [...document.querySelectorAll('''+json.dumps(selector)+''')].map(e=>{const s=getComputedStyle(e),fill=lum(s.backgroundColor),ink=lum(s.color);return {fill,ink,contrast:(Math.max(fill,ink)+.05)/(Math.min(fill,ink)+.05)}})})()''')
        assert values and all(row['contrast']>=4.5 and (row['fill']<row['ink'] if mode=='light' else row['fill']>row['ink']) for row in values),dict(mode=mode,selector=selector,colors=values)
        return values
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
        wait('document.querySelectorAll(".episode-btn").length===13')
        assert d.js('!document.querySelector(".line-picker") && document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("video").lines.length===3')
        checks.append('mobile detail retains all three routes and leaves route selection to the player drawer')
        d.rpc('SaveWatchHistory',dict(source_key=keys[0],vod_id=Fixture.identity,ep_num=1,position=20))
        d.rpc('SaveWatchHistory',dict(source_key=keys[0],vod_id=Fixture.identity,ep_num=2,position=55,duration=100))
        # Seed the existing persisted resume API's key, independently of old
        # cross-source episode history retained in the acceptance application.
        resume_key='vp_t_vp_t_player_'+Fixture.identity+'_0'
        d.js('localStorage.setItem('+json.dumps(resume_key)+',JSON.stringify({version:1,value:20}))')
        d.js('(()=>{const raw=JSON.parse(localStorage.getItem("vp_settings")||"{}");localStorage.setItem("vp_settings",JSON.stringify({version:1,value:{...(raw.value||raw),auto_resume_jump:"0"}}))})()')
        d.js('localStorage.setItem("cczj_auto_play",JSON.stringify({version:1,value:true}))')
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
        checks.append('automatic playback starts without clicking the resume prompt')
        # Re-enter with auto resume enabled: no prompt or manual play is needed.
        d.visit(f'/detail/{keys[0]}/{identity}?vod={Fixture.identity}')
        wait('!!document.querySelector(".detail-page")')
        d.js('(()=>{const raw=JSON.parse(localStorage.getItem("vp_settings")||"{}");localStorage.setItem("vp_settings",JSON.stringify({version:1,value:{...(raw.value||raw),auto_resume_jump:"1"}}))})()')
        d.visit(f'/player/{keys[0]}/{identity}/0?vod={Fixture.identity}')
        wait('document.querySelector("video")?.currentTime>=18 && !document.querySelector("video").paused',30)
        assert not d.js('!!document.querySelector(".cczj-mobile-resume")'), 'remembered auto resume still asks for a click'
        checks.append('remembered automatic resume restores history and plays without a click')
        # A compact section header must not stretch the episode grid away.
        geometry=d.js('(()=>{const header=document.querySelector(".side-section-title").getBoundingClientRect(),grid=document.querySelector(".ep-grid").getBoundingClientRect(),button=document.querySelector(".sort-toggle-btn").getBoundingClientRect();return {gap:grid.top-header.bottom,sortHeight:button.height,episodes:document.querySelectorAll(".ep-item").length,animation:getComputedStyle(document.querySelector(".ep-item.active")).animationName}})()')
        assert 0<=geometry['gap']<=10 and 36<=geometry['sortHeight']<=40 and geometry['episodes']==13 and 'active-ep-pulse' in geometry['animation'],geometry
        evidence['episodeLayout']=geometry;shot('android-episode-layout')
        checks.append('13-episode grid has a compact sort action and restores the current episode breathing glow')
        evidence['themeButtons']={}
        for mode in ['light','dark']:
            theme(mode)
            evidence['themeButtons'][mode]=dict(episodes=action_colors('.ep-item.watched,.ep-item.active,.sort-toggle-btn',mode))
            states=d.js('''(()=>{const current=document.querySelector('.ep-item.active'),watched=document.querySelector('.ep-item.watched:not(.active)'),future=document.querySelector('.ep-item.future');const read=e=>({background:getComputedStyle(e).backgroundColor,shadow:getComputedStyle(e).boxShadow,outline:getComputedStyle(e).outlineStyle});const percent=current.querySelector('.ep-watched-pct'),progress=current.querySelector('.ep-watched-progress');return {current:read(current),watched:read(watched),future:read(future),surface:getComputedStyle(current.closest('aside')).backgroundColor,percent:getComputedStyle(percent).color,percentBackground:getComputedStyle(percent).backgroundColor,progress:getComputedStyle(progress).backgroundColor,watchedProgress:getComputedStyle(watched.querySelector('.ep-watched-progress')).backgroundColor,bars:getComputedStyle(current.querySelector('.bar')).backgroundColor}})()''')
            import re
            bars=list(map(int,re.findall(r'\d+',states['bars'])[:3]))
            progress=list(map(int,re.findall(r'\d+',states['progress'])[:3]))
            assert states['current']['background']==states['watched']['background'] and states['watched']['background']!=states['future']['background'] and states['future']['background']!=states['surface'],states
            assert states['current']['shadow']!='none' and states['current']['outline']=='none' and states['percentBackground']=='rgba(0, 0, 0, 0)' and bars[2]>180 and bars[2]>bars[0],states
            assert states['progress']==states['watchedProgress'] and states['progress']!=states['current']['background'] and progress[0]>progress[2],states
            evidence['themeButtons'][mode]['states']=states
            shot('android-episodes-'+mode)
        reveal_controls()
        wait('document.querySelector(".player-wrapper").classList.contains("show-controls")')
        tap('document.querySelector('+json.dumps('.cczj-mobile-player-controls button[aria-label="选集"]')+')')
        wait('document.querySelectorAll(".cczj-mobile-drawer-episode-grid button").length===13')
        for mode in ['light','dark']:
            theme(mode)
            evidence['themeButtons'][mode]['episodeDrawer']=action_colors('.cczj-mobile-drawer-episode-grid button,.cczj-mobile-drawer-episode-head button',mode)
        tap('document.querySelector(".cczj-mobile-drawer-episode-grid").closest("[role=dialog]").querySelector("header button")')
        reveal_controls()
        wait('document.querySelector(".cczj-mobile-player-lines")?.getBoundingClientRect().height>0')
        tap('document.querySelector(".cczj-mobile-player-lines")')
        wait('!![...document.querySelectorAll("[role=dialog]")].find(e=>e.getAttribute("aria-label")==="切换线路"&&e.getBoundingClientRect().height>0)')
        row_selector='.playback-routes-mobile [data-source="'+keys[0]+'"]'
        wait('document.querySelectorAll('+json.dumps(row_selector)+').length===3 && document.querySelector(".playback-routes-mobile").textContent.includes("1080P")')
        rows=d.js('[...document.querySelectorAll(".playback-routes-mobile .route-item")].filter(e=>e.dataset.source==='+json.dumps(keys[0])+').map(e=>({line:e.dataset.line,text:e.textContent}))')
        assert [r['line'] for r in rows]==['2','1','0'],rows;evidence['routes']=rows;time.sleep(.4);shot('android-lines')
        for mode in ['light','dark']:
            theme(mode)
            evidence['themeButtons'][mode]['routes']=action_colors('.playback-routes-mobile .route-item,.player-route-retry',mode)
            shot('android-lines-'+mode)
        checks.append('light theme uses dark filled actions; dark theme uses light filled actions, with readable text in episode and route drawers')
        tap('document.querySelector('+json.dumps(row_selector+'[data-line="2"]')+')')
        wait('document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("video").activeLineIndex===2')
        wait('(()=>{const url=document.querySelector("video")?.__hls?.url;if(!url)return false;try{return atob(new URL(url,location.href).searchParams.get("u").replace(/-/g,"+").replace(/_/g,"/")).includes("/quality/1080.m3u8")}catch{return false}})()')
        wait('document.querySelector("video").currentTime>.2')
        wait('![...document.querySelectorAll(".cczj-mobile-quick-mask")].some(e=>e.getBoundingClientRect().height>0)')
        time.sleep(.25)
        checks.append('native line drawer sorts 1080P, 720P, unknown and selects the original line identity')
        reveal_controls()
        wait('document.querySelector(".cczj-mobile-fullscreen").getBoundingClientRect().height>0')
        tap('document.querySelector(".cczj-mobile-fullscreen")');wait('!!document.fullscreenElement&&innerWidth>innerHeight')
        reveal_controls()
        tap('document.querySelector(".cczj-mobile-player-lines")')
        wait('!!document.fullscreenElement.querySelector('+json.dumps('[role=dialog][aria-label="切换线路"] .route-item')+')')
        time.sleep(.4);shot('android-fullscreen-lines');checks.append('native fullscreen line drawer remains inside fullscreen host')
        # Back first dismisses the open drawer, then exits fullscreen.
        d.native('shell','input','keyevent','KEYCODE_BACK')
        wait('![...document.querySelectorAll(".cczj-mobile-quick-mask")].some(e=>e.getBoundingClientRect().height>0)')
        d.native('shell','input','keyevent','KEYCODE_BACK');wait('!document.fullscreenElement')
        d.js(theme_store+'.currentId='+json.dumps(original['theme']))
        d.visit('/')
        wait('!!document.querySelector(".cczj-mobile-navigation-featured")')
        navigation=d.js('(()=>{const nav=document.querySelector(".cczj-mobile-navigation"),action=nav.querySelector(".cczj-mobile-navigation-featured"),icon=action.querySelector(".cczj-mobile-navigation-icon");return {items:nav.querySelectorAll("a").length,label:action.textContent.trim(),width:icon.getBoundingClientRect().width,mask:getComputedStyle(nav,"::before").webkitMaskImage}})()')
        assert navigation['items']==5 and navigation['label']=='搜索' and navigation['width']==48 and 'radial-gradient' in navigation['mask'],navigation
        assert d.js('(()=>{const action=document.querySelector(".cczj-mobile-navigation-featured"),r=action.querySelector(".cczj-mobile-navigation-icon").getBoundingClientRect();return document.elementFromPoint(r.x+r.width/2,r.y+3)?.closest("a")===action})()'), 'content covers the raised search action'
        evidence['navigation']=navigation;shot('android-navigation')
        viewport_height=d.js('innerHeight')
        try:
            for width in [320,390]:
                d.call('Emulation.setDeviceMetricsOverride',dict(width=width,height=viewport_height,deviceScaleFactor=3,mobile=True))
                time.sleep(.15)
                assert d.js('[...document.querySelectorAll(".cczj-mobile-navigation a")].every(e=>{const r=e.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&r.width>=44&&r.height>=44})'), f'navigation overflows at {width}px'
        finally:d.call('Emulation.clearDeviceMetricsOverride')
        tap('document.querySelector(".cczj-mobile-navigation-featured .cczj-mobile-navigation-icon")')
        wait('location.hash.includes("/search") && document.querySelector(".cczj-mobile-navigation-featured")?.getAttribute("aria-current")==="page"')
        checks.append('bottom navigation has five labeled actions and a circular search cradle')
        (out/'android.json').write_text(json.dumps(dict(checks=checks,**evidence),ensure_ascii=False,indent=2),encoding='utf8');print(json.dumps(checks,ensure_ascii=False),flush=True)
    except Exception as error:
        shot('android-failure')
        evidence['failureState']=d.js('({url:location.href,storage:Object.fromEntries(Object.entries(localStorage).filter(([k])=>k.startsWith("vp_t_")||k==="vp_settings")),video:[...document.querySelectorAll("video")].map(v=>({time:v.currentTime,ready:v.readyState,url:v.__hls?.url})),classes:document.querySelector(".player-wrapper")?.className})')
        (out/'android-failure.json').write_text(json.dumps(dict(error=str(error),checks=checks,evidence=evidence,body=d.js('document.body.innerText')),ensure_ascii=False,indent=2),encoding='utf8');raise
    finally:
        Fixture.gate.set()
        try:
            d.js(theme_store+'.currentId='+json.dumps(original['theme']))
            if d.js('!!document.fullscreenElement'):d.native('shell','input','keyevent','KEYCODE_BACK');time.sleep(.3)
            d.visit('/sources')
            for key in keys:d.rpc('DeleteSource',key)
            for source in original.get('sources',[]):d.rpc('UpdateSource',source)
            d.js('localStorage.removeItem('+json.dumps('vp_t_vp_t_player_'+Fixture.identity+'_0')+')')
            d.rpc('SetSetting','default_source_key',original['default'] or '');d.rpc('SetAllowPrivateNetwork',original['private'])
            d.js('localStorage.'+('removeItem("vp_settings")' if original['settings'] is None else 'setItem("vp_settings",'+json.dumps(original['settings'])+')'))
            d.js('localStorage.'+('removeItem("cczj_auto_play")' if original['autoplay'] is None else 'setItem("cczj_auto_play",'+json.dumps(original['autoplay'])+')'))
            d.js('location.href='+json.dumps(original['url']))
        finally:d.close();d.native('reverse','--remove',f'tcp:{port}');Fixture.link.configure(10_000_000);server.shutdown();server.server_close()

if __name__=='__main__':main()
