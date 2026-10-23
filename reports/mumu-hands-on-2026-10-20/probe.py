import subprocess,time,json,sys
from pathlib import Path
from urllib.request import urlopen
import websocket
adb=['adb','-s','emulator-5554']
args=sys.argv[1:]
name=args[0]
if len(args)>1:
    subprocess.run(adb+['shell','input']+args[1:],check=True,capture_output=True)
    time.sleep(0.7)
p=Path(__file__).parent
(p/(name+'.png')).write_bytes(subprocess.check_output(adb+['exec-out','screencap','-p']))
pid=subprocess.check_output(adb+['shell','pidof','com.cczj.video.preview'],text=True).strip()
port=subprocess.check_output(adb+['forward','tcp:0','localabstract:webview_devtools_remote_'+pid],text=True).strip()
try:
    targets=json.load(urlopen('http://127.0.0.1:'+port+'/json/list'))
    target=next(t for t in targets if t.get('type')=='page')
    ws=websocket.create_connection(target['webSocketDebuggerUrl'],timeout=10,origin='http://localhost')
    ws.send(json.dumps({'id':1,'method':'Runtime.evaluate','params':{'expression':'JSON.stringify({url:location.href,text:document.body.innerText,links:Array.from(document.querySelectorAll("a")).map(a=>({text:a.innerText,href:a.getAttribute("href")}))})','returnByValue':True}}))
    while True:
        r=json.loads(ws.recv())
        if r.get('id')==1:break
    value=json.loads(r['result']['result']['value'])
    (p/(name+'.json')).write_text(json.dumps(value,ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps({**value,'text':value['text'][:1800]},ensure_ascii=True))
    ws.close()
finally:subprocess.run(adb+['forward','--remove','tcp:'+port],capture_output=True)
