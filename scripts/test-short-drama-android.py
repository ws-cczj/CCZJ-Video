"""Native short-drama acceptance using the isolated acceptance APK and real Go bindings.

Requires its current amd64 build, adb, FFmpeg and websocket-client. Creates one
temporary loopback CMS source; restores settings and removes that source afterward.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import subprocess
import threading
import time
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

ROOT = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location('upgrade', ROOT/'scripts/test-upgrade-android.py')
upgrade = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(upgrade)
OUT = ROOT/'dist/audit/short-drama'


class Fixture(SimpleHTTPRequestHandler):
    identity = str(int(time.time()))

    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(OUT/'native-media'), **kwargs)

    def log_message(self, *args):
        pass

    def do_GET(self):
        request = urlparse(self.path)
        if request.path != '/cms':
            super().do_GET(); return
        base = f'http://127.0.0.1:{self.server.server_port}'
        episodes = '#'.join(f'第{i}集${base}/index.m3u8?episode={i}' for i in range(1, 81))
        videos = [dict(vod_id=f'{self.identity}{i}', vod_name=f'竖屏验收 {self.identity} 第{i}部', type_id='1', type_name='竖屏验收短剧'+self.identity,
                       vod_content='她在雨夜重逢故人，决定重新选择自己的生活。'*20, vod_play_from='HLS', vod_play_url=episodes) for i in range(1,8)]
        videos.append(dict(vod_id=f'{self.identity}99', vod_name='不能进入短剧推荐的电影', type_id='2', type_name='竖屏验收长视频'+self.identity, vod_play_from='HLS', vod_play_url=episodes))
        ids = parse_qs(request.query).get('ids', [''])[0].split(',')
        if ids != ['']:
            videos = [video for video in videos if video['vod_id'] in ids]
        body = json.dumps(dict(code=1,page=1,pagecount=1,total=len(videos),list=videos,**{'class':[{'type_id':'1','type_name':'竖屏验收短剧'+self.identity},{'type_id':'2','type_name':'竖屏验收长视频'+self.identity}]}),ensure_ascii=False).encode()
        self.send_response(200); self.send_header('Content-Type','application/json'); self.send_header('Content-Length',str(len(body))); self.end_headers(); self.wfile.write(body)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    parser.add_argument('--catalog-recovery', action='store_true', help='Check stale catalog and deleted-source recovery with native bindings')
    args = parser.parse_args()
    OUT.mkdir(parents=True,exist_ok=True)
    media = OUT/'native-media';media.mkdir(exist_ok=True)
    subprocess.run(['ffmpeg','-hide_banner','-loglevel','error','-y','-f','lavfi','-i','testsrc2=size=270x480:rate=24','-f','lavfi','-i','sine=frequency=440:sample_rate=48000','-t','40','-c:v','libx264','-preset','ultrafast','-b:v','240k','-g','48','-pix_fmt','yuv420p','-c:a','aac','-b:a','48k','-f','hls','-hls_time','2','-hls_list_size','0',str(media/'index.m3u8')],check=True)
    server=ThreadingHTTPServer(('127.0.0.1',0),Fixture)
    threading.Thread(target=server.serve_forever,daemon=True).start();port=server.server_port
    device=upgrade.Device('adb',args.device)
    device.native('reverse',f'tcp:{port}',f'tcp:{port}')
    device.native('shell','am','start','-W','-n',upgrade.PACKAGE+'/com.wails.app.MainActivity')
    device.connect()
    original={'url':device.js('location.href'),'private':device.rpc('GetAllowPrivateNetwork'),'default':device.rpc('GetSetting','default_source_key'),'mode':device.rpc('GetSetting','application_mode'),'settings':device.js('localStorage.getItem("vp_settings")')}
    key='';checks=[]
    def wait(expression, seconds=25):
        until=time.monotonic()+seconds
        while time.monotonic()<until:
            if device.js(expression): return
            time.sleep(.1)
        raise AssertionError('timeout: '+expression)
    def tap(selector):
        point=device.js('(()=>{const e=document.querySelector('+json.dumps(selector)+');e.scrollIntoView({block:"center",behavior:"instant"});const r=e.getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()')
        device.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[point]});time.sleep(.08)
        device.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
    def shot(name):
        device.js('''(async()=>{
            const animations=[...document.querySelectorAll('.cczj-mobile-quick-mask')].flatMap(e=>e.getAnimations({subtree:true}));
            await Promise.all(animations.map(a=>a.finished.catch(()=>{})));
            await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
            return true;
        })()''')
        (OUT/(name+'.png')).write_bytes(device.native('exec-out','screencap','-p',binary=True))
    ready='!!document.querySelector("video") && document.querySelector("video").readyState>=2 && !document.querySelector("video").paused'
    try:
        device.rpc('SetAllowPrivateNetwork',True)
        source=device.rpc('CreateSource',dict(name='Short drama acceptance '+Fixture.identity,api_url=f'http://127.0.0.1:{port}/cms',enabled=1,kind='video'))
        key=source['source_key']
        result=device.rpc('SearchSourceRequest',key,'竖屏',1,20,'short-drama-audit')
        device.rpc('ImportSourceVideos',key,result['videos'])
        rows=device.rpc('GetRecommend',dict(source_key=key,limit=99,exclude_ids=[],short_drama=True))
        assert len(rows)==5 and all('短剧' in row['type_name'] for row in rows), rows
        checks.append('real CMS search/import and native recommendation returns five short dramas, excluding film')
        device.visit('/')
        device.store('source','s.loadSources(true)')
        device.store('source','s.switchSource('+json.dumps(key)+')')
        device.visit('/short-drama');wait(ready)
        assert device.js('document.querySelector("video").videoHeight>document.querySelector("video").videoWidth')
        assert device.js('document.querySelectorAll(".cczj-mobile-player-controls,.ctrl-bar").length')==0
        assert device.js('[...document.querySelectorAll(".cczj-mobile-navigation a")].map(e=>e.getAttribute("href"))')==['#/','#/short-drama','#/search','#/favorites','#/settings']
        shot('android-playback');checks.append('embedded short-drama route autoplays actual portrait HLS with mobile navigation')
        if args.catalog_recovery:
            device.visit('/settings')
            device.rpc('RunSourceAction',dict(source_key=key,action='truncate',vod_id=''))
            device.visit('/short-drama')
            wait('document.body.innerText.includes("还没有可播放的短剧") && !document.body.innerText.includes("短剧加载失败") && !document.querySelector("video")')
            shot('android-cleared-catalog')
            checks.append('retired cached entries refresh to an empty catalog instead of catalog lookup failure')
            device.rpc('ImportSourceVideos',key,result['videos'])
            tap('.drama-placeholder .ui-btn');wait(ready)
            checks.append('retry after importing catalog entries plays native HLS again')
            device.visit('/settings')
            device.rpc('DeleteSource',key);key=''
            device.visit('/short-drama')
            wait('document.body.innerText.includes("请先添加视频源并采集短剧内容。") && !document.body.innerText.includes("短剧加载失败") && !document.querySelector("video")')
            shot('android-deleted-source')
            checks.append('reentry refreshes removed sources and clears stale errors and media')
            (OUT/'android-catalog-recovery-result.json').write_text(json.dumps(dict(checks=checks,scope='isolated acceptance APK; native Go/SQLite/CMS/HLS; targeted catalog recovery'),ensure_ascii=False,indent=2),encoding='utf-8')
            print(json.dumps(checks,ensure_ascii=False))
            return
        first=device.js('document.querySelector(".drama-title").textContent')
        device.js('document.querySelector("video").currentTime=document.querySelector("video").duration-.1;true')
        wait('document.querySelector(".drama-title").textContent!=='+json.dumps(first));wait(ready)
        wait('document.querySelector(".drama-episodes").textContent.includes("第1集")')
        checks.append('recommendation playback ends on a different drama rather than advancing its episodes')
        point=device.js('(()=>{const r=document.querySelector("video").getBoundingClientRect();return{x:r.x+r.width*.55,y:r.y+r.height*.3}})()')
        device.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[point]})
        wait('document.querySelector("video").playbackRate===2')
        shot('android-hold')
        device.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
        wait('document.querySelector("video").playbackRate===1 && !document.querySelector("video").paused')
        checks.append('native touch hold boosts to 2x, release restores 1x without pausing')
        # Touch the real range and verify a committed seek, then flush under its original identity.
        slider=device.js('(()=>{const r=document.querySelector(".portrait-progress").getBoundingClientRect();return{x:r.x+r.width*.5,y:r.y+r.height/2}})()')
        device.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[slider]})
        device.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
        wait('document.querySelector("video").currentTime>15')
        previous=device.js('document.querySelector(".drama-title").textContent')
        device.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[point]})
        device.call('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[dict(point,y=point['y']-100)]})
        device.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
        wait(ready);wait('document.querySelector(".drama-title").textContent!=='+json.dumps(previous))
        history=device.rpc('GetRecentHistory',100)
        assert any(row['source_key']==key and row['position']>15 for row in history), history
        checks.append('native timeline seeks, vertical swipe switches drama, SQLite retains previous progress')
        tap('.drama-episodes')
        wait('document.querySelector("[role=dialog]")?.getBoundingClientRect().height>0')
        assert device.js('document.querySelectorAll(".drama-episode").length')==20
        assert device.js('document.querySelectorAll(".drama-range").length')==4
        shot('android-episodes')
        assert device.js('getComputedStyle(document.querySelector("[role=dialog]")).backgroundColor')=='rgb(24, 25, 28)'
        device.native('shell','input','keyevent','KEYCODE_BACK')
        wait('![...document.querySelectorAll("[role=dialog]")].some(e=>e.getBoundingClientRect().height>0)')
        assert device.js('location.hash')=='#/short-drama'
        tap('.drama-episodes');tap('.drama-range:last-child');tap('.drama-episode:last-child');wait(ready)
        wait('document.querySelector(".drama-episodes").textContent.includes("80")')
        checks.append('80 episodes in four groups, Android Back dismisses without navigation, touch selects last episode')
        series_title=device.js('document.querySelector(".drama-title").textContent')
        assert device.js('document.querySelector(".drama-switch")===null')
        device.js('document.querySelector("video").currentTime=document.querySelector("video").duration-.1;true')
        wait('document.querySelector(".drama-position")?.textContent==="本剧已播完"')
        assert device.js('document.querySelector(".drama-title").textContent')==series_title
        device.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[point]})
        device.call('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[dict(point,y=point['y']+100)]})
        device.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
        wait(ready)
        wait('document.querySelector(".drama-episodes").textContent.includes("第79集")')
        device.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[point]})
        device.call('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[dict(point,y=point['y']-100)]})
        device.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
        wait('document.querySelector(".drama-episodes").textContent.includes("第80集")');wait(ready)
        assert device.js('document.querySelector(".drama-title").textContent')==series_title
        checks.append('series touch navigation remains in the same drama and completion stops at its last episode')
        device.js('window.__oldDramaVideo=document.querySelector("video");true')
        device.visit('/favorites')
        wait('window.__oldDramaVideo.paused && !window.__oldDramaVideo.getAttribute("src")')
        device.visit('/short-drama')
        # A saved position near the end can finish before visit() settles.
        # Returning to a completed series must stay there rather than start another drama.
        wait('''(()=>{const video=document.querySelector('video');return video?.readyState>=2 &&
            (!video.paused || (video.ended && document.querySelector('.drama-position')?.textContent==='本剧已播完'))})()''')
        wait('document.querySelector(".drama-episodes").textContent.includes("80")')
        checks.append('hidden kept-alive tab releases media and resumes its selected episode on return')
        tap('.drama-refresh');wait(ready)
        assert device.js('document.querySelector(".drama-refresh").getAttribute("aria-label")')=='换一批'
        checks.append('returning from a completed series starts recommendations rather than retaining the ended frame')
        target=rows[0]
        device.visit('/detail/'+key+'/'+str(target['global_id'])+'?vod='+target['vod_id'])
        wait('!!document.querySelector(".detail-page .poster-actions .ui-btn--primary")')
        tap('.detail-page .poster-actions .ui-btn--primary')
        wait('location.hash.startsWith("#/short-drama?")');wait(ready)
        assert device.js('new URLSearchParams(location.hash.split("?")[1]).get("vod")')==target['vod_id']
        assert device.js('new URLSearchParams(location.hash.split("?")[1]).get("source")')==key
        assert device.js('document.querySelector(".drama-refresh").textContent.includes("返回推荐")')
        shot('android-series-playback')
        tap('.drama-refresh');wait(ready)
        wait('location.hash==="#/short-drama"')
        checks.append('actual detail play button opens its portrait series with source identity and can return to recommendations')
        catalog=device.rpc('GetVideoList',dict(source_key=key,short_drama=True,page=1,page_size=24))
        assert catalog['total']==7 and all('短剧' in row['type_name'] for row in catalog['videos']),catalog
        device.js('window.__oldDramaVideo=document.querySelector("video");true')
        tap('.drama-heading .drama-mobile-tab[aria-pressed=false]')
        wait('location.hash==="#/short-drama?view=theatre"')
        wait('document.querySelectorAll(".drama-theatre .video-card").length===7')
        wait('!document.querySelector("video") && window.__oldDramaVideo.paused && !window.__oldDramaVideo.getAttribute("src")')
        assert device.js('getComputedStyle(document.querySelector(".theatre-grid")).gridTemplateColumns.split(" ").length')==2
        shot('android-theatre')
        tap('.drama-theatre .video-card')
        wait(ready)
        assert device.js('new URLSearchParams(location.hash.split("?")[1]).get("source")')==key
        wait('!!document.querySelector(".drama-return")')
        tap('.drama-return')
        wait('location.hash==="#/short-drama?view=theatre" && !document.querySelector("video")')
        wait('document.querySelectorAll(".drama-theatre .video-card").length===7')
        tap('.theatre-random');wait(ready)
        checks.append('mobile theatre releases the recommendation player, browses only short dramas in two columns, enters a series and returns to recommendations')
        (OUT/'android-result.json').write_text(json.dumps(dict(checks=checks,scope='isolated acceptance APK; real Go/SQLite/CMS/HLS and Android touch'),ensure_ascii=False,indent=2),encoding='utf-8')
        print(json.dumps(checks,ensure_ascii=False))
    except Exception:
        shot('android-failure')
        (OUT/'android-failure.json').write_text(json.dumps(dict(checks=checks,body=device.js('document.body.innerText'),events=device.events[-20:]),ensure_ascii=False,indent=2),encoding='utf-8')
        raise
    finally:
        try:
            device.visit('/sources')
            if key: device.rpc('DeleteSource',key)
            device.rpc('SetSetting','default_source_key',original['default'] or '')
            device.store('source','s.loadSources(true)')
            device.rpc('SetSetting','application_mode',original['mode'] or 'video')
            device.rpc('SetAllowPrivateNetwork',original['private'])
            device.js('localStorage.'+('removeItem("vp_settings")' if original['settings'] is None else 'setItem("vp_settings",'+json.dumps(original['settings'])+')'))
            device.js('location.href='+json.dumps(original['url']))
        finally:
            device.close();device.native('reverse','--remove',f'tcp:{port}');server.shutdown();server.server_close()


if __name__=='__main__': main()
