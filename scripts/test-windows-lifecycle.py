"""Native isolated WebView2 close/exit checks against this project's buildApp.

The Go overlay changes only the single-instance identity, hidden initial window
and CDP port. Production storage and browser profiles are never opened.
"""
import argparse,ctypes,json,os,re,socket,sqlite3,subprocess,tempfile,time,uuid
from pathlib import Path
from urllib.request import urlopen
import websocket
from video_domain_probe import probe_video_domain

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--output',default='dist/audit/lifecycle/windows')
    args=parser.parse_args()
    root=Path.cwd().resolve();out=(root/args.output).resolve();out.mkdir(parents=True,exist_ok=True)
    run=Path(tempfile.mkdtemp(prefix='run-',dir=out))
    source=(root/'app.go').read_text(encoding='utf-8')
    source=source.replace('UniqueID: "com.cczj.video",','UniqueID: "com.cczj.video.lifecycle.'+uuid.uuid4().hex+'",')
    source=source.replace('Name:   "main",','Name:   "main",\n Hidden: true,')
    source=source.replace('windowsOptions := application.WindowsOptions{}','windowsOptions := application.WindowsOptions{AdditionalBrowserArgs: []string{"--remote-debugging-address=127.0.0.1", "--remote-debugging-port=" + os.Getenv("CCZJ_LIFECYCLE_CDP_PORT")}}')
    patched=run/'app.overlay';patched.write_text(source,encoding='utf-8')
    overlay=run/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(root/'app.go'):str(patched)}}),encoding='utf-8')
    binary=run/'lifecycle.exe'
    subprocess.run(['go','build','-tags','production','-overlay',str(overlay),'-o',str(binary),'.'],check=True,cwd=root)
    bindings=(root/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
    ids={m[1]:int(m[2]) for m in re.finditer(r'export function (\w+)\([^)]*\) \{\s*return \$Call.ByID\((\d+)',bindings)}
    user=ctypes.windll.user32
    enumproc=ctypes.WINFUNCTYPE(ctypes.c_bool,ctypes.c_void_p,ctypes.c_void_p)
    user.GetWindowThreadProcessId.argtypes=[ctypes.c_void_p,ctypes.POINTER(ctypes.c_ulong)]
    user.GetWindowTextW.argtypes=[ctypes.c_void_p,ctypes.c_wchar_p,ctypes.c_int]
    user.IsWindow.argtypes=[ctypes.c_void_p]
    user.PostMessageW.argtypes=[ctypes.c_void_p,ctypes.c_uint,ctypes.c_size_t,ctypes.c_ssize_t]
    def window(pid):
        matches=[]
        def visit(handle,_):
            owner=ctypes.c_ulong();user.GetWindowThreadProcessId(handle,ctypes.byref(owner))
            name=ctypes.create_unicode_buffer(256);user.GetWindowTextW(handle,name,256)
            if owner.value==pid and name.value=='CCZJ Video':matches.append(handle)
            return True
        user.EnumWindows(enumproc(visit),0)
        return matches[0] if matches else None
    results=[]
    for mode in ['user_close','explicit_quit','startup_failure']:
        profile=run/mode;data=profile/'CCZJ Video';data.mkdir(parents=True)
        with sqlite3.connect(data/'cczj_video.db') as conn:
            conn.execute('CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT NOT NULL)')
            conn.executemany('INSERT INTO settings VALUES (?,?)',[('application_mode','novel'),('outbound_proxy_pool_v1','{"enabled":false}'),('close_to_tray','1')])
            if mode=='startup_failure':conn.execute('INSERT INTO settings VALUES (?,?)',('network_request_policy_v1','{broken-private-policy'))
        with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
        env=dict(os.environ,APPDATA=str(profile),XDG_CONFIG_HOME=str(profile),CCZJ_LIFECYCLE_CDP_PORT=str(port))
        env.pop('WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS',None)
        env.pop('WEBVIEW2_USER_DATA_FOLDER',None)
        startup=subprocess.STARTUPINFO();startup.dwFlags|=subprocess.STARTF_USESHOWWINDOW;startup.wShowWindow=0
        with (profile/'process.log').open('w') as log:
            proc=subprocess.Popen([str(binary)],cwd=root,env=env,startupinfo=startup,stdout=log,stderr=log)
            ws=None
            try:
                if mode=='startup_failure':
                    deadline=time.monotonic()+25
                    handle=None
                    while not handle:
                        handle=window(proc.pid)
                        if time.monotonic()>deadline or proc.poll() is not None:raise RuntimeError('native failure dialog did not appear')
                        time.sleep(.05)
                    messages=[]
                    def read_child(child,_):
                        text=ctypes.create_unicode_buffer(4096);user.GetWindowTextW(child,text,4096)
                        messages.append(text.value);return True
                    user.EnumChildWindows(handle,enumproc(read_child),0)
                    text='\n'.join(messages)
                    assert '读取网络请求策略失败' in text and 'STORAGE' in text,text
                    assert 'broken-private-policy' not in text and 'panic:' not in text,text
                    user.PostMessageW(handle,0x0010,0,0)
                    proc.wait(timeout=25)
                    assert proc.returncode==1,proc.returncode
                    logtext='\n'.join(p.read_text(encoding='utf-8',errors='replace') for p in (data/'applog').glob('*.log'))
                    assert 'application failed' in logtext and 'request_id=' in logtext,logtext
                    results.append({'scenario':mode,'passed':True,'exitCode':proc.returncode,'publicCode':'STORAGE','scope':'real native dialog after startup rollback; private policy body hidden'})
                    continue
                deadline=time.monotonic()+25
                while True:
                    try:
                        with urlopen(f'http://127.0.0.1:{port}/json/list',timeout=1) as response:targets=json.load(response)
                        target=next(t for t in targets if t.get('type')=='page' and t.get('url','').startswith('http'))
                        ws=websocket.create_connection(target['webSocketDebuggerUrl'],timeout=10,suppress_origin=True);break
                    except (OSError,StopIteration):
                        if time.monotonic()>deadline or proc.poll() is not None:raise RuntimeError('native host did not become ready')
                        time.sleep(.1)
                sequence=0
                def evaluate(expression):
                    nonlocal sequence
                    sequence+=1;ws.send(json.dumps({'id':sequence,'method':'Runtime.evaluate','params':{'expression':expression,'awaitPromise':True,'returnByValue':True}}))
                    while True:
                        reply=json.loads(ws.recv())
                        if reply.get('id')!=sequence:continue
                        if 'exceptionDetails' in reply.get('result',{}):raise RuntimeError(reply)
                        return reply.get('result',{}).get('result',{}).get('value')
                def rpc(name,values):
                    body={'object':0,'method':0,'args':{'methodID':ids[name],'args':values,'call-id':uuid.uuid4().hex}}
                    return evaluate(f'''fetch('/wails/runtime',{{method:'POST',headers:{{'Content-Type':'application/json'}},body:{json.dumps(json.dumps(body))}}}).then(async r=>({{ok:r.ok,text:await r.text()}}))''')
                deadline=time.monotonic()+25
                while not evaluate("!!document.querySelector('#app')?.__vue_app__ && location.protocol.startsWith('http')"):
                    if time.monotonic()>deadline:raise RuntimeError('native Vue page did not mount')
                    time.sleep(.1)
                assert rpc('GetCloseBehavior',[])['text']=='true'
                video=None
                if mode=='user_close':
                    def video_rpc(name,values):
                        reply=rpc(name,values)
                        assert reply['ok'],(name,reply)
                        if not reply['text']:return None
                        try:return json.loads(reply['text'])
                        except json.JSONDecodeError:return reply['text']
                    def check_export(path,key):
                        exported=Path(path).resolve()
                        prefix='backup_' if key=='$backup' else 'source_'+key+'_'
                        assert exported.is_relative_to((data/'exports').resolve()) and exported.name.startswith(prefix),path
                        assert exported.stat().st_size>0,path
                        exported.unlink()
                    video=probe_video_domain(video_rpc,check_export)
                handle=window(proc.pid);assert handle,'main window handle not found'
                if mode=='user_close':
                    user.PostMessageW(handle,0x0010,0,0)
                    time.sleep(.4)
                    assert proc.poll() is None and user.IsWindow(handle),'close-to-tray destroyed the main window'
                    assert rpc('SetCloseBehavior',[False])['ok']
                    user.PostMessageW(handle,0x0010,0,0)
                else:
                    body={'object':0,'method':0,'args':{'methodID':ids['QuitApp'],'args':[],'call-id':uuid.uuid4().hex}}
                    evaluate(f"void fetch('/wails/runtime',{{method:'POST',headers:{{'Content-Type':'application/json'}},body:{json.dumps(json.dumps(body))}}}).catch(()=>{{}})")
                proc.wait(timeout=25)
                assert proc.returncode==0,proc.returncode
                logtext='\n'.join(p.read_text(encoding='utf-8',errors='replace') for p in (data/'applog').glob('*.log'))
                assert '应用正常退出' in logtext,'service cleanup did not finish'
                results.append({'scenario':mode,'passed':True,'exitCode':proc.returncode,'dataDir':str(data),'video':video})
            finally:
                if ws:ws.close()
                if proc.poll() is None:proc.terminate();proc.wait(timeout=5)
    report={'scope':'real Windows WebView2 + production buildApp; isolated profile and instance identity','results':results,'output':str(run)}
    (out/'result.json').write_text(json.dumps(report,ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps(report,ensure_ascii=True))

if __name__=='__main__':main()
