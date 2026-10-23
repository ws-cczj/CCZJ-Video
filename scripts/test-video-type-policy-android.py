"""Exercise type bans through the acceptance APK's actual Go/SQLite bindings."""
import importlib.util
import json
from pathlib import Path
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from android_webview import AndroidWebView

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('compiled', ROOT/'scripts/collect-android-diagnostics.py')
compiled = importlib.util.module_from_spec(spec)
spec.loader.exec_module(compiled)
rows = [dict(vod_id=str(i), type_id=str(i), type_name=kind, vod_name='Policy fixture '+str(i))
        for i,kind in enumerate(['福利', '伦理片', '倫理片', '剧情片'],1)]

class Fixture(BaseHTTPRequestHandler):
    def do_GET(self):
        body=json.dumps(dict(code=1,page=1,pagecount=1,total=4,list=rows),ensure_ascii=False).encode()
        self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(body)
    def log_message(self,*args): pass

def main():
    output=ROOT/'reports/video-type-policy-android-2026-10-21.json'
    server=ThreadingHTTPServer(('127.0.0.1',0),Fixture)
    port=server.server_address[1]
    threading.Thread(target=server.serve_forever,daemon=True).start()
    with AndroidWebView(application_id='com.cczj.video.acceptance') as view:
        def rpc(name,*args):
            path,alias=compiled.compiled_binding(name)
            return view.evaluate('(async()=>{const m=await import('+json.dumps(path)+');return m['+json.dumps(alias)+'](...'+json.dumps(args)+')})()')
        key='acceptance_type_policy_'+str(int(time.time()))
        old_types={row['TypeName']:row['CollectEnabled'] for row in rpc('GetGlobalTypes')}
        old_mode=rpc('GetSetting','application_mode')
        old_default=rpc('GetSetting','default_source_key')
        old_url=view.evaluate('location.href')
        added=False
        subprocess.run(view.adb+['reverse',f'tcp:{port}',f'tcp:{port}'],check=True,capture_output=True)
        try:
            rpc('SetSetting','application_mode','video')
            view.evaluate('location.href="https://wails.localhost/?application-mode=video#/"')
            time.sleep(1)
            rpc('AddSource',dict(source_key=key,name='Type policy acceptance',api_url=f'http://127.0.0.1:{port}',enabled=1))
            added=True
            for kind in ['福利','伦理片','倫理片']:
                rpc('SetGlobalTypeCollectEnabled',dict(type_name=kind,enabled=True))
            assert rpc('ImportSourceVideos',key,rows)==4
            for kind in ['福利','伦理片']:
                rpc('SetGlobalTypeCollectEnabled',dict(type_name=kind,enabled=False))
            checks=[]
            for name,args in [('SearchSource',[key,'fixture',1,20]),('SearchVideos',[dict(source_key=key,keyword='fixture',page=1,page_size=20)]),('GetVideoList',[dict(source_key=key,page=1,page_size=20)])]:
                result=rpc(name,*args)
                videos=result.get('videos') or []
                assert len(videos)==1 and videos[0]['type_name']=='剧情片',(name,result)
                checks.append(name+' returns only allowed category')
            assert rpc('ImportSourceVideos',key,rows)==1
            rpc('StartCollect',dict(source_key=key,mode='once'))
            deadline=time.monotonic()+20
            while time.monotonic()<deadline:
                status=rpc('GetCollectStatus',key)
                if not status['running'] and status.get('finished_at_unix'): break
                time.sleep(.1)
            else: raise AssertionError('collection did not finish')
            assert status['saved']==1 and not status.get('last_error_kind'),status
            checks+=['Import rejects three banned aliases','Native collection saves/counts one allowed item']
            output.write_text(json.dumps(dict(passed=True,native=True,package='com.cczj.video.acceptance',checks=checks,status=status),ensure_ascii=False,indent=2),encoding='utf-8')
            print(output.read_text(encoding='utf-8'))
        finally:
            if added: rpc('DeleteSource',key)
            for kind in ['福利','伦理片','倫理片']:
                rpc('SetGlobalTypeCollectEnabled',dict(type_name=kind,enabled=bool(old_types.get(kind,1))))
            rpc('SetSetting','default_source_key',old_default or '')
            rpc('SetSetting','application_mode',old_mode or '')
            view.evaluate('location.href='+json.dumps(old_url))
            server.shutdown();server.server_close()
            subprocess.run(view.adb+['reverse','--remove',f'tcp:{port}'],capture_output=True)

if __name__=='__main__': main()
