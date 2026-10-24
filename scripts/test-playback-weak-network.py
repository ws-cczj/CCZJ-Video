"""Real HLS decoding through the production Go proxy under a shared uplink limit.

Uses the existing player browser fixture. Media is locally generated H.264/AAC;
bandwidth is shared by all connections, including prefetch and duplicate reads.
Only unrelated Wails bindings are replaced. Results describe this controlled link.
"""
import argparse, json, mimetypes, os, re, shutil, subprocess, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse, quote
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parent.parent

class Link:
    def __init__(self):
        self.lock=threading.Lock();self.rate=3_000_000/8;self.tokens=0;self.at=time.monotonic();self.latency=.08;self.records=[];self.active=0
    def configure(self,bps,latency=.08):
        with self.lock:self.rate=bps/8;self.tokens=0;self.at=time.monotonic();self.latency=latency
    def take(self,size):
        while True:
            with self.lock:
                now=time.monotonic();self.tokens=min(8192,self.tokens+(now-self.at)*self.rate);self.at=now
                if self.tokens>=size:self.tokens-=size;return
            time.sleep(.01)

class Media(BaseHTTPRequestHandler):
    directory=None;link=None
    def log_message(self,*unused):pass
    def do_GET(self):
        path=urlparse(self.path).path
        if path=='/stall.ts':
            self.send_response(200);self.send_header('Content-Type','video/mp2t');self.send_header('Content-Length','131072');self.end_headers()
            try:self.wfile.write(b'\x47'*8192);self.wfile.flush();time.sleep(5);self.wfile.write(b'\x47'*(131072-8192))
            except (BrokenPipeError,ConnectionResetError,ConnectionAbortedError):pass
            return
        file=(self.directory/path.removeprefix('/media/')).resolve()
        if not file.is_relative_to(self.directory.resolve()) or not file.is_file():self.send_error(404);return
        data=file.read_bytes();is_playlist=file.suffix=='.m3u8'
        record={'path':path,'started':time.monotonic(),'bytes':0,'size':len(data),'completed':False}
        with self.link.lock:self.link.records.append(record);self.link.active+=1;latency=self.link.latency
        try:
            time.sleep(latency)
            self.send_response(200);self.send_header('Content-Type','application/vnd.apple.mpegurl' if is_playlist else 'video/mp2t');self.send_header('Content-Length',str(len(data)));self.send_header('Cache-Control','no-store');self.end_headers()
            for offset in range(0,len(data),8192):
                chunk=data[offset:offset+8192];self.link.take(len(chunk));self.wfile.write(chunk);self.wfile.flush();record['bytes']+=len(chunk)
            record['completed']=True
        except (BrokenPipeError,ConnectionResetError,ConnectionAbortedError):pass
        finally:
            record['finished']=time.monotonic()
            with self.link.lock:self.link.active-=1

def prepare_media(directory):
    directory.mkdir(parents=True,exist_ok=True)
    for name,rate,width,height in [('low',280,320,180),('high',1400,640,360)]:
        folder=directory/name;folder.mkdir(exist_ok=True)
        if (folder/'index.m3u8').exists():continue
        command=[shutil.which('ffmpeg'),'-hide_banner','-loglevel','error','-f','lavfi','-i',f'testsrc2=size={width}x{height}:rate=24','-f','lavfi','-i','sine=frequency=440:sample_rate=48000','-t','100','-c:v','libx264','-preset','ultrafast','-b:v',f'{rate}k','-minrate',f'{rate}k','-maxrate',f'{rate}k','-bufsize',f'{rate*2}k','-x264-params','nal-hrd=cbr:force-cfr=1','-g','48','-keyint_min','48','-sc_threshold','0','-pix_fmt','yuv420p','-c:a','aac','-b:a','48k','-f','hls','-hls_time','2','-hls_list_size','0',str(folder/'index.m3u8')]
        subprocess.run(command,check=True,creationflags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0)
    (directory/'master.m3u8').write_text('#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=350000,RESOLUTION=320x180\nlow/index.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=1550000,RESOLUTION=640x360\nhigh/index.m3u8\n')

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--tag',default='current');parser.add_argument('--seconds',type=float,default=70);parser.add_argument('--channel',default='msedge');parser.add_argument('--scenario',choices=['adaptive','single','single-high','offline'],default='adaptive');parser.add_argument('--output',default='dist/audit/weak-network');args=parser.parse_args()
    if not re.fullmatch(r'[a-zA-Z0-9_-]{1,48}',args.tag):raise ValueError('invalid tag')
    from playwright.sync_api import sync_playwright
    output=ROOT/args.output;output.mkdir(parents=True,exist_ok=True)
    directory=ROOT/'.cache/weak-network/media';prepare_media(directory)
    Media.directory=directory;Media.link=Link();server=ThreadingHTTPServer(('127.0.0.1',0),Media);threading.Thread(target=server.serve_forever,daemon=True).start();media_port=server.server_port
    flags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0;procs=[];result={};console=[];errors=[]
    try:
        helper=output/'media-proxy.exe';subprocess.run(['go','build','-o',str(helper),'./scripts/fixtures/weak-playback-proxy'],cwd=ROOT,check=True,creationflags=flags)
        procs.append(subprocess.Popen(['node','node_modules/vite/bin/vite.js','--port','9356','--strictPort'],cwd=ROOT/'frontend',stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,creationflags=flags))
        procs.append(subprocess.Popen([str(helper),'127.0.0.1:9355','http://127.0.0.1:9356',str(output/f'{args.tag}-logs')],cwd=ROOT,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,creationflags=flags))
        for _ in range(150):
            try:urlopen('http://127.0.0.1:9355/tests/browser/player.html',timeout=1).close();break
            except OSError:time.sleep(.1)
        names=re.findall(r'^export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf8'),re.M)
        mock='\n'.join(f'export async function {name}(...args){{ '+("return fetch('/audit-log',{method:'POST',body:JSON.stringify(args[0])}).then(r=>r.json()).then(()=>true);" if name=='WriteLog' else 'return '+json.dumps([] if name in ('ListPlugins','RescanPlugins') else False if name.startswith('WindowIs') else '')+';')+' }' for name in names)
        with sync_playwright() as p:
            browser=p.chromium.launch(headless=True,channel=args.channel,args=['--autoplay-policy=no-user-gesture-required']);page=browser.new_page(viewport={'width':390,'height':844});page.on('console',lambda m:console.append(m.text[:700]));page.on('pageerror',lambda e:errors.append(str(e)))
            page.add_init_script("window._wails={environment:{OS:'windows'},flags:{frameless:true}};window.chrome={webview:{postMessage:()=>{}}};")
            page.route('**/bindings/cczjVideo/app/service/app*',lambda r:r.fulfill(body=mock,content_type='text/javascript'))
            def fixture(route):
                body=route.fetch().text()+'''\nTsCache.setEpisodes([{source_key:'weak_fixture',vod_id:'weak',ep_num:1,ep_url:'fixture'}]);TsCache.setCurrentEpisode(0);'''
                route.fulfill(body=body,content_type='text/javascript')
            page.route('**/tests/browser/player.ts',fixture)
            media=f'http://127.0.0.1:{media_port}/media/'+('low/index.m3u8' if args.scenario=='single' else 'high/index.m3u8' if args.scenario=='single-high' else 'master.m3u8')
            page.goto('http://127.0.0.1:9355/tests/browser/player.html?resume=0&media='+quote(media,safe=''))
            page.wait_for_selector('video');page.evaluate('''()=>{window.weakEvents=[];const v=document.querySelector('video');v.muted=true;for(const name of ['waiting','playing','stalled','error','seeking','seeked'])v.addEventListener(name,()=>weakEvents.push({event:name,wall:performance.now(),time:v.currentTime,ready:v.readyState}));return v.play()}''')
            started=time.monotonic();samples=[];phase=None
            while time.monotonic()-started<args.seconds:
                elapsed=time.monotonic()-started
                new_phase='fast' if elapsed<10 or elapsed>=50 else 'offline' if args.scenario=='offline' and 20<=elapsed<25 else 'weak'
                if new_phase!=phase:
                    phase=new_phase;Media.link.configure(3_000_000 if phase=='fast' else 0 if phase=='offline' else 650_000,.08 if phase=='fast' else .25)
                sample=page.evaluate('''()=>{const v=document.querySelector('video'),h=v.__hls;return {time:v.currentTime,paused:v.paused,ready:v.readyState,buffer:Array.from({length:v.buffered.length},(_,i)=>[v.buffered.start(i),v.buffered.end(i)]),level:h?.currentLevel,next:h?.nextAutoLevel,bps:h?.bandwidthEstimate,cache:playerTest.segmentStats()}}''');sample.update(wall=round(elapsed,3),phase=phase);samples.append(sample);time.sleep(.5)
            events=page.evaluate('weakEvents');page.evaluate('document.querySelector("video").pause()')
            fault=page.evaluate('''async(url)=>{const {TsCacheLoader}=await import('/src/utils/ts-cache/loader.ts');const loader=new TsCacheLoader({});const state={progress:0,timeout:0,error:0,success:0};loader.load({url,type:'media-fragment',responseType:'arraybuffer'},{timeout:500,loadPolicy:{maxTimeToFirstByteMs:500,maxLoadTimeMs:500,errorRetry:null,timeoutRetry:null},maxRetry:0,retryDelay:0,maxRetryDelay:0,highWaterMark:4096},{onSuccess:()=>state.success++,onError:()=>state.error++,onTimeout:()=>state.timeout++,onProgress:()=>state.progress++});await new Promise(r=>setTimeout(r,1200));const value={...state,loaded:loader.stats.loaded,first:loader.stats.loading.first};loader.destroy();return value}''',f'http://127.0.0.1:9355/__cczj/hls?u='+__import__('base64').urlsafe_b64encode(f'http://127.0.0.1:{media_port}/stall.ts'.encode()).decode().rstrip('='))
            page.evaluate('playerTest.state.open=false');time.sleep(.3)
            result={'tag':args.tag,'scenario':args.scenario,'samples':samples,'events':events,'fault':fault,'requests':Media.link.records,'errors':errors,'console':console,'media_seconds':samples[-1]['time'] if samples else 0}
            active_wait=None;stalls=[]
            for event in events:
                if event['event']=='waiting' and event['time']>.1 and active_wait is None:active_wait=event['wall']
                if event['event']=='playing' and active_wait is not None:stalls.append(event['wall']-active_wait);active_wait=None
            result['completed_stall_ms']=round(sum(stalls));result['stall_count']=len(stalls);result['open_stall']=active_wait is not None
            (output/f'{args.tag}.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf8');print(json.dumps({k:result[k] for k in ['tag','scenario','media_seconds','completed_stall_ms','stall_count','open_stall','fault','errors']}));browser.close()
            assert not errors,errors
            assert result['media_seconds']>args.seconds*.5,'playback failed to advance'
            assert not result['open_stall'],'playback did not recover'
            assert fault['progress'] and fault['timeout']==1 and not fault['success'],'stalled body did not emit progress and time out'
            if args.seconds>=60 and args.scenario in ('adaptive','offline'):
                assert any(s['level']==1 for s in samples),'never selected higher bitrate'
                assert any(s['phase']=='weak' and s['wall']>30 and s['level']==0 for s in samples),'did not downswitch on weak link'
                assert len({round(s['bps']) for s in samples if s.get('bps')})>1,'bandwidth sampling stopped'
    finally:
        for proc in reversed(procs):
            proc.terminate()
            try:proc.wait(timeout=5)
            except subprocess.TimeoutExpired:proc.kill();proc.wait()
        Media.link.configure(3_000_000);server.shutdown();server.server_close()

if __name__=='__main__':main()
