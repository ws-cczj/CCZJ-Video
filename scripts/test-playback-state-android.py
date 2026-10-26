"""Verify resume, episode percentages, favorite folders and danmaku size on Android.

Uses a unique acceptance-only source and restores settings and browser caches.
"""
import argparse, importlib.util, json, threading, time, uuid
from pathlib import Path
from http.server import ThreadingHTTPServer
from urllib.parse import urlparse

ROOT=Path(__file__).resolve().parent.parent
spec=importlib.util.spec_from_file_location('routes',ROOT/'scripts/test-playback-routes-android.py')
r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)

class Fixture(r.w.Media):
    identity='state-'+uuid.uuid4().hex
    def do_GET(self):
        if urlparse(self.path).path!='/cms':super().do_GET();return
        base=f'http://127.0.0.1:{self.server.server_port}'
        episodes='#'.join(f'Episode {i}${base}/media/low/index.m3u8' for i in range(1,6))
        row=dict(vod_id=self.identity,vod_name='Playback state '+self.identity,type_id='1',type_name='Drama',vod_play_from='hls',vod_play_url=episodes)
        body=json.dumps(dict(code=1,page=1,pagecount=1,total=1,list=[row])).encode()
        self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--device',default='emulator-5554');args=parser.parse_args()
    out=ROOT/'dist/audit/playback-state';out.mkdir(parents=True,exist_ok=True)
    Fixture.directory=ROOT/'.cache/weak-network/media';r.w.prepare_media(Fixture.directory);Fixture.link=r.w.Link();Fixture.link.configure(10_000_000,.01)
    server=ThreadingHTTPServer(('127.0.0.1',0),Fixture);threading.Thread(target=server.serve_forever,daemon=True).start();port=server.server_port
    d=r.u.Device('adb',args.device);d.native('reverse',f'tcp:{port}',f'tcp:{port}');d.connect()
    keys=['cczj_fav_folders','cczj_fav_mapping','danmaku_preferences_v1','default_source_key']
    original={'settings':{key:d.rpc('GetSetting',key) for key in keys},'private':d.rpc('GetAllowPrivateNetwork'),'route':d.js('location.hash'),'storage':d.js('JSON.stringify(Object.fromEntries(Object.entries(localStorage).filter(([k])=>k.startsWith("vp_")||k.startsWith("cczj_fav_")||k==="cczj_ep_prog_v1")))')}
    (out/'before-fixture.json').write_text(json.dumps(original,ensure_ascii=False,indent=2),encoding='utf-8')
    source=None;checks=[]
    def wait(expression,seconds=25):
        end=time.monotonic()+seconds
        while time.monotonic()<end:
            if d.js(expression):return
            time.sleep(.1)
        raise AssertionError(expression)
    def tap(expression,hold=.08):
        wait('(()=>{const e=('+expression+');return !!e&&e.getBoundingClientRect().width>0&&e.getBoundingClientRect().height>0})()')
        d.js('('+expression+').scrollIntoView({block:"center",behavior:"instant"})');time.sleep(.15)
        point=d.js('(()=>{const r=('+expression+').getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()')
        d.call('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[point]));time.sleep(hold);d.call('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]))
    def shot(name):(out/(name+'.png')).write_bytes(d.native('exec-out','screencap','-p',binary=True))
    try:
        d.rpc('SetAllowPrivateNetwork',True)
        source=d.rpc('CreateSource',dict(name=Fixture.identity,api_url=f'http://127.0.0.1:{port}/cms',enabled=1,kind='video'))['source_key']
        result=d.rpc('SearchSourceRequest',source,'Playback',1,20,'state-audit');d.rpc('ImportSourceVideos',source,result['videos'])
        gid=d.rpc('GetGlobalIdForVideo',source,Fixture.identity)
        for episode in range(1,6):d.rpc('SaveWatchHistory',dict(source_key=source,vod_id=Fixture.identity,ep_num=episode,position=99 if episode<5 else 15,duration=100))
        detail=f'/detail/{source}/{gid}?vod={Fixture.identity}'
        d.visit('/favorites');wait('!!document.querySelector(".cczj-mobile-folder-create")')
        tap('document.querySelector(".cczj-mobile-folder-create")');wait('!!document.querySelector(".folder-input")')
        name='State regression '+uuid.uuid4().hex[:6]
        d.js('(()=>{const input=document.querySelector(".folder-input");input.value='+json.dumps(name)+';input.dispatchEvent(new Event("input",{bubbles:true}));})()')
        tap('document.querySelector(".ui-modal__footer .ui-btn--primary")');wait('!document.querySelector(".ui-modal")')
        assert name in d.rpc('GetSetting','cczj_fav_folders')
        d.visit(detail);wait('document.querySelectorAll(".episode-btn").length===5')
        assert not d.js('!!document.querySelector(".episodes-section .line-picker")'),'Mobile details mounted the removed route probe'
        checks.append('mobile episode details omit the route probe card and retry action')
        percentages=d.js('Array.from(document.querySelectorAll(".episode-btn")).map(e=>e.textContent)')
        assert sum('99%' in text for text in percentages)==4,percentages
        assert '15%' in percentages[4],percentages
        tap('document.querySelector(".detail-page button[aria-pressed]")');wait('!!document.querySelector(".folder-select-item")')
        wait('!document.querySelector(".ui-modal__footer .ui-btn--primary").disabled')
        tap('Array.from(document.querySelectorAll(".folder-select-item")).find(e=>e.textContent.includes('+json.dumps(name)+'))')
        shot('folder-selection')
        tap('document.querySelector(".ui-modal__footer .ui-btn--primary")');wait('!document.querySelector(".ui-modal")')
        assert d.rpc('IsFavorite',dict(source_key=source,vod_id=Fixture.identity)) is True
        checks.append('new folder is visible in the picker and a single touch saves the favorite')
        d.restart();d.visit('/favorites')
        wait('Array.from(document.querySelectorAll(".folder-row")).some(e=>e.textContent.includes('+json.dumps(name)+'))')
        tap('Array.from(document.querySelectorAll(".folder-row")).find(e=>e.textContent.includes('+json.dumps(name)+'))')
        wait('document.querySelectorAll(".favorites-page .video-card").length===1');shot('favorite-after-restart')
        checks.append('folder and assigned favorite survive native app restart')
        d.visit(detail);wait('document.querySelectorAll(".episode-btn").length===5')
        d.js('localStorage.setItem("vp_settings",JSON.stringify({version:1,value:{auto_resume_jump:"0"}}))')
        tap('document.querySelector(".detail-page .ui-btn--primary")')
        wait('location.hash.includes("resume=1")')
        wait('document.querySelector("video")?.currentTime>=14',45)
        wait('!document.querySelector("video").paused && !document.querySelector(".cczj-mobile-video-loading")?.getBoundingClientRect().height',45)
        d.js('document.querySelector("video").pause()');time.sleep(.4)
        video=d.js('({time:document.querySelector("video").currentTime,duration:document.querySelector("video").duration})')
        assert 14<=video['time']<24,video
        assert not d.js('!!document.querySelector(".cczj-mobile-resume")')
        history=[row for row in d.rpc('GetRecentHistory',100) if row.get('global_id')==gid]
        assert all(row['position']==99 for row in history if row['ep_num']<5),history
        assert next(row for row in history if row['ep_num']==5)['position']>=14,history
        contrast=d.js('''(()=>{
          const element=document.querySelector('.ep-item.watched:not(.active)'),label=element.querySelector('.ep-item-num');
          const rgb=value=>value.match(/[\\d.]+/g).map(Number);
          const layers=[];for(let node=element;node;node=node.parentElement)layers.unshift(rgb(getComputedStyle(node).backgroundColor));
          let bg=[255,255,255];for(const layer of layers){const a=layer[3]??1;bg=bg.map((v,i)=>layer[i]*a+v*(1-a))}
          const color=rgb(getComputedStyle(label).color),a=color[3]??1,fg=color.slice(0,3).map((v,i)=>v*a+bg[i]*(1-a));
          const luminance=c=>c.map(v=>{v/=255;return v<=.04045?v/12.92:((v+.055)/1.055)**2.4}).reduce((sum,v,i)=>sum+v*[.2126,.7152,.0722][i],0);
          const x=luminance(fg),y=luminance(bg);return {ratio:(Math.max(x,y)+.05)/(Math.min(x,y)+.05),foreground:fg,background:bg};
        })()''')
        assert contrast['ratio']>=4.5,contrast
        checks.append('watched episode labels retain at least 4.5:1 text contrast')
        shot('resume-and-percentages');checks.append('continue restores episode 5 at 15%, preserves four 99% episodes and saves real duration')
        if not d.js('document.querySelector(".player-title-bar").getBoundingClientRect().height>0'):
            point=d.js('(()=>{const r=document.querySelector("video").getBoundingClientRect();return{x:r.x+24,y:r.y+r.height/2}})()')
            d.call('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[point]));time.sleep(.08);d.call('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]))
            wait('document.querySelector(".player-title-bar").getBoundingClientRect().height>0');time.sleep(.3)
        tap('document.querySelector(".cczj-mobile-player-more-button")')
        wait('document.querySelector(".cczj-mobile-player-more-button").getAttribute("aria-expanded")==="true"')
        time.sleep(.4)
        tap('document.querySelectorAll(".cczj-mobile-player-categories button")[1]')
        wait('document.querySelectorAll(".cczj-mobile-player-categories button")[1].getAttribute("aria-pressed")==="true"')
        time.sleep(.4)
        tap('document.querySelector("[data-player-action=danmaku-settings]")')
        wait('!!document.querySelector(".cczj-danmaku-panel")')
        d.js('(()=>{const input=document.querySelectorAll(".cczj-danmaku-range input")[2];input.value="36";input.dispatchEvent(new Event("input",{bubbles:true}));})()')
        wait('document.querySelectorAll(".cczj-danmaku-range output")[2].textContent==="150%"')
        wait('!document.querySelectorAll(".cczj-danmaku-range input")[2].disabled')
        time.sleep(.7)
        assert json.loads(d.rpc('GetSetting','danmaku_preferences_v1'))['fontSize']==36
        assert float(d.js('parseFloat(getComputedStyle(document.querySelector(".cczj-danmaku-range span")).fontSize)'))==13
        shot('danmaku-size');checks.append('danmaku size is editable, shows percentage, persists, and settings labels use 13px mobile text')
        (out/'result.json').write_text(json.dumps(dict(checks=checks,video=video,watchedContrast=contrast),indent=2),encoding='utf-8');print(json.dumps(checks),flush=True)
    except Exception:
        if d.socket:
            shot('failure')
            details=d.js('Array.from(document.querySelectorAll("[data-player-action],.cczj-danmaku-panel,.cczj-mobile-player-more-mask")).map(e=>({text:e.textContent.slice(0,200),display:getComputedStyle(e).display,rect:{x:e.getBoundingClientRect().x,y:e.getBoundingClientRect().y,w:e.getBoundingClientRect().width,h:e.getBoundingClientRect().height}}))')
            (out/'failure.json').write_text(json.dumps(dict(elements=details,events=d.events[-20:]),ensure_ascii=False,indent=2),encoding='utf-8')
        raise
    finally:
        try:
            if not d.socket:d.connect()
            d.visit('/sources')
            if source:d.rpc('DeleteSource',source)
            for key,value in original['settings'].items():d.rpc('SetSetting',key,value or '')
            d.rpc('SetAllowPrivateNetwork',original['private'])
            saved=json.loads(original['storage'])
            d.js('for(const key of Object.keys(localStorage))if(key.startsWith("vp_")||key.startsWith("cczj_fav_")||key==="cczj_ep_prog_v1")localStorage.removeItem(key)')
            for key,value in saved.items():d.js('localStorage.setItem('+json.dumps(key)+','+json.dumps(value)+')')
            d.restart();d.visit(original['route'].removeprefix('#'))
        finally:d.close();d.native('reverse','--remove',f'tcp:{port}');server.shutdown();server.server_close()

if __name__=='__main__':main()
