"""Source save/search and weak-link playback in the isolated acceptance APK.

Reuses the shared-link fixture and existing Android CDP/device helpers. Does not
clear application data or modify the user's preview application.
"""
import argparse, importlib.util, json, subprocess, threading, time
from http.server import ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse,parse_qs

ROOT=Path(__file__).resolve().parent.parent
def module(name,file):
    spec=importlib.util.spec_from_file_location(name,ROOT/'scripts'/file);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m
weak=module('weak','test-playback-weak-network.py');upgrade=module('upgrade','test-upgrade-android.py')

class Fixture(weak.Media):
    def do_GET(self):
        path=urlparse(self.path).path;query=parse_qs(urlparse(self.path).query)
        if path.startswith('/api.php/') or path=='/index.php/ajax/suggest':
            base=f'http://127.0.0.1:{self.server.server_port}'
            row=dict(vod_id='1',vod_name='Weak network native video',type_id='1',type_name='剧情片',vod_play_from='HLS',vod_play_url=f'第一集${base}/media/master.m3u8',vod_year='2026',vod_content='Isolated acceptance fixture')
            body={'code':1002,'msg':'keyword search disabled'} if query.get('wd') and path.startswith('/api.php/') else {'code':1,'page':1,'pagecount':1,'total':1,'list':[row]}
            raw=json.dumps(body,ensure_ascii=False).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw);return
        super().do_GET()

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--device',default='127.0.0.1:16384');parser.add_argument('--seconds',type=float,default=70);parser.add_argument('--scenario',choices=['adaptive','offline'],default='adaptive');args=parser.parse_args()
    output=ROOT/'dist/audit/weak-network';output.mkdir(parents=True,exist_ok=True);Fixture.directory=ROOT/'.cache/weak-network/media';weak.prepare_media(Fixture.directory);Fixture.link=weak.Link()
    server=ThreadingHTTPServer(('127.0.0.1',0),Fixture);port=server.server_port;threading.Thread(target=server.serve_forever,daemon=True).start()
    d=upgrade.Device('adb',args.device);d.native('reverse',f'tcp:{port}',f'tcp:{port}');d.connect();checks=[];samples=[];name='Weak network audit '+str(int(time.time()));source_key=None
    original={'url':d.js('location.href'),'mode':d.rpc('GetSetting','application_mode'),'default':d.rpc('GetSetting','default_source_key'),'private':d.rpc('GetAllowPrivateNetwork'),'search':d.js('localStorage.getItem("source_search_mode")')}
    def wait(expression,seconds=25):
        end=time.monotonic()+seconds
        while time.monotonic()<end:
            if d.js(expression):return
            time.sleep(.15)
        raise AssertionError('timeout: '+expression)
    def tap(expression):
        rect=d.js('(()=>{const e='+expression+';if(!e)throw Error("Missing touch target");e.scrollIntoView({block:"center"});const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()')
        d.call('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[rect]));d.call('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]));time.sleep(.2)
    def fill(index,value):
        d.js(f'document.querySelectorAll(".form-group input")[{index}].focus()');d.call('Input.dispatchKeyEvent',dict(type='keyDown',key='a',code='KeyA',modifiers=2,windowsVirtualKeyCode=65));d.call('Input.dispatchKeyEvent',dict(type='keyUp',key='a',code='KeyA',modifiers=2,windowsVirtualKeyCode=65));d.call('Input.insertText',dict(text=value))
    try:
        d.rpc('SetAllowPrivateNetwork',True);d.visit('/sources')
        tap('[...document.querySelectorAll("button")].find(e=>e.textContent.trim()==="添加源")');wait('document.querySelectorAll(".form-group input").length>=3')
        fill(0,f'http://127.0.0.1:{port}/api.php/provide/vod/');fill(2,name)
        tap('[...document.querySelectorAll("button")].find(e=>e.textContent.trim()==="保存")')
        wait('document.body.textContent.includes('+json.dumps(name)+') && !document.querySelector(".source-form-modal")')
        wait('document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("source")?.sources.some(s=>s.name==='+json.dumps(name)+')')
        source_key=next(s['source_key'] for s in d.rpc('GetAllSources') if s['name']==name);checks.append('source saved through native touch form appears immediately without restarting')
        result=d.rpc('SearchSourceRequest',source_key,'weak',1,20,'native-weak-search');assert result['limited'] and len(result['videos'])==1,result;checks.append('native Go search uses same-source website suggestions and resolves original details')
        records=d.rpc('GetLogRecords',0,5000);assert any('source operation complete' in r['message'] and source_key in r['message'] for r in records);assert any('Sources.list' in r['message'] and 'published' in r['message'] and source_key in r['message'] for r in records);checks.append('native source write, UI publication and search stages are recorded')
        assert d.rpc('ImportSourceVideos',source_key,result['videos'])==1
        d.rpc('GetVideoDetail',dict(source_key=source_key,vod_id='1',refresh=True));identity=d.rpc('GetGlobalIdForVideo',source_key,'1');d.visit(f'/player/{source_key}/{identity}/0?vod=1')
        wait('document.querySelector("video")?.readyState>=2 && !!document.querySelector("video").__hls',40)
        d.js('(()=>{window.weakEvents=[];const v=document.querySelector("video");v.muted=true;for(const event of ["waiting","playing","stalled","error"])v.addEventListener(event,()=>weakEvents.push({event,wall:performance.now(),time:v.currentTime,ready:v.readyState}));void v.play()})()')
        wait('document.querySelector("video").currentTime>.2');started=time.monotonic();phase=None
        while time.monotonic()-started<args.seconds:
            elapsed=time.monotonic()-started;new_phase='fast' if elapsed<10 or elapsed>=50 else 'offline' if args.scenario=='offline' and 20<=elapsed<25 else 'weak'
            if new_phase!=phase:phase=new_phase;Fixture.link.configure(3_000_000 if phase=='fast' else 0 if phase=='offline' else 650_000,.08 if phase=='fast' else .25)
            sample=d.js('(()=>{const v=document.querySelector("video"),h=v.__hls;return {time:v.currentTime,ready:v.readyState,level:h?.currentLevel,next:h?.nextAutoLevel,bps:h?.bandwidthEstimate,buffer:Array.from({length:v.buffered.length},(_,i)=>[v.buffered.start(i),v.buffered.end(i)])}})()');sample.update(wall=round(elapsed,3),phase=phase);samples.append(sample);time.sleep(.5)
        events=d.js('weakEvents');perf=d.store('perf','s.snapshot()');assert samples[-1]['time']>args.seconds*.8,samples[-1];assert len({round(s['bps']) for s in samples if s.get('bps')})>1,'bandwidth estimate never updated';assert any(s['level']==1 for s in samples),'high level never selected';assert any(s['phase']=='weak' and s['wall']>30 and s['level']==0 for s in samples),'no weak-link downswitch'
        assert not d.js('document.querySelector("video").error'),'native media error';checks.append('native HLS decoding advances with measured bandwidth and automatic weak-link downswitch')
        records=d.rpc('GetLogRecords',0,5000);policy=[r for r in records if 'player_network_policy' in r['message']];assert policy,'missing network policy logs';checks.append('native log records link policy, bitrate level, buffer and deadlines')
        (output/f'android-{args.scenario}.png').write_bytes(d.native('exec-out','screencap','-p',binary=True))
        active_wait=None;stalls=[]
        for event in events:
            if event['event']=='waiting' and event['time']>.1 and active_wait is None:active_wait=event['wall']
            if event['event']=='playing' and active_wait is not None:stalls.append(event['wall']-active_wait);active_wait=None
        result={'checks':checks,'samples':samples,'events':events,'perf':perf,'policy_logs':policy,'requests':Fixture.link.records,'package':upgrade.PACKAGE,'device':args.device,'stall_count':len(stalls),'completed_stall_ms':round(sum(stalls)),'open_stall':active_wait is not None}
        (output/f'android-{args.scenario}.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf8');print(json.dumps({'checks':checks,'media_seconds':samples[-1]['time'],'levels':sorted({s['level'] for s in samples})},ensure_ascii=False),flush=True)
    except Exception as error:
        (output/f'android-{args.scenario}-failure.json').write_text(json.dumps({'error':str(error),'checks':checks,'samples':samples,'state':d.js('({url:location.href,text:document.body.innerText.slice(-10000)})')},ensure_ascii=False,indent=2),encoding='utf8')
        (output/f'android-{args.scenario}-failure.png').write_bytes(d.native('exec-out','screencap','-p',binary=True))
        raise
    finally:
        try:
            d.visit('/sources')
            for row in d.rpc('GetAllSources'):
                if row['name']==name:d.rpc('DeleteSource',row['source_key'])
            d.rpc('SetSetting','default_source_key',original['default'] or '');d.rpc('SetAllowPrivateNetwork',original['private'])
            d.js('localStorage.'+('removeItem("source_search_mode")' if original['search'] is None else 'setItem("source_search_mode",'+json.dumps(original['search'])+')'))
            if d.rpc('GetSetting','application_mode')!=original['mode']:d.rpc('SetSetting','application_mode',original['mode'] or 'video')
            d.js('location.href='+json.dumps(original['url']))
        finally:d.close();d.native('reverse','--remove',f'tcp:{port}');Fixture.link.configure(3_000_000);server.shutdown();server.server_close()

if __name__=='__main__':main()
