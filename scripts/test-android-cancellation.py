"""Exercise real Android Java -> JNI -> Go cancellation with blocked HTTP probes.

Uses only the acceptance package, adds/removes three uniquely named test sources,
restores preferences, and releases its own adb reverse mapping. No data deletion.
"""
import argparse,http.server,json,re,select,subprocess,threading,time,uuid
from pathlib import Path
from android_webview import AndroidWebView

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--device',required=True)
    parser.add_argument('--output',default='dist/audit/lifecycle/android-cancellation')
    args=parser.parse_args()
    out=Path(args.output);out.mkdir(parents=True,exist_ok=True)
    package='com.cczj.video.acceptance'
    binding_text=Path('frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
    ids={m[1]:int(m[2]) for m in re.finditer(r'export function (\w+)\([^)]*\) \{\s*return \$Call.ByID\((\d+)',binding_text)}
    hits=[];closed=[];lock=threading.Lock();release=threading.Event()
    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            with lock:hits.append(self.path)
            while not release.wait(.02):
                ready,_,_=select.select([self.connection],[],[],0)
                if ready and not self.connection.recv(1):
                    with lock:closed.append(self.path)
                    return
            try:
                self.send_response(200);self.end_headers();self.wfile.write(b'{"code":1,"list":[]}')
            except OSError:pass
        def log_message(self,*unused):pass
    server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    port=server.server_port
    adb=['adb','-s',args.device]
    subprocess.run(adb+['reverse',f'tcp:{port}',f'tcp:{port}'],check=True,capture_output=True)
    keys=['cancel_'+uuid.uuid4().hex[:14] for _ in range(3)]
    result={'scope':'actual acceptance APK Java/JNI/Go/HTTP chain','device':args.device}
    try:
        with AndroidWebView(args.device,application_id=package) as view:
            view.evaluate('''(() => {
              const previous=window._wailsAndroidCallback, pending=new Map();
              const results={};let seq=0;
              window._wailsAndroidCallback=(id,response,error)=>{
                if(!pending.has(id)){previous?.(id,response,error);return}
                const done=pending.get(id);pending.delete(id);
                let result;try{result=error?{transportError:error}:JSON.parse(response)}catch(e){result={parseError:String(e)}}
                results[id]=result;done(result);
              };
              window.__cancelProbe={results,call(methodID,args,callID){
                const id='audit-'+(++seq),binding=callID||id;
                const done=new Promise(resolve=>pending.set(id,resolve));
                wails.invokeAsync(id,JSON.stringify({object:0,method:0,args:{methodID,args,'call-id':binding}}));
                return {id,done};
              },cancel(binding){
                const id='audit-'+(++seq),done=new Promise(resolve=>pending.set(id,resolve));
                wails.invokeAsync(id,JSON.stringify({object:10,method:0,args:{'call-id':binding}}));
                return {id,done};
              },dispose(){window._wailsAndroidCallback=previous;delete window.__cancelProbe}};
            })()''')
            def rpc(name,values):
                reply=view.evaluate(f'__cancelProbe.call({ids[name]},{json.dumps(values)}).done')
                assert reply.get('ok'),(name,reply)
                return reply.get('data',reply.get('text'))
            original_allow=rpc('GetAllowPrivateNetwork',[])
            original_default=rpc('GetSetting',['default_source_key'])
            try:
                rpc('SetAllowPrivateNetwork',[True])
                for i,key in enumerate(keys):
                    rpc('CreateSource',[{'source_key':key,'name':'Cancellation verification','api_url':f'http://127.0.0.1:{port}/{i}','kind':'video','enabled':0}])
                for i in range(2):view.evaluate(f'__cancelProbe.call({ids["ProbeSource"]},[{json.dumps(keys[i])}],"blocked-{i}").id')
                deadline=time.monotonic()+6
                while len(hits)<2:
                    if time.monotonic()>deadline:raise AssertionError('two probes did not reach blocked HTTP: '+str(view.evaluate('__cancelProbe.results')))
                    time.sleep(.05)
                queued=view.evaluate(f'__cancelProbe.call({ids["ProbeSource"]},[{json.dumps(keys[2])}],"queued").id')
                began=time.monotonic()
                ack=view.evaluate('__cancelProbe.cancel("queued").done')
                assert ack.get('ok'),ack
                reply=view.evaluate(f'__cancelProbe.results[{json.dumps(queued)}]')
                assert reply and reply.get('error',{}).get('code')=='CANCELLED',reply
                for i in range(2):
                    assert view.evaluate(f'__cancelProbe.cancel("blocked-{i}").done').get('ok')
                deadline=time.monotonic()+3
                while len(closed)<2:
                    if time.monotonic()>deadline:raise AssertionError('cancel did not close upstream HTTP sockets')
                    time.sleep(.02)
                result.update(cancelToSocketCloseMS=round((time.monotonic()-began)*1000),httpStarts=len(hits),httpCloses=len(closed),queuedNeverStarted=not any(p.startswith('/2') for p in hits))
                assert result['queuedNeverStarted'],hits
                result['passed']=True
            finally:
                release.set()
                for key in keys:rpc('DeleteSource',[key])
                rpc('SetSetting',['default_source_key',original_default])
                rpc('SetAllowPrivateNetwork',[original_allow])
                view.evaluate('__cancelProbe.dispose()')
    finally:
        release.set();server.shutdown();server.server_close()
        subprocess.run(adb+['reverse','--remove',f'tcp:{port}'],capture_output=True)
    (out/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps(result,ensure_ascii=True))

if __name__=='__main__':main()
