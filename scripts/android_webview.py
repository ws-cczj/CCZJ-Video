"""Scoped CDP connection to the installed CCZJ Android debug preview."""
import json
import subprocess
import time
from urllib.request import urlopen
import websocket

class AndroidWebView:
    def __init__(self, device='emulator-5554', application_id='com.cczj.video.preview'):
        self.adb = ['adb', '-s', device]
        self.application_id = application_id
        self.socket = None
        self.port = None
        self.identifier = 0
    def __enter__(self):
        subprocess.run(self.adb + ['shell','am','start','-W','-n',self.application_id+'/com.wails.app.MainActivity'],check=True,capture_output=True)
        # A cold Activity can appear before its process/CDP target is ready.
        deadline=time.monotonic()+10
        while True:
            result=subprocess.run(self.adb+['shell','pidof',self.application_id],text=True,capture_output=True)
            pid=result.stdout.strip().split()
            if pid:break
            if time.monotonic()>=deadline:raise RuntimeError('Android app process did not start: '+self.application_id)
            time.sleep(.1)
        pid=pid[0]
        self.port = subprocess.check_output(self.adb+['forward','tcp:0',f'localabstract:webview_devtools_remote_{pid}'],text=True).strip()
        try:
            while True:
                try:
                    with urlopen(f'http://127.0.0.1:{self.port}/json/list',timeout=1) as response: targets=json.load(response)
                    target=next(t for t in targets if t.get('url','').startswith('https://wails.localhost'))
                    break
                except (OSError,StopIteration):
                    if time.monotonic()>=deadline:raise
                    time.sleep(.1)
            self.socket=websocket.create_connection(target['webSocketDebuggerUrl'],timeout=20,origin='http://localhost')
            return self
        except BaseException:
            self.__exit__(None,None,None)
            raise
    def call(self, method, params=None):
        self.identifier += 1
        self.socket.send(json.dumps({'id':self.identifier,'method':method,'params':params or {}}))
        while True:
            result=json.loads(self.socket.recv())
            if result.get('id') != self.identifier: continue
            if 'error' in result: raise RuntimeError(result['error'])
            result=result.get('result',{})
            if 'exceptionDetails' in result: raise RuntimeError(result['exceptionDetails'])
            return result
    def evaluate(self, expression):
        return self.call('Runtime.evaluate',{'expression':expression,'awaitPromise':True,'returnByValue':True}).get('result',{}).get('value')
    def screenshot(self, path):
        path.write_bytes(subprocess.check_output(self.adb+['exec-out','screencap','-p']))
    def __exit__(self, *unused):
        try:
            if self.socket: self.socket.close()
        finally:
            if self.port: subprocess.run(self.adb+['forward','--remove',f'tcp:{self.port}'],capture_output=True)
