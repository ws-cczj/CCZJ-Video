"""Actual player-page acceptance in the isolated native Android application.

Uses a loopback CMS and generated H.264/AAC MP4/HLS fixtures, actual Go bindings,
actual Vue page, trusted touch input and the native fullscreen/back bridge.
"""
import argparse, base64, importlib.util, json, mimetypes, subprocess, threading, time
from pathlib import Path
from http.server import ThreadingHTTPServer
from urllib.parse import urlparse

ROOT=Path(__file__).resolve().parent.parent
spec=importlib.util.spec_from_file_location('upgrade',ROOT/'scripts/test-upgrade-android.py')
u=importlib.util.module_from_spec(spec);spec.loader.exec_module(u)

class MediaFixture(u.Fixture):
    directory=None
    def do_GET(self,head=False):
        path=urlparse(self.path).path
        port=self.server.server_port
        if path=='/video-api':
            base=f'http://127.0.0.1:{port}/media/'
            row=dict(vod_id='1',vod_name='Android acceptance movie',type_id='1',type_name='测试',vod_remarks='2集',vod_play_from='MP4$$$HLS',vod_play_url=f'第一集${base}movie.mp4#第二集${base}movie.mp4?episode=2$$$第一集${base}movie.m3u8#第二集${base}movie.m3u8?episode=2',vod_content='Controlled acceptance fixture',vod_year='2026')
            body=json.dumps(dict(code=1,page=1,pagecount=1,limit=20,total=1,list=[row],**{'class':[dict(type_id=1,type_name='测试')]})).encode()
            self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(body)));self.end_headers()
            if not head:self.wfile.write(body)
        elif path.startswith('/media/'):
            file=self.directory/Path(path).name
            if not file.is_file():self.send_error(404);return
            body=file.read_bytes();start,end=0,len(body)-1
            value=self.headers.get('Range','')
            if value.startswith('bytes='):
                a,b=value[6:].split('-');start=int(a or 0);end=min(int(b) if b else end,end)
            self.send_response(206 if value else 200)
            self.send_header('Content-Type', 'application/vnd.apple.mpegurl' if file.suffix=='.m3u8' else 'video/mp2t' if file.suffix=='.ts' else 'video/mp4')
            self.send_header('Content-Length',str(end-start+1));self.send_header('Accept-Ranges','bytes')
            if value:self.send_header('Content-Range',f'bytes {start}-{end}/{len(body)}')
            self.end_headers()
            if not head:
                if file.suffix=='.ts':time.sleep(.15)
                try:self.wfile.write(body[start:end+1])
                except (BrokenPipeError,ConnectionResetError):pass
        else:super().do_GET(head)

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--adb',default='adb');p.add_argument('--device',default='emulator-5554');p.add_argument('--ffmpeg',default='ffmpeg')
    args=p.parse_args()
    out=ROOT/'reports/upgrade-acceptance-2026-10-19/player-android';out.mkdir(parents=True,exist_ok=True)
    media=ROOT/'dist/audit/upgrade-native-media';media.mkdir(parents=True,exist_ok=True)
    if not (media/'movie.mp4').exists():
        subprocess.run([args.ffmpeg,'-hide_banner','-loglevel','error','-f','lavfi','-i','testsrc2=size=320x180:rate=15','-f','lavfi','-i','sine=frequency=440:sample_rate=44100','-t','120','-c:v','libx264','-preset','ultrafast','-pix_fmt','yuv420p','-g','30','-c:a','aac','-movflags','+faststart',str(media/'movie.mp4')],check=True)
        subprocess.run([args.ffmpeg,'-hide_banner','-loglevel','error','-i',str(media/'movie.mp4'),'-c','copy','-hls_time','4','-hls_list_size','0','-hls_segment_filename',str(media/'segment%03d.ts'),str(media/'movie.m3u8')],check=True)
    MediaFixture.directory=media
    server=ThreadingHTTPServer(('127.0.0.1',0),MediaFixture);threading.Thread(target=server.serve_forever,daemon=True).start()
    port=server.server_port
    d=u.Device(args.adb,args.device);d.native('reverse',f'tcp:{port}',f'tcp:{port}');d.connect()
    checks=[]
    def require(value,detail):
        if not value:raise AssertionError(detail)
    def wait(expression,timeout=25):
        until=time.monotonic()+timeout
        while time.monotonic()<until:
            if d.js(expression):return
            time.sleep(.12)
        raise AssertionError('Timed out: '+expression)
    def touch(kind,x=0,y=0):d.call('Input.dispatchTouchEvent',dict(type=kind,touchPoints=[] if kind=='touchEnd' else [dict(x=x,y=y)]))
    def tap(selector):
        r=d.js('(()=>{const e=document.querySelector('+json.dumps(selector)+');if(!e)throw Error("Missing touch target");const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()')
        touch('touchStart',**r);touch('touchEnd');time.sleep(.4)
    def drag(x,y,dx,dy):
        r=d.js('(()=>{const r=document.querySelector(".player-wrapper").getBoundingClientRect();return {x:r.x,y:r.y,w:r.width,h:r.height}})()')
        touch('touchStart',r['x']+r['w']*x,r['y']+r['h']*y)
        for i in range(1,17):
            touch('touchMove',r['x']+r['w']*(x+dx*i/16),r['y']+r['h']*(y+dy*i/16));time.sleep(.045)
        time.sleep(.4)
        hud=d.js('({text:document.querySelector(".cczj-mobile-seek-feedback,.cczj-mobile-level-feedback")?.textContent,preview:!!document.querySelector(".cczj-mobile-seek-feedback img"),compact:document.querySelector(".cczj-mobile-level-feedback")?.className})')
        touch('touchEnd');time.sleep(.4);return hud
    def shot(name):
        (out/(name+'.png')).write_bytes(base64.b64decode(d.call('Page.captureScreenshot',dict(format='png'))['data']))
    def check(name,fn):
        try:result=fn();row=dict(name=name,status='passed',result=result)
        except Exception as error:row=dict(name=name,status='failed',error=str(error))
        checks.append(row);(out/'checks.json').write_text(json.dumps(checks,ensure_ascii=False,indent=2),encoding='utf-8');print(json.dumps(row,ensure_ascii=True),flush=True)
    def snapshot():return d.store('perf','s.snapshot()')
    recording=None
    try:
        d.rpc('SetAllowPrivateNetwork',True)
        source_key='acceptance_video_'+str(int(time.time()))
        for old in d.rpc('GetAllSources'):
            if old['source_key'].startswith('acceptance_video'):
                old['enabled']=0;d.rpc('UpdateSource',old)
        source=dict(source_key=source_key,name='Native acceptance video',api_url=f'http://127.0.0.1:{port}/video-api',enabled=1,kind='video')
        d.rpc('AddSource',source)
        d.rpc('SearchVideos',dict(source_key=source_key,keyword='Android',page=1,page_size=20))
        detail=d.rpc('GetVideoDetail',dict(source_key=source_key,vod_id='1',refresh=True))
        (out/'detail.json').write_text(json.dumps(detail,ensure_ascii=False,indent=2),encoding='utf-8')
        global_id=d.rpc('GetGlobalIdForVideo',source_key,'1')
        route=f'/player/{source_key}/{global_id}/0?vod=1'
        d.visit('/settings?group=diagnostics')
        wait('[...document.querySelectorAll("button")].some(b=>b.textContent.trim()==="开始记录")')
        d.js('[...document.querySelectorAll("button")].find(b=>b.textContent.trim()==="开始记录").click()')
        wait('document.body.textContent.includes("结束并导出")')
        recording=d.rpc('GetDiagnosticsRecordingStatus')
        def playback():
            started=time.monotonic();d.visit(route)
            wait('document.querySelector("video")?.readyState>=2 && document.querySelector("video").videoWidth>0')
            wait('document.querySelector("video").currentTime>1')
            state=snapshot();shot('initial-actual-page')
            require(d.js('document.querySelectorAll(".player-desktop-controls").length')==0,'Desktop controls mounted on mobile')
            return dict(elapsedSeconds=time.monotonic()-started,perf=state,video=d.js('({time:document.querySelector("video").currentTime,duration:document.querySelector("video").duration,error:document.querySelector("video").error?.code,src:document.querySelector("video").currentSrc})'))
        check('actual-mp4-page-playback',playback)
        def click_behavior():
            tap('.native-video')
            wait('!!document.querySelector(".player-title-bar")')
            r=d.js('(()=>{const r=document.querySelector("video").getBoundingClientRect();return {x:r.x+r.width*.5,y:r.y+r.height*.35}})()')
            before=d.js('document.querySelector("video").paused')
            for _ in range(2):touch('touchStart',**r);touch('touchEnd');time.sleep(.08)
            time.sleep(.5)
            after=d.js('document.querySelector("video").paused')
            require(after!=before,dict(before=before,after=after))
            return dict(pausedAfterDoubleTap=after)
        check('single-tap-controls-double-tap-pause',click_behavior)
        def gestures():
            before=d.js('document.querySelector("video").currentTime')
            seek=drag(.2,.35,.25,0)
            after=d.js('document.querySelector("video").currentTime')
            require(after>before+5,dict(before=before,after=after,hud=seek))
            require(seek['preview'],'Scrubbing did not produce a preview frame')
            native=d.js('JSON.parse(wails.playerState())')
            d.js('wails.playerVolume(.25)');time.sleep(.2)
            picture_before=d.js('getComputedStyle(document.querySelector("video")).filter')
            brightness=drag(.18,.45,0,-.25)
            changed=d.js('JSON.parse(wails.playerState())')
            volume=drag(.8,.45,0,-.25)
            final=d.js('JSON.parse(wails.playerState())')
            picture_after=d.js('getComputedStyle(document.querySelector("video")).filter')
            require(picture_after!=picture_before,'Picture brightness gesture had no effect')
            require(final['volume']>.25,'Volume gesture had no effect')
            shot('gestures')
            return dict(seekBefore=before,seekAfter=after,seekHUD=seek,brightnessHUD=brightness,volumeHUD=volume,pictureBefore=picture_before,pictureAfter=picture_after,nativeBefore=native,nativeAfter=final)
        check('seek-preview-brightness-volume',gestures)
        def fullscreen():
            d.js('window.__acceptanceVideo=document.querySelector("video")')
            loads=snapshot()['browser_resources']['architecture']
            if not d.js('document.querySelector(".player-wrapper").classList.contains("show-controls")'):tap('.native-video')
            tap('.cczj-mobile-fullscreen')
            wait('!!document.fullscreenElement&&innerWidth>innerHeight')
            require(d.js('window.__acceptanceVideo===document.querySelector("video")'),'Fullscreen recreated media')
            shot('fullscreen')
            tap('.cczj-mobile-control-popover > button')
            wait('!!document.querySelector(".cczj-mobile-speed-options")')
            d.js('[...document.querySelectorAll(".cczj-mobile-speed-options button")].find(b=>b.textContent==="1.5x").click()')
            time.sleep(.6)
            require(d.js('document.querySelector("video").playbackRate')==1.5,'Speed sheet did not apply')
            if not d.js('document.querySelector(".player-wrapper").classList.contains("show-controls")'):tap('.native-video')
            tap('.cczj-mobile-player-lock')
            require(d.js('document.querySelector(".cczj-mobile-player-lock").getAttribute("aria-pressed")')=='true','Lock did not engage')
            tap('.cczj-mobile-player-lock')
            d.native('shell','input','keyevent','KEYCODE_BACK');time.sleep(1)
            require(d.js('!document.fullscreenElement && innerHeight>innerWidth'),'System Back failed to restore portrait')
            require(d.js('window.__acceptanceVideo===document.querySelector("video")'),'Fullscreen exit recreated media')
            return dict(before=loads,after=snapshot()['browser_resources']['architecture'])
        check('native-fullscreen-speed-lock-back-stable-media',fullscreen)
        def background():
            d.js('document.querySelector("video").play()');time.sleep(.8)
            before=d.js('document.querySelector("video").currentTime')
            d.native('shell','input','keyevent','KEYCODE_HOME');time.sleep(2)
            d.native('shell','am','start','-n',u.PACKAGE+'/com.wails.app.MainActivity');time.sleep(1)
            wait('document.querySelector("video").currentTime>'+str(before))
            return dict(before=before,after=d.js('document.querySelector("video").currentTime'),mediaRetained=d.js('window.__acceptanceVideo===document.querySelector("video")'))
        check('native-background-foreground-resume',background)
        def pip():
            capability=d.js('JSON.parse(wails.playerState()).pip')
            if not capability:return dict(supported=False,limitation='Device reports PiP unavailable')
            if not d.js('document.querySelector(".player-wrapper").classList.contains("show-controls")'):tap('.native-video')
            tap('.cczj-mobile-player-controls > button:nth-of-type(3)')
            wait('document.querySelector(".cczj-mobile-player-controls > button:nth-of-type(3)")?.getAttribute("aria-pressed")==="true"')
            d.native('shell','am','start','-n',u.PACKAGE+'/com.wails.app.MainActivity');time.sleep(1)
            return dict(supported=True,entered=True,videoRetained=d.js('window.__acceptanceVideo===document.querySelector("video")'))
        check('native-picture-in-picture',pip)
        def subtitles():
            if d.js('!!document.fullscreenElement'):
                d.native('shell','input','keyevent','KEYCODE_BACK');time.sleep(.7)
            if not d.js('document.querySelector(".player-wrapper").classList.contains("show-controls")'):tap('.native-video')
            tap('.cczj-mobile-player-more-button')
            d.js('[...document.querySelectorAll(".cczj-mobile-player-categories button")].at(-1).click()');time.sleep(.3)
            require(d.js('!!document.querySelector(".subtitle-file-input")'),'Subtitle picker missing')
            # Supply a file through the actual input/change handler. Android's
            # system file-picker UI itself is a separate manual acceptance item.
            d.js('(()=>{const input=document.querySelector(".subtitle-file-input"),dt=new DataTransfer();dt.items.add(new File(["WEBVTT\\n\\n00:00.000 --> 02:00.000\\nNative subtitle acceptance\\n"],"acceptance.vtt",{type:"text/vtt"}));input.files=dt.files;input.dispatchEvent(new Event("change",{bubbles:true}))})()')
            wait('document.querySelector(".vp-subtitle")?.textContent.includes("Native subtitle acceptance")')
            visible=d.js('getComputedStyle(document.querySelector(".vp-subtitle")).display!=="none"')
            require(visible,'Parsed subtitle not rendered')
            d.js('document.querySelector(".subtitle-group > button").click()')
            d.js('[...document.querySelectorAll(".subtitle-item")].find(b=>b.textContent.includes("隐藏字幕")).click()')
            wait('getComputedStyle(document.querySelector(".vp-subtitle")).display==="none"')
            return dict(fileParsed=True,displayed=True,hidden=True)
        check('actual-subtitle-file-parsing-render-toggle',subtitles)
        def hls_switch_episode():
            if d.js('!!document.querySelector(".cczj-mobile-player-drawer")'):
                d.native('shell','input','keyevent','KEYCODE_BACK');time.sleep(.5)
            before=d.js('document.querySelector("video").currentTime')
            d.js('[...document.querySelectorAll(".line-chip")].find(b=>b.textContent.includes("HLS")).click()')
            wait('document.querySelector("video").readyState>=2 && document.querySelector("video").currentTime>='+str(max(0,before-.5)))
            require(d.js('window.__acceptanceVideo===document.querySelector("video")'),'Switching line recreated video element')
            after=d.js('document.querySelector("video").currentTime')
            hls=snapshot()
            require(hls['browser_resources']['player']['hls']['active'],'HLS runtime not active')
            d.js('document.querySelectorAll(".ep-grid button")[1].click()')
            wait('document.querySelector("video").readyState>=2 && document.querySelector("video").currentTime<4')
            return dict(before=before,after=after,hls=hls,secondEpisodeTime=d.js('document.querySelector("video").currentTime'))
        check('actual-hls-line-resume-and-episode-reset',hls_switch_episode)
        def hls_download():
            task='acceptance-hls-'+str(int(time.time()))
            d.rpc('StartVideoDownload',dict(task_id=task,url=f'http://127.0.0.1:{port}/media/movie.m3u8',filename=task+'.ts',save_dir='',force=False))
            time.sleep(.35);d.rpc('PauseDownload',task)
            paused=d.rpc('GetDownloadProgress',task)
            require(paused['status']=='paused',paused)
            d.rpc('ResumeDownload',task)
            for _ in range(60):
                final=d.rpc('GetDownloadProgress',task)
                if final['status'] in ('done','error'):break
                time.sleep(.3)
            require(final['status']=='done',final)
            content=d.native('exec-out','run-as',u.PACKAGE,'cat',final['save_path'],binary=True)
            expected=b''.join((media/name).read_bytes() for name in (media/'movie.m3u8').read_text().splitlines() if name and not name.startswith('#'))
            require(content==expected,'HLS segment order or resumed file content differs')
            return dict(paused=paused,final=final,verifiedBytes=len(content))
        check('native-hls-download-pause-resume-file-order',hls_download)
        def close_resources():
            active=snapshot();d.visit('/settings?group=diagnostics');time.sleep(6)
            idle=snapshot();require(d.js('document.querySelectorAll("video").length')==0,'Player decoder retained after route exit')
            arch=idle['browser_resources']['architecture'];require(arch['active_player_count']==0,arch)
            shot('diagnostics-after-close')
            return dict(active=active,idle=idle,native=d.rpc('GetRuntimeMetrics'))
        check('actual-route-exit-resource-release',close_resources)
    finally:
        try:
            if recording:
                stopped=d.rpc('StopDiagnosticsRecording')
                raw=d.native('exec-out','run-as',u.PACKAGE,'cat',stopped['path'],binary=True)
                (out/'playback-recording.jsonl').write_bytes(raw)
                (out/'recording-status.json').write_text(json.dumps(stopped,indent=2),encoding='utf-8')
            (out/'events.json').write_text(json.dumps(d.events,ensure_ascii=False,indent=2),encoding='utf-8')
        finally:
            d.close();d.native('reverse','--remove',f'tcp:{port}');server.shutdown()
    raise SystemExit(1 if any(c['status']=='failed' for c in checks) else 0)

if __name__=='__main__':main()
